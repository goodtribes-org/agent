# Quickstart — from zero to your first merged release

This guide walks you through setting up Claude Code on a new laptop and running the full pipeline: file an issue, let the agents plan and implement it, review the PR, and ship.

---

## Step 1 — Install the tools (one time)

You need three things: Git, the GitHub CLI, and Claude Code.

**macOS:**

```bash
xcode-select --install          # Git (skip if already installed)
brew install gh                 # GitHub CLI
npm install -g @anthropic-ai/claude-code   # Claude Code
```

**Ubuntu/Debian:**

```bash
sudo apt install git gh
npm install -g @anthropic-ai/claude-code
```

---

## Step 2 — Log in (one time)

```bash
gh auth login       # opens a browser — log in with your GitHub account
claude login        # opens a browser — log in with your Claude account (Pro or Teams required)
```

Verify:

```bash
gh auth status      # Logged in to github.com as <you>
claude --version    # prints a version number
```

---

## Step 3 — Set up an SSH key (one time, if you haven't already)

The sub-project repos use SSH. If `ssh -T git@github.com` already says "Hi <you>!" you can skip this.

```bash
ssh-keygen -t ed25519 -C "your@email.com"   # accept defaults, set a passphrase
cat ~/.ssh/id_ed25519.pub                    # copy the output
```

Paste the public key at **https://github.com/settings/keys**, then test:

```bash
ssh -T git@github.com   # Hi <you>! You've successfully authenticated.
```

---

## Step 4 — Clone the workspace (one time)

```bash
mkdir -p ~/projects
git clone git@github.com:goodtribes-org/agent.git ~/projects/goodtribes.org
cd ~/projects/goodtribes.org
```

The sub-project repos will be cloned automatically the first time you run `/gh-apply` (step 7 below). You can also clone them manually now if you prefer:

```bash
git clone git@github.com:goodtribes-org/kickfix.git kickfix
git clone git@github.com:goodtribes-org/asylguiden.se.git asylguiden.se
```

After cloning manually, rename the default remote for the two sub-projects so the agents can push:

```bash
git -C kickfix remote rename origin goodtribes
git -C asylguiden.se remote rename origin goodtribes
```

---

## Step 5 — Open Claude Code

Always launch from the monorepo root so the skills are available:

```bash
cd ~/projects/goodtribes.org
claude
```

You should see the Claude Code prompt. The skills in `.claude/commands/` load automatically.

---

## Step 6 — Start the workers

```
/gh-start
```

This launches three background agents. You will see them appear in the task list:

```
gh-request — watching 'new' issues       → posts outline, moves to 'request'
gh-plan    — watching 'plan' issues      → posts implementation plan, moves to 'review'
gh-apply   — watching 'apply' issues     → implements code, opens PR, moves to 'test'
```

On first startup, `gh-apply` runs a pre-flight check on the sub-project repos. If any are missing it will clone them. If a remote is misconfigured it will ask you what to do.

The workers poll every 5 minutes. Leave Claude Code open in a terminal tab while you work.

---

## Step 7 — File an issue

Go to the GitHub repo for the project you want to change:

| Change | Repo |
|--------|------|
| Kickfix (job marketplace) | https://github.com/goodtribes-org/kickfix/issues/new |
| Asylguiden.se (refugee site) | https://github.com/goodtribes-org/asylguiden.se/issues/new |

Write a clear title and body. The agents read this to understand what to build:

```markdown
## What
Add a "Save job" button to each job card. Saved jobs appear at /saved in the dashboard.

## Why
Users lose track of interesting listings without a way to bookmark them.

## Acceptance criteria
- Logged-in users can save/unsave any job with one click
- /saved shows all saved jobs for the current user
- Saving persists across sessions
- Unauthenticated users see a "Log in to save" prompt
```

Then add the issue to the right project board:

1. Open the issue → right sidebar → **Projects** → select the matching board
2. Set status to **`new`**
3. Add the sub-project label (`kickfix`, `asylguiden.se`, or `goodtribes.org`)

The `gh-request` worker picks it up within 5 minutes.

---

## Step 8 — Approve the outline (checkpoint 1)

The `gh-request` worker posts a comment on the issue and moves the card to `request`. Open the issue and read the comment. It looks like:

```
## Request Outline
**Scope:** Small — ~4 files
**Sensitive data:** None
**Stack check:** Passes

### Steps
1. kickfix/backend/models/SavedJob.js — new Prisma model
2. kickfix/backend/routes/savedJobs.js — GET/POST/DELETE endpoints
...
```

If the outline looks right, **move the card from `request` → `plan`** on the project board. That's it — this is your approval.

If something is wrong, comment on the issue with corrections and leave the card at `request` (or move it back to `new` to re-run the outline worker).

---

## Step 9 — Approve the plan (checkpoint 2)

The `gh-plan` worker reads the actual source files and posts a detailed implementation plan, then moves the card to `review`. Read the comment — it names exact file paths, function names, and steps in dependency order.

If the plan looks right, **move the card from `review` → `apply`** on the project board. This is your second and final approval.

If something needs changing, comment on the issue and move the card back to `plan` to re-trigger the planner.

---

## Step 10 — Wait for the PR

Once the card is at `apply`, the `gh-apply` worker:

1. Checks out (or clones) the sub-project repo
2. Creates a feature branch: `feat/issue-<N>-<slug>`
3. Implements every step from the plan
4. Commits and pushes to `goodtribes-org/<repo>`
5. Opens a pull request
6. Posts the PR link as a comment on the issue
7. Moves the card to `test`

If anything goes wrong (push conflict, PR creation error), the worker pauses and asks you directly in the Claude Code conversation. You can retry, skip, or stop.

---

## Step 11 — Review and merge the PR

Open the PR link from the issue comment. Review the diff — check that:

- The changes match what the plan described
- No unexpected files are included (no `.env`, no `node_modules`)
- The verification steps in the PR description are plausible

If it looks good, **merge the PR** on GitHub. The card can be moved to done manually, or leave it at `test` until you've verified the deployment.

---

## Daily workflow summary

```
You                              Agents
─────────────────────────────    ────────────────────────────────────────
1. File issue + add to board
2. Set status = new              gh-request picks up → posts outline
3. Review outline
4. Move card: request → plan     gh-plan picks up → posts implementation plan
5. Review plan
6. Move card: review → apply     gh-apply picks up → implements + opens PR
7. Review and merge PR
```

---

## Troubleshooting

**Worker stopped / not picking up issues**

Open Claude Code and check the task list (Ctrl+T or the sidebar). If the background agents have exited, run `/gh-start` again.

**Issue stuck at a stage**

Read the comments on the issue — the worker always posts a comment explaining why it stopped or moved the card back. Common causes:
- Missing sub-project label → add `kickfix`, `asylguiden.se`, or `goodtribes.org`
- No implementation plan found → move card back to `plan` to re-run `/gh-plan`
- A `picked-by-<hostname>` label is still on the issue from a crashed worker → remove it manually

**Clone or SSH failure at startup**

The `gh-apply` worker will ask you via `AskUserQuestion` if it cannot clone a sub-project. Answer in the Claude Code conversation. Most common cause: SSH key not added to GitHub (see step 3).

**Push rejected (non-fast-forward)**

The worker will pause and ask you. Usually means the branch already exists with different history. You can resolve it locally:

```bash
git -C kickfix fetch goodtribes
git -C kickfix checkout feat/issue-<N>-<slug>
git -C kickfix rebase goodtribes/main
```

Then answer A (retry) in the worker's question.

---

## Keeping skills up to date

When teammates update the skill files, pull the latest:

```bash
cd ~/projects/goodtribes.org
git pull origin main
```

No restart needed — skills are read fresh each time a worker picks up an issue.
