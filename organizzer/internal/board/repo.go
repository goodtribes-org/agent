package board

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
)

// interestingRoot are the root files worth reading for any repository: they
// identify the stack and the build gate without the model having to guess.
var interestingRoot = []string{
	"README.md", "package.json", "go.mod", "compose.yaml", "docker-compose.yml", "Makefile",
}

// noisyPrefixes never help a plan and would crowd out real source. The tree is
// capped, so what gets dropped here is what gets kept elsewhere.
var noisyPrefixes = []string{
	".git/", "node_modules/", "vendor/", "bin/", "dist/", ".next/", "coverage/",
	".github/workflows/codeql", "package-lock.json", "go.sum",
}

// RepoContext reads a bounded live view of a repository: description, default
// branch, a capped file tree and the root files that identify the stack.
//
// Live rather than baked, because a plan that names paths from a stale tree is
// a plan the implementing agent cannot follow. Bounded, because the whole tree
// of postfix-client would swamp the prompt.
func (c *Client) RepoContext(ctx context.Context, owner, repo string) (RepoContext, error) {
	rc := RepoContext{
		NameWithOwner: owner + "/" + repo,
		Files:         map[string]string{},
	}

	var meta struct {
		Repository struct {
			Description      string `json:"description"`
			DefaultBranchRef struct {
				Name string `json:"name"`
			} `json:"defaultBranchRef"`
		} `json:"repository"`
	}
	if err := c.graphql(ctx, qRepoContext, map[string]any{"owner": owner, "repo": repo}, &meta); err != nil {
		return rc, fmt.Errorf("read repo %s: %w", rc.NameWithOwner, err)
	}
	rc.Description = meta.Repository.Description
	rc.DefaultBranch = meta.Repository.DefaultBranchRef.Name
	if rc.DefaultBranch == "" {
		rc.DefaultBranch = "main"
	}

	tree, err := c.tree(ctx, owner, repo, rc.DefaultBranch)
	if err != nil {
		return rc, err
	}
	rc.Tree = tree

	for _, name := range interestingRoot {
		if !rc.InTree(name) {
			continue
		}
		body, err := c.File(ctx, owner, repo, rc.DefaultBranch, name)
		if err != nil {
			c.log.Warn("could not read root file", "repo", rc.NameWithOwner, "path", name, "err", err)
			continue
		}
		rc.Files[name] = body
	}

	return rc, nil
}

// tree lists the repository's files, filtered and capped.
func (c *Client) tree(ctx context.Context, owner, repo, ref string) ([]string, error) {
	p := fmt.Sprintf("/repos/%s/%s/git/trees/%s?recursive=1", owner, repo, url.PathEscape(ref))
	body, err := c.rest(ctx, "GET", p, nil)
	if err != nil {
		return nil, fmt.Errorf("read tree of %s/%s: %w", owner, repo, err)
	}

	var resp struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode tree of %s/%s: %w", owner, repo, err)
	}

	var paths []string
	for _, e := range resp.Tree {
		if e.Type != "blob" || isNoisy(e.Path) {
			continue
		}
		paths = append(paths, e.Path)
	}
	sort.Strings(paths)

	// Report truncation rather than presenting a partial tree as complete —
	// the plan stage drops steps naming paths outside the tree, so a silently
	// short tree would look like the model inventing files.
	if resp.Truncated {
		c.log.Warn("repository tree truncated by github", "repo", owner+"/"+repo)
	}
	if max := c.cfg.RepoTreeMax; max > 0 && len(paths) > max {
		c.log.Warn("repository tree capped",
			"repo", owner+"/"+repo, "kept", max, "dropped", len(paths)-max)
		paths = paths[:max]
	}
	return paths, nil
}

func isNoisy(p string) bool {
	for _, prefix := range noisyPrefixes {
		if strings.HasPrefix(p, prefix) || p == strings.TrimSuffix(prefix, "/") {
			return true
		}
	}
	// Lock files and build output carry no design information.
	switch path.Base(p) {
	case "package-lock.json", "go.sum", "yarn.lock", "pnpm-lock.yaml":
		return true
	}
	return false
}

// File reads one file's contents, truncated to REPO_BLOB_MAX_BYTES.
func (c *Client) File(ctx context.Context, owner, repo, ref, filePath string) (string, error) {
	p := fmt.Sprintf("/repos/%s/%s/contents/%s?ref=%s",
		owner, repo, escapePath(filePath), url.QueryEscape(ref))
	body, err := c.rest(ctx, "GET", p, nil)
	if err != nil {
		return "", err
	}

	var resp struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
		Size     int    `json:"size"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("decode contents of %s: %w", filePath, err)
	}
	if resp.Encoding != "base64" {
		return "", fmt.Errorf("unexpected encoding %q for %s", resp.Encoding, filePath)
	}

	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(resp.Content, "\n", ""))
	if err != nil {
		return "", fmt.Errorf("decode %s: %w", filePath, err)
	}
	if max := c.cfg.RepoBlobMax; max > 0 && len(raw) > max {
		return string(raw[:max]) + "\n… (truncated)", nil
	}
	return string(raw), nil
}

// FetchFiles adds the given paths to a context, skipping anything not in the
// tree and stopping at the configured cap. Errors on individual files are
// logged and skipped: a missing file is worth less than the whole context.
func (c *Client) FetchFiles(ctx context.Context, rc *RepoContext, owner, repo string, paths []string, max int) {
	fetched := 0
	for _, p := range paths {
		if fetched >= max {
			c.log.Info("file fetch cap reached", "repo", rc.NameWithOwner, "cap", max)
			return
		}
		p = strings.TrimPrefix(strings.TrimSpace(p), "./")
		if p == "" || !rc.InTree(p) {
			continue
		}
		if _, done := rc.Files[p]; done {
			continue
		}
		body, err := c.File(ctx, owner, repo, rc.DefaultBranch, p)
		if err != nil {
			if !errors.Is(err, ErrNotFound) {
				c.log.Warn("could not read file", "repo", rc.NameWithOwner, "path", p, "err", err)
			}
			continue
		}
		rc.Files[p] = body
		fetched++
	}
}

// escapePath escapes each path segment, leaving the separators alone.
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}
