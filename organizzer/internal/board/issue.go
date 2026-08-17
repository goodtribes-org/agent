package board

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// GetIssue reads one issue in full, including the comment tail.
func (c *Client) GetIssue(ctx context.Context, owner, repo string, number int) (Issue, error) {
	var resp struct {
		Repository struct {
			Issue struct {
				ID     string `json:"id"`
				Number int    `json:"number"`
				Title  string `json:"title"`
				Body   string `json:"body"`
				URL    string `json:"url"`
				State  string `json:"state"`
				Labels struct {
					Nodes []struct {
						Name string `json:"name"`
					} `json:"nodes"`
				} `json:"labels"`
				Comments struct {
					Nodes []struct {
						ID        string `json:"id"`
						Body      string `json:"body"`
						CreatedAt string `json:"createdAt"`
						Author    struct {
							Login string `json:"login"`
						} `json:"author"`
					} `json:"nodes"`
				} `json:"comments"`
			} `json:"issue"`
		} `json:"repository"`
	}

	err := c.graphql(ctx, qIssue, map[string]any{
		"owner":    owner,
		"repo":     repo,
		"number":   number,
		"comments": c.cfg.MaxIssueCmts,
	}, &resp)
	if err != nil {
		return Issue{}, fmt.Errorf("read %s/%s#%d: %w", owner, repo, number, err)
	}

	r := resp.Repository.Issue
	if r.ID == "" {
		return Issue{}, fmt.Errorf("read %s/%s#%d: %w", owner, repo, number, ErrNotFound)
	}

	is := Issue{
		ID:     r.ID,
		Number: r.Number,
		Title:  r.Title,
		Body:   r.Body,
		URL:    r.URL,
		State:  r.State,
	}
	for _, l := range r.Labels.Nodes {
		is.Labels = append(is.Labels, l.Name)
	}
	for _, cm := range r.Comments.Nodes {
		is.Comments = append(is.Comments, Comment{
			ID:        cm.ID,
			Body:      cm.Body,
			Author:    cm.Author.Login,
			CreatedAt: cm.CreatedAt,
		})
	}
	return is, nil
}

// AddComment posts a comment on an issue.
func (c *Client) AddComment(ctx context.Context, issueID, body string) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("refusing to post an empty comment")
	}
	err := c.graphql(ctx, mAddComment, map[string]any{
		"subjectId": issueID,
		"body":      body,
	}, nil)
	if err != nil {
		return fmt.Errorf("add comment: %w", err)
	}
	return nil
}

// BlockerRef is one entry from the board's "Blocked by" field.
type BlockerRef struct {
	Owner  string
	Repo   string
	Number int
}

func (b BlockerRef) String() string { return fmt.Sprintf("%s#%d", b.Repo, b.Number) }

// blockerCache remembers which blockers are already closed. Only CLOSED is
// cached: an open blocker can close at any moment and a stale "still open"
// would hold a ready card back, whereas a closed issue does not reopen itself.
type blockerCache struct {
	mu     sync.Mutex
	closed map[string]time.Time
}

var blockers = blockerCache{closed: map[string]time.Time{}}

// BlockerStates returns, for each ref, whether that issue is closed. All refs
// are resolved in one aliased query.
//
// The "Blocked by" field is free text typed by a human, so every value goes
// through a GraphQL variable — never string interpolation into the document.
func (c *Client) BlockerStates(ctx context.Context, refs []BlockerRef) (map[string]bool, error) {
	out := make(map[string]bool, len(refs))
	if len(refs) == 0 {
		return out, nil
	}

	var pending []BlockerRef
	blockers.mu.Lock()
	for _, r := range refs {
		key := r.Owner + "/" + r.Repo + "#" + fmt.Sprint(r.Number)
		if at, ok := blockers.closed[key]; ok && time.Since(at) < c.cfg.BlockerCache {
			out[r.String()] = true
			continue
		}
		pending = append(pending, r)
	}
	blockers.mu.Unlock()

	if len(pending) == 0 {
		return out, nil
	}

	var (
		decl    []string
		body    []string
		vars    = map[string]any{}
		aliases = make(map[string]BlockerRef, len(pending))
	)
	for i, r := range pending {
		alias := fmt.Sprintf("b%d", i)
		aliases[alias] = r
		decl = append(decl, fmt.Sprintf("$o%d: String!, $r%d: String!, $n%d: Int!", i, i, i))
		body = append(body, fmt.Sprintf(
			"  %s: repository(owner: $o%d, name: $r%d) { issue(number: $n%d) { state } }", alias, i, i, i))
		vars[fmt.Sprintf("o%d", i)] = r.Owner
		vars[fmt.Sprintf("r%d", i)] = r.Repo
		vars[fmt.Sprintf("n%d", i)] = r.Number
	}

	query := "query(" + strings.Join(decl, ", ") + ") {\n" + strings.Join(body, "\n") + "\n}"

	var raw map[string]json.RawMessage
	if err := c.graphql(ctx, query, vars, &raw); err != nil {
		return nil, fmt.Errorf("read blocker states: %w", err)
	}

	now := time.Now()
	blockers.mu.Lock()
	defer blockers.mu.Unlock()
	for alias, ref := range aliases {
		var node struct {
			Issue *struct {
				State string `json:"state"`
			} `json:"issue"`
		}
		payload, ok := raw[alias]
		if !ok {
			// The repository or issue does not resolve. Treat an unresolvable
			// blocker as still blocking — a typo in the field should hold the
			// card, not silently release it.
			out[ref.String()] = false
			c.log.Warn("blocker did not resolve", "ref", ref.String())
			continue
		}
		if err := json.Unmarshal(payload, &node); err != nil || node.Issue == nil {
			out[ref.String()] = false
			c.log.Warn("blocker did not resolve", "ref", ref.String())
			continue
		}
		closed := strings.EqualFold(node.Issue.State, "CLOSED")
		out[ref.String()] = closed
		if closed {
			blockers.closed[ref.Owner+"/"+ref.Repo+"#"+fmt.Sprint(ref.Number)] = now
		}
	}
	return out, nil
}
