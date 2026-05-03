# GitHub Plan Writer

Find an issue in the 'plan' stage, read the actual codebase, write a detailed implementation plan, and move the card to 'review'. Runs continuously — polls every 5 minutes when the queue is empty.

## Usage

```
/gh-plan
```

Run from the monorepo root (`~/projects/goodtribes.org`). The skill detects the project board and target sub-project automatically.

## Status flow context

```
new → request → plan → review → apply → test
                  ↑
             You are here
```

Issues arrive in 'plan' after a human approves the outline written by `/gh-request`. This skill writes the full file-level implementation plan and moves the card to 'review' for final human sign-off before implementation begins.

## Workflow

Step 0 runs **once** at startup. Steps 1–11 repeat in a loop until interrupted.

### 0. Find all project boards

```bash
gh project list --owner goodtribes-org --format json --limit 50
```

Collect ALL projects returned. Note each project's `number` and `id` (node ID starting with `PVT_`). There is no title filter — all org projects are processed.

If no projects found — report "No GitHub Project boards found" and stop entirely.

---

### ↻ LOOP — repeat from here after each issue or after sleeping

### 1. List items in 'plan' status across all boards

For **each** project number collected in step 0, run:

```bash
gh project item-list <number> --owner goodtribes-org --format json --limit 100
```

Collect all items where `status` equals `plan` (case-insensitive) across all boards.

For each candidate item (prefer unassigned first), fetch its issue labels:

```bash
gh issue view <content.number> --repo <content.repository> --json labels --jq '.labels[].name'
```

Skip the item if any label name starts with `picked-by-`. Pick the first item that passes this check.

Note the item's:
- `id` — project item node ID (starts with `PVTI_`)
- `content.number` — linked issue number
- `content.repository` — the repo the issue lives in (e.g. `goodtribes-org/kickfix`)
- `projectNumber` and `projectNodeId` — the board this item came from (needed for step 9)

**If no items in 'plan' status on any board, OR all candidates have a `picked-by-*` label:**

```bash
echo "No issues in plan — sleeping 5 minutes..." && sleep 300
```

Then go back to step 1 (do not re-run step 0).

### 1.5. Claim the issue with a picked-by label

Get the local hostname and immediately label the issue to prevent other agents from picking it up:

```bash
PICKED_LABEL="picked-by-$(hostname)"
gh label create "$PICKED_LABEL" --repo <issueRepo> --color "f9a825" --force
gh issue edit <issueNumber> --repo <issueRepo> --add-label "$PICKED_LABEL"
```

Report: "Claimed issue #<issueNumber> with label `$PICKED_LABEL`."

Store `$PICKED_LABEL` — you will need it when removing the label in step 10.

### 2. Claim the item — move to 'plan' (keep status, set assignee context)

Look up the Status field and option IDs:

```bash
gh project field-list <number> --owner goodtribes-org --format json
```

Note:
- Status field `id` → `<statusFieldId>` (starts with `PVTSSF_`)
- Option `review` → `<reviewOptionId>`

The card stays at `plan` while you work. You will move it to `review` only after the plan comment is posted successfully.

### 3. Read the issue

```bash
gh issue view <issueNumber> --repo <issueRepo> --json title,body,labels,number,url,comments
```

Display the issue:

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 ISSUE #<number>: <title>
 Repo: <issueRepo>
 URL:  <url>
 Labels: <labels>
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

<body>

PREVIOUS COMMENTS:
<comments — include the gh-request outline if present>
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

Read both the original request (`body`) and any existing plan outline posted by `/gh-request` (in `comments`). The outline is the starting point — the goal here is to expand it into a precise, file-level plan.

### 4. Validate the sub-project label

Check the issue labels for exactly one of these values:
- `kickfix`
- `asylguiden.se`
- `goodtribes.org`

Then verify the directory exists in the monorepo:

```bash
ls /home/mattias/projects/goodtribes.org/<label>/
```

**If no valid label is present, OR the directory does not exist — reject the issue:**

Write the rejection comment to a temp file and post it:

```bash
cat > /tmp/gh-reject.md << 'EOF'
## Missing or invalid project label

This issue cannot be planned because it has no valid sub-project label. `/gh-plan` only processes issues labelled with an existing project:

- `kickfix`
- `asylguiden.se`
- `goodtribes.org`

Please add the correct label and move the card back to **plan** when ready.

*Card returned to request.*
EOF
gh issue comment <issueNumber> --repo <issueRepo> --body-file /tmp/gh-reject.md
rm /tmp/gh-reject.md
```

Move card back to `request`:

```bash
gh project item-edit \
  --id <itemNodeId> \
  --field-id <statusFieldId> \
  --project-id <projectNodeId> \
  --single-select-option-id <requestOptionId>
```

Look up `requestOptionId` from `gh project field-list` if not already known — it is the Status option named `request`.

Update labels:

```bash
gh issue edit <issueNumber> --repo <issueRepo> --remove-label "plan" --add-label "request"
```

Report: "Issue #<issueNumber> has no valid sub-project label — rejected back to request." and stop.

**If valid:** note `<subProject>` and `<localPath>` and continue.

### 5. Identify the target sub-project path

Map the validated label to its local source path:

| Label | Local path |
|---|---|
| `kickfix` | `kickfix/` |
| `asylguiden.se` | `asylguiden.se/` |
| `goodtribes.org` | `goodtribes.org/` |

Report: "Target sub-project: **<subProject>** at `<localPath>`"

### 6. Read the codebase

Read the sub-project's documentation and structure first:

```bash
cat /home/mattias/projects/goodtribes.org/<localPath>/CLAUDE.md 2>/dev/null
cat /home/mattias/projects/goodtribes.org/CLAUDE.md
```

Then explore the relevant source files. Use the issue body and the `gh-request` outline to guide which files to read. Typical reads:

**For a backend change:**
```bash
# Understand routing structure
ls /home/mattias/projects/goodtribes.org/<localPath>/backend/
# Read relevant route files
cat /home/mattias/projects/goodtribes.org/<localPath>/backend/routes/<relevant>.js
# Read the schema/model
cat /home/mattias/projects/goodtribes.org/<localPath>/backend/prisma/schema.prisma 2>/dev/null
cat /home/mattias/projects/goodtribes.org/<localPath>/backend/index.js
```

**For a frontend change:**
```bash
ls /home/mattias/projects/goodtribes.org/<localPath>/frontend/src/
# Read relevant components or pages
cat /home/mattias/projects/goodtribes.org/<localPath>/frontend/src/<relevant>
```

**For a static site change:**
```bash
cat /home/mattias/projects/goodtribes.org/<localPath>/index.html 2>/dev/null
cat /home/mattias/projects/goodtribes.org/<localPath>/Dockerfile
```

Read enough files to write specific, accurate steps. Do not guess file names — use `ls` to confirm they exist before referencing them in the plan.

### 7. Write the detailed implementation plan

The plan must be specific enough that a developer (or the `/ticket` skill) can execute it without needing to read the issue again.

Structure:

```markdown
## Implementation Plan

**Issue:** #<number> — <title>
**Sub-project:** <name>
**Estimated scope:** <Small (~N files) / Medium (~N files)>

### Background
<2–4 sentences summarising what the issue asks for and why, synthesising the request body and the gh-request outline>

### Implementation steps

1. **<File path>** — <what to change and why>
   - <specific detail: function name, HTML element, CSS class, route path, etc.>
   - <exact change: add/edit/remove what>

2. **<Next file>** — <what to change>
   - <detail>

(continue for all files)

### Code notes
<Any non-obvious decisions: naming conventions to follow, existing patterns to reuse, things to avoid>

### Verification
1. <Exact command or browser action to test the change>
2. <Expected output or visual result>
3. <Edge case to check>

---
*Plan written by /gh-plan — move card to 'apply' to begin implementation.*
```

Requirements:
- Every step names an exact file path relative to the monorepo root
- Steps are in dependency order (models before routes, routes before frontend components)
- Reference existing function names, class names, and patterns found during step 5
- Do not introduce libraries or services not already in the sub-project

### 8. Post the plan as a comment

Write the plan to a temp file and post with `--body-file` to avoid shell-quoting issues with backticks and special characters:

```bash
cat > /tmp/gh-plan-comment.md << 'EOF'
<plan_text>
EOF
gh issue comment <issueNumber> --repo <issueRepo> --body-file /tmp/gh-plan-comment.md
rm /tmp/gh-plan-comment.md
```

If the comment command fails — print the plan to the terminal so it is not lost, report the error, and stop without moving the card.

### 9. Move card to 'review'

```bash
gh project item-edit \
  --id <itemNodeId> \
  --field-id <statusFieldId> \
  --project-id <projectNodeId> \
  --single-select-option-id <reviewOptionId>
```

### 10. Update issue label

```bash
gh label create "review" --repo <issueRepo> --color "d93f0b" --force
gh issue edit <issueNumber> --repo <issueRepo> --remove-label "plan" --remove-label "$PICKED_LABEL" --add-label "review"
```

### 11. Report and loop

```
Implementation plan posted to #<issueNumber>.
Card moved to 'review' — human must approve before implementation begins.

Sub-project: <name>
Files in scope: <list>
```

Go back to step 1 immediately to check for the next issue.

---

## Notes

- Read real files before writing the plan — never invent file paths. Use `ls` to confirm structure.
- The `gh-request` outline comment (from the previous stage) is your starting point. Expand it, don't contradict it.
- If the issue repo is `goodtribes-org/deploy` but the code is in the monorepo, that is expected — the deploy repo holds Kubernetes manifests, the source code is in the local monorepo at `/home/mattias/projects/goodtribes.org/`.
- Processes ALL project boards under goodtribes-org (goodtribes.org #2, kickfix #3, asylguiden.se #4). No title filter. Field and option IDs are discovered dynamically per board in step 2.
- Label colors: `review` → `d93f0b` (orange-red, signals needs human attention).
