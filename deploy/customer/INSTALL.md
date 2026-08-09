# Lurus Tally — On-Premise Installation Guide

This guide is for deploying Lurus Tally on your own server from the source
code you were handed, using Docker Compose. It has no dependency on any
Lurus-hosted infrastructure, VPN, or internal network — everything runs on
your machine.

## 1. Prerequisites

| Requirement | Minimum version | Check with |
|---|---|---|
| Docker Engine | 24+ | `docker --version` |
| Docker Compose plugin | v2 | `docker compose version` |
| Free disk | 5 GB+ (images + Postgres/NATS data grow over time) | — |
| Open ports on the host | `18200` (API), `3000` (web UI) — or whatever you remap them to | — |

You do **not** need Go, Bun, or Node installed on the host — the backend and
web images are built inside Docker from the `Dockerfile` / `web/Dockerfile`
shipped in this repository.

## 2. First-time setup

Run all commands from the repository root (the directory containing this
repo's top-level `Dockerfile`).

```bash
# 1. Copy the environment template and fill in the blanks
cp deploy/customer/.env.customer.example deploy/customer/.env
# Edit deploy/customer/.env:
#   - POSTGRES_PASSWORD: pick a strong password
#   - AUTH_SECRET: generate with `openssl rand -base64 32`
#   - OIDC_ISSUER / OIDC_CLIENT_ID / OIDC_AUDIENCE: your identity provider's
#     values (any standards-compliant OIDC provider — Tally does not bind to
#     one vendor). Leave blank only if you intend to run with
#     TALLY_DEV_MODE=true (not recommended beyond a first smoke test).

# 2. Build the images from source and start everything
docker compose -f deploy/customer/docker-compose.customer.yml --env-file deploy/customer/.env up -d --build

# 3. Watch the backend come up (it runs embedded SQL migrations on first boot;
#    see "Initialization" below)
docker compose -f deploy/customer/docker-compose.customer.yml logs -f backend
```

The first boot will:
1. Wait for Postgres/Redis/NATS to report healthy (compose `depends_on` +
   healthchecks handle the ordering).
2. Run all embedded database migrations (see "Initialization").
3. Start the HTTP API on `:18200` inside the container, published to the
   host port you set as `TALLY_BACKEND_PORT` (default `18200`).

## 3. Initialization (database migrations)

Tally embeds all SQL migrations into the backend binary
(`migrations/embed.go`, using `golang-migrate`) — there is no separate
migration container or manual step required for a fresh install.

- Controlled by `MIGRATE_ON_BOOT` (default `true` in
  `.env.customer.example`): the backend runs pending migrations against
  `DATABASE_DSN` every time it starts, before serving traffic.
- **Idempotent**: already-applied migrations are skipped; re-running the
  container (e.g. after a restart) is always safe.
- If you prefer a DBA-reviewed migration step instead of auto-run-on-boot,
  set `MIGRATE_ON_BOOT=false` and apply migrations manually before starting
  the backend:
  ```bash
  # Requires DATABASE_DSN to point at a reachable Postgres (e.g. after
  # `docker compose up -d postgres`), and Go installed on the operator's
  # machine — or run this one-off inside the backend build stage.
  DATABASE_DSN="postgres://tally:<password>@localhost:5432/lurus?sslmode=disable&search_path=tally" \
    make migrate-up
  ```

## 4. Verification

After `docker compose up -d --build` reports all services started:

```bash
# Backend health check (expects HTTP 200)
curl -i http://localhost:${TALLY_BACKEND_PORT:-18200}/internal/v1/tally/health
# Expected body: {"service":"lurus-tally","status":"ok","version":"..."}

# Web UI reachable (expects HTTP 200 or a redirect to the login page)
curl -i http://localhost:${TALLY_WEB_PORT:-3000}/
```

If the health check does not return `200` within ~30 seconds of container
start, check logs:

```bash
docker compose -f deploy/customer/docker-compose.customer.yml logs backend
docker compose -f deploy/customer/docker-compose.customer.yml logs postgres
```

Common first-boot issues:
- `OIDC_AUDIENCE is required when OIDC_ISSUER is set` — fill in both, or
  unset `OIDC_ISSUER` and set `TALLY_DEV_MODE=true` for a throwaway smoke
  test (never in a real deployment: this disables auth on the entire API).
  Note: this only unblocks direct backend API calls with trusted headers —
  the web UI's login button still needs a real OIDC IdP; see "5. 登录
  (Login)" below.
- Postgres connection refused — the `postgres` healthcheck has not passed
  yet; `depends_on: condition: service_healthy` should already wait for
  this, but slow disks can exceed the default 20 retries × 5 s. Increase
  `retries` in `docker-compose.customer.yml` if needed.

Note: the backend's runtime container is built `FROM scratch` (no shell, no
`curl`/`wget` inside it) to minimize attack surface, so there is no
container-level `HEALTHCHECK` on the `backend` service itself — verify
liveness from the host with the `curl` command above, or point your own
monitoring at the same endpoint.

## 5. 登录 (Login)

**A working OIDC identity provider is a hard prerequisite for logging into
the web UI in this deployment form.** The customer Docker image builds the
web container with `NODE_ENV=production`, which unconditionally disables
its offline dev auth path (see `TALLY_DEV_MODE` note above and the "Known
limitations" note at the end of this section) — there is no bundled local
account system to fall back to.

### 5.1 The three required env vars

Set these in `deploy/customer/.env` (all currently blank in
`.env.customer.example`):

| Variable | Read by | Purpose |
|---|---|---|
| `OIDC_ISSUER` | backend + web | Your IdP's issuer URL (OIDC discovery document must be reachable at `<issuer>/.well-known/openid-configuration`). |
| `OIDC_AUDIENCE` | backend | Expected `aud` claim on the JWTs the backend validates. |
| `OIDC_CLIENT_ID` | web | The OAuth client ID NextAuth uses to start the login flow (pair with `OIDC_CLIENT_SECRET` if your IdP requires a confidential client). |

Leaving any of these blank does not crash the container — the login page
still renders — but clicking "sign in" fails with a NextAuth error because
there is no IdP to redirect to.

**Port linkage**: if you remap the web port away from the `3000` default,
`TALLY_WEB_PORT`, `TALLY_WEB_PUBLIC_URL`, and `NEXTAUTH_URL` in `.env` must
all change together (see the comment block above `TALLY_WEB_PORT` in
`.env.customer.example`), and the redirect URI registered at your IdP must
match the new `TALLY_WEB_PUBLIC_URL`. A stale value on any one of these
four breaks the OIDC callback.

### 5.2 Minimal walkthrough (generic OIDC provider — e.g. Keycloak or Casdoor)

Tally's web login (`web/auth.ts`) uses NextAuth's generic `type: "oidc"`
provider with discovery — any standards-compliant OIDC IdP works, nothing
is hardcoded to one vendor.

1. In your IdP, create an OIDC client (Keycloak: a "Client" inside a
   Realm; Casdoor: an "Application"). Set its redirect/callback URI to
   `${TALLY_WEB_PUBLIC_URL}/api/auth/callback/oidc` — with the defaults
   that is `http://localhost:3000/api/auth/callback/oidc`.
2. Note the values your IdP shows you: the issuer URL (Keycloak:
   `https://<idp-host>/realms/<realm>`; Casdoor: your application's OIDC
   issuer, typically `https://<idp-host>`), the client ID, and — if the
   client is confidential — the client secret.
3. Fill in `deploy/customer/.env`:
   ```
   OIDC_ISSUER=<issuer URL from step 2>
   OIDC_AUDIENCE=<audience your IdP issues — often the client ID; check your IdP's docs>
   OIDC_CLIENT_ID=<client ID from step 2>
   # OIDC_CLIENT_SECRET=<client secret from step 2, if confidential client>
   ```
4. Recreate the backend and web containers so they pick up the new env:
   ```bash
   docker compose -f deploy/customer/docker-compose.customer.yml --env-file deploy/customer/.env up -d --build backend web
   ```
5. Open `${TALLY_WEB_PUBLIC_URL}` and click sign-in — you should be
   redirected to your IdP, authenticate there, and land back on the Tally
   dashboard.

### Known limitation: no built-in local accounts

This customer deployment does **not** ship an "out-of-the-box local
account" login path (username/password stored in Tally itself, no external
IdP). Whether to build one is an owner product decision that has not been
made — this INSTALL.md will not silently implement it. If you need to
evaluate Tally's functionality before standing up an IdP, the supported
path is to run from source in a non-customer deployment form (e.g. `bun
run dev` under `web/`, where `NODE_ENV` is not `production`) with
`AUTH_DEV_PROVIDER=true`, which enables an offline Credentials provider for
local testing only. That escape hatch is intentionally unavailable in this
Docker Compose customer image — see `web/auth.ts`'s `devProviderEnabled()`
production hard-block.

## 6. Upgrading

```bash
# 1. Pull/copy the new source tree (new git tag or tarball from your vendor).
# 2. Rebuild and recreate only the app images; dependency data volumes
#    (Postgres/Redis/NATS) are untouched.
docker compose -f deploy/customer/docker-compose.customer.yml --env-file deploy/customer/.env up -d --build backend web

# 3. New migrations embedded in the upgraded backend binary run automatically
#    on boot (same mechanism as first-time setup, step 3). Take a database
#    backup first (see below) if the release notes call out schema changes.
```

## 7. Backup & Restore

Three named volumes hold all persistent state: `tally_pgdata`,
`tally_redisdata`, `tally_natsdata`. Postgres is authoritative; Redis is a
cache; NATS JetStream holds in-flight event data — back up at minimum
Postgres before any upgrade.

**Backup (Postgres logical dump — recommended, restorable across versions):**

```bash
docker compose -f deploy/customer/docker-compose.customer.yml exec -T postgres \
  pg_dump -U ${POSTGRES_USER:-tally} -d ${POSTGRES_DB:-lurus} --format=custom \
  > tally-backup-$(date +%Y%m%d-%H%M%S).dump
```

**Restore:**

```bash
# Stop the backend so it does not write during restore.
docker compose -f deploy/customer/docker-compose.customer.yml stop backend

cat tally-backup-YYYYMMDD-HHMMSS.dump | \
  docker compose -f deploy/customer/docker-compose.customer.yml exec -T postgres \
  pg_restore -U ${POSTGRES_USER:-tally} -d ${POSTGRES_DB:-lurus} --clean --if-exists

docker compose -f deploy/customer/docker-compose.customer.yml start backend
```

**Full volume-level backup** (covers Redis/NATS too; use if you cannot
tolerate any downtime-free logical restore path):

```bash
docker compose -f deploy/customer/docker-compose.customer.yml stop
docker run --rm -v customer_tally_pgdata:/data -v $(pwd):/backup alpine \
  tar czf /backup/tally-pgdata-$(date +%Y%m%d).tar.gz -C /data .
# Repeat for customer_tally_redisdata / customer_tally_natsdata as needed.
docker compose -f deploy/customer/docker-compose.customer.yml start
```

(Volume names are prefixed with the Compose project name — `customer` by
default when running from `deploy/customer/`; confirm with
`docker volume ls | grep tally`.)

## 8. Stopping / Tearing down

```bash
# Stop containers, keep data volumes
docker compose -f deploy/customer/docker-compose.customer.yml down

# Stop and DELETE all data (irreversible — back up first)
docker compose -f deploy/customer/docker-compose.customer.yml down -v
```
