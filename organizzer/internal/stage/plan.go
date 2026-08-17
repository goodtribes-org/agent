package stage

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/goodtribes-org/agent/organizzer/internal/board"
	"github.com/goodtribes-org/agent/organizzer/internal/config"
	"github.com/goodtribes-org/agent/organizzer/internal/llm"
	"github.com/goodtribes-org/agent/organizzer/internal/prompt"
	"github.com/goodtribes-org/agent/organizzer/internal/render"
	"github.com/goodtribes-org/agent/organizzer/internal/verdict"
)

// Plan writes the implementation plan: plan → review.
type Plan struct{}

func (Plan) Name() string      { return config.StagePlan }
func (Plan) From() string      { return config.StatusPlan }
func (Plan) Ordered() bool     { return false }
func (Plan) SkipBlocked() bool { return false }

func (Plan) Process(ctx context.Context, r *Runner, item board.Item) error {
	issue, err := r.GH.GetIssue(ctx, item.Owner, item.Repo, item.Number)
	if err != nil {
		return err
	}

	// A human can drag a blocked card straight into `plan`, so the check runs
	// again here rather than being trusted from selection.
	open, err := r.openBlockers(ctx, item)
	if err != nil {
		return err
	}
	if len(open) > 0 {
		r.Log.Info("card is still blocked", "issue", item.Key(), "blockers", open)
		if err := r.GH.AddComment(ctx, issue.ID, render.BlockerOpen(open, config.StatusRequest)); err != nil {
			return err
		}
		return r.GH.SetStatus(ctx, r.Meta, item, config.StatusRequest)
	}

	if issue.HasComment(render.SentinelPlan) {
		r.Log.Info("plan already present, completing the transition", "issue", item.Key())
		r.applyLabels(ctx, item, reviewLabels())
		return r.GH.SetStatus(ctx, r.Meta, item, config.StatusReview)
	}

	if !r.Cfg.AllowsRepo(item.Repo) {
		return r.reject(ctx, item, issue.ID,
			render.RejectUnknownRepo(item.Repo, r.Cfg.AllowedRepos, r.Cfg.NeedsHumanLabel),
			config.StatusRequest)
	}

	// The approved outline is the starting point. It is expanded, never
	// contradicted — a human approved that shape at the request → plan gate.
	outline := ""
	if c, ok := issue.LatestCommentContaining(render.SentinelOutline); ok {
		outline = c.Body
	} else {
		r.Log.Warn("no approved outline found, planning from the issue alone", "issue", item.Key())
	}

	rc, err := r.repoContext(ctx, item)
	if err != nil {
		return err
	}
	// Second pass: pull the actual source of the files the outline and the
	// issue name. A plan written from a bare file tree names paths but cannot
	// say what is already in them.
	r.GH.FetchFiles(ctx, &rc, item.Owner, item.Repo,
		candidatePaths(outline, issue.Body), r.Cfg.PlanFileFetch)

	system, err := prompt.System()
	if err != nil {
		return err
	}
	user, err := prompt.Render(config.StagePlan, prompt.Data{
		Repo:        item.NameWithOwner(),
		Number:      issue.Number,
		Title:       issue.Title,
		Body:        issue.Body,
		Phase:       item.Phase,
		Track:       item.Track,
		Size:        item.Size,
		RepoContext: rc.Render(),
		Outline:     outline,
	})
	if err != nil {
		return err
	}

	var v verdict.PlanVerdict
	if err := r.LLM.JSON(ctx, system, user, &v); err != nil {
		// Running out of tokens is a decision the model already made, not a
		// transient failure. Left to the normal error path the card would back
		// off, be picked up again, produce the same overlong answer and fail
		// identically — for as long as nobody was watching.
		if errors.Is(err, llm.ErrTruncated) {
			r.Log.Warn("plan exceeded the token budget", "issue", item.Key(), "maxTokens", r.Cfg.MaxTokens)
			return r.reject(ctx, item, issue.ID,
				render.PlanTooLong(r.Cfg.MaxTokens, config.StatusRequest, r.Cfg.NeedsHumanLabel),
				config.StatusRequest)
		}
		return fmt.Errorf("plan verdict for %s: %w", item.Key(), err)
	}
	if err := v.Validate(item.Repo); err != nil {
		return fmt.Errorf("plan verdict for %s is not usable: %w", item.Key(), err)
	}

	if v.Blocked {
		r.Log.Info("model declined to write a plan", "issue", item.Key(), "reason", v.BlockedReason)
		return r.reject(ctx, item, issue.ID,
			render.PlanBlocked(v.BlockedReason, config.StatusRequest, r.Cfg.NeedsHumanLabel),
			config.StatusRequest)
	}

	// Drop steps naming files that are neither in the repository nor described
	// as new. The plan is executed verbatim by an agent, and a step pointing at
	// a file that does not exist sends it hunting.
	if dropped := v.DropUnknownFiles(rc.InTree); len(dropped) > 0 {
		r.Log.Warn("dropped plan steps naming unknown files",
			"issue", item.Key(), "files", dropped)
	}
	if len(v.Steps) == 0 {
		return fmt.Errorf("plan for %s had no steps left after dropping unknown files", item.Key())
	}

	return r.commentThenMove(ctx, item, issue.ID,
		render.Plan(issue, v.SubProject, v), config.StatusReview, reviewLabels())
}

func reviewLabels() labelSwap {
	return labelSwap{
		Add:        board.LabelReview,
		AddColor:   board.ColorReview,
		AddDescr:   "plan written, waiting for a human to approve it",
		Remove:     []string{board.LabelRequest},
		AlsoRemove: true,
	}
}

// pathLike matches things that look like repository paths in free text: a
// backticked path, or a bare token with a slash or a known source extension.
var pathLike = regexp.MustCompile("`([A-Za-z0-9._/\\-\\[\\]]+\\.[A-Za-z0-9]+)`|(?m)^\\s*[-*]\\s+([A-Za-z0-9._/\\-]+\\.[A-Za-z0-9]+)")

// candidatePaths pulls likely file paths out of the outline and the issue
// body, in first-seen order. Anything not actually in the tree is dropped
// later by FetchFiles, so a false positive costs nothing.
func candidatePaths(texts ...string) []string {
	seen := map[string]bool{}
	var out []string
	for _, text := range texts {
		for _, m := range pathLike.FindAllStringSubmatch(text, -1) {
			for _, group := range m[1:] {
				p := strings.TrimPrefix(strings.TrimSpace(group), "./")
				if p == "" || seen[p] {
					continue
				}
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

func (Plan) DryRun(ctx context.Context, r *Runner, item board.Item) error {
	return reportDryRun(ctx, r, item, config.StatusPlan, config.StatusReview, func() (string, error) {
		return "would ask " + r.Cfg.Model + " for an implementation plan, then post it", nil
	})
}
