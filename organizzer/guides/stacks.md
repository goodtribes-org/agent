# Sub-projects, stacks and build gates

Three product repos under the `goodtribes-org` org, cloned as siblings under a
working root. The directory name always equals the repo short name. A fourth
repo, `agent`, holds the shared tooling — including this service — and a fifth,
`deploy`, is the GitOps manifest repo that only CI writes to.

Each project board watches exactly one repo:

| Board | Repo | What it is | Stack |
|---|---|---|---|
| #2 | `goodtribes.org` | Platform connecting skilled volunteers with impact-driven organisations | Next.js 16 (App Router) + React + Prisma/PostgreSQL + Meilisearch + Redis + MinIO |
| #3 | `kickfix` | Swedish freelance job marketplace | React 19 (CRA) + Express + Prisma/MongoDB |
| #4 | `asylguiden.se` | Multi-language refugee resource site | Next.js 16 + Strapi 5 + PostgreSQL + Meilisearch |

## Build gates

The command that proves a change works. The agent sandbox has **no Docker
daemon**, so the `docker compose build` step the working root prescribes for a
human cannot be the gate here — these are the npm scripts each repo declares.

| Repo | Gate |
|---|---|
| `goodtribes.org` | `npm ci && npm run lint --workspace=frontend && npm run build:frontend` |
| `asylguiden.se` | `npm ci && npm run lint --workspace=frontend && npm run build:frontend` |
| `kickfix` | `(cd frontend && npm ci && npm run build) && (cd backend && npm ci && npx prisma generate && npm test)` |
| `agent` | `cd organizzer && go vet ./... && go test ./...` |

`goodtribes.org` and `asylguiden.se` are npm **workspace roots** — the root
`package.json` has no `lint`/`build` of its own, only `build:frontend` style
delegators, so a plan that says "run `npm run build`" at the root is wrong.
`kickfix`'s root `package.json` is literally `{}`: `frontend/` and `backend/`
are two independent packages and each needs its own `npm ci`.

## Per-stack rules a plan must respect

**goodtribes.org (Next.js 16 + Prisma/Postgres).** App Router under
`frontend/src/app/`, internationalised with `next-intl` — every user-facing page
route lives under `src/app/[locale]/`, while `src/app/api/**`, `manifest.ts`,
`sw.ts`, `robots.ts`, `sitemap.ts` and `storage/` stay outside `[locale]`. Auth
is NextAuth v5 with a Prisma adapter and Resend magic links (`src/auth.ts`).
All product and auth tables share one `public` Postgres schema defined in
`frontend/prisma/schema.prisma`. Editorial copy (About/Privacy/Terms and further
footer pages) lives in the `SitePage` model and is edited inline by site admins
— it is strictly for static copy, so a plan must never model a new product
concept as a `SitePage` row. Search goes straight from the browser to
Meilisearch with a read-only public key. Every AI feature is gated on
`ANTHROPIC_API_KEY` and must degrade to "feature unavailable", never to a crash.
After implementing, `npx tsc --noEmit` from `frontend/` must be clean.

**kickfix (React CRA + Express + Prisma/MongoDB).** Two packages. The frontend
keeps its JWT in `context/AuthContext.jsx` and every request goes through
`utils/apiFetch.js`, which attaches the Bearer token and handles `FormData` — a
plan that calls `fetch` directly from a page is wrong. The backend mounts four
route groups under `/api` (`auth`, `jobs`, `messages`, `payments`) from
`backend/routes/`, with JWT verification in `middleware/auth.js` and multer
uploads in `middleware/upload.js` served at `/uploads/*`. Prisma uses the
`mongodb` provider and needs a replica set; **`npx prisma generate` from
`backend/` is required after any change to `backend/prisma/schema.prisma`** and
belongs as an explicit step in the plan.

**asylguiden.se (Next.js 16 + Strapi 5 + collector).** Three npm workspaces:
`frontend/`, `backend/` (Strapi 5, TypeScript, content types `article`,
`category`, `tag`, `faq`, `homepage`) and `collector/` (a Node cron service that
scrapes UNHCR/Eurostat/SCB/Migrationsverket/Frontex and POSTs to the Strapi
API). `frontend/middleware.ts` owns locale routing and guards `/bookmarks` and
`/profile` on a NextAuth session cookie. NextAuth here has its **own user
database, separate from Strapi's** — do not conflate the two. Collector →
Strapi writes need an API token (`COLLECTOR_STRAPI_API_TOKEN`); a plan that has
the collector writing to Postgres directly is wrong.

## Ordering

Implementation steps go in dependency order: migration → Prisma schema →
server-side data access → route handler / server action → component. A plan that
edits a component before the migration that gives it a column is wrong.

For `goodtribes.org` specifically, a schema change is *three* steps, not one:
edit `schema.prisma`, hand-create the reviewed migration directory, then
`npx prisma migrate deploy`. See `invariants.md` — `prisma migrate dev` is
forbidden in that repo and has already destroyed the database once.
