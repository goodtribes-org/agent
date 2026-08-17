package render

import (
	"fmt"
	"strings"

	"github.com/goodtribes-org/agent/organizzer/internal/board"
	"github.com/goodtribes-org/agent/organizzer/internal/verdict"
)

// Outline renders the request-stage comment.
func Outline(v verdict.RequestVerdict) string {
	var b strings.Builder

	b.WriteString(HeadingOutline + "\n\n")
	fmt.Fprintf(&b, "**Sub-project:** %s\n", v.SubProject)
	fmt.Fprintf(&b, "**Scope:** %s — ~%d files\n", label(v.Scope.Label), v.Scope.EstimatedFiles)

	// Sensitive data, stack and invariant findings are reported, never acted
	// on. The human decides at the request → plan gate; that gate is the whole
	// reason this comment exists.
	if v.SensitiveData.Found {
		fmt.Fprintf(&b, "**Sensitive data:** Yes — %s — implementer must document storage and access control before this proceeds\n",
			oneLine(v.SensitiveData.Reason))
	} else {
		b.WriteString("**Sensitive data:** None\n")
	}

	if v.StackCheck.Passes {
		b.WriteString("**Stack check:** Passes\n")
	} else {
		b.WriteString("**Stack check:** Flags:\n")
		for _, f := range v.StackCheck.Flags {
			fmt.Fprintf(&b, "  - %s\n", oneLine(f))
		}
	}

	if v.InvariantCheck.Passes {
		b.WriteString("**Invariant check:** Passes\n")
	} else {
		b.WriteString("**Invariant check:** Conflicts:\n")
		for _, c := range v.InvariantCheck.Conflicts {
			fmt.Fprintf(&b, "  - %s\n", oneLine(c))
		}
	}

	b.WriteString("\n### Context\n")
	b.WriteString(strings.TrimSpace(v.Context) + "\n")

	b.WriteString("\n### Steps\n")
	n := 0
	for _, s := range v.Steps {
		if strings.TrimSpace(s) == "" {
			continue
		}
		n++
		fmt.Fprintf(&b, "%d. %s\n", n, oneLine(s))
	}

	b.WriteString("\n### Files to change\n")
	for _, f := range v.Files {
		if strings.TrimSpace(f.Path) == "" {
			continue
		}
		fmt.Fprintf(&b, "- `%s` — %s\n", strings.TrimSpace(f.Path), oneLine(f.Why))
	}

	b.WriteString("\n### Testing\n")
	for _, t := range v.Testing {
		if strings.TrimSpace(t) == "" {
			continue
		}
		fmt.Fprintf(&b, "- %s\n", oneLine(t))
	}

	b.WriteString("\n---\n")
	b.WriteString(SentinelOutline + "\n")
	return b.String()
}

// Plan renders the plan-stage comment. This is the text handed verbatim to the
// implementation agent, so everything it needs has to survive into it.
func Plan(issue board.Issue, subProject string, v verdict.PlanVerdict) string {
	var b strings.Builder

	b.WriteString(HeadingPlan + "\n\n")
	fmt.Fprintf(&b, "**Issue:** #%d — %s\n", issue.Number, oneLine(issue.Title))
	fmt.Fprintf(&b, "**Sub-project:** %s\n", subProject)
	fmt.Fprintf(&b, "**Estimated scope:** %s (~%d files)\n", label(v.ScopeLabel), v.ScopeFiles)

	b.WriteString("\n### Background\n")
	b.WriteString(strings.TrimSpace(v.Background) + "\n")

	b.WriteString("\n### Implementation steps\n")
	for i, s := range v.Steps {
		fmt.Fprintf(&b, "%d. **%s** — %s\n", i+1, strings.TrimSpace(s.File), oneLine(s.Change))
		for _, d := range s.Details {
			if strings.TrimSpace(d) == "" {
				continue
			}
			fmt.Fprintf(&b, "   - %s\n", oneLine(d))
		}
	}

	if notes := nonEmpty(v.CodeNotes); len(notes) > 0 {
		b.WriteString("\n### Code notes\n")
		for _, n := range notes {
			fmt.Fprintf(&b, "- %s\n", oneLine(n))
		}
	}

	if len(v.Coverage) > 0 {
		b.WriteString("\n### Acceptance criteria coverage\n")
		for _, c := range v.Coverage {
			fmt.Fprintf(&b, "- [ ] %s → step %d\n", oneLine(c.Criterion), c.Step)
		}
	}

	b.WriteString("\n### Verification\n")
	n := 0
	for _, s := range v.Verification {
		if strings.TrimSpace(s) == "" {
			continue
		}
		n++
		fmt.Fprintf(&b, "%d. %s\n", n, oneLine(s))
	}

	b.WriteString("\n---\n")
	b.WriteString(SentinelPlan + "\n")
	return b.String()
}

// Handoff renders the comment posted once work has been dispatched to the
// implementation agent.
//
// It says plainly that no pull request exists yet. The card moves to `test` on
// the webhook's acknowledgement, which is well before the agent has done
// anything, and someone reading the board needs to know that.
func Handoff(taskName, namespace, repo, branch, agent string, skills []string) string {
	var b strings.Builder
	b.WriteString("## Implementation dispatched\n\n")
	b.WriteString("The approved plan has been handed to the implementation agent.\n\n")
	fmt.Fprintf(&b, "**Task:** `%s`", taskName)
	if namespace != "" {
		fmt.Fprintf(&b, " (namespace `%s`)", namespace)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "**Repository:** `%s`\n", repo)
	fmt.Fprintf(&b, "**Base branch:** `%s`\n", branch)
	fmt.Fprintf(&b, "**Agent:** `%s`", agent)
	if len(skills) > 0 {
		fmt.Fprintf(&b, " with skills `%s`", strings.Join(skills, ", "))
	}
	b.WriteString("\n\n")
	b.WriteString("The agent opens the pull request itself when it finishes. **No pull request " +
		"exists yet** — this card being in `test` means the work was dispatched, not that it is done. " +
		"If no pull request appears, check the task:\n\n")
	fmt.Fprintf(&b, "```\nkubectl -n %s get st %s -o yaml\n```\n\n", nonBlank(namespace, "kubefoundry"), taskName)
	b.WriteString("---\n")
	b.WriteString(SentinelHandoff + "\n")
	return b.String()
}

// RejectTooLarge renders the comment for an issue the model judged too big to
// take on as one card.
func RejectTooLarge(v verdict.RequestVerdict, needsHumanLabel string) string {
	var b strings.Builder
	b.WriteString("## Too large for one issue\n\n")
	b.WriteString("This issue was not turned into an outline because it is bigger than one card should be.\n\n")
	b.WriteString("**Why:**\n")
	for _, r := range nonEmpty(v.Scope.Reasons) {
		fmt.Fprintf(&b, "- %s\n", oneLine(r))
	}
	b.WriteString("\n**Suggested split:**\n")
	for _, s := range nonEmpty(v.Scope.SplitSuggestion) {
		fmt.Fprintf(&b, "- %s\n", oneLine(s))
	}
	fmt.Fprintf(&b, "\nThe card stays in `new` and carries the `%s` label. "+
		"Split it, or remove the label to have it reassessed as it stands.\n", needsHumanLabel)
	return b.String()
}

// RejectUnknownRepo renders the comment for a card pointing at a repository
// this worker is not allowed to act on.
func RejectUnknownRepo(repo string, allowed []string, needsHumanLabel string) string {
	return fmt.Sprintf(`## Unknown sub-project

This card belongs to `+"`%s`"+`, which is not one of the repositories this worker handles.

Repositories it handles: %s

Move the issue to the right repository, or add this one to `+"`ALLOWED_REPOS`"+`. The card
carries the `+"`%s`"+` label until then.
`, repo, "`"+strings.Join(allowed, "`, `")+"`", needsHumanLabel)
}

// BlockerOpen renders the comment for a card dragged past its blockers.
func BlockerOpen(open []string, returnedTo string) string {
	return fmt.Sprintf(`## Still blocked

This card cannot be worked yet — the following blockers are still open:

%s

It has been moved back to `+"`%s`"+`. Close the blockers, or clear them from the
**Blocked by** field if they no longer apply.
`, bullets(open), returnedTo)
}

// NoPlan renders the comment for a card in `apply` with no approved plan.
func NoPlan(returnedTo string) string {
	return fmt.Sprintf(`## No implementation plan found

This card is in `+"`apply`"+` but no comment on it carries the plan sentinel, so there is
nothing to hand to the implementation agent.

Move it back through `+"`plan`"+` to have one written. The card has been returned to
`+"`%s`"+`.
`, returnedTo)
}

// PlanBlocked renders the comment for a plan the model declined to write.
func PlanBlocked(reason, returnedTo, needsHumanLabel string) string {
	return fmt.Sprintf(`## Cannot write a plan

No implementation plan was written, because the issue does not yet contain enough
to write one an agent could follow without guessing.

**What is missing:**

%s

The card has been returned to `+"`%s`"+` and carries the `+"`%s`"+` label. Fill in the
missing detail on the issue and clear the label.
`, oneLine(reason), returnedTo, needsHumanLabel)
}

// PlanTooLong renders the comment for a plan that ran past the token budget.
//
// This is a decision, not a hiccup. The model stopped mid-object because it ran
// out of room, and asking again produces the same overlong answer — so the card
// is handed back with the needs-human label rather than retried until someone
// notices the loop.
func PlanTooLong(maxTokens int, returnedTo, needsHumanLabel string) string {
	return fmt.Sprintf(`## The plan did not fit

The model was still writing when it reached its %d-token limit, so the plan
arrived truncated and was discarded rather than posted half-written.

Retrying produces the same answer. What usually helps:

- **Split the issue.** A card whose plan runs past the budget is normally more
  than one card. Two smaller issues each get a plan an agent can follow.
- **Narrow the scope on the issue itself.** The plan expands whatever the
  approved outline asks for, so trimming the outline trims the plan.

The card has been returned to `+"`%s`"+` and carries the `+"`%s`"+` label. Clear the
label once the issue has been split or narrowed.
`, maxTokens, returnedTo, needsHumanLabel)
}

// DispatchFailed renders the comment for a failed hand-off to the agent.
func DispatchFailed(err string, returnedTo, needsHumanLabel string) string {
	return fmt.Sprintf(`## Could not dispatch the implementation

Handing this plan to the implementation agent failed:

`+"```"+`
%s
`+"```"+`

**No agent was started and no pull request will appear.** The card has been returned
to `+"`%s`"+` and carries the `+"`%s`"+` label. Clear the label once the cause is fixed.
`, strings.TrimSpace(err), returnedTo, needsHumanLabel)
}

func bullets(in []string) string {
	var b strings.Builder
	for _, s := range in {
		fmt.Fprintf(&b, "- %s\n", oneLine(s))
	}
	return strings.TrimRight(b.String(), "\n")
}

// oneLine collapses newlines. Every one of these values lands inside a bullet
// or a bold field, where a stray newline would break the markdown.
func oneLine(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\r\n", "\n")
	return strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
}

// label normalises the model's scope word to the two the format allows.
func label(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "small":
		return "Small"
	case "medium":
		return "Medium"
	default:
		return "Medium"
	}
}

func nonBlank(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
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
