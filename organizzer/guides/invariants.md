# Security and safety invariants

These hold for the life of the project. A change that breaks one is rejected
however convenient it is. Flag any issue whose implementation would require
breaking one — the flag goes into the outline for a human to decide, it does not
by itself reject the issue.

## goodtribes.org

1. **Never `prisma migrate dev`.** It diffs the *live* database against the
   migration history and will `DROP SCHEMA public CASCADE` on anything it reads
   as drift. On 2026-07-15 a non-interactive run did exactly that and wiped
   every table, with no backup and no seed script to restore from. The rule is
   permanent regardless of what else changes in the schema. A schema change is:
   edit `frontend/prisma/schema.prisma` → `prisma migrate diff` against a
   throwaway shadow DB → review the SQL by hand and strip anything unrelated →
   hand-create `prisma/migrations/<timestamp>_<name>/migration.sql` →
   `npx prisma migrate deploy`. Several migrations in the repo are already
   hand-crafted this way.
2. **`SitePage` is editorial copy, not a data model.** One row per slug of
   static site text, sanitised through `sanitizeHtml()` on both save and render,
   gated on `isSiteAdmin()`. A plan that models a product concept as a
   `SitePage` row is wrong.
3. **The Meilisearch key that reaches the browser is read-only.**
   `NEXT_PUBLIC_MEILI_SEARCH_KEY` is a public search key; the master key never
   leaves the cluster.
4. **Every AI feature degrades, never crashes.** All of them are gated on
   `ANTHROPIC_API_KEY`; when it is unset the feature is simply unavailable.
5. **Token and money flows are ledger-first.** `TokenLedger`, `GtLedger`,
   `ImpactFundLedger`, `ProfitDistribution` and `PersonalProfitAllocation` are
   append-only records of what happened. Never mutate a ledger row to correct a
   balance — write a compensating entry.
6. **Site-admin-executed transitions stay site-admin-executed.** Sandbox
   graduation, legal-type changes, profit distributions and exclusion cases all
   end in a decision a human with the right role makes. A plan that automates
   the decision itself, rather than the paperwork around it, is wrong.

## kickfix

1. **Every authenticated request goes through `utils/apiFetch.js`.** It is the
   single place the JWT is attached. A component calling `fetch` directly will
   silently drop auth.
2. **JWT verification lives in `middleware/auth.js`, applied per route group.**
   A new protected route adds the middleware; it does not re-implement token
   parsing.
3. **A record that exists but is not yours returns 404, not 403.** Job,
   message and transaction lookups must not confirm the existence of another
   user's data.
4. **Uploads are validated by the multer config in `middleware/upload.js`.**
   Never take a user-supplied string as a file path or a filename on disk.
5. **`npx prisma generate` after every `backend/prisma/schema.prisma` change.**
   MongoDB via Prisma needs a replica set; a plan that assumes a standalone
   mongod is wrong.

## asylguiden.se

1. **NextAuth's user database and Strapi's are separate.** The frontend's
   Prisma/NextAuth tables are not Strapi's `up_users`. Never join or migrate
   between them.
2. **Route protection is `middleware.ts`.** `/bookmarks` and `/profile` are
   guarded there on the session cookie; a page-level check is not a substitute.
3. **The collector writes only through the Strapi API**, authenticated with
   `COLLECTOR_STRAPI_API_TOKEN`. It never touches PostgreSQL directly.
4. **This site serves refugees.** Anything that stores, logs or exposes a
   user's country of origin, asylum status, case number or location is sensitive
   by default — flag it, do not design it silently.

## All projects

- **Never touch cluster networking.** Do not add or change any URL or hostname,
  and do not edit the cluster ingress — Traefik routes, Helm ingress values, or
  ArgoCD/manifest ingress. Surface the need instead.
- **Infrastructure changes need explicit human approval**: a new service, a new
  database, a new cache or queue, a change to `docker-compose.yml` service
  topology, a new volume, or a new Helm/deployment component.
- **Smallest change that satisfies the request.** One focused change at a time,
  not a large multi-concern edit.
- **Secrets are never committed and never rendered into the GitOps repo.**
  Production values live in a Kubernetes Secret in the project's namespace
  (`goodtribes-secret`, and the equivalents for kickfix and asylguiden), managed
  outside git. A plan that puts a real value in `chart/values.yaml` is wrong.
- **The developer has no cluster access.** A plan whose verification step
  requires `kubectl` against production cannot be verified and must say so.

## Sensitive data

Report — do not reject — when the work touches any of:

- passwords or password hashing
- payment card or bank data (PCI scope) — this includes the Stripe paths in
  `goodtribes.org`
- the PII triple: full name **and** email **and** physical address together
- health or medical data
- government identifiers (personnummer, passport numbers, national IDs)
- asylum status, country of origin, or case identifiers on `asylguiden.se`
- anything that changes the 404-vs-403 boundary, or who can read whose records

Flagging means the outline says so and names the reason; a human decides whether
storage and encryption need documenting before the work proceeds.
