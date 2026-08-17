package foundry

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/goodtribes-org/agent/organizzer/internal/board"
	"github.com/goodtribes-org/agent/organizzer/internal/config"
)

func testCfg() config.Config {
	return config.Config{
		FoundryAgent:   "open-code",
		FoundrySkills:  []string{"berget"},
		FoundrySecret:  "factory-creds-goodtribes",
		FoundryBranch:  "main",
		FoundryRetries: 1,
		GitAuthorName:  "organizzer",
		GitAuthorEmail: "organizzer@goodtribes.org",
	}
}

func testItem() board.Item {
	return board.Item{
		Owner: "goodtribes-org", Repo: "kickfix", Number: 42,
		Title: "Add keyboard navigation",
	}
}

func testIssue() board.Issue {
	return board.Issue{
		Number: 42,
		Title:  "Add keyboard navigation",
		URL:    "https://github.com/goodtribes-org/kickfix/issues/42",
	}
}

// The webhook's body is flat and it does not reject unknown fields. A
// `credentials` object — which is how the SoftwareTask CRD nests these — would
// be silently dropped and the task would run against the wrong secret. The
// shape of the marshalled JSON is therefore worth asserting directly.
func TestTaskRequestWireShape(t *testing.T) {
	req := BuildTaskRequest(testCfg(), testItem(), testIssue(), "## Implementation Plan\n\nstep one")

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// secretRef and githubToken are top level, not under credentials.
	if _, nested := got["credentials"]; nested {
		t.Error("body has a credentials object; the webhook expects secretRef at the top level")
	}
	if got["secretRef"] != "factory-creds-goodtribes" {
		t.Errorf("secretRef = %v, want factory-creds-goodtribes", got["secretRef"])
	}

	// kube-foundry v0.1.0 has no name and no resources field. Sending them is
	// harmless but misleading — the task name is server-generated and cpu,
	// memory and timeout can only be set by creating the CR directly.
	for _, absent := range []string{"name", "resources", "cpu", "memory", "timeoutMinutes"} {
		if _, present := got[absent]; present {
			t.Errorf("body carries %q, which the webhook does not accept", absent)
		}
	}

	// The webhook defaults agent to claude-code and secretRef to
	// factory-creds. Neither works here, so both must be sent explicitly.
	if got["agent"] != "open-code" {
		t.Errorf("agent = %v, want open-code sent explicitly", got["agent"])
	}

	// The CRD constrains repo to ^https:// — an SSH remote is rejected.
	repo, _ := got["repo"].(string)
	if !strings.HasPrefix(repo, "https://") {
		t.Errorf("repo = %q, want an https URL", repo)
	}
	if repo != "https://github.com/goodtribes-org/kickfix" {
		t.Errorf("repo = %q", repo)
	}

	if got["branch"] != "main" {
		t.Errorf("branch = %v, want main", got["branch"])
	}
	skills, _ := got["skills"].([]any)
	if len(skills) != 1 || skills[0] != "berget" {
		t.Errorf("skills = %v, want [berget]", got["skills"])
	}
}

// The plan was approved by a human at the review → apply gate. Paraphrasing it
// here would mean the thing that was approved is not the thing implemented.
func TestTaskBodyCarriesThePlanVerbatim(t *testing.T) {
	const plan = "## Implementation Plan\n\n1. **src/a.ts** — do the thing\n   - detail"
	req := BuildTaskRequest(testCfg(), testItem(), testIssue(), plan)

	if !strings.Contains(req.Task, plan) {
		t.Fatalf("the approved plan is not present verbatim in the task body:\n%s", req.Task)
	}
	for _, want := range []string{
		"--- APPROVED PLAN ---",
		"--- END PLAN ---",
		"Closes #42",
		"https://github.com/goodtribes-org/kickfix/issues/42",
		// The house rules the agent must not break.
		"Do not change any hostname, URL, ingress",
		"npx prisma generate", // part of kickfix's build gate
	} {
		if !strings.Contains(req.Task, want) {
			t.Errorf("task body is missing %q", want)
		}
	}
}

func TestBuildGate(t *testing.T) {
	tests := map[string]string{
		"goodtribes.org": "npm ci && npm run lint --workspace=frontend && npm run build:frontend",
		"asylguiden.se":  "npm ci && npm run lint --workspace=frontend && npm run build:frontend",
		"kickfix":        "(cd frontend && npm ci && npm run build) && (cd backend && npm ci && npx prisma generate && npm test)",
		"agent":          "cd organizzer && go vet ./... && go test ./...",
	}
	for repo, want := range tests {
		if got := BuildGate(repo); got != want {
			t.Errorf("BuildGate(%q) = %q, want %q", repo, got, want)
		}
	}
	if got := BuildGate("something-else"); !strings.Contains(got, "README") {
		t.Errorf("an unknown repo should fall back to a generic instruction, got %q", got)
	}
}

func TestMaxRetriesIsSentAsANumber(t *testing.T) {
	cfg := testCfg()
	cfg.FoundryRetries = 0
	req := BuildTaskRequest(cfg, testItem(), testIssue(), "plan")

	raw, _ := json.Marshal(req)
	// A plain int with omitempty would drop a deliberate zero, which the CRD
	// reads as "do not retry". The pointer keeps it on the wire.
	if !strings.Contains(string(raw), `"maxRetries":0`) {
		t.Fatalf("maxRetries=0 did not survive marshalling: %s", raw)
	}
}
