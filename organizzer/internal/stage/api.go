package stage

import (
	"context"

	"github.com/goodtribes-org/agent/organizzer/internal/board"
	"github.com/goodtribes-org/agent/organizzer/internal/foundry"
)

// The three collaborators a stage talks to, as interfaces.
//
// They exist so the stages can be tested against fakes. Every failure mode in
// these workers is about *where a card ends up* when something goes wrong —
// an unparseable model reply, a webhook timeout, a comment that posts but a
// move that does not. Those are the paths worth pinning down, and none of them
// can be exercised against the real GitHub API.
//
// The concrete clients satisfy these implicitly; nothing in board, llm or
// foundry knows these interfaces exist.

// BoardAPI is the GitHub surface the stages use.
type BoardAPI interface {
	DiscoverProject(ctx context.Context) (board.ProjectMeta, error)
	ListItems(ctx context.Context) ([]board.Item, error)
	GetIssue(ctx context.Context, owner, repo string, number int) (board.Issue, error)
	AddComment(ctx context.Context, issueID, body string) error
	SetStatus(ctx context.Context, meta board.ProjectMeta, item board.Item, target string) error
	BlockerStates(ctx context.Context, refs []board.BlockerRef) (map[string]bool, error)
	RepoContext(ctx context.Context, owner, repo string) (board.RepoContext, error)
	FetchFiles(ctx context.Context, rc *board.RepoContext, owner, repo string, paths []string, max int)
	EnsureLabel(ctx context.Context, owner, repo, name, color, description string) error
	AddLabels(ctx context.Context, owner, repo string, number int, names ...string) error
	RemoveLabel(ctx context.Context, owner, repo string, number int, name string) error
}

// LLMAPI is the model surface: one call, JSON in, JSON out.
type LLMAPI interface {
	JSON(ctx context.Context, system, user string, out any) error
}

// FoundryAPI is the implementation-agent surface.
type FoundryAPI interface {
	Submit(ctx context.Context, req foundry.TaskRequest) (foundry.TaskResponse, error)
	URL() string
}
