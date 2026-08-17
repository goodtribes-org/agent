// Package stage holds the three workers and the loop they share.
package stage

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/goodtribes-org/agent/organizzer/internal/board"
	"github.com/goodtribes-org/agent/organizzer/internal/config"
	"github.com/goodtribes-org/agent/organizzer/internal/foundry"
	"github.com/goodtribes-org/agent/organizzer/internal/llm"
	"github.com/goodtribes-org/agent/organizzer/internal/selector"
)

// Runner carries everything a stage needs to do its work.
type Runner struct {
	Cfg  config.Config
	GH   BoardAPI
	LLM  LLMAPI
	Fnd  FoundryAPI
	Log  *slog.Logger
	Meta board.ProjectMeta

	// backoff holds items that failed transiently, so the loop does not spin
	// on the same broken card every ten seconds. It is in-memory on purpose:
	// a transient failure should not outlive the process, and a decision that
	// *should* outlive it gets the needs-human label instead.
	backoff map[string]time.Time

	// dispatched counts issues processed in the last hour, for the optional
	// MAX_ISSUES_PER_HOUR cap.
	recent []time.Time

	// scanFrom is where the next blocker scan starts in the ordered candidate
	// list. A poll checks a bounded window; if every card in it is blocked the
	// cursor advances so the next poll looks further down instead of at the
	// same twenty. It resets to zero after a successful pick, so priority
	// order is what decides between cards that are actually workable.
	scanFrom int

	// sweeping reports that the last poll left candidates unexamined behind
	// its window. That is not an idle board, so the loop keeps the busy
	// interval and carries on down the list.
	sweeping bool

	// swept counts candidates examined since the last pick. Once it reaches
	// the length of the list, every card has been looked at and none was
	// workable — the column is blocked rather than busy, and hurrying through
	// it again at the busy interval only spends rate limit to learn the same
	// thing. It resets on a pick and when a lap completes.
	swept int
}

// NewRunner returns a Runner with its clients wired up.
func NewRunner(cfg config.Config, log *slog.Logger) *Runner {
	r := &Runner{
		Cfg:     cfg,
		GH:      board.New(cfg, log),
		Log:     log,
		backoff: map[string]time.Time{},
	}
	if cfg.Stage == config.StageRequest || cfg.Stage == config.StagePlan {
		r.LLM = llm.New(cfg, log)
	}
	if cfg.Stage == config.StageApply {
		r.Fnd = foundry.New(cfg, log)
	}
	return r
}

// penalise holds an item out of selection for a while after a transient error.
func (r *Runner) penalise(item board.Item) {
	r.backoff[item.Key()] = time.Now().Add(r.Cfg.ItemBackoff)
}

// penalised reports whether an item is currently held out.
func (r *Runner) penalised(item board.Item) bool {
	until, ok := r.backoff[item.Key()]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(r.backoff, item.Key())
		return false
	}
	return true
}

// note records that an issue was processed, for the hourly cap.
func (r *Runner) note() {
	cutoff := time.Now().Add(-time.Hour)
	kept := r.recent[:0]
	for _, t := range r.recent {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	r.recent = append(kept, time.Now())
}

// capped reports whether the hourly cap has been reached. A cap of zero means
// no cap, which is the default.
func (r *Runner) capped() bool {
	if r.Cfg.MaxIssuesHour <= 0 {
		return false
	}
	cutoff := time.Now().Add(-time.Hour)
	n := 0
	for _, t := range r.recent {
		if t.After(cutoff) {
			n++
		}
	}
	return n >= r.Cfg.MaxIssuesHour
}

// openBlockers returns the blockers on a card that are not yet closed.
func (r *Runner) openBlockers(ctx context.Context, item board.Item) ([]string, error) {
	refs, bad := selector.ParseBlockedBy(item.Owner, item.Repo, item.BlockedBy)
	if len(bad) > 0 {
		// A typo in a hand-typed field must not read as "nothing is blocking
		// this". Hold the card and say why.
		r.Log.Warn("unparsable Blocked by entries", "issue", item.Key(), "entries", strings.Join(bad, ", "))
		return bad, nil
	}
	if len(refs) == 0 {
		return nil, nil
	}

	states, err := r.GH.BlockerStates(ctx, refs)
	if err != nil {
		return nil, err
	}
	var open []string
	for _, ref := range refs {
		if !states[ref.String()] {
			open = append(open, ref.String())
		}
	}
	sort.Strings(open)
	return open, nil
}

// commentThenMove is the ordering every stage follows: post the artifact
// first, then move the card.
//
// The card's status is the commit point. A crash before the move leaves the
// card in its input column and the work is simply redone — and the sentinel in
// the comment we just posted makes that redo cheap. The reverse order, which
// the slash-commands used for the request stage, can strand a card in a column
// with no artifact and no worker that reads it.
func (r *Runner) commentThenMove(ctx context.Context, item board.Item, issueID, body, target string, labels labelSwap) error {
	if err := r.GH.AddComment(ctx, issueID, body); err != nil {
		return err
	}
	r.applyLabels(ctx, item, labels)
	return r.GH.SetStatus(ctx, r.Meta, item, target)
}

// labelSwap describes the label changes that accompany a transition.
type labelSwap struct {
	Add        string
	AddColor   string
	AddDescr   string
	Remove     []string
	AlsoRemove bool // remove the configured claim label too
}

// applyLabels performs a label swap. Label failures are warnings, never
// errors: labels are a convenience for humans reading the repository, and
// failing an issue over one would be trading something that matters for
// something that does not.
func (r *Runner) applyLabels(ctx context.Context, item board.Item, s labelSwap) {
	if s.Add != "" {
		if err := r.GH.EnsureLabel(ctx, item.Owner, item.Repo, s.Add, s.AddColor, s.AddDescr); err != nil {
			r.Log.Warn("could not ensure label", "label", s.Add, "issue", item.Key(), "err", err)
		} else if err := r.GH.AddLabels(ctx, item.Owner, item.Repo, item.Number, s.Add); err != nil {
			r.Log.Warn("could not add label", "label", s.Add, "issue", item.Key(), "err", err)
		}
	}
	remove := append([]string{}, s.Remove...)
	if s.AlsoRemove && r.Cfg.ClaimLabel != "" {
		remove = append(remove, r.Cfg.ClaimLabel)
	}
	for _, name := range remove {
		if err := r.GH.RemoveLabel(ctx, item.Owner, item.Repo, item.Number, name); err != nil {
			r.Log.Warn("could not remove label", "label", name, "issue", item.Key(), "err", err)
		}
	}
}

// reject posts a comment, marks the issue for a human and puts the card where
// it belongs.
//
// The needs-human label is what makes a rejection stick. Without it the
// selector would pick the same card up on the next poll, reach the same
// conclusion, and post the same comment again — every ten seconds, forever,
// spending tokens each time.
func (r *Runner) reject(ctx context.Context, item board.Item, issueID, body, target string) error {
	if err := r.GH.AddComment(ctx, issueID, body); err != nil {
		return err
	}
	r.applyLabels(ctx, item, labelSwap{
		Add:        r.Cfg.NeedsHumanLabel,
		AddColor:   r.Cfg.NeedsHumanColor,
		AddDescr:   "organizzer stopped on this issue and needs a person to look at it",
		AlsoRemove: true,
	})
	// A rejection that leaves the card where it already is needs no move, and
	// asking for one would fail the human-checkpoint guard for the plan stage.
	if strings.EqualFold(item.Status, target) {
		return nil
	}
	return r.GH.SetStatus(ctx, r.Meta, item, target)
}

// repoContext reads the live view of the item's repository.
func (r *Runner) repoContext(ctx context.Context, item board.Item) (board.RepoContext, error) {
	rc, err := r.GH.RepoContext(ctx, item.Owner, item.Repo)
	if err != nil {
		return rc, fmt.Errorf("repo context for %s: %w", item.NameWithOwner(), err)
	}
	return rc, nil
}
