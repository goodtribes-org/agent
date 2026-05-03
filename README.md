# goodtribes-org/agent

Shared [Claude Code](https://claude.ai/code) skills for the Goodtribes team. This repo contains the AI-assisted planning and implementation pipeline that runs against the three project boards.

---

## Table of contents

1. [Prerequisites](#prerequisites)
2. [Set up your workspace](#set-up-your-workspace)
3. [Repository structure](#repository-structure)
4. [Working with the agent repo](#working-with-the-agent-repo)
5. [How the project board flow works](#how-the-project-board-flow-works)
6. [How to write a good issue](#how-to-write-a-good-issue)
7. [Running the workers](#running-the-workers)
8. [Skills reference](#skills-reference)
9. [Project boards](#project-boards)

---

## Prerequisites

### Accounts

- **GitHub** — member of the [goodtribes-org](https://github.com/goodtribes-org) organisation
- **Claude** — active [Claude Pro or Teams](https://claude.ai) subscription (background agents require a paid plan)

### Tools

Install these before anything else:

**Git**

```bash
# macOS (via Xcode tools — usually already installed)
xcode-select --install

# Ubuntu/Debian
sudo apt install git
```

**GitHub CLI (`gh`)**

```bash
# macOS
brew install gh

# Ubuntu/Debian
sudo apt install gh
```

**Claude Code CLI**

```bash
npm install -g @anthropic-ai/claude-code
```

### Authenticate

```bash
# GitHub CLI — opens a browser login flow
gh auth login

# Claude Code — opens a browser login flow to your Claude account
claude login
```

Confirm everything works:

```bash
gh auth status    # should show: Logged in to github.com as <you>
claude --version  # should print the Claude Code version
```

> **Anthropic API key:** Not needed for local use. Claude Code uses your Claude account (Pro/Teams) when running interactively on your machine. An API key is only required for headless or automated deployments (CI, servers, cron agents).

### SSH key for GitHub

The sub-project repos use SSH remotes. If you have not added an SSH key to GitHub:

```bash
# Generate a key (accept defaults, set a passphrase)
ssh-keygen -t ed25519 -C "your@email.com"

# Copy the public key
cat ~/.ssh/id_ed25519.pub

# Add it at: https://github.com/settings/keys
```

Test the connection:

```bash
ssh -T git@github.com   # should say: Hi <you>! You've successfully authenticated.
```

---

## Set up your workspace

### 1. Clone this repo

```bash
mkdir -p ~/projects
cd ~/projects
git clone git@github.com:goodtribes-org/agent.git goodtribes.org
cd goodtribes.org
```

The local directory is named `goodtribes.org` so all three sub-projects live side by side at consistent paths.

### 2. Clone the sub-project repos

Each sub-project is a separate git repository cloned into the monorepo directory:

```bash
# Swedish job marketplace
git clone git@github.com:goodtribes-org/kickfix.git kickfix

# Swedish refugee resource site
git clone git@github.com:goodtribes-org/asylguiden.se.git asylguiden.se
```

After cloning, the workspace should look like this:

```
~/projects/goodtribes.org/
├── .claude/commands/     ← AI skill definitions (this repo)
├── kickfix/              ← kickfix sub-project (separate git repo)
├── asylguiden.se/        ← asylguiden.se sub-project (separate git repo)
├── goodtribes.org/       ← future project (placeholder)
├── argocd/               ← Kubernetes/ArgoCD config
├── CLAUDE.md
└── README.md
```

### 3. (Optional) Add upstream remotes to sub-projects

The sub-projects also have upstream repos on a different org. Add them if you need to pull from or compare against the upstream:

```bash
git -C kickfix remote add upstream git@github.com:viodlar/kickfix.git
git -C asylguiden.se remote add upstream git@github.com:Hacking-Robots-and-Beer/asyulguiden.git
```

### 4. Open Claude Code

```bash
cd ~/projects/goodtribes.org
claude
```

The skills in `.claude/commands/` are available automatically because you opened Claude Code from this directory.

---

## Repository structure

| Repo | GitHub | Local path | Purpose |
|------|--------|------------|---------|
| agent | `goodtribes-org/agent` | `~/projects/goodtribes.org/` | This repo — skills, docs, ArgoCD config |
| kickfix | `goodtribes-org/kickfix` | `~/projects/goodtribes.org/kickfix/` | Job marketplace source code |
| asylguiden.se | `goodtribes-org/asylguiden.se` | `~/projects/goodtribes.org/asylguiden.se/` | Refugee resource site source code |

**Important:** The local `~/projects/goodtribes.org/` directory corresponds to `goodtribes-org/agent`, not `goodtribes-org/goodtribes.org`. The `git remote get-url origin` output from this directory is `git@github.com:goodtribes-org/agent.git`.

---

## Working with the agent repo

### Make changes to a skill

The skills live in `.claude/commands/`. Each file is a markdown document that instructs Claude Code how to run a workflow. Edit them like any other file.

```bash
# See what changed
git status
git diff .claude/commands/gh-apply.md
```

### Commit and push changes

```bash
# Stage specific files (avoid git add . which can pick up unexpected files)
git add .claude/commands/gh-apply.md CLAUDE.md README.md

# Commit — use a short present-tense summary
git commit -m "Add gh-apply worker for the apply stage"

# Push to the agent repo on GitHub
git push origin main
```

The agent repo has no CI pipeline — push directly to `main`. Teammates pull to get updated skills:

```bash
git pull origin main
```

### Keep skills in sync globally

To use these skills from any directory (not just the monorepo), copy them to your user config:

```bash
cp -r ~/projects/goodtribes.org/.claude/commands/. ~/.claude/commands/
```

Re-run this after pulling new skill updates.

---

## How the project board flow works

Every issue moves through six stages. The AI workers handle the automated stages; humans approve at two checkpoints before the next stage begins.

```
┌─────┐   ┌─────────┐   ┌──────┐   ┌────────┐   ┌───────┐   ┌──────┐
│ new │──▶│ request │──▶│ plan │──▶│ review │──▶│ apply │──▶│ test │
└─────┘   └─────────┘   └──────┘   └────────┘   └───────┘   └──────┘
    ↑           ↑            ↑           ↑            ↑
  You file   Worker      Human ✓      Worker       Human ✓    Worker
  the issue  posts       approves     posts        approves   opens PR
             outline     outline      plan         plan
```

### Stage by stage

| Stage | Who moves it there | What happens |
|-------|--------------------|--------------|
| **new** | You | Issue is filed and added to the board. Nothing else happens yet. |
| **request** | `/gh-request` (automatic) | Worker reads the issue, checks scope and sensitive data, posts a file-level outline as a comment. |
| **plan** | **You** | You review the outline comment. If it looks right, move the card to `plan`. This is your first approval checkpoint. |
| **review** | `/gh-plan` (automatic) | Worker reads the actual source files, writes a detailed implementation plan (file paths, function names, steps in dependency order) as a comment. |
| **apply** | **You** | You review the implementation plan comment. If it looks right, move the card to `apply`. This is your second and final approval checkpoint. |
| **test** | `/gh-apply` (automatic) | Worker creates a feature branch, implements all changes from the plan, pushes to `goodtribes-org/<repo>`, opens a pull request, and posts the PR link as a comment. |

After `test`, review and merge the PR on GitHub. Move the card to done manually (or use a GitHub Actions workflow triggered on merge).

### Human checkpoints in detail

**Checkpoint 1: `request → plan`**

Read the outline comment on the issue. Check:
- Does the scope make sense? (too large → break it up; too narrow → expand the description)
- Are the files identified correct?
- Any unexpected sensitive data or stack flags?

If yes — move the card to `plan` on the project board. If no — comment on the issue with corrections and leave the card at `request`. You can re-trigger `/gh-request` by moving it back to `new`.

**Checkpoint 2: `review → apply`**

Read the implementation plan comment. Check:
- Are the file paths real and specific?
- Are the steps in the right order (models before routes, routes before UI)?
- Does the verification section describe how to actually test the change?

If yes — move the card to `apply`. If no — comment with what needs to change. Move the card back to `plan` to re-trigger `/gh-plan`.

### What happens if a worker rejects an issue

Workers can move cards backwards automatically in two cases:

- **No valid sub-project label** (`kickfix`, `asylguiden.se`, or `goodtribes.org`) — card returned to the previous stage with an explanation comment.
- **Issue too large** (>10 files or 3+ major components) — card returned to `new` with a request to split it.

Check the issue comments to see why — there will always be an explanation.

### Multi-machine collision prevention

When multiple machines run `/gh-start` at the same time, workers use a `picked-by-<hostname>` GitHub label as a soft lock so two agents never work the same ticket.

**How it works:**

1. Before claiming a ticket, the worker checks its labels. Any issue already carrying a `picked-by-*` label is skipped.
2. Immediately after selecting an issue, the worker adds `picked-by-<your-hostname>` (amber colour) to the issue.
3. When the worker finishes and transitions the card to the next stage, the `picked-by-*` label is removed.

**Side benefit:** The label history on each issue shows which machine worked on it and when. If a worker crashes mid-flight the label stays visible so you can see the stuck ticket and clear it manually by removing the label.

---

## How to write a good issue

### Which repo to file in

File issues in the repo that owns the code being changed:

| Change | File in |
|--------|---------|
| Kickfix frontend or backend | `goodtribes-org/kickfix` |
| Asylguiden.se frontend, Strapi, or collector | `goodtribes-org/asylguiden.se` |
| Infrastructure, ArgoCD, Kubernetes | `goodtribes-org/deploy` |
| AI skills / workflow tooling | `goodtribes-org/agent` |

### Adding the issue to a project board

After filing the issue:

1. Open the issue on GitHub
2. In the right sidebar, click **Projects**
3. Select the matching board (`kickfix`, `asylguiden.se`, or `goodtribes.org`)
4. The issue appears on the board — set its status to **`new`**

The workers pick it up automatically on their next poll (within 5 minutes if `/gh-start` is running).

### Issue title

Use a plain action phrase describing the change:

```
Good:  Add pagination to the job listing page
Good:  Fix broken image upload on mobile
Good:  Translate /om-oss page to Arabic
Bad:   Various improvements
Bad:   Bug
Bad:   Phase 2 — complete authentication system overhaul
```

### Issue body

Write enough for someone unfamiliar with the code to understand what you want. The worker reads this to generate the outline. A weak body produces a weak outline.

Include:

- **What** — what should change or be added
- **Why** — why this matters (user need, bug report, business requirement)
- **Acceptance criteria** — how you'll know it's done

Example:

```markdown
## What
Add a "Save job" button to each job card on the listing page. Saved jobs should
appear in a new /saved route in the user's dashboard.

## Why
Users frequently ask to bookmark jobs they want to apply to later. Without this,
they lose track of interesting listings.

## Acceptance criteria
- Logged-in users can save and unsave any job with one click
- /saved shows all saved jobs for the current user
- Saving persists across sessions (stored in the database)
- Unauthenticated users see a "Log in to save" prompt instead
```

### What to avoid

- **Multiple unrelated changes in one issue** — the scope check will flag it. One deployable unit per issue.
- **Vague descriptions** — "improve performance" without saying which page, which metric, or what counts as improved.
- **Implementation instructions** — describe the desired outcome, not how to achieve it. The planner reads the code and figures out the how.
- **Sensitive data in the issue body** — do not paste credentials, real user data, or PII. The worker will flag this as a warning and a human must sign off before it proceeds.

### Sub-project label

The workers look for a label matching the sub-project name. If `/gh-request` cannot determine the sub-project from the issue text it will ask you to add the label manually.

To add it yourself upfront:
1. Open the issue
2. In the right sidebar, click **Labels**
3. Add `kickfix`, `asylguiden.se`, or `goodtribes.org`

This speeds things up and avoids a rejection loop.

---

## Running the workers

### Start everything at once

```bash
cd ~/projects/goodtribes.org
claude
```

Then in Claude Code:

```
/gh-start
```

This launches three background agents in parallel — one for each stage of the pipeline. All three poll every project board every 5 minutes.

```
gh-request — watching for 'new' issues   → moves to 'request', posts outline
gh-plan    — watching for 'plan' issues  → moves to 'review', posts detailed plan
gh-apply   — watching for 'apply' issues → implements code, opens PR, moves to 'test'
```

The workers run until you interrupt them from the Claude Code task list (Ctrl+C or the task panel).

### Run a single worker

```bash
/gh-request   # only the outline worker
/gh-plan      # only the planning worker
/gh-apply     # only the implementation worker
```

Useful when you want to process one stage without launching all three.

### Manual single-issue intake

```bash
/gh-intake
```

One-shot: claims the next unclaimed issue from the board matching the current repo and chains into `/gh-request`. Run from inside a sub-project directory if you want to target a specific board.

---

## Skills reference

| Command | Stage | What it does |
|---------|-------|--------------|
| `/gh-start` | — | Launches `/gh-request`, `/gh-plan`, and `/gh-apply` as three parallel background workers |
| `/gh-request` | `new → request` | Validates scope, sensitive data, and stack; posts file-level outline; moves card |
| `/gh-plan` | `plan → review` | Reads actual source files; writes detailed implementation plan; moves card |
| `/gh-apply` | `apply → test` | Creates feature branch; implements changes; opens PR on goodtribes-org repo; moves card |
| `/gh-intake` | `new → request` | One-shot: claims next unclaimed issue, chains to `/gh-request` |

---

## Project boards

| Project | Board URL |
|---------|-----------|
| goodtribes.org | https://github.com/orgs/goodtribes-org/projects/2 |
| kickfix | https://github.com/orgs/goodtribes-org/projects/3 |
| asylguiden.se | https://github.com/orgs/goodtribes-org/projects/4 |
