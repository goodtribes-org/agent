package verdict

import (
	"strings"
	"testing"
)

func goodRequest() RequestVerdict {
	var v RequestVerdict
	v.SubProject = "kickfix"
	v.Scope.Label = "Small"
	v.Scope.EstimatedFiles = 3
	v.StackCheck.Passes = true
	v.InvariantCheck.Passes = true
	v.Context = "Adds a keyboard shortcut."
	v.Steps = []string{"edit the inbox route"}
	v.Files = []FileChange{{Path: "src/app/inbox/page.tsx", Why: "wire the handler"}}
	v.Testing = []string{"npm run lint && npm run build"}
	return v
}

func TestRequestVerdictAccepted(t *testing.T) {
	v := goodRequest()
	if err := v.Validate("kickfix"); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// The model does not get to reassign work to another repository. A mismatch is
// corrected, not rejected — the card's own repository is the truth.
func TestRequestVerdictForcesRealRepo(t *testing.T) {
	v := goodRequest()
	v.SubProject = "asylguiden.se"
	if err := v.Validate("kickfix"); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if v.SubProject != "kickfix" {
		t.Fatalf("sub_project = %q, want it forced to kickfix", v.SubProject)
	}
}

// A rejection stops the card and costs a person's attention, so it has to come
// with reasons and a way forward.
func TestRequestVerdictTooLargeNeedsReasonsAndSplit(t *testing.T) {
	var v RequestVerdict
	v.Scope.TooLarge = true

	err := v.Validate("kickfix")
	if err == nil {
		t.Fatal("Validate accepted a too_large verdict with no reasons and no split")
	}
	for _, want := range []string{"scope.reasons", "scope.split_suggestion"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}

	v.Scope.Reasons = []string{"touches the whole store layer"}
	v.Scope.SplitSuggestion = []string{"split the migration out"}
	if err := v.Validate("kickfix"); err != nil {
		t.Fatalf("Validate rejected a complete too_large verdict: %v", err)
	}
}

func TestRequestVerdictRejectsIncomplete(t *testing.T) {
	tests := map[string]func(*RequestVerdict){
		"empty steps":               func(v *RequestVerdict) { v.Steps = nil },
		"empty files":               func(v *RequestVerdict) { v.Files = nil },
		"empty testing":             func(v *RequestVerdict) { v.Testing = nil },
		"empty context":             func(v *RequestVerdict) { v.Context = "" },
		"bad label":                 func(v *RequestVerdict) { v.Scope.Label = "Enormous" },
		"file with no path":         func(v *RequestVerdict) { v.Files = []FileChange{{Why: "x"}} },
		"sensitive with no reason":  func(v *RequestVerdict) { v.SensitiveData.Found = true },
		"stack fails with no flags": func(v *RequestVerdict) { v.StackCheck.Passes = false },
		"invariant fails with none": func(v *RequestVerdict) { v.InvariantCheck.Passes = false },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			v := goodRequest()
			mutate(&v)
			if err := v.Validate("kickfix"); err == nil {
				t.Fatal("Validate accepted an unusable verdict")
			}
		})
	}
}

func goodPlan() PlanVerdict {
	return PlanVerdict{
		SubProject: "kickfix",
		ScopeLabel: "Small",
		Background: "The inbox needs a shortcut.",
		Steps: []PlanStep{
			{File: "src/a.ts", Change: "add handler"},
			{File: "src/b.ts", Change: "wire it up"},
		},
		Coverage:     []Coverage{{Criterion: "pressing j moves down", Step: 2}},
		Verification: []string{"npm run build"},
	}
}

func TestPlanVerdictCoverageMustPointAtARealStep(t *testing.T) {
	v := goodPlan()
	v.Coverage = []Coverage{{Criterion: "x", Step: 9}}
	if err := v.Validate("kickfix"); err == nil {
		t.Fatal("Validate accepted a criterion pointing at a step that does not exist")
	}
	v.Coverage = []Coverage{{Criterion: "x", Step: 0}}
	if err := v.Validate("kickfix"); err == nil {
		t.Fatal("Validate accepted a zero step index; the field is 1-based")
	}
}

func TestPlanVerdictBlockedNeedsAReason(t *testing.T) {
	v := PlanVerdict{Blocked: true}
	if err := v.Validate("kickfix"); err == nil {
		t.Fatal("Validate accepted blocked with no reason")
	}
	v.BlockedReason = "the issue does not say which mailbox"
	if err := v.Validate("kickfix"); err != nil {
		t.Fatalf("Validate rejected a complete blocked verdict: %v", err)
	}
}

// A plan is executed verbatim by an agent, so a step pointing at a file that
// does not exist sends it hunting. Dropping the step is the lesser harm.
func TestDropUnknownFiles(t *testing.T) {
	v := goodPlan()
	v.Steps = []PlanStep{
		{File: "src/real.ts", Change: "edit it"},
		{File: "src/invented.ts", Change: "edit it"},
		{File: "src/brand-new.ts", Change: "new file holding the helper"},
	}
	v.Coverage = []Coverage{
		{Criterion: "first", Step: 1},
		{Criterion: "second", Step: 2}, // points at the dropped step
		{Criterion: "third", Step: 3},
	}

	inTree := func(p string) bool { return p == "src/real.ts" }
	dropped := v.DropUnknownFiles(inTree)

	if len(dropped) != 1 || dropped[0] != "src/invented.ts" {
		t.Fatalf("dropped = %v, want [src/invented.ts]", dropped)
	}
	if len(v.Steps) != 2 {
		t.Fatalf("kept %d steps, want 2 (the real one and the new file)", len(v.Steps))
	}
	if v.Steps[1].File != "src/brand-new.ts" {
		t.Errorf("a step describing a new file should survive, got %q", v.Steps[1].File)
	}

	// Coverage indexes are positions in Steps, so they have to follow the
	// renumbering — otherwise every criterion after a dropped step points at
	// the wrong work.
	if len(v.Coverage) != 2 {
		t.Fatalf("coverage = %v, want the entry for the dropped step removed", v.Coverage)
	}
	if v.Coverage[0].Step != 1 || v.Coverage[1].Step != 2 {
		t.Fatalf("coverage steps = %d,%d, want 1,2 after renumbering",
			v.Coverage[0].Step, v.Coverage[1].Step)
	}
	if v.Coverage[1].Criterion != "third" {
		t.Errorf("coverage[1] = %q, want the third criterion remapped onto step 2",
			v.Coverage[1].Criterion)
	}
}

func TestDropUnknownFilesLeavesAGoodPlanAlone(t *testing.T) {
	v := goodPlan()
	before := len(v.Steps)
	if dropped := v.DropUnknownFiles(func(string) bool { return true }); dropped != nil {
		t.Fatalf("dropped %v from a plan whose files all exist", dropped)
	}
	if len(v.Steps) != before {
		t.Fatalf("steps = %d, want %d untouched", len(v.Steps), before)
	}
}
