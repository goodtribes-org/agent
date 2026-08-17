package render

import (
	"strings"
	"testing"

	"github.com/goodtribes-org/agent/organizzer/internal/board"
	"github.com/goodtribes-org/agent/organizzer/internal/verdict"
)

// The sentinels are a contract, not decoration. The apply stage finds the
// approved plan by searching for SentinelPlan, and each stage decides whether
// it has already run by looking for its own. Changing one of these strings —
// including the em dashes and the straight quotes — makes a card get worked
// twice, which for the apply stage means two agent runs and two pull requests.
func TestSentinelsAreExact(t *testing.T) {
	tests := map[string]string{
		SentinelOutline: "*Outline written by /gh-request — move card to 'plan' to approve and trigger detailed planning.*",
		SentinelPlan:    "*Plan written by /gh-plan — move card to 'apply' to begin implementation.*",
		SentinelHandoff: "*Implementation dispatched by organizzer — card moved to 'test'.*",
	}
	for got, want := range tests {
		if got != want {
			t.Errorf("sentinel changed:\n got %q\nwant %q", got, want)
		}
	}
}

func fullVerdict() verdict.RequestVerdict {
	var v verdict.RequestVerdict
	v.SubProject = "kickfix"
	v.Scope.Label = "Small"
	v.Scope.EstimatedFiles = 2
	v.StackCheck.Passes = true
	v.InvariantCheck.Passes = true
	v.Context = "The inbox has no keyboard navigation."
	v.Steps = []string{"add a key handler to the inbox route", "document the shortcut"}
	v.Files = []verdict.FileChange{
		{Path: "src/app/inbox/page.tsx", Why: "holds the list"},
		{Path: "README.md", Why: "document it"},
	}
	v.Testing = []string{"npm run lint && npm run build"}
	return v
}

func TestOutlineGolden(t *testing.T) {
	const want = `## Request Outline

**Sub-project:** kickfix
**Scope:** Small — ~2 files
**Sensitive data:** None
**Stack check:** Passes
**Invariant check:** Passes

### Context
The inbox has no keyboard navigation.

### Steps
1. add a key handler to the inbox route
2. document the shortcut

### Files to change
- ` + "`src/app/inbox/page.tsx`" + ` — holds the list
- ` + "`README.md`" + ` — document it

### Testing
- npm run lint && npm run build

---
` + SentinelOutline + `
`

	if got := Outline(fullVerdict()); got != want {
		t.Fatalf("outline mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A flagged issue is still outlined. gh-request reported sensitive data,
// stack flags and invariant conflicts and let the human decide at the
// request → plan gate; auto-rejecting on them would take that decision away.
func TestOutlineReportsFlagsWithoutRejecting(t *testing.T) {
	v := fullVerdict()
	v.SensitiveData.Found = true
	v.SensitiveData.Reason = "stores a personnummer"
	v.StackCheck.Passes = false
	v.StackCheck.Flags = []string{"introduces Redis"}
	v.InvariantCheck.Passes = false
	v.InvariantCheck.Conflicts = []string{"adds OFFSET pagination"}

	got := Outline(v)
	for _, want := range []string{
		"**Sensitive data:** Yes — stores a personnummer — implementer must document storage and access control before this proceeds",
		"**Stack check:** Flags:",
		"  - introduces Redis",
		"**Invariant check:** Conflicts:",
		"  - adds OFFSET pagination",
		SentinelOutline,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("outline is missing %q\n%s", want, got)
		}
	}
}

func TestPlanGolden(t *testing.T) {
	issue := board.Issue{Number: 42, Title: "Add keyboard navigation to the inbox"}
	v := verdict.PlanVerdict{
		ScopeLabel: "Small",
		ScopeFiles: 2,
		Background: "The inbox list is mouse-only today.",
		Steps: []verdict.PlanStep{
			{
				File:    "src/app/inbox/page.tsx",
				Change:  "add a keydown handler",
				Details: []string{"handler name: onListKeyDown", "j moves down, k moves up"},
			},
			{File: "README.md", Change: "document the shortcut"},
		},
		CodeNotes:    []string{"the list is virtualised, so index maths matters"},
		Coverage:     []verdict.Coverage{{Criterion: "j and k move the selection", Step: 1}},
		Verification: []string{"npm run lint && npm run build", "the list scrolls with j"},
	}

	const want = `## Implementation Plan

**Issue:** #42 — Add keyboard navigation to the inbox
**Sub-project:** kickfix
**Estimated scope:** Small (~2 files)

### Background
The inbox list is mouse-only today.

### Implementation steps
1. **src/app/inbox/page.tsx** — add a keydown handler
   - handler name: onListKeyDown
   - j moves down, k moves up
2. **README.md** — document the shortcut

### Code notes
- the list is virtualised, so index maths matters

### Acceptance criteria coverage
- [ ] j and k move the selection → step 1

### Verification
1. npm run lint && npm run build
2. the list scrolls with j

---
` + SentinelPlan + `
`

	if got := Plan(issue, "kickfix", v); got != want {
		t.Fatalf("plan mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// The apply stage locates the plan by searching comment bodies for the
// sentinel. If a rendered plan did not contain it, every card in `apply` would
// bounce back to `review` claiming there was no plan.
func TestRenderedPlanIsFindableBySentinel(t *testing.T) {
	body := Plan(board.Issue{Number: 1, Title: "t"}, "kickfix", verdict.PlanVerdict{
		Background:   "b",
		Steps:        []verdict.PlanStep{{File: "a.go", Change: "c"}},
		Verification: []string{"make test"},
	})
	issue := board.Issue{Comments: []board.Comment{
		{Body: "unrelated chatter"},
		{Body: body},
	}}
	if _, ok := issue.LatestCommentContaining(SentinelPlan); !ok {
		t.Fatal("a rendered plan is not findable by its own sentinel")
	}
}

// The card moves to `test` when the webhook acknowledges the task, which is
// long before a pull request exists. Anyone reading the board has to be told
// that, or `test` reads as "ready to review".
func TestHandoffSaysNoPullRequestYet(t *testing.T) {
	got := Handoff("task-1711612800000", "kubefoundry", "goodtribes-org/kickfix",
		"main", "open-code", []string{"berget"})

	for _, want := range []string{
		"task-1711612800000",
		"**No pull request exists yet**",
		"kubectl -n kubefoundry get st task-1711612800000",
		SentinelHandoff,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("handoff is missing %q\n%s", want, got)
		}
	}
}

func TestDispatchFailedIsUnambiguous(t *testing.T) {
	got := DispatchFailed("connection refused", "review", "needs-human")
	if !strings.Contains(got, "No agent was started") {
		t.Errorf("a failed dispatch must say plainly that nothing was started\n%s", got)
	}
}

// Model output arrives with newlines in places the markdown cannot take them.
func TestOneLineCollapsesNewlines(t *testing.T) {
	v := fullVerdict()
	v.Steps = []string{"first line\nsecond line"}
	v.Context = "fine"
	got := Outline(v)
	if strings.Contains(got, "1. first line\nsecond line") {
		t.Fatalf("a multi-line step broke out of its list item\n%s", got)
	}
	if !strings.Contains(got, "1. first line second line") {
		t.Fatalf("expected the step collapsed onto one line\n%s", got)
	}
}
