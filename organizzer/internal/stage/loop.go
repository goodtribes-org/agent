package stage

import (
	"context"
	"errors"
	"time"

	"github.com/goodtribes-org/agent/organizzer/internal/board"
	"github.com/goodtribes-org/agent/organizzer/internal/selector"
	"github.com/goodtribes-org/agent/organizzer/internal/service"
)

// Stage is one worker: which column it reads, and what it does with a card.
type Stage interface {
	// Name is the stage name, matching the subcommand.
	Name() string
	// From is the board status this stage picks cards up from.
	From() string
	// Ordered reports whether candidates are sorted by phase, track and size
	// before one is picked. Only the request stage needs it: `plan` and
	// `apply` cards were put there by a human, and second-guessing that order
	// would override the decision they just made.
	Ordered() bool
	// SkipBlocked reports whether a card with open blockers is silently passed
	// over (the request stage, matching gh-request) rather than handled inside
	// Process by commenting and handing it back (the later stages, where a
	// human moved the card and deserves to be told why it bounced).
	SkipBlocked() bool
	// Process handles exactly one card.
	Process(ctx context.Context, r *Runner, item board.Item) error
	// DryRun reports what Process would do, without changing anything.
	DryRun(ctx context.Context, r *Runner, item board.Item) error
}

// Run drives one stage until the context is cancelled.
func Run(ctx context.Context, r *Runner, s Stage, health *service.Health, once bool) error {
	meta, err := r.GH.DiscoverProject(ctx)
	if err != nil {
		return err
	}
	r.Meta = meta

	r.Log.Info("worker started",
		"from", s.From(),
		"busy", r.Cfg.PollBusy.String(),
		"idle", r.Cfg.PollIdle.String(),
		"dryRun", r.Cfg.DryRun)

	for {
		if ctx.Err() != nil {
			return nil
		}

		worked, err := r.cycle(ctx, s)
		if err != nil {
			r.Log.Error("poll cycle failed", "err", err)
		}
		if health != nil {
			health.Tick()
		}
		if once {
			return err
		}

		// Ten seconds between issues so a queue drains promptly; a full minute
		// when there is nothing to do, because polling an idle board faster
		// buys nothing and spends rate limit. A poll that ran out of blocker
		// checks before it ran out of candidates counts as busy — it has not
		// seen the whole column yet, and waiting a minute per twenty cards to
		// find that out is how a long queue looks idle for ten minutes.
		wait := r.Cfg.PollIdle
		if worked || r.sweeping {
			wait = r.Cfg.PollBusy
		}
		if !sleep(ctx, wait) {
			return nil
		}
	}
}

// cycle runs one poll: list the board, pick at most one card, process it.
// It reports whether it did any work.
func (r *Runner) cycle(ctx context.Context, s Stage) (bool, error) {
	if r.capped() {
		r.Log.Warn("hourly issue cap reached, idling", "cap", r.Cfg.MaxIssuesHour)
		return false, nil
	}

	items, err := r.GH.ListItems(ctx)
	if err != nil {
		// A board id that no longer resolves means the board was edited
		// underneath us; rediscovering is cheaper than a restart.
		var gqlErr *board.GraphQLErrors
		if errors.As(err, &gqlErr) && gqlErr.StaleSchema() {
			r.Log.Warn("board schema looks stale, rediscovering", "err", err)
			if meta, derr := r.GH.DiscoverProject(ctx); derr == nil {
				r.Meta = meta
			}
		}
		return false, err
	}

	candidates := selector.Candidates(items, s.From(), r.Cfg, r.penalised)
	if len(candidates) == 0 {
		r.Log.Info("nothing to do", "status", s.From())
		return false, nil
	}
	if s.Ordered() {
		selector.Order(candidates, r.Cfg)
	}

	// The blocked-by check costs an API call per card, so it runs here on the
	// ordered candidates rather than across the whole board.
	//
	// For the request stage a blocked card is passed over silently, matching
	// gh-request — and the search continues past it. Stopping at the first
	// blocked card would starve the stage: the ordering is stable, so the same
	// blocked card would sit at the head of the queue on every poll and
	// nothing behind it would ever be worked.
	//
	// The later stages do not come through here at all. A human moved those
	// cards, so Process comments and hands the card back, which takes it out
	// of the column rather than leaving it to be skipped forever.
	item := candidates[0]
	if s.SkipBlocked() {
		// Bounded so one poll cannot spend an unlimited number of API calls
		// on a board where everything is blocked. The window has to move: a
		// board with more blocked cards than the bound would otherwise starve
		// exactly as it did when the scan stopped at the first blocked card,
		// just twenty places further down.
		const maxChecks = 20

		start := r.scanFrom
		if start >= len(candidates) {
			start = 0
		}
		checks := maxChecks
		if checks > len(candidates) {
			checks = len(candidates)
		}

		picked := false
		for n := 0; n < checks; n++ {
			c := candidates[(start+n)%len(candidates)]
			open, err := r.openBlockers(ctx, c)
			if err != nil {
				r.penalise(c)
				return false, err
			}
			if len(open) > 0 {
				r.Log.Info("skipping blocked card", "issue", c.Key(), "blockers", open)
				continue
			}
			item = c
			picked = true
			break
		}
		if picked {
			// Back to the head, so the next poll picks by priority rather than
			// from wherever this scan happened to stop.
			r.scanFrom = 0
			r.sweeping = false
			r.swept = 0
		} else {
			r.scanFrom = (start + checks) % len(candidates)
			r.swept += checks

			// A full lap with nothing workable means the column is blocked,
			// not busy. Keep going at the idle interval: sweeping it again at
			// the busy one is twenty blocker checks every ten seconds to learn
			// the same thing, and the rate limit is shared with the work the
			// stage exists to do.
			if r.swept >= len(candidates) {
				r.sweeping = false
				r.swept = 0
				r.Log.Info("every candidate is blocked", "status", s.From(),
					"candidates", len(candidates), "resumeAt", r.scanFrom)
			} else {
				r.sweeping = true
				r.Log.Info("no workable card in this window, continuing down the list",
					"status", s.From(), "checked", checks,
					"candidates", len(candidates), "resumeAt", r.scanFrom)
			}
			return false, nil
		}
	}

	// One card per cycle. Draining the whole queue in a single pass would mean
	// a bad prompt or a broken webhook damages every card before anyone can
	// stop it; one at a time keeps the blast radius to one issue.

	r.Log.Info("picked", "issue", item.Key(), "title", item.Title,
		"phase", item.Phase, "track", item.Track, "size", item.Size)

	if r.Cfg.DryRun {
		return true, s.DryRun(ctx, r, item)
	}

	r.note()
	if err := s.Process(ctx, r, item); err != nil {
		r.Log.Error("processing failed", "issue", item.Key(), "err", err)
		r.penalise(item)
		return true, nil // the failure is already reported; the loop continues
	}
	return true, nil
}

// sleep waits, returning false if the context was cancelled first.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
