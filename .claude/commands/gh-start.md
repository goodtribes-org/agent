# GitHub Worker Start

Launch gh-request, gh-plan, and gh-apply as parallel background agents. All run continuously, polling for work every 5 minutes.

## Usage

```
/gh-start
```

Run from the monorepo root (`~/projects/goodtribes.org`).

## Workflow

### 1. Launch all three agents in parallel

Send a single message with three Agent tool calls, all with `run_in_background: true`.

**Agent 1 — gh-request worker:**

```
subagent_type: general-purpose
run_in_background: true
description: "gh-request background worker"
prompt:
  You are a background worker running the /gh-request skill continuously.
  Working directory: /home/mattias/projects/goodtribes.org

  Follow the workflow defined in /home/mattias/projects/goodtribes.org/.claude/commands/gh-request.md exactly.

  Summary of what to do:
  1. Run `git remote get-url origin` to detect the owner (goodtribes-org).
  2. Run `gh project list --owner goodtribes-org --format json --limit 50` to get ALL project boards (goodtribes.org #2, kickfix #3, asylguiden.se #4). Note each board's number and node ID.
  3. LOOP:
     a. For each project number, run `gh project item-list <number> --owner goodtribes-org --format json --limit 100` and collect items with status = "new" across ALL boards.
     b. If none found on any board: run `echo "gh-request: no new issues, sleeping 5 min" && sleep 300` then go to step 3.
     c. If found: process the issue following the full gh-request.md workflow (read issue, validate sub-project label, move card to request, tag labels, read codebase, scope check, sensitive data check, stack check, write outline, post as comment using --body-file). Track which project number and node ID the item came from.
     d. After processing: immediately go back to step 3.

  Read /home/mattias/projects/goodtribes.org/.claude/commands/gh-request.md for the complete step-by-step instructions before starting.
  Run indefinitely until interrupted.
```

**Agent 2 — gh-plan worker:**

```
subagent_type: general-purpose
run_in_background: true
description: "gh-plan background worker"
prompt:
  You are a background worker running the /gh-plan skill continuously.
  Working directory: /home/mattias/projects/goodtribes.org

  Follow the workflow defined in /home/mattias/projects/goodtribes.org/.claude/commands/gh-plan.md exactly.

  Summary of what to do:
  1. Run `gh project list --owner goodtribes-org --format json --limit 50` to get ALL project boards (goodtribes.org #2, kickfix #3, asylguiden.se #4). Note each board's number and node ID.
  2. LOOP:
     a. For each project number, run `gh project item-list <number> --owner goodtribes-org --format json --limit 100` and collect items with status = "plan" across ALL boards.
     b. If none found on any board: run `echo "gh-plan: no plan issues, sleeping 5 min" && sleep 300` then go to step 2.
     c. If found: process the issue following the full gh-plan.md workflow (read issue + comments, validate sub-project label against existing monorepo directory, reject back to request if invalid, read codebase files, write detailed implementation plan, post using --body-file, move card to review, update labels). Track which project number and node ID the item came from.
     d. After processing: immediately go back to step 2.

  Read /home/mattias/projects/goodtribes.org/.claude/commands/gh-plan.md for the complete step-by-step instructions before starting.
  Run indefinitely until interrupted.
```

**Agent 3 — gh-apply worker:**

```
subagent_type: general-purpose
run_in_background: true
description: "gh-apply background worker"
prompt:
  You are a background worker running the /gh-apply skill continuously.
  Working directory: /home/mattias/projects/goodtribes.org

  Follow the workflow defined in /home/mattias/projects/goodtribes.org/.claude/commands/gh-apply.md exactly.

  Summary of what to do:
  1. Run `gh project list --owner goodtribes-org --format json --limit 50` to get ALL project boards (goodtribes.org #2, kickfix #3, asylguiden.se #4). Note each board's number and node ID.
  2. LOOP:
     a. For each project number, run `gh project item-list <number> --owner goodtribes-org --format json --limit 100` and collect items with status = "apply" across ALL boards.
     b. If none found on any board: run `echo "gh-apply: no apply issues, sleeping 5 min" && sleep 300` then go to step 2.
     c. If found: process the issue following the full gh-apply.md workflow:
        - Discover status field IDs (statusFieldId, testOptionId, reviewOptionId) for this board
        - Read issue and all comments with `gh issue view --json title,body,labels,number,url,comments`
        - Find the most recent gh-plan comment (sentinel: "*Plan written by /gh-plan — move card to 'apply' to begin implementation.*")
        - If no plan comment: post rejection, move card back to review, loop
        - Validate sub-project label (kickfix, asylguiden.se, goodtribes.org) against existing monorepo directory
        - If invalid label: post rejection, move card back to review, loop
        - Map label → localPath, pushRemote (goodtribes for kickfix/asylguiden.se, origin for goodtribes.org), ghRepo (goodtribes-org/<name>)
        - Compute branch name: feat/issue-<N>-<slug> (lowercase, alphanumeric+hyphen, max 40 chars)
        - git -C <subProjectDir> fetch <pushRemote>
        - Checkout existing branch or create: git -C <subProjectDir> checkout -b feat/issue-<N>-<slug> <pushRemote>/main
        - Implement ALL changes from the plan using Read/Edit/Write tools only (NOT shell file writes)
        - git -C <subProjectDir> add -A && git -C <subProjectDir> commit -m "feat: <title> (closes #<N>)"
        - git -C <subProjectDir> push <pushRemote> feat/issue-<N>-<slug> (never force push)
        - gh pr create --repo goodtribes-org/<repoName> --title "feat: <title> (#<N>)" --head <branch> --base main --body-file /tmp/gh-apply-pr.md
        - Post PR link as issue comment using --body-file
        - Move card to test status
        - Update issue label to test
        Track which project number and node ID the item came from.
     d. After processing: immediately go back to step 2.

  Read /home/mattias/projects/goodtribes.org/.claude/commands/gh-apply.md for the complete step-by-step instructions before starting.
  Run indefinitely until interrupted.
```

### 2. Report to user

```
Three workers launched as background agents.

  gh-request — watching for 'new' issues   → moves to 'request', posts outline
  gh-plan    — watching for 'plan' issues  → moves to 'review', posts detailed plan
  gh-apply   — watching for 'apply' issues → implements code, opens PR, moves to 'test'

All poll every 5 minutes when idle. You will be notified when any completes work.
```

---

## Notes

- All three agents must be launched in a **single message** with three parallel Agent tool calls so they run concurrently.
- Set `run_in_background: true` on all three so they do not block the main conversation.
- If one agent errors out, the others continue independently — rerun `/gh-start` to restart the failed one.
- To stop all workers, interrupt the background agents from the Claude Code task list.
