// Package verdict holds the JSON contracts the models must satisfy, and the
// validation that decides whether a reply is usable.
//
// Validation lives in Go rather than in the prompt on purpose: a prompt is a
// request, not a guarantee. Everything a stage acts on — whether to reject an
// issue, which files a plan touches — is checked here before it is believed.
package verdict

import (
	"errors"
	"fmt"
	"strings"
)

// RequestVerdict is what the request stage asks for: an assessment of the
// issue plus the material for the outline comment.
type RequestVerdict struct {
	SubProject string `json:"sub_project"`

	Scope struct {
		TooLarge        bool     `json:"too_large"`
		EstimatedFiles  int      `json:"estimated_files"`
		Label           string   `json:"label"` // "Small" | "Medium"
		Reasons         []string `json:"reasons"`
		SplitSuggestion []string `json:"split_suggestion"`
	} `json:"scope"`

	SensitiveData struct {
		Found  bool   `json:"found"`
		Reason string `json:"reason"`
	} `json:"sensitive_data"`

	StackCheck struct {
		Passes bool     `json:"passes"`
		Flags  []string `json:"flags"`
	} `json:"stack_check"`

	InvariantCheck struct {
		Passes    bool     `json:"passes"`
		Conflicts []string `json:"conflicts"`
	} `json:"invariant_check"`

	Context string       `json:"context"`
	Steps   []string     `json:"steps"`
	Files   []FileChange `json:"files"`
	Testing []string     `json:"testing"`
}

// FileChange is one file an outline expects to touch.
type FileChange struct {
	Path string `json:"path"`
	Why  string `json:"why"`
}

// Validate checks a request verdict is internally consistent and usable.
//
// actualRepo is the repository the issue really lives in. The model does not
// get to reassign work to another repository, so a mismatch is corrected here
// rather than trusted.
func (v *RequestVerdict) Validate(actualRepo string) error {
	var errs []error

	if !strings.EqualFold(strings.TrimSpace(v.SubProject), actualRepo) {
		v.SubProject = actualRepo
	}

	if v.Scope.TooLarge {
		// A rejection stops the card and costs a human's attention. It has to
		// come with reasons and a way forward, or it is not actionable.
		if len(nonEmpty(v.Scope.Reasons)) == 0 {
			errs = append(errs, errors.New("scope.too_large is true but scope.reasons is empty"))
		}
		if len(nonEmpty(v.Scope.SplitSuggestion)) == 0 {
			errs = append(errs, errors.New("scope.too_large is true but scope.split_suggestion is empty"))
		}
		return errors.Join(errs...)
	}

	switch strings.ToLower(strings.TrimSpace(v.Scope.Label)) {
	case "small", "medium":
	case "":
		errs = append(errs, errors.New("scope.label is empty"))
	default:
		errs = append(errs, fmt.Errorf("scope.label must be Small or Medium, got %q", v.Scope.Label))
	}

	if strings.TrimSpace(v.Context) == "" {
		errs = append(errs, errors.New("context is empty"))
	}
	if len(nonEmpty(v.Steps)) == 0 {
		errs = append(errs, errors.New("steps is empty"))
	}
	if len(v.Files) == 0 {
		errs = append(errs, errors.New("files is empty"))
	}
	for i, f := range v.Files {
		if strings.TrimSpace(f.Path) == "" {
			errs = append(errs, fmt.Errorf("files[%d] has no path", i))
		}
	}
	if len(nonEmpty(v.Testing)) == 0 {
		errs = append(errs, errors.New("testing is empty"))
	}

	if v.SensitiveData.Found && strings.TrimSpace(v.SensitiveData.Reason) == "" {
		errs = append(errs, errors.New("sensitive_data.found is true but no reason was given"))
	}
	if !v.StackCheck.Passes && len(nonEmpty(v.StackCheck.Flags)) == 0 {
		errs = append(errs, errors.New("stack_check.passes is false but no flags were given"))
	}
	if !v.InvariantCheck.Passes && len(nonEmpty(v.InvariantCheck.Conflicts)) == 0 {
		errs = append(errs, errors.New("invariant_check.passes is false but no conflicts were given"))
	}

	return errors.Join(errs...)
}

// PlanVerdict is what the plan stage asks for: the implementation plan an
// autonomous agent will execute without asking anyone anything.
type PlanVerdict struct {
	Blocked       bool   `json:"blocked"`
	BlockedReason string `json:"blocked_reason"`

	SubProject   string     `json:"sub_project"`
	ScopeLabel   string     `json:"scope_label"`
	ScopeFiles   int        `json:"scope_files"`
	Background   string     `json:"background"`
	Steps        []PlanStep `json:"steps"`
	CodeNotes    []string   `json:"code_notes"`
	Coverage     []Coverage `json:"acceptance_criteria"`
	Verification []string   `json:"verification"`
}

// PlanStep is one file-level change.
type PlanStep struct {
	File    string   `json:"file"`
	Change  string   `json:"change"`
	Details []string `json:"details"`
}

// Coverage ties an acceptance criterion to the step that satisfies it.
type Coverage struct {
	Criterion string `json:"criterion"`
	Step      int    `json:"step"` // 1-based index into Steps
}

// Validate checks a plan verdict.
func (v *PlanVerdict) Validate(actualRepo string) error {
	var errs []error

	if !strings.EqualFold(strings.TrimSpace(v.SubProject), actualRepo) {
		v.SubProject = actualRepo
	}

	if v.Blocked {
		if strings.TrimSpace(v.BlockedReason) == "" {
			errs = append(errs, errors.New("blocked is true but blocked_reason is empty"))
		}
		return errors.Join(errs...)
	}

	if strings.TrimSpace(v.Background) == "" {
		errs = append(errs, errors.New("background is empty"))
	}
	if len(v.Steps) == 0 {
		errs = append(errs, errors.New("steps is empty"))
	}
	for i, s := range v.Steps {
		if strings.TrimSpace(s.File) == "" {
			errs = append(errs, fmt.Errorf("steps[%d] has no file", i))
		}
		if strings.TrimSpace(s.Change) == "" {
			errs = append(errs, fmt.Errorf("steps[%d] has no change description", i))
		}
	}
	for i, c := range v.Coverage {
		if c.Step < 1 || c.Step > len(v.Steps) {
			errs = append(errs, fmt.Errorf("acceptance_criteria[%d] points at step %d, which does not exist",
				i, c.Step))
		}
	}
	if len(nonEmpty(v.Verification)) == 0 {
		errs = append(errs, errors.New("verification is empty"))
	}

	return errors.Join(errs...)
}

// DropUnknownFiles removes steps naming a file that is neither in the
// repository nor described as a new file, and returns what it dropped.
//
// This is the closest available substitute for gh-plan's rule that the planner
// must list a directory before naming a path in it. A model working from a
// capped tree will occasionally invent a plausible path, and a plan that sends
// an autonomous agent to edit a file that does not exist is worse than a
// shorter plan.
func (v *PlanVerdict) DropUnknownFiles(inTree func(string) bool) []string {
	if inTree == nil {
		return nil
	}
	var (
		kept    []PlanStep
		dropped []string
		remap   = make(map[int]int, len(v.Steps))
	)
	for i, s := range v.Steps {
		path := strings.TrimPrefix(strings.TrimSpace(s.File), "./")
		if inTree(path) || describesNewFile(s) {
			remap[i+1] = len(kept) + 1
			kept = append(kept, s)
			continue
		}
		dropped = append(dropped, path)
	}
	if len(dropped) == 0 {
		return nil
	}
	v.Steps = kept

	// Coverage indexes are 1-based positions in Steps, so they have to follow
	// the renumbering. A criterion whose step is gone is dropped with it.
	var cov []Coverage
	for _, c := range v.Coverage {
		if n, ok := remap[c.Step]; ok {
			c.Step = n
			cov = append(cov, c)
		}
	}
	v.Coverage = cov
	return dropped
}

// describesNewFile reports whether a step says outright that it creates the
// file. The prompt asks for the words "new file"; anything else is treated as
// a claim about an existing path.
func describesNewFile(s PlanStep) bool {
	hay := strings.ToLower(s.Change + " " + strings.Join(s.Details, " "))
	for _, needle := range []string{"new file", "create ", "creates ", "add a new", "brand new"} {
		if strings.Contains(hay, needle) {
			return true
		}
	}
	return false
}

func nonEmpty(in []string) []string {
	var out []string
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}
