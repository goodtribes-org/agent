package board

import (
	"fmt"
	"sort"
	"strings"
)

// ProjectMeta is what discovery resolves once at startup: the project's node
// id, the Status field and the option id for every status the workers use.
type ProjectMeta struct {
	ProjectID     string
	StatusFieldID string
	StatusOptions map[string]string // lowercased status name -> option id
}

// OptionID returns the single-select option id for a status name.
func (m ProjectMeta) OptionID(status string) (string, error) {
	id, ok := m.StatusOptions[strings.ToLower(status)]
	if !ok || id == "" {
		return "", fmt.Errorf("board has no %q option on the Status field", status)
	}
	return id, nil
}

// Item is one card on the board, flattened out of the GraphQL field values.
type Item struct {
	ID       string // project item node id, needed to move the card
	Archived bool

	// Content — present only for items backed by an issue. Draft items and
	// pull requests have none and are skipped.
	IsIssue     bool
	IssueID     string
	Number      int
	Title       string
	Body        string
	URL         string
	State       string // OPEN | CLOSED
	Owner       string
	Repo        string // short name, e.g. "kickfix"
	Labels      []string
	CommentsRaw []Comment

	// Single-select and text fields.
	Status    string
	Phase     string
	Track     string
	Size      string
	BlockedBy string
}

// NameWithOwner renders the "goodtribes-org/kickfix" form.
func (i Item) NameWithOwner() string { return i.Owner + "/" + i.Repo }

// Key identifies an item for backoff bookkeeping.
func (i Item) Key() string { return fmt.Sprintf("%s#%d", i.NameWithOwner(), i.Number) }

// HasLabel reports whether the issue carries a label, case-insensitively.
func (i Item) HasLabel(name string) bool {
	for _, l := range i.Labels {
		if strings.EqualFold(l, name) {
			return true
		}
	}
	return false
}

// HasLabelPrefix reports whether any label starts with the given prefix. This
// is how a card claimed by someone running the old slash-commands is spotted.
func (i Item) HasLabelPrefix(prefix string) bool {
	for _, l := range i.Labels {
		if strings.HasPrefix(strings.ToLower(l), strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}

// Comment is one issue comment.
type Comment struct {
	ID        string
	Body      string
	Author    string
	CreatedAt string
}

// Issue is the full read of an issue, including the comment history the
// stages search for sentinels.
type Issue struct {
	ID       string
	Number   int
	Title    string
	Body     string
	URL      string
	State    string
	Labels   []string
	Comments []Comment
}

// HasLabel reports whether the issue carries a label, case-insensitively.
func (is Issue) HasLabel(name string) bool {
	for _, l := range is.Labels {
		if strings.EqualFold(l, name) {
			return true
		}
	}
	return false
}

// LatestCommentContaining returns the most recent comment whose body contains
// the given sentinel, and whether one was found. Comments are returned by the
// API oldest-first, so the scan runs backwards.
func (is Issue) LatestCommentContaining(sentinel string) (Comment, bool) {
	for i := len(is.Comments) - 1; i >= 0; i-- {
		if strings.Contains(is.Comments[i].Body, sentinel) {
			return is.Comments[i], true
		}
	}
	return Comment{}, false
}

// HasComment reports whether any comment contains the sentinel.
func (is Issue) HasComment(sentinel string) bool {
	_, ok := is.LatestCommentContaining(sentinel)
	return ok
}

// RepoContext is the live view of a repository handed to the model. It is
// deliberately bounded: a whole tree would not fit and would not help.
type RepoContext struct {
	NameWithOwner string
	Description   string
	DefaultBranch string
	Tree          []string          // paths, capped at REPO_TREE_MAX_ENTRIES
	Files         map[string]string // path -> contents, capped at REPO_BLOB_MAX_BYTES each
}

// InTree reports whether a path exists in the fetched tree. Used to drop plan
// steps that name files the model invented.
func (rc RepoContext) InTree(path string) bool {
	path = strings.TrimPrefix(strings.TrimSpace(path), "./")
	for _, p := range rc.Tree {
		if p == path {
			return true
		}
	}
	return false
}

// Render turns the context into the markdown block the prompts embed.
func (rc RepoContext) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Repository: %s\n", rc.NameWithOwner)
	if rc.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", rc.Description)
	}
	fmt.Fprintf(&b, "Default branch: %s\n\n", rc.DefaultBranch)

	b.WriteString("## File tree\n\n```\n")
	for _, p := range rc.Tree {
		b.WriteString(p)
		b.WriteByte('\n')
	}
	b.WriteString("```\n")

	if len(rc.Files) > 0 {
		b.WriteString("\n## Selected files\n")
		// Sorted, not a bare map range: an unstable prompt order would change
		// the request bytes on every call for no reason.
		paths := make([]string, 0, len(rc.Files))
		for p := range rc.Files {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			fmt.Fprintf(&b, "\n### %s\n\n```\n%s\n```\n", p, rc.Files[p])
		}
	}
	return b.String()
}
