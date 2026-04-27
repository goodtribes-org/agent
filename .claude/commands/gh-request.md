# GitHub Request Planner

Find the next issue in 'new' status, move it to 'request', analyse the scope, and post an outline plan. Runs continuously — polls every 5 minutes when the queue is empty.

## Usage

```
/gh-request
```

Run from within the monorepo. The skill detects the project board and repo automatically.

## Status flow context

```
new → request → plan → review → apply → test
 ↑
Start here
```

## Workflow

Steps 0–1 run **once** at startup. Steps 2–14 repeat in a loop until interrupted.

### 0. Detect repo and owner

```bash
git remote get-url origin
```

Parse the remote URL to extract `owner` and repo short name. Handle both SSH (`git@github.com:owner/repo.git`) and HTTPS formats. Strip `.git`.

If there is no git remote — report "Not inside a git repository with a GitHub remote" and stop entirely.

### 1. Find all project boards

```bash
gh project list --owner <owner> --format json --limit 50
```

Collect ALL projects returned. Note each project's `number` and `id` (node ID starting with `PVT_`). There is no title filter — all org projects are processed.

If no projects found — report "No GitHub Project boards found under @<owner>" and stop entirely.

---

### ↻ LOOP — repeat from here after each issue or after sleeping

### 2. List items in 'new' status across all boards

For **each** project number collected in step 1, run:

```bash
gh project item-list <number> --owner <owner> --format json --limit 100
```

Collect all items where `status` equals `new` (case-insensitive) across all boards. Pick the first item with no assignee, or the first item overall.

Note the item's:
- `id` — project item node ID (starts with `PVTI_`)
- `content.number` — linked issue number
- `content.repository` — repo the issue lives in (e.g. `goodtribes-org/kickfix`)
- `projectNumber` and `projectNodeId` — the board this item came from (needed for step 5)

**If no items in 'new' status on any board:**

```bash
echo "No new issues — sleeping 5 minutes..." && sleep 300
```

Then go back to step 2 (do not re-run steps 0–1).

### 3. Read the issue

```bash
gh issue view <issueNumber> --repo <issueRepo> --json title,body,labels,assignees,number,url
```

Display the issue:

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 ISSUE #<number>: <title>
 Repo:      <issueRepo>
 URL:       <url>
 Labels:    <labels or "none">
 Assignees: <assignees or "none">
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

<body>
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

### 4. Discover Status field IDs

```bash
gh project field-list <number> --owner <owner> --format json
```

Find the field named `Status`. Note its `id` as `<statusFieldId>`. From its `options` array note:
- `new` option `id` → `<newOptionId>`
- `request` option `id` → `<requestOptionId>`

Match option names case-insensitively.

### 5. Move card to 'request'

```bash
gh project item-edit \
  --id <itemNodeId> \
  --field-id <statusFieldId> \
  --project-id <projectNodeId> \
  --single-select-option-id <requestOptionId>
```

If this fails — report the error and stop.

Report: "Issue #<number> claimed — card moved to 'request'."

### 6. Identify and validate the sub-project

Analyze the issue title and body to determine which sub-project it targets. The only valid values are exactly:
- `kickfix`
- `asylguiden.se`
- `goodtribes.org`

Then verify the directory actually exists in the monorepo:

```bash
ls /home/mattias/projects/goodtribes.org/<candidate>/
```

If the directory is missing — the label is invalid regardless of what the issue says.

**If the sub-project cannot be determined with confidence, OR the directory does not exist:**

Write the rejection to a temp file and post it:

```bash
cat > /tmp/gh-reject.md << 'EOF'
## Missing project label

This issue could not be linked to a known sub-project. Before planning can begin, the issue must be labelled with exactly one of:

- `kickfix`
- `asylguiden.se`
- `goodtribes.org`

*Card returned to new.*
EOF
gh issue comment <issueNumber> --repo <issueRepo> --body-file /tmp/gh-reject.md
rm /tmp/gh-reject.md
```

Move card back to `new`:

```bash
gh project item-edit \
  --id <itemNodeId> \
  --field-id <statusFieldId> \
  --project-id <projectNodeId> \
  --single-select-option-id <newOptionId>
```

Report: "Could not identify sub-project — card returned to new." and stop.

**If confirmed:** note `<subProject>` as the validated label and continue.

### 7. Tag the issue with labels

```bash
gh label create "<subProject>" --repo <issueRepo> --color "0075ca" --force
gh label create "request" --repo <issueRepo> --color "e4e669" --force
gh issue edit <issueNumber> --repo <issueRepo> --add-label "<subProject>,request"
```

Do not stop if label creation fails — report a warning and continue.

### 8. Read codebase context

Read the root CLAUDE.md for overall stack context:

```bash
cat /home/mattias/projects/goodtribes.org/CLAUDE.md
```

Map the sub-project label to its local path and read its context:

| Label | Local path |
|---|---|
| `kickfix` | `kickfix/` |
| `asylguiden.se` | `asylguiden.se/` |
| `goodtribes.org` | `goodtribes.org/` |

```bash
cat /home/mattias/projects/goodtribes.org/<subProjectPath>/CLAUDE.md 2>/dev/null
cat /home/mattias/projects/goodtribes.org/<subProjectPath>/package.json 2>/dev/null
cat /home/mattias/projects/goodtribes.org/<subProjectPath>/docker-compose.yml 2>/dev/null
```

Note all existing services (databases, caches, queues, external APIs) and libraries.

### 9. Scope check

Flag as TOO LARGE if any of the following apply:
- Describes an entire system: chat, auth from scratch, complete CMS, full payment pipeline, entire blog
- Estimated changes touch more than 10 files
- Requires 3 or more major new components simultaneously
- Describes multiple phases or weeks of work

**If too large:**

```bash
python3 -c "
import subprocess
body = '''## Scope Review: Too Large for a Single Issue

This issue describes work that is too large to plan and implement as one focused change.

**Why:** Estimated scope exceeds 10 files or 3 major new components — too much for a single PR.

**Action needed:** Break this into smaller issues, each representing one deployable unit of change.

*Card returned to new.*'''
subprocess.run(['gh', 'issue', 'comment', '<issueNumber>', '--repo', '<issueRepo>', '--body', body])
"
```

Move card back to `new`:

```bash
gh project item-edit \
  --id <itemNodeId> \
  --field-id <statusFieldId> \
  --project-id <projectNodeId> \
  --single-select-option-id <newOptionId>
```

Remove the `request` label:

```bash
gh issue edit <issueNumber> --repo <issueRepo> --remove-label "request"
```

Report: "Issue #<number> is too large — comment posted, card returned to new." and stop.

### 10. Sensitive data check

Scan the issue body for:
- Passwords or password hashing
- Payment card or bank data (PCI scope)
- PII combination: full name AND email AND physical address together
- Health or medical data
- Government IDs (personal number, passport, national ID)

If found, set `sensitiveDataFlag = true` and note `sensitiveDataReason`.

### 11. Stack consistency check

Flag if the request would require any of the following not already present in the sub-project:
- A new database engine
- A new cache layer (e.g. Redis)
- A new message queue
- A new significant external API

For each flag, suggest using an existing service instead. Flags are warnings, not blockers.

### 12. Write the outline plan

```markdown
## Request Outline

**Scope:** [Small — ~N files / Medium — ~N files]
**Sensitive data:** [None / Yes — <reason> — implementer must document storage and encryption before this proceeds]
**Stack check:** [Passes / Flags: <one per line>]

### Context
<1–3 sentences: what problem this solves and why it matters>

### Steps
1. <File-level step — name the exact path and what changes>
2. <Next step in dependency order>

### Files to change
- `<path/to/file>` — <why>

### Testing
<Concrete steps: command to run, URL to visit, expected result>

---
*Outline written by /gh-request — move card to 'plan' to approve and trigger detailed planning.*
```

Each step must name a specific file or command. Do not introduce services or libraries not already present.

### 13. Post the outline as a comment

Write the outline to a temp file and post it with `--body-file` to avoid shell-quoting issues:

```bash
cat > /tmp/gh-outline.md << 'EOF'
<outline_text>
EOF
gh issue comment <issueNumber> --repo <issueRepo> --body-file /tmp/gh-outline.md
rm /tmp/gh-outline.md
```

If the comment command fails — print the outline to the terminal and report the error. Do not move the card.

### 14. Report and loop

```
Outline posted to issue #<issueNumber>.
Card is at 'request' — move it to 'plan' on the project board to approve.

Sub-project:    <name>
Scope:          <Small/Medium — N files>
Sensitive data: <None / Yes — reason>
Stack check:    <Passes / Flags: ...>
```

Go back to step 2 immediately to check for the next issue.

---

## Notes

- The card stays at `request` after the outline is posted. The human moves it to `plan` to approve.
- Do not move the card to `plan` automatically — that is a human decision.
- Label colors: use `--force` on `gh label create` so it is idempotent.
- Board status flow: `new → request → plan → review → apply → test`.
- Processes ALL project boards under the org (goodtribes.org #2, kickfix #3, asylguiden.se #4). No title filter. Field IDs are discovered dynamically per board in step 4.
