// Package foundry hands an approved plan to kube-foundry, which runs the
// OpenCode agent that actually writes the code and opens the pull request.
package foundry

import (
	"fmt"
	"strings"

	"github.com/goodtribes-org/agent/organizzer/internal/board"
	"github.com/goodtribes-org/agent/organizzer/internal/config"
)

// TaskRequest is the webhook's request body.
//
// This mirrors kube-foundry v0.1.0's CreateTaskRequest exactly, and the shape
// has two traps worth stating out loud:
//
//   - It is flat. `secretRef` and `githubToken` are top level, not nested under
//     a `credentials` object the way the SoftwareTask CRD nests them. The
//     handler does not reject unknown fields, so a nested `credentials` would
//     be silently dropped and the task would run with the wrong secret.
//   - There is no `name` field and no `resources` field. The task name is
//     server-generated as task-<unixMilli>, and cpu/memory/timeout can only be
//     set by creating the custom resource directly.
//
// The second point is why the handoff comment's sentinel is the only guard
// against dispatching the same plan twice: the server cannot reject a
// duplicate, because every submission gets a fresh name.
type TaskRequest struct {
	Repo           string   `json:"repo"`
	Branch         string   `json:"branch,omitempty"`
	Task           string   `json:"task"`
	Agent          string   `json:"agent,omitempty"`
	SecretRef      string   `json:"secretRef,omitempty"`
	Skills         []string `json:"skills,omitempty"`
	MaxRetries     *int32   `json:"maxRetries,omitempty"`
	CallbackURL    string   `json:"callbackURL,omitempty"`
	GitAuthorName  string   `json:"gitAuthorName,omitempty"`
	GitAuthorEmail string   `json:"gitAuthorEmail,omitempty"`
}

// TaskResponse is the webhook's 201 body.
type TaskResponse struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Message   string `json:"message"`
}

// buildGates maps a repository to the command that proves a change works
// there. It matches the table in guides/stacks.md.
// The sandbox has no Docker daemon, so the `docker compose build` gate the
// root CLAUDE.md prescribes for a human cannot be the gate here. These are the
// npm scripts each repo actually declares — goodtribes.org and asylguiden.se
// are npm workspace roots, kickfix is two independent packages whose root
// package.json is empty.
var buildGates = map[string]string{
	"goodtribes.org": "npm ci && npm run lint --workspace=frontend && npm run build:frontend",
	"asylguiden.se":  "npm ci && npm run lint --workspace=frontend && npm run build:frontend",
	"kickfix":        "(cd frontend && npm ci && npm run build) && (cd backend && npm ci && npx prisma generate && npm test)",
	"agent":          "cd organizzer && go vet ./... && go test ./...",
}

// BuildGate returns the build command for a repository, or a generic
// instruction when the repository is not one we know.
func BuildGate(repo string) string {
	if g, ok := buildGates[strings.ToLower(repo)]; ok {
		return g
	}
	return "the build and test commands documented in the repository's README"
}

// BuildTaskRequest turns an issue plus its approved plan into a webhook body.
//
// This function is deliberately the only place in the service that knows the
// kube-foundry wire format. Everything above it deals in "issue and approved
// plan"; everything below it deals in HTTP. If the contract changes, the blast
// radius is this file.
func BuildTaskRequest(cfg config.Config, item board.Item, issue board.Issue, plan string) TaskRequest {
	retries := int32(cfg.FoundryRetries)

	req := TaskRequest{
		// The CRD constrains repo to ^https:// — an SSH remote is rejected.
		Repo:   "https://github.com/" + item.NameWithOwner(),
		Branch: cfg.FoundryBranch,
		Task:   taskBody(item, issue, plan),
		// agent and secretRef are always sent. The webhook defaults them to
		// claude-code and factory-creds, and neither works on this cluster:
		// claude-code speaks the Anthropic Messages API, which berget does not
		// serve, and factory-creds is not the secret that exists.
		Agent:          cfg.FoundryAgent,
		SecretRef:      cfg.FoundrySecret,
		Skills:         cfg.FoundrySkills,
		MaxRetries:     &retries,
		CallbackURL:    cfg.CallbackURL,
		GitAuthorName:  cfg.GitAuthorName,
		GitAuthorEmail: cfg.GitAuthorEmail,
	}
	return req
}

// taskBody is the natural-language instruction the agent receives: a preamble
// that restates the house rules, the approved plan verbatim, and what the pull
// request must say.
//
// The plan goes in unmodified. It was approved by a human at the review → apply
// gate, and paraphrasing it here would mean the thing that was approved is not
// the thing that gets implemented.
func taskBody(item board.Item, issue board.Issue, plan string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "You are implementing an approved plan for issue #%d in %s.\n",
		issue.Number, item.NameWithOwner())
	fmt.Fprintf(&b, "Issue: %s\n", issue.URL)
	fmt.Fprintf(&b, "Title: %s\n\n", issue.Title)

	b.WriteString("Rules:\n")
	b.WriteString("- Read every file named below before you change it. Do not guess at locations.\n")
	b.WriteString("- Implement exactly the plan. Do not restructure unrelated code and do not touch\n")
	b.WriteString("  files the plan does not name.\n")
	b.WriteString("- Do not add a database, cache, queue or external API that the repository does\n")
	b.WriteString("  not already use.\n")
	b.WriteString("- Do not change any hostname, URL, ingress or Helm ingress value. Those are\n")
	b.WriteString("  owned by ops and are off limits.\n")
	b.WriteString("- If a step would require breaking one of the repository's stated security\n")
	b.WriteString("  invariants, stop and say so in the pull request instead of doing it.\n")
	fmt.Fprintf(&b, "- Run the repository's own build gate before you commit: %s\n", BuildGate(item.Repo))
	b.WriteString("- In the pull request description, state which files you changed and why.\n\n")

	b.WriteString("--- APPROVED PLAN ---\n")
	b.WriteString(strings.TrimSpace(plan))
	b.WriteString("\n--- END PLAN ---\n\n")

	fmt.Fprintf(&b, "The pull request body must end with: Closes #%d\n", issue.Number)
	return b.String()
}
