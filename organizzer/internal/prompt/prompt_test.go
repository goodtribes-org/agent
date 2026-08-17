package prompt

import (
	"strings"
	"testing"
)

func sample() Data {
	return Data{
		Repo:        "goodtribes-org/kickfix",
		Number:      42,
		Title:       "Add keyboard navigation",
		Body:        "The job list is mouse-only.",
		Phase:       "M2 Marketplace",
		Track:       "Frontend",
		Size:        "S",
		RepoContext: "Repository: goodtribes-org/kickfix",
		Outline:     "## Request Outline\n\nsome outline",
	}
}

// A typo in a template field would otherwise surface only at runtime, on a
// real issue, after the model call has already been paid for.
func TestTemplatesRender(t *testing.T) {
	for _, stage := range []string{"request", "plan"} {
		t.Run(stage, func(t *testing.T) {
			got, err := Render(stage, sample())
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if strings.Contains(got, "<no value>") {
				t.Error("a template referenced a field that does not exist")
			}
			for _, want := range []string{
				"goodtribes-org/kickfix",
				"Add keyboard navigation",
				"M2 Marketplace",
				"Repository: goodtribes-org/kickfix",
				// The guides have to be baked in and interpolated, or the
				// model is judging against nothing.
				"Never `prisma migrate dev`",       // from guides/invariants.md
				"npm run build:frontend",           // from guides/stacks.md
				"Never touch cluster networking",   // from guides/working-root.md
			} {
				if !strings.Contains(got, want) {
					t.Errorf("%s prompt is missing %q", stage, want)
				}
			}
		})
	}
}

// The outline is optional: an issue can reach the plan stage without one if a
// human moved it straight through.
func TestPlanPromptWithoutAnOutline(t *testing.T) {
	d := sample()
	d.Outline = ""
	got, err := Render("plan", d)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(got, "approved request outline") {
		t.Error("the outline section rendered even though there was no outline")
	}
	if !strings.Contains(got, "Repository context") {
		t.Error("the rest of the prompt did not render")
	}
}

func TestUnknownStageIsRejected(t *testing.T) {
	if _, err := Render("apply", sample()); err == nil {
		t.Fatal("Render returned a prompt for the apply stage, which calls no model")
	}
}

func TestSystemPromptDemandsBareJSON(t *testing.T) {
	got, err := System()
	if err != nil {
		t.Fatalf("System: %v", err)
	}
	for _, want := range []string{"one JSON object", "Never invent file paths"} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt is missing %q", want)
		}
	}
}

func TestGuidesAreEmbedded(t *testing.T) {
	g, err := BakedGuides()
	if err != nil {
		t.Fatalf("BakedGuides: %v", err)
	}
	if g.WorkingRoot == "" || g.Invariants == "" || g.Stacks == "" {
		t.Fatal("one of the baked guides is empty")
	}
}
