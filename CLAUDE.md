# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository layout

This is a monorepo containing two independent web applications and a placeholder:

| Directory | Project | Stack |
|-----------|---------|-------|
| `kickfix/` | Swedish freelance job marketplace | React 19 (CRA) + Express + MongoDB (Prisma) |
| `asylguiden.se/` | Swedish refugee resource site | Next.js 16 + Strapi 5 + PostgreSQL + Meilisearch |
| `goodtribes.org/` | Future project (empty) | — |

Each project has its own `docker-compose.yml` and deploys independently via GitHub Actions → ghcr.io → GitOps manifest repo.

> **Note:** This local directory (`~/projects/goodtribes.org/`) is the `goodtribes-org/agent` GitHub repo — not `goodtribes-org/goodtribes.org`. The git remote points to `git@github.com:goodtribes-org/agent.git`.

---

## GitHub Projects & AI workflow

Three project boards under the goodtribes-org org, one per project:

| Board | Number | URL |
|-------|--------|-----|
| goodtribes.org | #2 | https://github.com/orgs/goodtribes-org/projects/2 |
| kickfix | #3 | https://github.com/orgs/goodtribes-org/projects/3 |
| asylguiden.se | #4 | https://github.com/orgs/goodtribes-org/projects/4 |

All boards share the same 6-stage status workflow: `new → request → plan → review → apply → test`

### Agent commands (`.claude/commands/`)

| Command | What it does |
|---------|-------------|
| `/gh-start` | Launches `/gh-request`, `/gh-plan`, and `/gh-apply` as parallel background workers covering all three boards |
| `/gh-request` | Polls `new` issues → validates scope/stack/sensitive data → posts outline → moves to `request` |
| `/gh-plan` | Polls `plan` issues → reads codebase → writes file-level implementation plan → moves to `review` |
| `/gh-apply` | Polls `apply` issues → reads plan comment → creates feature branch → implements changes → opens PR → moves to `test` |
| `/gh-intake` | One-shot: claims next unclaimed issue from the board matching the current repo, chains to `/gh-request` |

### Starting the workers

Run from this directory in Claude Code:

```
/gh-start
```

Human checkpoints: move card `request → plan` to approve an outline; move `review → apply` to approve a plan. Workers never cross these transitions automatically.

`/gh-apply` opens a pull request on the `goodtribes-org` org repo and moves the card to `test`. Review and merge the PR manually.

---

## Cluster / Kubernetes

- **Kubeconfig**: `~/.kube/confighrb` — always use `--kubeconfig ~/.kube/confighrb` (the default `~/.kube/config` points to a different cluster)
- **Namespaces**: `kickfix`, `asylguiden`, `goodtribes`
- **Pull secret**: `ghcr-pull-secret` (kubernetes.io/dockerconfigjson) deployed in all three namespaces for pulling from `ghcr.io`
- **Recreate pull secrets**: `GHCR_USERNAME=x GHCR_TOKEN=y ./argocd/create-pull-secrets.sh`

---

## Kickfix

Swedish job marketplace for posting/accepting freelance gigs with in-app messaging and payment tracking. See `kickfix/CLAUDE.md` for full details.

### Quick commands

```bash
# Docker (recommended)
cd kickfix && docker compose up --build
# Frontend → http://localhost:3003  Backend → http://localhost:5000/api

# Without Docker
cd kickfix/backend && cp .env.example .env && npm install && npx prisma generate && npm start
cd kickfix/frontend && npm install && npm start
```

### Architecture

- **Frontend** (`kickfix/frontend/src/`): React Router 7 SPA. `context/AuthContext.jsx` holds JWT (localStorage). `utils/apiFetch.js` auto-attaches Bearer token.
- **Backend** (`kickfix/backend/`): Express server (`index.js`). Four route groups under `/api`: `auth`, `jobs`, `messages`, `payments`. JWT middleware in `middleware/auth.js`. Multer uploads in `middleware/upload.js`, served at `/uploads/*`.
- **Database**: MongoDB via Prisma ORM (requires replica set). Models: `User`, `Job`, `Message`, `Transaction`. Run `npx prisma generate` from `kickfix/backend/` after schema changes.
- **Deployment**: Helm chart at `kickfix/chart/kickfix/`. Traefik ingress: `kickfix.se` → frontend, `api.kickfix.se` → backend. MongoDB as StatefulSet. Images pushed to `ghcr.io/viodlar/kickfix/`.

---

## Asylguiden.se

Multi-language content hub (Swedish, Arabic, Farsi, etc.) for refugees, backed by a headless Strapi CMS and automated data collectors.

### Quick commands

```bash
# Docker (recommended)
cd asylguiden.se && docker compose up --build
# Frontend → http://localhost:3000  Strapi → http://localhost:1337

# npm workspaces
cd asylguiden.se
npm run dev:frontend        # Next.js dev server (:3000)
npm run dev:backend         # Strapi dev server (:1337)
npm run dev:collector       # Collector in watch mode
npm run dev:services        # Start postgres + meilisearch only

npm run build:frontend
npm run build:backend
npm run lint --workspace=frontend   # ESLint (Next.js + TypeScript config)

# One-time collector run
npx --workspace=collector npm run dev:once
npx --workspace=collector npm run dev:once -- --collector=unhcr
```

### Architecture

**Frontend** (`asylguiden.se/frontend/`):
- Next.js 16 App Router. Pages under `src/app/[locale]/` for i18n (next-intl).
- `middleware.ts` handles locale routing and protects `/bookmarks` and `/profile` with NextAuth session cookie check.
- Auth: NextAuth.js 5 with Prisma adapter (separate user DB from Strapi).
- Search: Meilisearch client.
- Forms: react-hook-form + Zod.
- Tailwind CSS 4. `@/*` path alias maps to `src/`.

**Backend / CMS** (`asylguiden.se/backend/`):
- Strapi 5 (TypeScript). Content types: `article`, `category`, `tag`, `faq`, `homepage`.
- PostgreSQL database. Meilisearch plugin syncs content for full-text search.
- API token required for collector → Strapi writes. Set `STRAPI_API_TOKEN` / `COLLECTOR_STRAPI_API_TOKEN` in `.env`.

**Collector** (`asylguiden.se/collector/`):
- Node.js cron service. Scrapes government migration statistics weekly (UNHCR, Eurostat, SCB, Migrationsverket, Frontex) and POSTs to Strapi API.
- Each collector runs Monday mornings staggered by hour (06:00–11:00 UTC).
- Cheerio for HTML scraping, ExcelJS for spreadsheet parsing, Pino for logging, Zod for validation.

**CI/CD**: GitHub Actions builds Docker images tagged with commit SHA, pushes to `ghcr.io`, then triggers deployment by committing Helm-rendered manifests to the manifest repo (GitOps).

### Required environment variables for docker-compose

Create `asylguiden.se/.env` (not committed):
```
POSTGRES_USER, POSTGRES_PASSWORD, POSTGRES_DB
MEILI_MASTER_KEY, MEILI_SEARCH_KEY
STRAPI_APP_KEYS, STRAPI_API_TOKEN_SALT, STRAPI_ADMIN_JWT_SECRET
STRAPI_TRANSFER_TOKEN_SALT, STRAPI_JWT_SECRET
STRAPI_API_TOKEN, COLLECTOR_STRAPI_API_TOKEN
NEXTAUTH_URL, NEXTAUTH_SECRET
```
