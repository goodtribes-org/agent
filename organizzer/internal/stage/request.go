package stage

import (
	"context"
	"fmt"

	"github.com/goodtribes-org/agent/organizzer/internal/board"
	"github.com/goodtribes-org/agent/organizzer/internal/config"
	"github.com/goodtribes-org/agent/organizzer/internal/prompt"
	"github.com/goodtribes-org/agent/organizzer/internal/render"
	"github.com/goodtribes-org/agent/organizzer/internal/verdict"
)

// Request turns a new issue into a request outline: new → request.
type Request struct{}

func (Request) Name() string      { return config.StageRequest }
func (Request) From() string      { return config.StatusNew }
func (Request) Ordered() bool     { return true }
func (Request) SkipBlocked() bool { return true }

func (Request) Process(ctx context.Context, r *Runner, item board.Item) error {
	issue, err := r.GH.GetIssue(ctx, item.Owner, item.Repo, item.Number)
	if err != nil {
		return err
	}

	// Already outlined — a previous run posted the comment and then failed
	// before moving the card. Finish the move, skip the model.
	if issue.HasComment(render.SentinelOutline) {
		r.Log.Info("outline already present, completing the transition", "issue", item.Key())
		r.applyLabels(ctx, item, labelSwap{
			Add:        board.LabelRequest,
			AddColor:   board.ColorRequest,
			AddDescr:   "outline written, waiting for a human to approve it",
			AlsoRemove: true,
		})
		return r.GH.SetStatus(ctx, r.Meta, item, config.StatusRequest)
	}

	if !r.Cfg.AllowsRepo(item.Repo) {
		r.Log.Warn("card points at a repository this worker does not handle",
			"issue", item.Key(), "repo", item.Repo)
		return r.reject(ctx, item, issue.ID,
			render.RejectUnknownRepo(item.Repo, r.Cfg.AllowedRepos, r.Cfg.NeedsHumanLabel),
			config.StatusNew)
	}

	rc, err := r.repoContext(ctx, item)
	if err != nil {
		return err
	}

	system, err := prompt.System()
	if err != nil {
		return err
	}
	user, err := prompt.Render(config.StageRequest, prompt.Data{
		Repo:        item.NameWithOwner(),
		Number:      issue.Number,
		Title:       issue.Title,
		Body:        issue.Body,
		Phase:       item.Phase,
		Track:       item.Track,
		Size:        item.Size,
		RepoContext: rc.Render(),
	})
	if err != nil {
		return err
	}

	var v verdict.RequestVerdict
	if err := r.LLM.JSON(ctx, system, user, &v); err != nil {
		// Nothing has been posted and nothing moved. The card stays in `new`
		// and comes back after the backoff.
		return fmt.Errorf("request verdict for %s: %w", item.Key(), err)
	}
	if err := v.Validate(item.Repo); err != nil {
		return fmt.Errorf("request verdict for %s is not usable: %w", item.Key(), err)
	}

	if v.Scope.TooLarge {
		r.Log.Info("issue judged too large", "issue", item.Key(), "reasons", v.Scope.Reasons)
		return r.reject(ctx, item, issue.ID,
			render.RejectTooLarge(v, r.Cfg.NeedsHumanLabel), config.StatusNew)
	}

	// Sensitive data, stack flags and invariant conflicts are reported in the
	// outline, never acted on. gh-request worked the same way: the human at
	// the request → plan gate decides whether a flagged issue proceeds.
	if v.SensitiveData.Found {
		r.Log.Info("sensitive data flagged", "issue", item.Key(), "reason", v.SensitiveData.Reason)
	}
	if !v.InvariantCheck.Passes {
		r.Log.Warn("invariant conflict flagged", "issue", item.Key(), "conflicts", v.InvariantCheck.Conflicts)
	}

	return r.commentThenMove(ctx, item, issue.ID, render.Outline(v), config.StatusRequest, labelSwap{
		Add:        board.LabelRequest,
		AddColor:   board.ColorRequest,
		AddDescr:   "outline written, waiting for a human to approve it",
		AlsoRemove: true,
	})
}

func (Request) DryRun(ctx context.Context, r *Runner, item board.Item) error {
	return reportDryRun(ctx, r, item, config.StatusNew, config.StatusRequest, func() (string, error) {
		if !r.Cfg.AllowsRepo(item.Repo) {
			return "would reject: repository not in ALLOWED_REPOS", nil
		}
		return "would ask " + r.Cfg.Model + " for a request verdict, then post a Request Outline", nil
	})
}
