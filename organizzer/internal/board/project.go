package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/goodtribes-org/agent/organizzer/internal/config"
)

// DiscoverProject resolves the project's node id, the Status field and every
// option id the workers move cards to.
//
// Discovery by name rather than hardcoded ids is the deliberate choice: the
// board is edited by hand, and a redeploy should not be the price of adding a
// field. The STATUS_OPTION_* pins exist for the case where a board edit breaks
// discovery and someone needs the workers running again immediately.
func (c *Client) DiscoverProject(ctx context.Context) (ProjectMeta, error) {
	if c.cfg.ProjectNodeID != "" && c.cfg.StatusFieldID != "" && allPinned(c.cfg) {
		meta := ProjectMeta{
			ProjectID:     c.cfg.ProjectNodeID,
			StatusFieldID: c.cfg.StatusFieldID,
			StatusOptions: map[string]string{},
		}
		for _, s := range config.AllStatuses {
			meta.StatusOptions[s] = c.cfg.StatusOptions[s]
		}
		c.log.Info("using pinned board ids", "project", meta.ProjectID)
		return meta, nil
	}

	var resp struct {
		Organization struct {
			ProjectV2 struct {
				ID     string `json:"id"`
				Title  string `json:"title"`
				Fields struct {
					Nodes []struct {
						ID      string `json:"id"`
						Name    string `json:"name"`
						Options []struct {
							ID   string `json:"id"`
							Name string `json:"name"`
						} `json:"options"`
					} `json:"nodes"`
				} `json:"fields"`
			} `json:"projectV2"`
		} `json:"organization"`
	}

	err := c.graphql(ctx, qProjectMeta, map[string]any{
		"org":    c.cfg.GitHubOrg,
		"number": c.cfg.ProjectNumber,
	}, &resp)
	if err != nil {
		return ProjectMeta{}, fmt.Errorf("discover project: %w", err)
	}

	p := resp.Organization.ProjectV2
	if p.ID == "" {
		return ProjectMeta{}, fmt.Errorf("project %d not found in org %s", c.cfg.ProjectNumber, c.cfg.GitHubOrg)
	}

	meta := ProjectMeta{ProjectID: p.ID, StatusOptions: map[string]string{}}
	for _, f := range p.Fields.Nodes {
		if !strings.EqualFold(f.Name, c.cfg.FieldNameStatus) {
			continue
		}
		meta.StatusFieldID = f.ID
		for _, o := range f.Options {
			meta.StatusOptions[strings.ToLower(o.Name)] = o.ID
		}
	}
	if meta.StatusFieldID == "" {
		return ProjectMeta{}, fmt.Errorf("project %q has no %q field", p.Title, c.cfg.FieldNameStatus)
	}

	// Fail here rather than on the first card. A board missing a column the
	// workers move cards into is a board they cannot drive, and the failure
	// belongs at startup where someone is watching.
	var missing []string
	for _, s := range config.AllStatuses {
		if meta.StatusOptions[s] == "" {
			missing = append(missing, s)
		}
	}
	if len(missing) > 0 {
		return ProjectMeta{}, fmt.Errorf("project %q Status field is missing options: %s",
			p.Title, strings.Join(missing, ", "))
	}

	// Env pins win over discovery when both are present.
	if c.cfg.ProjectNodeID != "" {
		meta.ProjectID = c.cfg.ProjectNodeID
	}
	if c.cfg.StatusFieldID != "" {
		meta.StatusFieldID = c.cfg.StatusFieldID
	}
	for _, s := range config.AllStatuses {
		if v := c.cfg.StatusOptions[s]; v != "" {
			meta.StatusOptions[s] = v
		}
	}

	c.log.Info("discovered board", "project", p.Title, "id", meta.ProjectID, "statusField", meta.StatusFieldID)
	return meta, nil
}

func allPinned(cfg config.Config) bool {
	for _, s := range config.AllStatuses {
		if cfg.StatusOptions[s] == "" {
			return false
		}
	}
	return true
}

// ListItems pages through every card on the board.
//
// The whole board is fetched rather than filtered server-side because
// ProjectsV2 has no field-value filter in the API — the slash-commands did the
// same thing with `gh project item-list --limit 200`. At ~100 items this is
// one or two round trips.
func (c *Client) ListItems(ctx context.Context) ([]Item, error) {
	names := fieldNames{
		Status:    c.cfg.FieldNameStatus,
		Phase:     c.cfg.FieldNamePhase,
		Track:     c.cfg.FieldNameTrack,
		Size:      c.cfg.FieldNameSize,
		BlockedBy: c.cfg.FieldNameBlockedBy,
	}

	var (
		out    []Item
		cursor *string
	)
	// A hard page ceiling so a pagination bug cannot spin forever against the
	// API. 50 pages at 100 items is far beyond any plausible board.
	for page := 0; page < 50; page++ {
		var resp struct {
			Organization struct {
				ProjectV2 struct {
					Items struct {
						PageInfo struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
						Nodes []rawItem `json:"nodes"`
					} `json:"items"`
				} `json:"projectV2"`
			} `json:"organization"`
		}

		vars := map[string]any{
			"org":    c.cfg.GitHubOrg,
			"number": c.cfg.ProjectNumber,
			"first":  c.cfg.ItemPageSize,
			"after":  nil,
		}
		if cursor != nil {
			vars["after"] = *cursor
		}

		if err := c.graphql(ctx, qProjectItems, vars, &resp); err != nil {
			return nil, fmt.Errorf("list project items: %w", err)
		}

		items := resp.Organization.ProjectV2.Items
		for _, r := range items.Nodes {
			out = append(out, flatten(r, names))
		}
		if !items.PageInfo.HasNextPage || items.PageInfo.EndCursor == "" {
			return out, nil
		}
		next := items.PageInfo.EndCursor
		cursor = &next
	}
	return out, nil
}
