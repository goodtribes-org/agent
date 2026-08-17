Write a full implementation plan for this issue. The plan is handed verbatim
to an autonomous coding agent, which will implement it without asking anyone
anything. Anything you leave vague, it will guess.

# Company context

{{ .Guides.WorkingRoot }}

# Security invariants

{{ .Guides.Invariants }}

# Stacks and build gates

{{ .Guides.Stacks }}

# The issue

Repository: {{ .Repo }}
Issue: #{{ .Number }} — {{ .Title }}
Phase: {{ .Phase }}   Track: {{ .Track }}   Size: {{ .Size }}

{{ .Body }}

{{ if .Outline }}# The approved request outline

This outline was approved by a human. Expand it — do not contradict it.

{{ .Outline }}
{{ end }}
# Repository context (read from the live repository)

{{ .RepoContext }}

# What to produce

Every step names one file and says what changes in it and why. Put the steps
in dependency order: migrations → store → handlers → frontend. Under each step
give the specifics the implementer cannot guess — function names, route paths,
migration numbers, config directives, struct fields.

Every acceptance criterion in the issue must be traceable to a numbered step.
If the issue has no explicit criteria, derive them from what it asks for.

Keep it to at most twelve steps. `change` is one sentence. Each entry in
`details` is a fragment — a function name, a route, a field, a directive — not
a sentence and never a code block. Write what the implementer cannot infer from
the repository it is standing in, and nothing it can: it has the code, it does
not need the code repeated back.

A card that genuinely needs more than twelve steps is too big to hand to an
agent in one piece. Set `blocked` to true and say where it should be split.
That is a useful answer; a plan that runs past the token budget is not an
answer at all, because it arrives truncated and gets thrown away.

Verification must be the real build gate for this repository, plus what a
passing run looks like and at least one edge case worth checking by hand.

Only name files that appear in the repository context above, unless the step
is explicitly creating a new file — in which case say "new file" in the change
description.

If the context you were given is not enough to write a plan an agent could
follow without guessing, set `blocked` to true and explain precisely what is
missing. A blocked plan is far better than a confident wrong one.

# Response schema

Return exactly this shape:

{
  "blocked": false,
  "blocked_reason": "",
  "sub_project": "<repository short name>",
  "scope_label": "Small",
  "scope_files": 3,
  "background": "<two to four sentences>",
  "steps": [
    {
      "file": "<path relative to the repository root>",
      "change": "<what changes and why, one line>",
      "details": ["<function name, route path, migration number, directive>"]
    }
  ],
  "code_notes": ["<anything the implementer must know that is not a step>"],
  "acceptance_criteria": [{ "criterion": "<from the issue>", "step": 1 }],
  "verification": ["<build gate command>", "<expected output>", "<edge case>"]
}

`scope_label` is "Small" or "Medium". `step` in `acceptance_criteria` is a
1-based index into `steps`. When `blocked` is true, `blocked_reason` is
required and the remaining fields may be empty.
