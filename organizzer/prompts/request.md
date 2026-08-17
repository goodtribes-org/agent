Assess this issue and produce a request outline.

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

# Repository context (read from the live repository)

{{ .RepoContext }}

# What to decide

**Scope.** The work is too large when it touches a whole subsystem, needs more
than ten files changed, introduces three or more new major components, or
spans more than one milestone phase. A `Size: L` card is *not* by itself too
large. When it is too large, you must give at least one reason and at least
one concrete way to split it into smaller issues.

**Sensitive data.** Report whether the work touches passwords or password
hashing, payment card data, the PII triple (name + email + address), health
data, government identifiers, or anything that changes `internal/store` access
filtering or the API's 404-vs-403 behaviour. Reporting it does not block the
issue — a human decides.

**Stack.** Flag anything that would add a database engine, a cache, a message
queue or a significant external API that the repository does not already use.
NATS already exists in postfix-client, so using it is not a flag.

**Invariants.** Flag any conflict with the security invariants above. These
are reported, not auto-rejected.

**Steps and files.** Give file-level steps in dependency order and name the
files each step touches, using paths relative to the repository root.

**Testing.** Give the concrete command that proves the change works. Use the
build gate for this repository from the table above.

# Response schema

Return exactly this shape:

{
  "sub_project": "<repository short name>",
  "scope": {
    "too_large": false,
    "estimated_files": 3,
    "label": "Small",
    "reasons": [],
    "split_suggestion": []
  },
  "sensitive_data": { "found": false, "reason": "" },
  "stack_check":    { "passes": true,  "flags": [] },
  "invariant_check":{ "passes": true,  "conflicts": [] },
  "context": "<one to three sentences of background>",
  "steps": ["<file-level step, exact path and what changes>"],
  "files": [{ "path": "<path/to/file>", "why": "<why it changes>" }],
  "testing": ["<concrete command>", "<what a passing run looks like>"]
}

`label` is "Small" or "Medium". When `too_large` is true, `reasons` and
`split_suggestion` must each hold at least one entry and the other fields may
be brief. When `too_large` is false, `steps`, `files` and `testing` must each
hold at least one entry.
