package board

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Stage labels mirror the ones the slash-commands created, colours included,
// so a board driven by either looks the same.
const (
	LabelRequest = "request"
	LabelReview  = "review"
	LabelTest    = "test"

	ColorRequest = "e4e669"
	ColorReview  = "d93f0b"
	ColorTest    = "0e8a16"
)

// EnsureLabel creates a label if it does not exist. An existing label is left
// exactly as it is — colours are not corrected, because a human may have
// changed one on purpose.
func (c *Client) EnsureLabel(ctx context.Context, owner, repo, name, color, description string) error {
	path := fmt.Sprintf("/repos/%s/%s/labels/%s", owner, repo, url.PathEscape(name))
	if _, err := c.rest(ctx, "GET", path, nil); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}

	_, err := c.rest(ctx, "POST", fmt.Sprintf("/repos/%s/%s/labels", owner, repo), map[string]string{
		"name":        name,
		"color":       strings.TrimPrefix(color, "#"),
		"description": description,
	})
	if err != nil {
		// A concurrent create loses the race with 422; that is a success for
		// our purposes — the label exists either way.
		if strings.Contains(err.Error(), "already_exists") {
			return nil
		}
		return fmt.Errorf("create label %q in %s/%s: %w", name, owner, repo, err)
	}
	return nil
}

// AddLabels adds labels to an issue. Adding one that is already there is a
// no-op on GitHub's side.
func (c *Client) AddLabels(ctx context.Context, owner, repo string, number int, names ...string) error {
	if len(names) == 0 {
		return nil
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/labels", owner, repo, number)
	if _, err := c.rest(ctx, "POST", path, map[string][]string{"labels": names}); err != nil {
		return fmt.Errorf("add labels %v to %s/%s#%d: %w", names, owner, repo, number, err)
	}
	return nil
}

// RemoveLabel drops one label. A label that is not on the issue is not an
// error — the caller usually cannot know whether it was there.
func (c *Client) RemoveLabel(ctx context.Context, owner, repo string, number int, name string) error {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/labels/%s", owner, repo, number, url.PathEscape(name))
	if _, err := c.rest(ctx, "DELETE", path, nil); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return fmt.Errorf("remove label %q from %s/%s#%d: %w", name, owner, repo, number, err)
	}
	return nil
}
