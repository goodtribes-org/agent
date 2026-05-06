# Goodtribe Work

Single-agent continuous worker. Scans all three project boards for one ready issue, processes it using the appropriate skill (gh-request, gh-plan, or gh-apply), then sleeps 3 minutes before the next cycle. Handles the full pipeline in one place.

## Usage

```
/goodtribeWork
```

Run from the monorepo root (`~/projects/goodtribes.org`).

## Priority order

Each cycle picks at most one issue. Issues are selected in this order:

1. `apply` — most ready to ship; picks up approved plans and opens PRs
2. `plan` — picks up approved outlines and writes implementation plans
3. `new` — picks up fresh issues and writes outlines

If multiple issues exist in the highest-priority stage, pick the one without a `picked-by-*` label. If all candidates at that stage are picked, drop to the next stage.

## Workflow

Step 0 and step 0.5 run **once** at startup. Steps 1–4 repeat in a loop.

---

### 0. Load skill instructions

Read all three skill files in full before starting the loop:

- `/home/mattias/projects/goodtribes.org/.claude/commands/gh-request.md`
- `/home/mattias/projects/goodtribes.org/.claude/commands/gh-plan.md`
- `/home/mattias/projects/goodtribes.org/.claude/commands/gh-apply.md`

These are the authoritative instructions for each stage. Follow them exactly when processing an issue.

Then find all project boards (runs once):

```bash
gh project list --owner goodtribes-org --format json --limit 50
```

Collect ALL projects returned. Note each project's `number` and `id` (node ID starting with `PVT_`).

If no projects found — report "No GitHub Project boards found" and stop entirely.

---

### 0.5. Verify local repo setup (runs once at startup)

Follow the pre-flight check defined in **gh-apply.md step 0.5** exactly — verify all three sub-project repos are cloned and have correct git remotes, auto-clone any that are missing, and use `AskUserQuestion` for any ambiguous setup issue.

---

### ↻ LOOP — repeat every 3 minutes

### 1. Find one issue to work on

For each project number, run:

```bash
gh project item-list <number> --owner goodtribes-org --format json --limit 100
```

Collect all items across all boards. For each candidate, fetch its labels:

```bash
gh issue view <content.number> --repo <content.repository> --json labels --jq '.labels[].name'
```

Skip any issue whose labels include one starting with `picked-by-`.

Now select one issue using the priority order:

1. First look for an item with `status = apply`
2. If none, look for `status = plan`
3. If none, look for `status = new`
4. If none at all — report "No work found" and go to step 4 (sleep).

Note the selected item's:
- `id` — project item node ID
- `content.number` — linked issue number
- `content.repository` — e.g. `goodtribes-org/kickfix`
- `projectNumber` and `projectNodeId` — board this item belongs to
- `status` — which stage it is in (`apply`, `plan`, or `new`)

Report:
```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 CYCLE START
 Issue:  #<number> — <title>
 Stage:  <status>
 Board:  <projectNumber>
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

### 2. Claim the issue

```bash
PICKED_LABEL="picked-by-$(hostname)"
gh label create "$PICKED_LABEL" --repo <issueRepo> --color "f9a825" --force
gh issue edit <issueNumber> --repo <issueRepo> --add-label "$PICKED_LABEL"
```

### 3. Process the issue

Based on the issue's `status`, follow the full workflow from the corresponding skill file read in step 0:

**If status = `apply`:**
Follow **gh-apply.md** steps 2–15 exactly (discover field IDs, read issue, find plan comment, validate label, resolve repo, set up branch, implement, commit, push, create PR, post comment, move card to test, update labels).

**If status = `plan`:**
Follow **gh-plan.md** steps 2 onward exactly (discover field IDs, read issue + comments, validate label, read codebase, write plan, post comment, move card to review, update labels).

**If status = `new`:**
Follow **gh-request.md** steps 2 onward exactly (discover field IDs, read issue, validate sub-project label, scope check, sensitive data check, stack check, write outline, post comment, move card to request, update labels).

The `$PICKED_LABEL` is removed by the individual skill's label-update step at the end of processing. If an error causes early exit before that step, remove it manually:

```bash
gh issue edit <issueNumber> --repo <issueRepo> --remove-label "$PICKED_LABEL"
```

### 4. Sleep 3 minutes

```bash
echo "Cycle complete — sleeping 3 minutes..." && sleep 180
```

Then go back to step 1.

---

## Notes

- Processes one issue per cycle. For higher throughput, run `/gh-start` instead (three parallel workers).
- Priority order (apply → plan → new) ensures the most deployment-ready work ships first.
- The `picked-by-$(hostname)` label prevents collisions if another instance is running on a different machine.
- All git operations use `git -C <subProjectDir>` — never `cd`.
- File edits use Read/Edit/Write tools only — never shell file writes.
- `AskUserQuestion` is used for any blocking issue (setup problems, push failures, PR creation failures) — the worker pauses and waits for your response before continuing.
