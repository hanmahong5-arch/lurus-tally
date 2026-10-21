# Lurus Tally — On-Premise Installation Guide

## 当前形态说明（请先读）

私有化包目前是**有条件交付**形态，与下文“无任何 Lurus 依赖”的字面表述有以下差异：

- 需自带 OIDC 身份服务（Zitadel / Keycloak 等，须兼容 OIDC id_token）；暂无内置本地账号。
- 多成员（团队）与计费功能依赖 Lurus 平台：私有化部署中团队功能返回 503，计费不可用，且永不锁店（不会因计费状态限制使用）。
- AI 助手需自行配置模型网关，未配置时 AI 功能不可用。
- standalone（本地账号）模式为条件触发项，**尚未提供**。

---

This guide is for deploying Lurus Tally on your own server from the source
code you were handed, using Docker Compose. It has no dependency on any
Lurus-hosted infrastructure, VPN, or internal network — everything runs on
your machine.

## 1. Prerequisites

| Requirement | Minimum version | Check with |
|---|---|---|
| Docker Engine | 24+ | `docker --version` |
| Docker Compose plugin | v2 (≥ 2.20 if using Path A's `--pull never`) | `docker compose version` |
| Free disk | 5 GB+ (images + Postgres/NATS data grow over time) | — |
| Free RAM | ~1.5 GB for the 5 running containers at idle; **≥ 4 GB** if you are building from source (Path B — Go/Node compilation is memory-hungry) | — |
| Open ports on the host | `18200` (API), `3000` (web UI) — or whatever you remap them to | — |
| Outbound network | **Path A (delivered image tarball): none.** **Path B (build from source): required** — `docker.io` (Go/Bun/Node base images), `proxy.golang.org` (Go module fetch), `registry.npmjs.org` (frontend deps) | — |

## What your delivery contains

Depending on how your vendor packaged this release, you have either:
- **Path A**: a `tally-app-<version>.tar.gz` (backend + web images,
  pre-built), optionally a `tally-deps-<date>.tar.gz` (Postgres/Redis/NATS
  images — needed only on a fully offline host that cannot pull them
  itself), a `SHA256SUMS` file, and this source tree.
- **Path B only**: just this source tree, with no image tarball. You must
  build from source (see "2. First-time setup — Path B") on a host with the
  outbound network access listed above.

If in doubt, check for a `.tar.gz` file next to this guide — its presence
means Path A is available and preferred (faster, no build-time network
dependency).

## 2. First-time setup

Run all commands from the repository root (the directory containing this
repo's top-level `Dockerfile`).

```bash
# 1. Copy the environment template and fill in the blanks
cp deploy/customer/.env.customer.example deploy/customer/.env
```

Edit `deploy/customer/.env`:
- `POSTGRES_PASSWORD`: generate with `openssl rand -hex 24` (see the comment
  above this variable in `.env.customer.example` for the character
  restrictions and why).
- `AUTH_SECRET`: generate with `openssl rand -base64 32`.
- `OIDC_ISSUER` / `OIDC_CLIENT_ID` / `OIDC_AUDIENCE`: your identity
  provider's values (any standards-compliant OIDC provider — Tally does not
  bind to one vendor). **You must decide one of two things here** — see
  "Common first-boot issues" below if you skip this:
  - fill in `OIDC_ISSUER` + `OIDC_AUDIENCE` (and `OIDC_CLIENT_ID` for the web
    login button — see "5. 登录 (Login)"), or
  - set `TALLY_DEV_MODE=true` for a throwaway smoke test (never in a
    deployment reachable by real users).

### Path A — load the delivered image tarball (no build, no network)

```bash
# A1. Verify the tarball(s) you were handed are intact.
sha256sum -c SHA256SUMS
# Every listed file must print "OK". Stop here if any prints "FAILED" —
# do not load a tarball that fails its checksum.

# A2. Load the images into the local Docker image store.
docker load -i tally-app-<version>.tar.gz
# On a fully offline host, also load the dependency images:
docker load -i tally-deps-<date>.tar.gz

# A3. `docker load` prints one "Loaded image: <tag>" line per image. Copy the
# backend and web tags EXACTLY (including the registry prefix, if any) into
# deploy/customer/.env:
#   TALLY_BACKEND_IMAGE=<tag from the backend "Loaded image:" line>
#   TALLY_WEB_IMAGE=<tag from the web "Loaded image:" line>

# A4. Start everything WITHOUT building. Both flags matter: --pull never
# stops compose from reaching a registry, and --no-build stops it from
# silently falling back to a source build when a tag is mistyped — with
# only --pull never, a wrong TALLY_*_IMAGE value triggers a full build.
# A mistyped tag now fails fast with "No such image", which is what you want.
docker compose -f deploy/customer/docker-compose.customer.yml --env-file deploy/customer/.env up -d --pull never --no-build
```

Continue at "4. Verification" below.

### Path B — build from source (needs Docker + the outbound network above)

```bash
# Build the images from source and start everything
docker compose -f deploy/customer/docker-compose.customer.yml --env-file deploy/customer/.env up -d --build

# Watch the backend come up (it runs embedded SQL migrations on first boot;
# see "3. Initialization" below)
docker compose -f deploy/customer/docker-compose.customer.yml logs -f backend
```

You do **not** need Go, Bun, or Node installed on the host — the backend and
web images are built inside Docker from the `Dockerfile` / `web/Dockerfile`
shipped in this repository. You **do** need the outbound network access
listed in "1. Prerequisites" at build time only.

Either path, the first boot will:
1. Wait for Postgres/Redis/NATS to report healthy (compose `depends_on` +
   healthchecks handle the ordering).
2. Run all embedded database migrations (see "3. Initialization").
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

After `up -d` reports all services started:

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

### Automated acceptance

The two `curl` checks above are a quick smoke test; `acceptance.sh` runs a
fuller check with explicit PASS/FAIL/UNKNOWN results (UNKNOWN never counts
as a pass) and a timestamped report file:

If you started the stack with `-p NAME`, run it as
`COMPOSE_PROJECT_NAME=NAME bash deploy/customer/acceptance.sh` — check 6
recreates the stack under that project name and refuses to run when the
project has nothing running, so it can never start a second copy by mistake.

```bash
bash deploy/customer/acceptance.sh
# or, if you moved the ports: bash deploy/customer/acceptance.sh <backend_url> <web_url>
```

| # | Checks | Catches |
|---|---|---|
| 1 | Backend `/health` 200 with the right body, plus a bogus path does NOT 200 | A proxy or wrong service answering in its place |
| 2 | Backend `/ready` 200 and not `degraded` | A dependency (e.g. Redis) silently unreachable while the app still answers `/health` |
| 3 | Web `/login` 200, `/api/auth/session` 200, anonymous `/` redirects to `/login` | Web auth misconfigured: `UntrustedHost` (no `AUTH_TRUST_HOST`) or `InvalidEndpoints` (no `OIDC_ISSUER`/`OIDC_CLIENT_ID`, see section 5) — both answer 500 on the session endpoint while `/` still redirects, so a status-only check would pass them |
| 4 | Unauthenticated request to a business API is rejected (401/403) | Auth gate not actually engaging |
| 5 | Real write, then read-back that finds the written content | A write endpoint that 200s but drops the data |
| 6 | Data survives full-stack `up -d --force-recreate` (including Postgres) | A volume that isn't actually mounted — `restart` alone would hide this |
| 7 | Exactly 5 services (`backend nats postgres redis web`) report `running` | A dependency container that exited without taking the app down |

Checks 5 and 6 need write credentials: if `TALLY_DEV_MODE=true` in your
`.env`, the script uses trusted headers automatically; with a real OIDC
deployment, export `TALLY_ACCEPT_BEARER=<a valid JWT>` before running the
script, or those two checks report `UNKNOWN` (not a failure — just
untested).

Exit code is the number of FAIL + UNKNOWN lines (`0` = everything PASS).

Common first-boot issues:
- `/api/auth/session` answers 500 and the web logs contain `UntrustedHost` —
  the web container is missing `AUTH_TRUST_HOST=true`. The shipped compose
  file sets it; if you run the image by hand or maintain your own compose,
  add it. Symptom for users: an anonymous visit to `/` is bounced to
  `/setup` instead of `/login`, and sign-in never completes.
- `OIDC_ISSUER is required when TALLY_DEV_MODE is not true` (or similar) —
  the default `.env.customer.example` combination (`TALLY_DEV_MODE=false`
  and `OIDC_ISSUER` blank) makes the backend **refuse to start**, and it
  will crash-loop forever under `restart: unless-stopped`. Either fill in
  `OIDC_ISSUER` + `OIDC_AUDIENCE`, or set `TALLY_DEV_MODE=true` for a
  throwaway smoke test (never in a real deployment: this disables auth on
  the entire API). Note: dev mode only unblocks direct backend API calls
  with trusted headers — the web UI's login button still needs a real OIDC
  IdP; see "5. 登录 (Login)" below.
- `password authentication failed` (backend logs) — usually means
  `POSTGRES_PASSWORD` contains a character that broke URL/DSN parsing (see
  the comment in `.env.customer.example`), or a same-named variable
  (`POSTGRES_PASSWORD`, `AUTH_SECRET`, etc.) is exported in your shell and
  is overriding the `--env-file` value during interpolation. Run `env |
  grep -E '^(POSTGRES_|AUTH_SECRET|TALLY_)'` and `unset` anything that
  shows up before retrying.
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
4. Recreate the backend and web containers so they pick up the new env
   (use the command for the path you installed with — the Path B form
   would start a source build on a Path A machine):
   ```bash
   # Path A (delivered images):
   docker compose -f deploy/customer/docker-compose.customer.yml --env-file deploy/customer/.env up -d --pull never --no-build backend web
   # Path B (built from source):
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

**Path A:**
```bash
# 1. Verify and load the new image tarball (same as "2. First-time setup — Path A").
sha256sum -c SHA256SUMS
docker load -i tally-app-<new-version>.tar.gz

# 2. Update TALLY_BACKEND_IMAGE / TALLY_WEB_IMAGE in deploy/customer/.env to
#    the new tags, then recreate only the app containers — no --build, and
#    dependency data volumes (Postgres/Redis/NATS) are untouched.
docker compose -f deploy/customer/docker-compose.customer.yml --env-file deploy/customer/.env up -d --pull never --no-build backend web
```

**Path B:**
```bash
# 1. Pull/copy the new source tree (new git tag or tarball from your vendor).
# 2. Rebuild and recreate only the app images; dependency data volumes
#    (Postgres/Redis/NATS) are untouched.
docker compose -f deploy/customer/docker-compose.customer.yml --env-file deploy/customer/.env up -d --build backend web
```

Either path: new migrations embedded in the upgraded backend binary run
automatically on boot (same mechanism as first-time setup, step 3). Take a
database backup first (see below) if the release notes call out schema
changes.

## 7. Backup & Restore

Three named volumes hold all persistent state: `tally_pgdata`,
`tally_redisdata`, `tally_natsdata`. Postgres is authoritative; Redis is a
cache; NATS JetStream holds in-flight event data — back up at minimum
Postgres before any upgrade.

**Backup (Postgres logical dump — recommended, restorable across versions):**

The `${POSTGRES_USER}` / `${POSTGRES_DB}` below are expanded by *your* shell,
not by compose — load the same `.env` first, or the defaults after `:-` are
what gets used and the dump silently targets the wrong role/database:

```bash
set -a; . deploy/customer/.env; set +a
docker compose -f deploy/customer/docker-compose.customer.yml exec -T postgres \
  pg_dump -U ${POSTGRES_USER:-tally} -d ${POSTGRES_DB:-lurus} --format=custom \
  > tally-backup-$(date +%Y%m%d-%H%M%S).dump
```

**Restore:**

```bash
# Stop the backend so it does not write during restore.
docker compose -f deploy/customer/docker-compose.customer.yml stop backend

set -a; . deploy/customer/.env; set +a   # same reason as in Backup above
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
default when running from `deploy/customer/`, or whatever you passed via
`-p`/`COMPOSE_PROJECT_NAME`; confirm with `docker volume ls | grep tally`.)

## 8. Stopping / Uninstalling

```bash
# Stop containers, keep data volumes
docker compose -f deploy/customer/docker-compose.customer.yml down

# Stop and DELETE all data (irreversible — back up first)
docker compose -f deploy/customer/docker-compose.customer.yml down -v
```

**Full uninstall** (also removes the built/loaded images):

```bash
docker compose -f deploy/customer/docker-compose.customer.yml down -v --remove-orphans

# Remove the app images (adjust tags if you used TALLY_BACKEND_IMAGE / TALLY_WEB_IMAGE)
docker rmi lurus-tally-backend:customer lurus-tally-web:customer
# Path A dependency images, if loaded for an offline host:
docker rmi pgvector/pgvector:pg16 redis:7-alpine nats:2.10-alpine

# Confirm nothing is left (all three should print nothing):
docker ps -a --filter label=com.docker.compose.project=customer -q
docker volume ls -q | grep '^customer_tally_'
docker network ls --format '{{.Name}}' | grep '^customer_tally_'
```

## Appendix: producing the delivery tarball (vendor-side)

This section is for whoever packages a release, not for the customer.

```bash
# 1. Pull the exact backend/web image build you intend to ship.
docker pull <registry>/<backend-image>:<tag>
docker pull <registry>/<web-image>:<tag>

# 2. Save the app images into one tarball.
docker save <registry>/<backend-image>:<tag> <registry>/<web-image>:<tag> \
  | gzip > tally-app-<version>.tar.gz

# 3. (Optional, for fully offline hosts) save the dependency images too.
docker save pgvector/pgvector:pg16 redis:7-alpine nats:2.10-alpine \
  | gzip > tally-deps-<date>.tar.gz

# 4. Generate checksums the customer will verify before loading anything.
sha256sum tally-app-<version>.tar.gz tally-deps-<date>.tar.gz > SHA256SUMS
```

Ship the tarball(s), `SHA256SUMS`, this source tree, and this guide.

## 接到同机的其它 Lurus 产品(可选)

`.env` 里的 `PLATFORM_BASE_URL` / `NEWAPI_BASE_URL` / `MEMORUS_BASE_URL` 留空时,Tally 独立运行,
依赖这些的功能各自优雅降级。要接上时,**填什么值**取决于对方装在哪:

| 对方在哪 | 填什么 |
|---|---|
| **同一台机、另一个 compose 项目**(最常见:各产品各自一套) | `http://host.docker.internal:<对方发布的宿主端口>`,例如 Platform 默认是 `http://host.docker.internal:18104` |
| 另一台机 / 有域名 | 直接写它的地址,如 `https://platform.example.com` |
| 同一个 compose 项目里(自己合并过 compose 文件) | 写服务名,如 `http://core:18104` |

> **为什么不能写 `http://core:18104`**:别的产品是**另一个 compose 项目**,有自己的网络,
> 服务名在本项目的容器里解析不出来。它们把端口发布在宿主上,所以走宿主网关。
>
> **为什么需要 `host.docker.internal`**:Linux 上这个名字默认不存在,
> 本包已在 `backend` 服务里声明 `extra_hosts: host.docker.internal:host-gateway` 替你解决。
> 2026-09-07 实测:不声明时容器内解析失败;声明后打宿主已发布端口返回 200。

**自检**(填完再起栈,确认真能通,别等功能报错才发现):

```bash
# 把 <PORT> 换成对方发布的宿主端口(Platform 默认 18104)
docker compose -f docker-compose.customer.yml run --rm --no-deps backend \
  sh -c 'wget -q -T 5 -O - http://host.docker.internal:<PORT>/health || echo "打不通:先确认对方栈在跑、且端口发布在宿主上"'
```

两侧默认端口互不冲突(已逐个比对:Tally 用 18200/3000,Platform 用 5432/6379/4222/8080/18104/18105/18900/3010 等),
同机并存不需要改端口。

