# organizzer

Board workers for the three goodtribes-org project boards. One Go binary, three
workers, one per automated transition:

| Worker | Moves | What it does |
|---|---|---|
| `organizzer request` | `new` → `request` | Reads the issue and the live repository, writes a **Request Outline** |
| `organizzer plan` | `plan` → `review` | Expands the approved outline into an **Implementation Plan** |
| `organizzer apply` | `apply` → `test` | Hands the approved plan to kube-foundry's OpenCode agent |

A process drives exactly one board, identified by `GITHUB_ORG` +
`PROJECT_NUMBER`. Three boards therefore mean three sets of workers, which the
chart renders by ranging over `boards` × `stages`:

| Board | Number | Repo it may touch |
|---|---|---|
| [goodtribes.org](https://github.com/orgs/goodtribes-org/projects/2) | 2 | `goodtribes.org` |
| [kickfix](https://github.com/orgs/goodtribes-org/projects/3) | 3 | `kickfix` |
| [asylguiden.se](https://github.com/orgs/goodtribes-org/projects/4) | 4 | `asylguiden.se` |

This replaces the `/gh-request`, `/gh-plan` and `/gh-apply` slash-commands in
`../.claude/commands/`, which only run while someone has a terminal open. Those
files remain the reference for the logic and the fallback if these workers are
down — and the two can run side by side: the workers skip any card carrying a
`picked-by-*` label, and both write the same sentinel lines, so neither will
re-do the other's work.

`docs/setup-guide.md` covers why it is shaped this way — including what the
first day in production changed — and how to stand one up for another project
board.

## The two human checkpoints

Moving a card from `request` to `plan` approves an outline. Moving it from
`review` to `apply` approves a plan. **No worker can perform either.** The
board client refuses those two targets outright, and a test runs every stage
over every column to prove none of them tries.

## How a worker behaves

Each stage runs its own Deployment at `replicas: 1` and loops:

```
list the board
  → filter to its column, skipping closed, archived, needs-human,
    picked-by-* and recently-failed cards
  → order them (request stage only: phase → track → size → issue number)
  → take ONE card
  → post the artifact, then move the card
  → sleep 10s   (60s if there was nothing to do)
```

Three things are worth understanding before changing any of it.

**The comment goes up before the card moves.** The status transition is the
commit point. A crash in between leaves the card in its input column with the
artifact already posted, and the sentinel line in that artifact makes the redo
free. The reverse order — which `/gh-request` used — can strand a card in a
column that has an approval gate in front of it and no artifact to approve.

**The sentinels are a contract.** `render.SentinelPlan` is how the apply stage
finds the approved plan, and each stage decides whether it has already run by
searching for its own. For the apply stage this is the *only* duplicate guard:
the kube-foundry webhook names every task `task-<unixMilli>`, so a second
submission is a second agent run — two sandboxes, two pull requests, twice the
spend — not something the server can reject.

**The blocker scan is a moving window, not a prefix.** Checking whether a card
is blocked costs an API call, so a poll checks at most twenty candidates. That
bound cannot start at the head every time: the kickfix board has ninety-odd
cards and most may be blocked, so a scan that always restarted at the top
would report "every candidate is blocked" while eighty cards it never looked at
sat behind the window — the same starvation as stopping at the first blocked
card, twenty places further down. A poll that finds nothing resumes where the
last one stopped; a poll that picks a card resets to the head, so priority still
decides between cards that can actually be worked. A lap that comes all the way
round without finding anything means the column is blocked rather than busy, and
the loop goes back to the idle interval instead of spending twenty checks every
ten seconds to keep learning that.

**A rejection needs the `needs-human` label to stick.** Without it the selector
picks the card up on the next poll, reaches the same conclusion and comments
again, every ten seconds, spending tokens each time. Removing the label is the
human's "I have dealt with this" signal.

**`test` does not mean a pull request exists.** The card moves when the webhook
acknowledges the task, which is well before the agent has done anything. The
hand-off comment says so explicitly. If the agent fails, the card sits in
`test` with no pull request; `kubectl -n kubefoundry get st <task>` says why.

## Building

There is no local Go toolchain by design — everything runs in a container.

```bash
make build        # compile into bin/
make test         # go test ./...
make vet          # go vet ./...
make images       # build the container image
make sync-guides  # refresh guides/working-root.md from the working root's CLAUDE.md
```

## Running it locally

Both read-only modes need only a GitHub token.

```bash
export GITHUB_TOKEN=ghp_...

# What would the request stage pick, and what would it post?
./bin/organizzer request -dry-run -once

# Print the exact JSON body the apply stage would POST to the webhook.
./bin/organizzer apply -dry-run -once
```

The second is worth replaying by hand before letting the apply stage run
unattended:

```bash
kubectl -n kubefoundry port-forward svc/kube-foundry-webhook 8080:80
curl -X POST localhost:8080/api/v1/tasks -H 'Content-Type: application/json' -d @body.json
```

## Configuration

Everything is an environment variable. Credentials are the only ones with no
default.

### GitHub

| Variable | Default | Notes |
|---|---|---|
| `GITHUB_TOKEN` | *required* | Classic PAT with `repo` + `project`. ProjectsV2 is GraphQL-only and fine-grained tokens handle org projects awkwardly. |
| `GITHUB_ORG` | `goodtribes-org` | |
| `GITHUB_API_URL` | `https://api.github.com` | GraphQL is derived as `…/graphql` |
| `PROJECT_NUMBER` | `2` | The chart always sets this per board (2, 3, 4) |
| `ALLOWED_REPOS` | the three product repos | Comma-separated short names. The chart narrows it to one repo per board |
| `PRIORITY_TRACKS` | *(empty)* | No goodtribes board has a Track field, so ordering falls through to phase, size, issue number |

### Board id pins

Normally unset: the workers discover the project, the Status field and every
option id by name at startup, so a board edit does not need a redeploy. Set
these only when a board change breaks discovery and the workers need to run
again immediately. **All six status options or none** — a half-pinned map is
rejected, because discovery filling the blanks around one typo'd pin would
move cards into the wrong column silently.

Current values, one column per board (recorded 2026-08-17):

| Variable | #2 goodtribes.org | #3 kickfix | #4 asylguiden.se |
|---|---|---|---|
| `PROJECT_NODE_ID` | `PVT_kwDOEJlAe84BVva7` | `PVT_kwDOEJlAe84BV11B` | `PVT_kwDOEJlAe84BV11C` |
| `STATUS_FIELD_ID` | `PVTSSF_lADOEJlAe84BVva7zhRJDLY` | `PVTSSF_lADOEJlAe84BV11BzhROp_A` | `PVTSSF_lADOEJlAe84BV11CzhROp_4` |
| `STATUS_OPTION_NEW` | `c5cc8b96` | `766c7a1a` | `63ad2d7c` |
| `STATUS_OPTION_REQUEST` | `c4d28c89` | `ae3b3020` | `5bf802f9` |
| `STATUS_OPTION_PLAN` | `332e4610` | `2ff8d4bc` | `4d006ed6` |
| `STATUS_OPTION_REVIEW` | `6baca8ca` | `ca6d6e09` | `f26fe873` |
| `STATUS_OPTION_APPLY` | `32bc77b6` | `35b47905` | `595598b9` |
| `STATUS_OPTION_TEST` | `70d772ed` | `7406cc44` | `8dd2c33f` |

Field *names* are configurable too, for a rename: `FIELD_NAME_STATUS`,
`FIELD_NAME_PHASE`, `FIELD_NAME_TRACK`, `FIELD_NAME_SIZE`,
`FIELD_NAME_BLOCKED_BY` (default `Blocked by`).

### Loop

| Variable | Default | Notes |
|---|---|---|
| `POLL_BUSY_SECONDS` | `10` | Between issues |
| `POLL_IDLE_SECONDS` | `60` | When the column is empty |
| `ITEM_BACKOFF_SECONDS` | `900` | How long a transiently failed card is skipped |
| `BLOCKER_CACHE_SECONDS` | `300` | Only *closed* blockers are cached — an open one can close at any moment |
| `MAX_ISSUES_PER_HOUR` | `0` (no cap) | Worth setting before turning the plan stage loose on a full column |
| `ITEM_PAGE_SIZE` | `100` | |
| `MAX_ISSUE_COMMENTS` | `50` | `comments(last:)` |
| `REPO_TREE_MAX_ENTRIES` | `400` | |
| `REPO_BLOB_MAX_BYTES` | `32768` | Per file |
| `PLAN_FILE_FETCH_MAX` | `8` | Source files the plan stage reads on its second pass |
| `NEEDS_HUMAN_LABEL` | `needs-human` | |
| `CLAIM_LABEL` | *(empty)* | Informational only. The workers never claim; they do stand aside for a `picked-by-*` label so a human running the old slash-commands is not trampled. |
| `HEALTH_LISTEN` | `:8080` | `/healthz`, `/readyz` |
| `LOG_LEVEL` | `info` | `debug` dumps prompts and replies |
| `DRY_RUN` | `false` | Same as `-dry-run` |

### Berget (request and plan stages)

| Variable | Default |
|---|---|
| `BERGET_API_KEY` | *required* |
| `BERGET_BASE_URL` | `https://api.berget.ai/v1` |
| `BERGET_TIMEOUT_SECONDS` | `900` |
| `MODEL_REQUEST` | `mistralai/Mistral-Small-3.2-24B-Instruct-2506` |
| `MODEL_PLAN` | `zai-org/GLM-5.2` |
| `LLM_TEMPERATURE` | `0.1` |
| `LLM_MAX_TOKENS_REQUEST` | `4000` |
| `LLM_MAX_TOKENS_PLAN` | `12000` |
| `LLM_MAX_ATTEMPTS` | `3` |
| `LLM_JSON_MODE` | `false` |

The request stage produces a short structured verdict and gets the cheap
model. The plan stage produces the artifact an autonomous agent executes
verbatim and gets the better one. The apply stage calls no model at all.

**Calls are streamed, and that is not an optimisation.** berget terminates a
non-streaming response at about sixty seconds: the socket is reset with no
status and no body, which surfaces as `connection reset by peer` or as
`Client.Timeout exceeded while awaiting headers` depending on where the clock
lands. Both read like a network fault and neither is one. Measured from two
different networks, `max_tokens` of 4000, 8000 and 12000 all died at 60–63s,
while the identical request with `"stream": true` returned 200 in 168s over
4878 chunks. A plan takes minutes to generate, so before streaming every plan
long enough to be worth having was impossible to obtain, and no client-side
timeout could have changed that.

Two consequences worth keeping in mind:

- **`data: [DONE]` is the completeness check.** A stream that stops early is a
  connection that died mid-sentence, and the deltas already in hand look exactly
  like a short answer. Without insisting on the terminator, a half-written plan
  would be posted as though the model had meant to stop there.
- **Token counts need asking for.** A stream omits the usage block unless
  `stream_options.include_usage` is set; it arrives in a final chunk carrying no
  choices, which anything looping over choices alone will skip.

`BERGET_TIMEOUT_SECONDS` still bounds the whole call as a backstop, and
`HEALTH_STALE_FOR` follows it automatically — the readiness window is widened
past whatever call the loop is allowed to make, so a stage waiting on the model
is not reported unready for doing its job.

### kube-foundry (apply stage)

| Variable | Default |
|---|---|
| `KUBEFOUNDRY_WEBHOOK_URL` | `http://kube-foundry-webhook.kubefoundry.svc.cluster.local` |
| `KUBEFOUNDRY_WEBHOOK_PATH` | `/api/v1/tasks` |
| `KUBEFOUNDRY_WEBHOOK_SECRET` | *(empty)* |
| `FOUNDRY_AGENT` | `open-code` |
| `FOUNDRY_SKILLS` | `berget` |
| `FOUNDRY_SECRET_REF` | `factory-creds-goodtribes` |
| `FOUNDRY_BRANCH` | `main` |
| `FOUNDRY_MAX_RETRIES` | `1` |
| `FOUNDRY_GIT_AUTHOR_NAME` | `organizzer` |
| `FOUNDRY_GIT_AUTHOR_EMAIL` | `organizzer@goodtribes.org` |
| `FOUNDRY_CALLBACK_URL` | *(empty)* |

Four things about that webhook, all verified against kube-foundry v0.1.0:

- The body is **flat**. `secretRef` is top level, not nested under
  `credentials` the way the SoftwareTask CRD nests it — and the handler drops
  unknown fields silently, so getting this wrong runs the task with the wrong
  secret rather than failing.
- There is **no `resources` field**. CPU, memory and timeout come from the CRD
  defaults (2 CPU, 4Gi, 30 min) and can only be changed by creating the custom
  resource directly.
- Its defaults are wrong for this cluster — `agent` defaults to `claude-code`,
  which speaks an API berget does not serve, and `secretRef` to
  `factory-creds`, which does not exist. Both are always sent explicitly.
- **The submit route has no authentication.** Anything that can reach the
  Service can spend model credits. That is why organizzer runs in the same
  cluster rather than reaching it over an ingress.

`FOUNDRY_SKILLS` order matters: the operator appends each skill's env in list
order and Kubernetes resolves duplicate names to the *last* one. Never list two
berget model-override skills together.

## Deploying

GitOps, the same path every other goodtribes project takes. Two workflows in the
repo root drive it:

1. **Organizzer Docker Publish** vets, tests, builds and pushes
   `ghcr.io/goodtribes-org/agent/organizzer:<sha>`. The package is private,
   like every other one in this org, so the `organizzer` namespace needs
   `ghcr-pull-secret` — `../argocd/create-pull-secrets.sh` creates it there
   along with the three product namespaces.
2. **Organizzer Deploy To Production** renders this chart with that sha and
   commits `organizzer/organizzer.yaml` to `goodtribes-org/deploy`, where the
   `organizzer` ArgoCD Application picks it up.

Both are filtered to `organizzer/**`, so a commit that only touches the skills
in `../.claude/commands/` does not redeploy the workers.

The render carries **no Secret**. Every entry under `.Values.secrets` is left
empty and — unlike the elino original — is never passed with `--set`, because
that would commit a GitHub PAT and a Berget key in plaintext to a public
manifest repo. Create it once, by hand, before the first rollout:

```bash
kubectl --kubeconfig ~/.kube/confighrb -n organizzer \
  create secret generic organizzer-secret \
  --from-literal=GITHUB_TOKEN=ghp_... --from-literal=BERGET_API_KEY=sk-...
```

ArgoCD prunes this app, but the secret is safe: the chart never renders it, so
it is not something the app believes it owns.

To render locally exactly as CI does:

```bash
helm template organizzer chart/ --set image.tag=$(git rev-parse HEAD)
```

The image tag is the commit sha on purpose: `latest` means nobody can tell what
is running.

`llm.maxIssuesPerHour` is `10` in `values.yaml` rather than the binary's
uncapped default — the kickfix board alone holds 94 cards, and an uncapped
request stage works through the lot in about twenty minutes.

### Rollout order

`values.yaml` ships with **only the request stage enabled**. Turn them on one at
a time, reading what each produces before the next:

1. `request` — three pods, one per board. Read the outlines.
2. `plan` — `--set stages.plan.enabled=true` in `values.yaml`, committed.
3. `apply` — last, and only after ops has approved installing kube-foundry in
   this cluster. It is the only stage that spends money outside the cluster:
   each card it takes starts a sandbox pod running a coding agent.

The apply stage additionally needs `factory-creds-goodtribes` in the
`kubefoundry` namespace:

```bash
# The berget key goes in ANTHROPIC_API_KEY. That is not a typo: the operator
# picks the env name via agentAPIKeyName(), which returns ANTHROPIC_API_KEY for
# every agent except codex. secretRef does not cross namespaces, so this must
# live in the same namespace as the task.
kubectl --kubeconfig ~/.kube/confighrb -n kubefoundry \
  create secret generic factory-creds-goodtribes \
  --from-literal=ANTHROPIC_API_KEY=<berget key> --from-literal=GITHUB_TOKEN=<PAT>
```

kube-foundry is **not installed in this cluster today**. Standing it up means a
new namespace, a `SoftwareTask` CRD, a cluster-scoped operator and sandbox pods
that default to 2 CPU / 4Gi — an infrastructure change, which the working root's
rules say needs explicit approval before anyone writes it.


## Known gaps

- Plan quality is below what `/gh-plan` produced: that read a real checkout,
  this sees a capped tree plus eight fetched files. Steps naming files that do
  not exist are dropped before the plan is posted, but the ceiling is lower.
- `guides/working-root.md` is a copy of `../CLAUDE.md`. `make sync-guides`
  refreshes it; a stale copy means an invariant quietly stops being checked.
- The build gates in `internal/foundry/request.go` cannot run `docker compose
  build`, which is what the working root tells a human to run. The sandbox has
  no Docker daemon, so they are the npm and go commands instead — a change that
  only breaks at image-build time will not be caught.
- There is no reconciliation between a card in `test` and an actual pull
  request. `FOUNDRY_CALLBACK_URL` is the hook for that; nothing consumes it yet.
