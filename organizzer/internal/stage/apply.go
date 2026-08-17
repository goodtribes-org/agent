package stage

import (
	"context"
	"encoding/json"

	"github.com/goodtribes-org/agent/organizzer/internal/board"
	"github.com/goodtribes-org/agent/organizzer/internal/config"
	"github.com/goodtribes-org/agent/organizzer/internal/foundry"
	"github.com/goodtribes-org/agent/organizzer/internal/render"
)

// Apply hands an approved plan to the implementation agent: apply → test.
//
// This stage never calls a language model. The plan was written by the plan
// stage and approved by a human; all that happens here is a hand-off.
type Apply struct{}

func (Apply) Name() string      { return config.StageApply }
func (Apply) From() string      { return config.StatusApply }
func (Apply) Ordered() bool     { return false }
func (Apply) SkipBlocked() bool { return false }

func (Apply) Process(ctx context.Context, r *Runner, item board.Item) error {
	issue, err := r.GH.GetIssue(ctx, item.Owner, item.Repo, item.Number)
	if err != nil {
		return err
	}

	open, err := r.openBlockers(ctx, item)
	if err != nil {
		return err
	}
	if len(open) > 0 {
		r.Log.Info("card is still blocked", "issue", item.Key(), "blockers", open)
		if err := r.GH.AddComment(ctx, issue.ID, render.BlockerOpen(open, config.StatusReview)); err != nil {
			return err
		}
		return r.GH.SetStatus(ctx, r.Meta, item, config.StatusReview)
	}

	// This check is load-bearing in a way the other stages' are not. The
	// webhook names every task task-<unixMilli>, so a second submission is a
	// second agent run rather than a duplicate the server can reject: two
	// sandboxes, two pull requests, twice the spend. The sentinel is the only
	// thing standing between a crash-and-restart and that outcome.
	if issue.HasComment(render.SentinelHandoff) {
		r.Log.Info("work already dispatched, completing the transition", "issue", item.Key())
		r.applyLabels(ctx, item, testLabels())
		return r.GH.SetStatus(ctx, r.Meta, item, config.StatusTest)
	}

	plan, ok := issue.LatestCommentContaining(render.SentinelPlan)
	if !ok {
		r.Log.Warn("no approved plan on the issue", "issue", item.Key())
		if err := r.GH.AddComment(ctx, issue.ID, render.NoPlan(config.StatusReview)); err != nil {
			return err
		}
		r.applyLabels(ctx, item, labelSwap{AlsoRemove: true})
		return r.GH.SetStatus(ctx, r.Meta, item, config.StatusReview)
	}

	req := foundry.BuildTaskRequest(r.Cfg, item, issue, plan.Body)

	resp, err := r.Fnd.Submit(ctx, req)
	if err != nil {
		// No agent started. Say so plainly on the issue and stop — retrying
		// unattended risks starting two.
		r.Log.Error("dispatch failed", "issue", item.Key(), "err", err)
		return r.reject(ctx, item, issue.ID,
			render.DispatchFailed(err.Error(), config.StatusReview, r.Cfg.NeedsHumanLabel),
			config.StatusReview)
	}

	body := render.Handoff(resp.Name, resp.Namespace, item.NameWithOwner(),
		req.Branch, req.Agent, req.Skills)

	return r.commentThenMove(ctx, item, issue.ID, body, config.StatusTest, testLabels())
}

func testLabels() labelSwap {
	return labelSwap{
		Add:        board.LabelTest,
		AddColor:   board.ColorTest,
		AddDescr:   "implementation dispatched, waiting for the agent's pull request",
		Remove:     []string{board.LabelReview},
		AlsoRemove: true,
	}
}

func (Apply) DryRun(ctx context.Context, r *Runner, item board.Item) error {
	return reportDryRun(ctx, r, item, config.StatusApply, config.StatusTest, func() (string, error) {
		issue, err := r.GH.GetIssue(ctx, item.Owner, item.Repo, item.Number)
		if err != nil {
			return "", err
		}
		plan, ok := issue.LatestCommentContaining(render.SentinelPlan)
		if !ok {
			return "would return the card to review: no approved plan on the issue", nil
		}

		// Print the exact body that would be posted to the webhook, so it can
		// be replayed with curl against a port-forwarded service before
		// anything runs unattended.
		req := foundry.BuildTaskRequest(r.Cfg, item, issue, plan.Body)
		pretty, err := json.MarshalIndent(req, "", "  ")
		if err != nil {
			return "", err
		}
		return "would POST to " + r.Fnd.URL() + ":\n" + string(pretty), nil
	})
}
