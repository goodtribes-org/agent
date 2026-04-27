# GitHub Worker Start

Launch gh-request and gh-plan as parallel background agents. Both run continuously, polling for work every 5 minutes.

## Usage

```
/gh-start
```

Run from the monorepo root (`~/projects/goodtribes.org`).

## Workflow

### 1. Launch both agents in parallel

Send a single message with two Agent tool calls, both with `run_in_background: true`.

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

### 2. Report to user

```
Both workers launched as background agents.

  gh-request — watching for 'new' issues → moves to 'request', posts outline
  gh-plan    — watching for 'plan' issues → moves to 'review', posts detailed plan

Both poll every 5 minutes when idle. You will be notified when either completes work.
```

---

## Notes

- Both agents must be launched in a **single message** with two parallel Agent tool calls so they run concurrently.
- Set `run_in_background: true` on both so they do not block the main conversation.
- If one agent errors out, the other continues independently — rerun `/gh-start` to restart the failed one.
- To stop both workers, interrupt the background agents from the Claude Code task list.
