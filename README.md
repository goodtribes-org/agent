# goodtribes-org/agent

Shared [Claude Code](https://claude.ai/code) skills for the Goodtribes team. Clone this repo to get the `/gh-start`, `/gh-request`, `/gh-plan`, and `/gh-intake` commands that run the AI-assisted issue planning pipeline.

---

## Prerequisites

### Accounts
- **GitHub** — member of the [goodtribes-org](https://github.com/goodtribes-org) organisation
- **Claude** — active [Claude Pro or Teams](https://claude.ai) subscription (background agents require a paid plan)
- **Anthropic API key** — from [console.anthropic.com](https://console.anthropic.com) → API keys

### Tools

```bash
# GitHub CLI — used by every skill to read/write issues and project boards
brew install gh          # macOS
sudo apt install gh      # Ubuntu/Debian

# Claude Code CLI — the AI coding tool that runs the skills
npm install -g @anthropic-ai/claude-code
```

### Authenticate

```bash
gh auth login
export ANTHROPIC_API_KEY=sk-ant-...   # add to ~/.bashrc or ~/.zshrc to persist
```

---

## Installation

Clone this repo alongside the monorepo and copy the commands into your Claude config:

```bash
cd ~/projects
git clone git@github.com:goodtribes-org/agent.git
```

The skills are picked up automatically when you open Claude Code from inside this directory. To make them available globally (from any directory), copy them to your user config:

```bash
cp -r ~/projects/agent/.claude/commands/. ~/.claude/commands/
```

The monorepo at `~/projects/goodtribes.org/` also ships these commands in its own `.claude/commands/` — they are kept in sync with this repo.

---

## Skills

### `/gh-start` — launch all workers

```
/gh-start
```

Launches `/gh-request` and `/gh-plan` as two parallel background agents. Both run continuously, polling all three project boards every 5 minutes. This is the only command you need to start the full pipeline — one run covers kickfix, asylguiden.se, and goodtribes.org simultaneously.

Run this from `~/projects/goodtribes.org/`.

---

### `/gh-request` — outline planner

```
/gh-request
```

Watches for issues in **`new`** status across all project boards. For each one:

1. Identifies which sub-project it targets (`kickfix`, `asylguiden.se`, or `goodtribes.org`)
2. Runs three checks:
   - **Scope** — too large? (>10 files, multiple phases) → returned to `new` with a breakdown request
   - **Sensitive data** — PII, payment data, health data, government IDs → flagged as a warning
   - **Stack** — new database, cache, or queue not already in the project → flagged as a warning
3. Reads the root `CLAUDE.md` and the sub-project's `CLAUDE.md` for context
4. Posts an outline as a GitHub comment:
   - Scope estimate, sensitive data and stack flags
   - Ordered implementation steps (file-level)
   - Testing instructions
5. Moves the card to **`request`**

The card stays at `request` until a human moves it to `plan` to approve the outline.

---

### `/gh-plan` — implementation planner

```
/gh-plan
```

Watches for issues in **`plan`** status across all project boards. For each one:

1. Reads the outline comment posted by `/gh-request`
2. Explores the actual source files in the monorepo (uses `ls` to confirm paths before referencing them)
3. Writes a detailed implementation plan as a GitHub comment:
   - **Background** — what the issue asks for and why
   - **Implementation steps** — file-by-file changes in dependency order, with exact function names, route paths, and class names
   - **Code notes** — naming conventions, patterns to reuse, things to avoid
   - **Verification** — exact commands to run, expected output, edge cases
4. Moves the card to **`review`**

The card stays at `review` until a human moves it to `apply` to approve the plan.

---

### `/gh-intake` — manual issue claim

```
/gh-intake
```

A one-shot skill that claims the next unclaimed issue from the project board matching the current repo, then chains into `/gh-request`. Use this to manually pick up a specific issue rather than waiting for the background workers.

Run from inside a git repository — it detects the repo and matching project board automatically.

---

## Typical workflow

```
1. File a feature request issue in the relevant repo
2. Add the issue to the project board, set status to "new"
3. Run /gh-start in Claude Code (from ~/projects/goodtribes.org/)
4. gh-request posts an outline → card moves to "request"
5. Review the outline → move card to "plan"
6. gh-plan writes the implementation plan → card moves to "review"
7. Review the plan → move card to "apply"
8. Run /ticket to implement, or do it manually
```

Human decisions are required at steps 5 and 7. Everything else is automated.

For the full workflow reference and setup guide see the [deploy repo docs](https://github.com/goodtribes-org/deploy/tree/main/docs).

---

## Project boards

| Project | Board |
|---------|-------|
| goodtribes.org | https://github.com/orgs/goodtribes-org/projects/2 |
| kickfix | https://github.com/orgs/goodtribes-org/projects/3 |
| asylguiden.se | https://github.com/orgs/goodtribes-org/projects/4 |
