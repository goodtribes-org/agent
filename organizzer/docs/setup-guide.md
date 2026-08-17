# organizzer: what it is, how it came about, and how to run one for another project

This is the document to read before changing organizzer, and before standing
one up for a different project board. The `README.md` describes what the thing
does today; this describes **why it is shaped the way it is**, which is mostly a
record of what went wrong the first day it ran for real.

---

## Part 1 — What it does

Three workers, one binary, one Deployment each, all `replicas: 1`:

| Worker | Moves | Produces |
|---|---|---|
| `organizzer request` | `new` → `request` | a **Request Outline** — scope, sensitive data, stack and invariant checks |
| `organizzer plan` | `plan` → `review` | an **Implementation Plan** — file-level steps an agent executes verbatim |
| `organizzer apply` | `apply` → `test` | a **task dispatched** to kube-foundry, which runs a coding agent and opens a PR |

Each stage loops: list the board → filter to its column → order → take **one**
card → post the artifact → move the card → sleep.

### The parts that are load-bearing

**Two gates stay human.** `request → plan` approves an outline; `review → apply`
approves a plan. The board client refuses both targets outright, and a test runs
every stage over every column to prove none of them tries. Everything else in
this document is negotiable; that is not.

**The comment goes up before the card moves.** The status transition is the
commit point. A crash in between leaves the card in its input column with the
artifact already posted, and the sentinel line in that artifact makes the redo
free. The reverse order can strand a card in a column that has an approval gate
in front of it and no artifact to approve.

**Sentinels are a contract.** `render.SentinelPlan` is how the apply stage finds
the approved plan, and each stage decides whether it has already run by looking
for its own. The slash-commands write the identical strings, so artifacts from
either producer are recognised by both — that is what keeps `/gh-plan` usable as
a fallback. Do not "tidy" those strings.

**One card per cycle.** Draining a column in one pass means a bad prompt or a
broken webhook damages every card before anyone can look.

**`replicas: 1` per stage.** The card's status is the lock. A second replica
reads the same column at the same moment and posts twice — and for the apply
stage that is two sandboxes, two pull requests and twice the spend, because the
webhook names every task `task-<unixMilli>` and cannot reject a duplicate.

---

## Part 2 — How it was built, and what production taught it

Written from scratch on 2026-08-12: board client, selector, three stages, the
LLM client, render templates, chart, CI. All of it green in tests before it ever
saw the board. Then it ran, and the first day found five real bugs — none of
which a unit test would have caught, because every one was about the world
rather than the code.

### The three that were the same bug

The loop's default assumption is "this failed, try again later". That is right
for a network blip and wrong for a decision, and three separate failures were
all *decisions* being retried forever, at model prices, until a human noticed.

1. **Starvation, twice.** The blocker check costs an API call per card, so a
   poll checks at most 20 candidates. But the scan restarted at the head every
   time — so a board whose first 20 cards were blocked reported "every candidate
   is blocked" while 81 unexamined cards sat behind the window. The first fix
   moved the starvation point from position 1 to position 20 rather than
   removing it; the real fix is a **moving window** that resumes where the last
   poll stopped and resets to the head after a successful pick. A completed lap
   with nothing workable then drops back to the idle interval, because otherwise
   the sweep spends 20 blocker checks every 10 seconds re-learning that a
   blocked column is still blocked.

2. **A timeout that could never be met** — see below.

3. **Truncation.** `finish_reason: length` came back as a plain error, so the
   card cycled between backoff and an identically overlong answer. It is now a
   decision: comment, `needs-human`, hand the card back to `request`.

**Rule to carry forward:** when adding a failure path, ask whether a retry could
plausibly produce a different result. If not, it is a decision — comment, label,
return the card. Never leave it to the backoff.

### The one that was infrastructure pretending to be code

Plans simply never appeared. The visible errors were
`Client.Timeout exceeded while awaiting headers` and
`connection reset by peer` — both of which read like a network fault, and
neither of which was one.

What was actually true: **berget terminates a response that has sent nothing for
about 60 seconds.** A non-streaming completion sends nothing at all until it
finishes, so every plan taking over a minute to generate was impossible to
obtain. Measured from two networks:

```
non-streaming max_tokens=4000/8000/12000   reset at 60.1s / 61.4s / 61.4s
STREAMING     max_tokens=12000             200 in 168.7s, 4878 chunks
```

Before that measurement, the timeout was raised from 300s to 900s on the
reasonable-sounding theory that a long plan needed longer. The arithmetic behind
it (12000 tokens ÷ ~21 tokens/sec ≈ 570s) was correct and irrelevant. **One
`curl` would have saved the detour** — and a wrong fix also destroys the
evidence for the next attempt.

Streaming details that are easy to get wrong:

- **`data: [DONE]` is the completeness check.** A stream that stops early is a
  connection that died mid-sentence, and the deltas in hand are indistinguishable
  from a short answer. Without insisting on the terminator, a half-written plan
  gets posted as though the model meant to stop there — and the apply stage hands
  that to an agent to execute verbatim.
- **Ask for usage.** A stream omits it unless `stream_options.include_usage` is
  set, and it arrives in a final chunk carrying **no choices**, so anything
  looping over choices alone drops it.
- **Reasoning is charged to your budget and is not your answer.** GLM-5.2 spends
  roughly a fifth of the completion thinking, delivered as `reasoning_content`
  deltas rather than `content`.

### The one that was the image

The first dispatched task failed with `Agent produced no changes`. The sandbox
was Debian 12 with `git`, `jq`, `gh` and `curl` — enough to read and edit a
repository, not to build one — and the plan's first step was
`npx shadcn@latest init`. The agent spent its run hunting for a way to install
Node, was refused permission to read `/etc`, and gave up.

An agent that cannot run the project's own build cannot tell whether what it
wrote compiles. Every pull request before that fix was a guess.

### What the prompts taught

`prompts/plan.md` asked for "the specifics the implementer cannot guess" and
never said how much that should be. On a vague issue the model expands
indefinitely — a *simpler* prompt than the first real card produced 22,469
characters. The plan is now capped at twelve steps, with instructions to set
`blocked` and suggest a split rather than write a thirteenth. A plan that
overruns is not a long answer; it is **no answer**, because it arrives truncated
and gets thrown away.

---

## Part 3 — Standing one up for another project

Roughly half a day, most of it credentials and board wiring.

### 1. The board

A GitHub Project (v2) with a `Status` single-select carrying **six** options in
this order: `new`, `request`, `plan`, `review`, `apply`, `test`. Optional but
used by the request stage's ordering: `Phase` (`M0 …` — the number is what
sorts), `Track`, `Size` (`S`/`M`/`L`), and a `Blocked by` text field holding
`repo#123, repo#456`.

Nothing needs pinning by id: the workers discover the project, the status field
and every option by name at startup, so a board edit does not need a redeploy.
The `STATUS_OPTION_*` pins exist only for when discovery breaks, and it is **all
six or none** — a half-pinned map would let discovery fill the blanks around one
typo'd pin and move cards into the wrong column silently.

### 2. The repository copy

Copy the repo, then change:

- `chart/values.yaml` — `github.org`, `github.projectNumber`, `namespace`,
  `image.repository`, and the model names if the endpoint differs.
- `ALLOWED_REPOS` — the short names this board is allowed to touch. A card
  pointing anywhere else is rejected rather than planned.
- `PRIORITY_TRACKS` — which tracks sort ahead of the rest.
- **`guides/`** — this is the part that actually matters. `working-root.md` is a
  copy of the working root's `CLAUDE.md`; `invariants.md` holds the security
  rules that must never be traded away; `stacks.md` holds the per-repo build
  gates. These are pasted into every prompt, and they are what makes the request
  stage reject an infrastructure change and the plan stage refuse to touch
  cluster networking. A generic copy produces generic, unsafe plans.
  `make sync-guides` refreshes the first from the working root.
- `prompts/` — keep the response schemas; rewrite the project-specific
  instructions. Keep the size cap.

### 3. Credentials

| What | Where | Notes |
|---|---|---|
| `GITHUB_TOKEN` | `organizzer-secret` in the namespace | **Classic PAT**, scopes `repo` + `project`. ProjectsV2 is GraphQL-only and fine-grained tokens handle org projects awkwardly. |
| `BERGET_API_KEY` | same secret | Only the request and plan stages use it; apply calls no model. |
| image pull secret | same namespace | Pull secrets do not cross namespaces. Copy an existing one rather than minting a new PAT. |
| agent credentials | the **agent's** namespace | For kube-foundry, `factory-creds-goodtribes` in `kubefoundry`, with the model key under `ANTHROPIC_API_KEY` — the operator picks the env name per agent and returns that for everything except codex. `secretRef` does not cross namespaces. |

Verify the model key before deploying, since a wrong model id fails at runtime
rather than at startup:

```bash
curl -s https://api.berget.ai/v1/models -H "Authorization: Bearer $KEY" | jq -r '.data[].id'
```

### 4. The agent sandbox

Whatever runs the implementation needs the toolchain of the repos it will touch
— Node for a Next.js repo, Go for a Go repo, a compiler for anything with native
npm modules. Check before trusting it:

```bash
docker run --rm -u 1000:1000 --entrypoint bash <image> -c 'node -v; go version; python3 -V'
```

Run that as the **unprivileged uid the sandbox uses**, not as root: caches and
global-install prefixes must live somewhere that user owns, or `npm i -g` and
the Go build cache fail partway through a task rather than at startup.

### 5. First run, in this order

```bash
./bin/organizzer request -dry-run -once   # what would it pick, what would it post
./bin/organizzer apply   -dry-run -once   # the exact webhook body, unsent
```

Then deploy with **`stages.apply.enabled: false`** and a real cap:

```yaml
llm:
  maxIssuesPerHour: 10      # an uncapped request stage sweeps 90 cards in ~20 minutes
stages:
  apply:
    enabled: false          # turn on only after the other two have run on real cards
```

Read the first few outlines before letting it run unattended. Enable `apply`
last — it is the only stage that spends money outside the cluster.

### 6. Pitfalls worth knowing in advance

- **The board `Status` option names must match** `config.AllStatuses` exactly.
  Discovery matches by name.
- **Keep deploy-time `--set` flags out of the habit.** Anything not in
  `values.yaml` is invisible to the next person who renders the chart, and what
  runs stops matching what the repo says runs.
- **A rejection needs its label.** Without `needs-human` the selector picks the
  card up on the next poll, reaches the same conclusion and comments again,
  every ten seconds.
- **`test` does not mean a pull request exists.** The card moves when the
  webhook acknowledges the task, well before the agent has done anything.
  Nothing reconciles a card in `test` against a real PR;
  `FOUNDRY_CALLBACK_URL` is the hook for that and nothing consumes it yet.
- **Sandbox logs may contain the push credential.** Check before pointing a log
  shipper at that namespace.
