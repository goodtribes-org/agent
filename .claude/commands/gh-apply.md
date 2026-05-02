# GitHub Apply Worker

Find an issue in the 'apply' stage, read its implementation plan, implement all code changes, create a pull request, and move the card to 'test'. Runs continuously — polls every 5 minutes when the queue is empty.

## Usage

```
/gh-apply
```

Run from the monorepo root (`~/projects/goodtribes.org`). The skill detects the project board and target sub-project automatically.

## Status flow context

```
new → request → plan → review → apply → test
                                   ↑
                              You are here
```

Issues arrive in 'apply' after a human moves the card from 'review → apply', signifying the implementation plan has been approved. This skill implements all code changes from the plan and opens a pull request on the goodtribes-org org repository.

## Workflow

Step 0 runs **once** at startup. Steps 1–15 repeat in a loop until interrupted.

### 0. Find all project boards

```bash
gh project list --owner goodtribes-org --format json --limit 50
```

Collect ALL projects returned. Note each project's `number` and `id` (node ID starting with `PVT_`). There is no title filter — all org projects are processed.

If no projects found — report "No GitHub Project boards found" and stop entirely.

---

### ↻ LOOP — repeat from here after each issue or after sleeping

### 1. List items in 'apply' status across all boards

For **each** project number collected in step 0, run:

```bash
gh project item-list <number> --owner goodtribes-org --format json --limit 100
```

Collect all items where `status` equals `apply` (case-insensitive) across all boards. Pick the first item that has no assignee, or the first item overall if all are assigned.

Note the item's:
- `id` — project item node ID (starts with `PVTI_`)
- `content.number` — linked issue number
- `content.repository` — the repo the issue lives in (e.g. `goodtribes-org/kickfix`)
- `projectNumber` and `projectNodeId` — the board this item came from (needed for step 13)

**If no items in 'apply' status on any board:**

```bash
echo "No issues in apply — sleeping 5 minutes..." && sleep 300
```

Then go back to step 1 (do not re-run step 0).

### 2. Discover Status field IDs

Look up the Status field and option IDs for this board:

```bash
gh project field-list <projectNumber> --owner goodtribes-org --format json
```

Note:
- Status field `id` → `<statusFieldId>` (starts with `PVTSSF_`)
- Option `apply` → `<applyOptionId>`
- Option `test` → `<testOptionId>`
- Option `review` → `<reviewOptionId>`

The card stays at `apply` while you work. You will move it to `test` only after the PR is created successfully.

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

COMMENTS:
<all comments, newest last>
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

### 4. Find the implementation plan comment

Scan the comments for the most recent comment that contains the sentinel line:

```
*Plan written by /gh-plan — move card to 'apply' to begin implementation.*
```

Extract the full text of that comment as `<planComment>`.

**If no matching comment is found:**

Write the rejection to a temp file and post it:

```bash
cat > /tmp/gh-apply-noplan.md << 'EOF'
## No implementation plan found

`/gh-apply` could not find a comment from `/gh-plan` on this issue. Implementation cannot begin without a plan.

Expected: a comment ending with:
> *Plan written by /gh-plan — move card to 'apply' to begin implementation.*

**Action needed:** Run `/gh-plan` on this issue first, then move the card back to 'apply'.

*Card returned to 'review'.*
EOF
gh issue comment <issueNumber> --repo <issueRepo> --body-file /tmp/gh-apply-noplan.md
rm /tmp/gh-apply-noplan.md
```

Move card back to `review`:

```bash
gh project item-edit \
  --id <itemNodeId> \
  --field-id <statusFieldId> \
  --project-id <projectNodeId> \
  --single-select-option-id <reviewOptionId>
```

Report: "Issue #<issueNumber> has no plan comment — returned to review." and go back to step 1.

### 5. Validate the sub-project label

Check the issue labels for exactly one of these values:
- `kickfix`
- `asylguiden.se`
- `goodtribes.org`

Then verify the directory exists in the monorepo:

```bash
ls /home/mattias/projects/goodtribes.org/<label>/
```

**If no valid label is present, OR the directory does not exist:**

Write the rejection to a temp file and post it:

```bash
cat > /tmp/gh-apply-reject.md << 'EOF'
## Missing or invalid project label

`/gh-apply` cannot implement this issue because it has no valid sub-project label. Only issues labelled with an existing project can be processed:

- `kickfix`
- `asylguiden.se`
- `goodtribes.org`

Please add the correct label and move the card back to **apply** when ready.

*Card returned to 'review'.*
EOF
gh issue comment <issueNumber> --repo <issueRepo> --body-file /tmp/gh-apply-reject.md
rm /tmp/gh-apply-reject.md
```

Move card back to `review`:

```bash
gh project item-edit \
  --id <itemNodeId> \
  --field-id <statusFieldId> \
  --project-id <projectNodeId> \
  --single-select-option-id <reviewOptionId>
```

Report: "Issue #<issueNumber> has no valid sub-project label — returned to review." and go back to step 1.

**If valid:** note `<subProject>` (the label value) and continue.

### 6. Resolve the target repo and git remote

Map the validated label to its local path, push remote, and GitHub org repo:

| Label | Local path | Push remote | GitHub repo |
|---|---|---|---|
| `kickfix` | `kickfix/` | `goodtribes` | `goodtribes-org/kickfix` |
| `asylguiden.se` | `asylguiden.se/` | `goodtribes` | `goodtribes-org/asylguiden.se` |
| `goodtribes.org` | `goodtribes.org/` | `origin` | `goodtribes-org/goodtribes.org` |

Set:
- `<subProjectDir>` = `/home/mattias/projects/goodtribes.org/<localPath>`
- `<pushRemote>` = remote name (`goodtribes` or `origin`)
- `<ghRepo>` = `goodtribes-org/<repoName>`

Report: "Target: **<subProject>** → `<subProjectDir>` → PR will target `<ghRepo>`"

### 7. Set up the feature branch

Compute the branch slug from the issue title: lowercase, replace non-alphanumeric characters with `-`, strip leading/trailing hyphens, collapse consecutive hyphens, truncate to 40 characters.

Branch name: `feat/issue-<issueNumber>-<slug>`

Example: issue #42 "Add user profile page" → `feat/issue-42-add-user-profile-page`

Fetch latest from the remote (without changing the working directory):

```bash
git -C <subProjectDir> fetch <pushRemote>
```

Check if the branch already exists locally:

```bash
git -C <subProjectDir> branch --list feat/issue-<issueNumber>-<slug>
```

If the branch already exists (a previous partial run), check it out:

```bash
git -C <subProjectDir> checkout feat/issue-<issueNumber>-<slug>
```

If it does not exist, create it off `<pushRemote>/main`:

```bash
git -C <subProjectDir> checkout -b feat/issue-<issueNumber>-<slug> <pushRemote>/main
```

**If the checkout or create fails** — report the full git error output, do NOT move the card, and go back to step 1.

Report: "Branch `feat/issue-<issueNumber>-<slug>` ready in `<subProjectDir>`."

### 8. Implement the plan

Read the plan comment carefully. Execute every implementation step in order.

**Before touching any file:**
- Confirm each file path exists (use `ls` or Read to check) before editing
- For new files, confirm the parent directory exists
- File paths in the plan are relative to the monorepo root — resolve them to absolute paths by
  prepending `/home/mattias/projects/goodtribes.org/`
  (e.g. `kickfix/backend/routes/profile.js` → `/home/mattias/projects/goodtribes.org/kickfix/backend/routes/profile.js`)

**For each step in the plan:**
- Use the Read tool to read existing files before editing
- Use the Edit tool to modify existing files
- Use the Write tool to create new files
- Do NOT use shell commands to write files (`cat >`, `echo >`, `tee`, heredocs)

After implementing all steps, check the working tree:

```bash
git -C <subProjectDir> status --short
```

If the output is empty, no files were changed. Report this as a warning and go to step 9 — the staging step will detect nothing to commit and stop cleanly.

### 9. Stage and commit

```bash
git -C <subProjectDir> add -A
git -C <subProjectDir> status --short
```

Review the staged files. If unexpected files appear (e.g. `node_modules/`, `.env`, build artifacts, lock files that should not be committed), unstage them:

```bash
git -C <subProjectDir> restore --staged <unexpected-file>
```

Then commit:

```bash
git -C <subProjectDir> commit -m "feat: <issueTitle> (closes #<issueNumber>)"
```

**If nothing is staged** (no files changed, or all changes were already committed):
- Report: "Nothing to commit — implementation may already be applied or the plan produced no changes. Aborting."
- Do NOT move the card.
- Go back to step 1.

**If the commit fails for any reason** — report the full error, do NOT move the card, and go back to step 1.

### 10. Push to remote

```bash
git -C <subProjectDir> push <pushRemote> feat/issue-<issueNumber>-<slug>
```

**If the push fails** — report the full error, do NOT move the card, and go back to step 1.

Do NOT force push under any circumstances. If a non-fast-forward error occurs, report it and stop — a human must resolve the conflict.

### 11. Create pull request

Build the PR body in a temp file to avoid quoting issues:

```bash
cat > /tmp/gh-apply-pr.md << 'EOF'
## Summary

Implements #<issueNumber>: <issueTitle>

<2–4 sentence summary of what was changed, derived from the plan's Background section>

## Changes

<bullet list of files changed, one per line, derived from the plan's Implementation steps — e.g.:
- `kickfix/backend/routes/profile.js` — added GET /api/profile endpoint
- `kickfix/frontend/src/pages/Profile.jsx` — new Profile page component
>

## Implementation notes

<Any non-obvious decisions from the plan's Code notes section, or "None." if absent>

## Verification

<The plan's Verification steps, verbatim>

---

Closes #<issueNumber>

*PR created by /gh-apply*
EOF
```

Create the PR:

```bash
gh pr create \
  --repo goodtribes-org/<repoName> \
  --title "feat: <issueTitle> (#<issueNumber>)" \
  --head feat/issue-<issueNumber>-<slug> \
  --base main \
  --body-file /tmp/gh-apply-pr.md
rm /tmp/gh-apply-pr.md
```

Capture the PR URL from the command output.

**If PR creation fails** — report the full error. Do NOT delete the temp file (leave it for debugging). Do NOT move the card. Go back to step 1.

### 12. Post PR link as issue comment

```bash
cat > /tmp/gh-apply-comment.md << 'EOF'
## Implementation complete

Pull request opened: <prUrl>

**Branch:** `feat/issue-<issueNumber>-<slug>`
**Repo:** `goodtribes-org/<repoName>`

All changes from the implementation plan have been applied and committed. The PR is ready for review and merge.

*Card moved to 'test' by /gh-apply*
EOF
gh issue comment <issueNumber> --repo <issueRepo> --body-file /tmp/gh-apply-comment.md
rm /tmp/gh-apply-comment.md
```

**If the comment fails** — report the warning but continue to step 13. The PR exists and is the canonical artifact; the comment is recoverable.

### 13. Move card to 'test'

```bash
gh project item-edit \
  --id <itemNodeId> \
  --field-id <statusFieldId> \
  --project-id <projectNodeId> \
  --single-select-option-id <testOptionId>
```

**If this fails** — report the error but continue. The PR exists and the comment was posted. A human can move the card manually.

### 14. Update issue label

```bash
gh label create "test" --repo <issueRepo> --color "0e8a16" --force
gh issue edit <issueNumber> --repo <issueRepo> --remove-label "apply" --add-label "test"
```

Do not stop if label update fails — report a warning and continue.

### 15. Report and loop

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 APPLIED: Issue #<issueNumber>
 PR:      <prUrl>
 Branch:  feat/issue-<issueNumber>-<slug>
 Repo:    goodtribes-org/<repoName>
 Card:    moved to 'test'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

Go back to step 1 immediately to check for the next issue.

---

## Notes

- All `git` operations use `git -C <subProjectDir>` — never `cd`. The monorepo working directory never changes throughout the loop.
- Always branch off `<pushRemote>/main` (the remote ref, not local `main`) to guarantee a clean state even if local `main` is behind.
- The card only moves to `test` after the PR is created successfully. A failed comment or label update is a warning, not a blocker — the PR is the canonical artifact.
- Never force push. If a non-fast-forward occurs, stop and let a human resolve it.
- Never create empty commits. If `git add -A` stages nothing, report and loop.
- File implementation uses Read/Edit/Write tools only — never shell file writes (`cat >`, `echo >`). Shell heredocs are only used for markdown comment bodies.
- Label colors: `test` → `0e8a16` (green, signals ready for QA).
- The `goodtribes.org` sub-project uses `origin` as its push remote (no `goodtribes` remote configured yet). All other sub-projects use the `goodtribes` remote.
- Processes ALL project boards under goodtribes-org (goodtribes.org #2, kickfix #3, asylguiden.se #4). No title filter. Field and option IDs are discovered dynamically per board in step 2.
