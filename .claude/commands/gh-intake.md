# GitHub Project Board Intake

Find and claim the next available issue from the GitHub Projects v2 board matching the current repo.

## Usage

```
/gh-intake
```

Run from within a git repository that has a GitHub remote. The skill detects the remote automatically.

## Workflow

Follow these steps exactly:

### 0. Detect repo and owner

```bash
git remote get-url origin
```

Parse the remote URL to extract `owner` and `repo`. Handle both SSH (`git@github.com:owner/repo.git`) and HTTPS (`https://github.com/owner/repo.git`) formats. Strip the `.git` suffix. The repo short name is the part after the final `/`.

Report: "Repository: `<owner>/<repo>`"

If there is no git remote — report "Not inside a git repository with a GitHub remote" and stop.

### 1. Find the matching project board

```bash
gh project list --owner <owner> --format json --limit 50
```

Parse the JSON array. Find the project whose `title` field matches the repo short name (case-insensitive substring match is acceptable). Note its `number` and `id` (the node ID, not the number).

If no project matches — report "No GitHub Project board found matching '<repo>' under @<owner>" and stop.

Report: "Project board: **<title>** (#<number>)"

### 2. List project items

```bash
gh project item-list <number> --owner <owner> --format json --limit 100
```

Parse the JSON array of items. Each item has a `status` field and may have `assignees`.

Filter to items where:
- `status` is one of: `New`, `Todo`, `Backlog` (case-insensitive, check all three)
- AND the item has no assignees

Pick the first matching item. Note its `id` (the project item node ID) and `content.number` (the linked issue number).

If no matching items — report "No unclaimed items in New/Todo/Backlog status on this board" and stop.

### 3. View the issue

```bash
gh issue view <issue_number> --repo <owner>/<repo> --json title,body,labels,assignees,number,url
```

Display the issue in full:

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 ISSUE #<number>: <title>
 URL: <url>
 Labels: <labels or "none">
 Assignees: <assignees or "none">
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

<body>
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

### 4. Discover Status field IDs

```bash
gh project field-list <number> --owner <owner> --format json
```

Find the field named `Status`. Note its `id` (the field node ID). Within its `options` array, find:
- Option named `In Progress` → note its `id` as `<in_progress_option_id>`
- Option named `Planned` → note its `id` as `<planned_option_id>`
- Option named `New` (or `Todo`/`Backlog`) → note its `id` as `<new_option_id>`

Match option names case-insensitively.

If no Status field exists — report "Project board has no Status field — cannot move card" and stop.

### 5. Claim the item — move to In Progress

```bash
gh project item-edit \
  --id <item_node_id> \
  --field-id <status_field_id> \
  --project-id <project_node_id> \
  --single-select-option-id <in_progress_option_id>
```

If the command fails — report the error, do not continue.

Report: "Claimed: issue #<number> moved to 'In Progress' on project board."

### 6. Tag the issue with labels

Ensure both labels exist (create them if missing):

```bash
gh label create "<repoShortName>" --repo <owner>/<repo> --color "0075ca" --force
gh label create "request" --repo <owner>/<repo> --color "e4e669" --force
```

Add them to the issue:

```bash
gh issue edit <issue_number> --repo <owner>/<repo> --add-label "<repoShortName>,request"
```

### 7. Save context for gh-plan

Write the following JSON to `/tmp/gh-intake-context.json` using python3:

```bash
python3 -c "
import json
ctx = {
  'owner': '<owner>',
  'repo': '<owner>/<repo>',
  'repoShortName': '<repo>',
  'issueNumber': <issue_number>,
  'projectNumber': <project_number>,
  'projectId': '<project_node_id>',
  'itemId': '<item_node_id>',
  'statusFieldId': '<status_field_id>',
  'inProgressOptionId': '<in_progress_option_id>',
  'plannedOptionId': '<planned_option_id>',
  'newOptionId': '<new_option_id>'
}
open('/tmp/gh-intake-context.json', 'w').write(json.dumps(ctx, indent=2))
print('Context saved.')
"
```

### 8. Chain to gh-request

Invoke `/gh-request` now. The context is saved at `/tmp/gh-intake-context.json` — the next skill reads it automatically.

---

## Notes

- The project item node ID (step 5 `--id`) looks like `PVI_...`. The project node ID (step 5 `--project-id`) looks like `PVT_...`. These appear in the `gh project list` JSON output as `id`.
- If step 5 fails because the card was already moved by someone else, report the conflict and stop — do not write context.
- Status option names may vary per board (e.g. "In Progress" vs "InProgress"). Use case-insensitive substring matching.
- Do not stop if label creation fails — just report the warning and continue to the context save step.
- The `request` label signals that a plan is being written. The human moves the card to `plan` status on the board to approve. Never use `in-progress` or `planned` labels — the board uses `request → plan → apply → test`.
