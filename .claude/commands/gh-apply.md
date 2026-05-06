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

Steps 0 and 0.5 run **once** at startup. Steps 1–15 repeat in a loop until interrupted.

### 0. Find all project boards

```bash
gh project list --owner goodtribes-org --format json --limit 50
```

Collect ALL projects returned. Note each project's `number` and `id` (node ID starting with `PVT_`). There is no title filter — all org projects are processed.

If no projects found — report "No GitHub Project boards found" and stop entirely.

---

### 0.5. Verify local repo setup (runs once at startup)

Check that all three sub-project repositories are cloned locally and have the correct git remotes. Run these checks for each sub-project in this order:

| Sub-project | Directory | Remote | Expected URL |
|---|---|---|---|
| `kickfix` | `kickfix/` | `goodtribes` | `git@github.com:goodtribes-org/kickfix.git` |
| `asylguiden.se` | `asylguiden.se/` | `goodtribes` | `git@github.com:goodtribes-org/asylguiden.se.git` |
| `goodtribes.org` | `goodtribes.org/` | `origin` | `git@github.com:goodtribes-org/goodtribes.org.git` |

**Check A — Directory exists:**

```bash
ls /home/mattias/projects/goodtribes.org/<subdir>/
```

If the directory is missing, clone it automatically (no confirmation needed — cloning is always safe):

```bash
git clone git@github.com:goodtribes-org/<repoName>.git /home/mattias/projects/goodtribes.org/<subdir>
```

For `kickfix` and `asylguiden.se` only, rename the default `origin` remote to `goodtribes`:

```bash
git -C /home/mattias/projects/goodtribes.org/<subdir> remote rename origin goodtribes
```

For `goodtribes.org`, keep `origin` as-is.

If `git clone` fails, pause and ask the user via `AskUserQuestion`:

```
Sub-project setup problem: <name>
Issue: clone failed

Error: <full error output>

Options:
  A) Retry the clone (fix SSH/network first, then select A)
  B) Skip this sub-project for this session (issues labelled '<name>' will be rejected at step 5)
  C) Stop the worker entirely
```

Wait for the user's response. If A: retry the clone. If B: mark sub-project as unavailable in session memory and continue to the next sub-project. If C: stop entirely.

**Check B — Is a git repo:**

```bash
git -C /home/mattias/projects/goodtribes.org/<subdir> rev-parse --git-dir 2>&1
```

If this fails (directory exists but is not a git repository), ask the user via `AskUserQuestion` — do not auto-fix:

```
Sub-project setup problem: <name>
Issue: directory exists at <subdir> but is not a git repository

Error: <command output>

Options:
  A) Skip this sub-project for this session
  B) Stop the worker entirely
```

**Check C — Remote name and URL correct:**

```bash
git -C /home/mattias/projects/goodtribes.org/<subdir> remote get-url <remoteName> 2>&1
```

If the remote doesn't exist or points to the wrong URL, ask the user via `AskUserQuestion` and offer to fix:

```
Sub-project setup problem: <name>
Issue: <remote '<remoteName>' not found | remote '<remoteName>' points to '<actualUrl>' instead of '<expectedUrl>'>

Options:
  A) Fix automatically (run: git remote <add|set-url> <remoteName> <expectedUrl>)
  B) Skip this sub-project for this session
  C) Stop the worker entirely
```

If A: run the appropriate fix command, then re-run Check C. If it still fails, ask again with the new error output.

Fix commands:
- Remote missing: `git -C <subdir> remote add <remoteName> <expectedUrl>`
- Remote wrong URL: `git -C <subdir> remote set-url <remoteName> <expectedUrl>`

**After all checks pass for all three sub-projects**, report:

```
Pre-flight OK: kickfix ✓  asylguiden.se ✓  goodtribes.org ✓
```

Then proceed to the loop.

---

### ↻ LOOP — repeat from here after each issue or after sleeping

### 1. List items in 'apply' status across all boards

For **each** project number collected in step 0, run:

```bash
gh project item-list <number> --owner goodtribes-org --format json --limit 100
```

Collect all items where `status` equals `apply` (case-insensitive) across all boards.

For each candidate item (prefer unassigned first), fetch its issue labels:

```bash
gh issue view <content.number> --repo <content.repository> --json labels --jq '.labels[].name'
```

Skip the item if any label name starts with `picked-by-`. Pick the first item that passes this check.

Note the item's:
- `id` — project item node ID (starts with `PVTI_`)
- `content.number` — linked issue number
- `content.repository` — the repo the issue lives in (e.g. `goodtribes-org/kickfix`)
- `projectNumber` and `projectNodeId` — the board this item came from (needed for step 13)

**If no items in 'apply' status on any board, OR all candidates have a `picked-by-*` label:**

```bash
echo "No issues in apply — sleeping 5 minutes..." && sleep 300
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

Store `$PICKED_LABEL` — you will need it when removing the label in step 14.

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

### 5. Validate the sub-project label and local repo

Check the issue labels for exactly one of these values:
- `kickfix`
- `asylguiden.se`
- `goodtribes.org`

**If no valid label is present:**

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

**If valid**, note `<subProject>` (the label value). Then verify the directory exists:

```bash
ls /home/mattias/projects/goodtribes.org/<label>/
```

If the directory is missing, attempt to clone it (same process as step 0.5 Check A):

```bash
git clone git@github.com:goodtribes-org/<repoName>.git /home/mattias/projects/goodtribes.org/<subdir>
# For kickfix and asylguiden.se only:
git -C /home/mattias/projects/goodtribes.org/<subdir> remote rename origin goodtribes
```

If clone fails, pause and ask the user via `AskUserQuestion`:

```
Cannot clone <repoName> to process issue #<issueNumber>.

Clone failed with:
<full error output>

Options:
  A) Retry the clone
  B) Skip this issue (card returns to 'review')
  C) Stop the worker entirely
```

If B: move the card to `review`, remove the `$PICKED_LABEL` label, and go back to step 1. If C: stop entirely.

Continue once the directory is confirmed to exist.

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

**If the checkout or create fails** — pause and ask the user via `AskUserQuestion`:

```
Branch setup failed for issue #<issueNumber> in <subProjectDir>.

Command: git checkout [-b] feat/issue-<issueNumber>-<slug> [<pushRemote>/main]
Error: <full git error output>

Options:
  A) Retry the branch setup
  B) Skip this issue (card → 'review', picked-by label removed)
  C) Stop the worker entirely
```

If A: retry the git checkout/create command. If B: move card to `review`, remove `$PICKED_LABEL` label, go back to step 1. If C: stop entirely.

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

Do NOT force push under any circumstances.

**If the push fails** — pause and ask the user via `AskUserQuestion`:

```
Push failed for issue #<issueNumber>.

Command: git push <pushRemote> feat/issue-<issueNumber>-<slug>
Error: <full error output>

Note: force push is not allowed. A non-fast-forward error must be resolved manually before retrying.

Options:
  A) Retry the push (after resolving any upstream conflicts)
  B) Skip this issue (branch left as-is, card → 'review', picked-by removed)
  C) Stop the worker entirely
```

If A: retry the push command. If B: post a GitHub comment on the issue noting the pushed branch name, move card to `review`, remove `$PICKED_LABEL` label, go back to step 1. Do NOT delete the branch. If C: stop entirely.

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

**If PR creation fails** — do NOT delete the temp file. Pause and ask the user via `AskUserQuestion`:

```
PR creation failed for issue #<issueNumber>.

The branch was pushed successfully to <ghRepo>.
The PR body is saved at /tmp/gh-apply-pr.md.

Error: <full error output>

Options:
  A) Retry PR creation
  B) Provide the PR URL manually (the PR may already exist — enter the URL and I'll complete steps 12–15)
  C) Skip this issue (card → 'review', branch left pushed, picked-by removed)
  D) Stop the worker entirely
```

If A: retry `gh pr create`. If B: user provides a PR URL → use it for steps 12–15 and complete the normal flow. If C: post a GitHub comment on the issue noting the pushed branch name, move card to `review`, remove `$PICKED_LABEL` label, go back to step 1. If D: stop entirely.

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
gh issue edit <issueNumber> --repo <issueRepo> --remove-label "apply" --remove-label "$PICKED_LABEL" --add-label "test"
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
- Step 0.5 verifies all three sub-repos at startup. Missing repos are cloned automatically; git/remote issues prompt `AskUserQuestion` before any config change is made.
- Blocking failures in steps 7, 10, and 11 pause the worker via `AskUserQuestion`. Skipped issues always have their `picked-by` label removed and card returned to `review`.
