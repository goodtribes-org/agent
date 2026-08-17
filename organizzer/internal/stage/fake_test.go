package stage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/goodtribes-org/agent/organizzer/internal/board"
	"github.com/goodtribes-org/agent/organizzer/internal/config"
	"github.com/goodtribes-org/agent/organizzer/internal/foundry"
)

// fakeBoard records what a stage did instead of doing it.
//
// It enforces the same human-checkpoint rule the real client does, so a test
// asserting that no worker can move a card into `plan` or `apply` is testing
// the rule rather than the fake.
type fakeBoard struct {
	issue board.Issue
	items []board.Item

	// blockerOpen maps "repo#N" to true when that blocker is still open.
	blockerOpen map[string]bool

	repoTree []string

	comments  []string
	statusSet []string
	added     []string
	removed   []string

	failComment bool
	failStatus  bool
	failIssue   bool
}

func (f *fakeBoard) DiscoverProject(context.Context) (board.ProjectMeta, error) {
	return board.ProjectMeta{
		ProjectID:     "PVT_test",
		StatusFieldID: "PVTSSF_test",
		StatusOptions: map[string]string{
			config.StatusNew: "o1", config.StatusRequest: "o2", config.StatusPlan: "o3",
			config.StatusReview: "o4", config.StatusApply: "o5", config.StatusTest: "o6",
		},
	}, nil
}

func (f *fakeBoard) ListItems(context.Context) ([]board.Item, error) { return f.items, nil }

func (f *fakeBoard) GetIssue(_ context.Context, _, _ string, _ int) (board.Issue, error) {
	if f.failIssue {
		return board.Issue{}, errors.New("github is having a moment")
	}
	return f.issue, nil
}

func (f *fakeBoard) AddComment(_ context.Context, _, body string) error {
	if f.failComment {
		return errors.New("comment failed")
	}
	f.comments = append(f.comments, body)
	return nil
}

func (f *fakeBoard) SetStatus(_ context.Context, _ board.ProjectMeta, _ board.Item, target string) error {
	if board.IsHumanCheckpoint(target) {
		return fmt.Errorf("refusing to move a card to %q: that transition is a human checkpoint", target)
	}
	if f.failStatus {
		return errors.New("status update failed")
	}
	f.statusSet = append(f.statusSet, target)
	return nil
}

func (f *fakeBoard) BlockerStates(_ context.Context, refs []board.BlockerRef) (map[string]bool, error) {
	out := map[string]bool{}
	for _, r := range refs {
		out[r.String()] = !f.blockerOpen[r.String()] // true means closed
	}
	return out, nil
}

func (f *fakeBoard) RepoContext(_ context.Context, owner, repo string) (board.RepoContext, error) {
	return board.RepoContext{
		NameWithOwner: owner + "/" + repo,
		DefaultBranch: "main",
		Tree:          f.repoTree,
		Files:         map[string]string{},
	}, nil
}

func (f *fakeBoard) FetchFiles(context.Context, *board.RepoContext, string, string, []string, int) {}

func (f *fakeBoard) EnsureLabel(context.Context, string, string, string, string, string) error {
	return nil
}

func (f *fakeBoard) AddLabels(_ context.Context, _, _ string, _ int, names ...string) error {
	f.added = append(f.added, names...)
	return nil
}

func (f *fakeBoard) RemoveLabel(_ context.Context, _, _ string, _ int, name string) error {
	f.removed = append(f.removed, name)
	return nil
}

// lastStatus returns the column the card ended in, or "" if it never moved.
func (f *fakeBoard) lastStatus() string {
	if len(f.statusSet) == 0 {
		return ""
	}
	return f.statusSet[len(f.statusSet)-1]
}

func (f *fakeBoard) commentsContain(s string) bool {
	for _, c := range f.comments {
		if strings.Contains(c, s) {
			return true
		}
	}
	return false
}

func (f *fakeBoard) hasLabel(name string) bool {
	for _, l := range f.added {
		if l == name {
			return true
		}
	}
	return false
}

// fakeLLM returns a canned reply, or an error.
type fakeLLM struct {
	reply any
	err   error
	calls int
}

func (f *fakeLLM) JSON(_ context.Context, _, _ string, out any) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	raw, err := json.Marshal(f.reply)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// fakeFoundry records submissions.
type fakeFoundry struct {
	err      error
	submits  []foundry.TaskRequest
	response foundry.TaskResponse
}

func (f *fakeFoundry) Submit(_ context.Context, req foundry.TaskRequest) (foundry.TaskResponse, error) {
	if f.err != nil {
		return foundry.TaskResponse{}, f.err
	}
	f.submits = append(f.submits, req)
	resp := f.response
	if resp.Name == "" {
		resp = foundry.TaskResponse{Name: "task-1711612800000", Namespace: "kubefoundry"}
	}
	return resp, nil
}

func (f *fakeFoundry) URL() string { return "http://webhook.test/api/v1/tasks" }

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newTestRunner(stage string, gh *fakeBoard) *Runner {
	cfg := config.Config{
		Stage:           stage,
		GitHubOrg:       "goodtribes-org",
		AllowedRepos:    []string{"kickfix", "asylguiden.se"},
		PriorityTracks:  []string{"Foundation", "Infra"},
		NeedsHumanLabel: "needs-human",
		NeedsHumanColor: "d4c5f9",
		Model:           "test-model",
		FoundryAgent:    "open-code",
		FoundrySkills:   []string{"berget"},
		FoundrySecret:   "factory-creds-goodtribes",
		FoundryBranch:   "main",
		PlanFileFetch:   8,
	}
	meta, _ := gh.DiscoverProject(context.Background())
	return &Runner{
		Cfg:     cfg,
		GH:      gh,
		Log:     quietLog(),
		Meta:    meta,
		backoff: map[string]time.Time{},
	}
}
