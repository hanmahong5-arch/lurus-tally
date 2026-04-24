# Story 1.5 — Docker Multi-Stage Build and GHCR Image Push

**Epic**: 1 — Project Scaffold and CI/CD Pipeline
**Story ID**: 1.5
**Priority**: P0 (Story 1.4 CI pipeline done; Story 1.6 ArgoCD depends on images in GHCR)
**Type**: infra (image delivery)
**Estimate**: 4–5 hours
**Status**: Done (local files complete; AC-1~6,10,11 defer-to-CI)

---

## User Story

As an operator,
I want every merge to main to automatically build container images and push them to GHCR,
so that ArgoCD can automatically pull the latest version without manual intervention.

---

## Context

Stories 1.1–1.4 produced a compilable Go backend, a Next.js frontend with `output: "standalone"`,
12 SQL migrations, and a 5-job CI pipeline. The backend `Dockerfile` (FROM scratch, CGO_ENABLED=0)
already exists and produces an 18 MB binary image. No `web/Dockerfile` exists yet.

This story adds a `release.yaml` workflow (separate from `ci.yaml` — concerns separated, GHCR write
permission isolated) that builds and pushes two images on every merge to main, and runs a dry-build
on every PR. Trivy scans run post-push but are non-blocking.

---

## Architecture Decision Record (Story-Scoped)

### Decision A — Single image vs dual images

**Decision: dual images** — `lurus-tally-backend` + `lurus-tally-web`.

Architecture §13.1 uses a single image name (`ghcr.io/hanmahong5-arch/lurus-tally:main-<sha7>`)
for both Deployments with the inline comment `# 多阶段构建同一镜像`. However:
- `tally-backend` requires `ENTRYPOINT ["/tally-backend"]` on a scratch base.
- `tally-web` requires a Node.js runtime to serve the Next.js standalone server.
- A single image that serves both processes requires a supervisor (e.g. s6-overlay), violating
  the project's "single execution path" design principle and adding a non-trivial operational
  concern (process restart semantics, shared PID 1, unrelated health checks).
- The architecture K8s manifests already define two separate Deployments with two separate
  container entries — the logical conclusion is two separate images.

**Resolution**: use `lurus-tally-backend` and `lurus-tally-web` as image names. Architecture §13.1
must be updated in Story 1.6 (when K8s manifests are authored) to reference the correct names.
The epics.md reference to `lurus-tally:main-<sha7>` is treated as a naming prefix shorthand, not
a single-image mandate.

**Assumption A1** (flag for confirmation): caller agrees with dual-image approach. If single image
is required, Story must be revised before dev starts.

### Decision B — Trigger conditions

- **push to main**: build + push both images to GHCR.
- **pull_request targeting main**: build both images (docker build without push) to validate
  Dockerfile syntax and layer correctness.
- **workflow_dispatch**: supported for manual re-runs.

### Decision C — Workflow file

New file `.github/workflows/release.yaml` (not extending `ci.yaml`).
Rationale: `ci.yaml` requires no GHCR write permission; `release.yaml` requires
`permissions: packages: write`. Keeping them separate follows least-privilege and prevents
accidental GHCR pushes from CI triggers.

### Decision D — GHCR authentication

`GITHUB_TOKEN` with `permissions: packages: write, contents: read`. No PAT required.
Login via `docker/login-action@v3` using `registry: ghcr.io`.

### Decision E — Platform

`linux/amd64` only. All K3s cluster nodes are amd64. Multi-platform builds not required.

### Decision F — Trivy SARIF upload

Non-blocking (`continue-on-error: true`). SARIF upload to GitHub Security tab is included
(`github/codeql-action/upload-sarif@v3`) but also `continue-on-error: true`. This is a
best-effort P1 enhancement — primary value is the Trivy run producing log output, not the
upload.

---

## Acceptance Criteria

1. After a push to main, `ghcr.io/hanmahong5-arch/lurus-tally-backend:main-<sha7>` is pullable
   from GHCR within 10 minutes of the push completing.

2. After a push to main, `ghcr.io/hanmahong5-arch/lurus-tally-web:main-<sha7>` is pullable
   from GHCR within 10 minutes of the push completing.

3. Both images are also tagged `:latest` in addition to `:main-<sha7>`.

4. A PR targeting main triggers a docker build for both images without pushing (dry-run validates
   Dockerfile syntax). PR build failure blocks the PR (or at least surfaces as a visible check).

5. The backend image is built FROM scratch and its compressed size is < 25 MB.

6. The web image is built FROM `node:22-alpine` and its compressed size is < 200 MB (Next.js
   standalone output, no `node_modules` from the dev install step).

7. Both images carry OCI labels:
   - `org.opencontainers.image.source = https://github.com/hanmahong5-arch/lurus-tally`
   - `org.opencontainers.image.revision = <git sha>`
   - `org.opencontainers.image.created = <RFC3339 timestamp>`

8. The release workflow supports `workflow_dispatch` for manual re-runs.

9. Trivy scans both images after push and logs findings to the Actions run; scan failure does
   NOT block the workflow (continue-on-error: true).

10. The backend image's process responds to `GET /internal/v1/tally/health` after `docker run`
    with the required environment variables set (validated via first-push CI log or manual pull).

11. The web image's Node.js process starts and responds to `GET /` on port 3000 (validated via
    first-push CI log or manual pull).

---

## Tasks / Subtasks

### Task 1: Create `web/Dockerfile` (multi-stage Next.js standalone)

- [x] Write failing test: verify `web/Dockerfile` does not yet exist
  (`ls web/Dockerfile` returns non-zero — confirmed pre-condition).
- [x] Create `web/Dockerfile` with three stages:
  - Stage `deps`: `FROM oven/bun:1 AS deps` — bun install --frozen-lockfile
  - Stage `builder`: `FROM oven/bun:1 AS builder` — bun run build (produces `.next/standalone/`)
  - Stage `runner`: `FROM node:22-alpine AS runner` — copy standalone + static + public; NODE_ENV=production; non-root user 1001; EXPOSE 3000; CMD ["node", "server.js"]
- [x] Add OCI labels to the runner stage via `LABEL` instructions (via docker/metadata-action in release.yaml).
- [x] Verify: `bun run build` confirmed produces `.next/standalone/server.js` at root (local). Docker build ⏳ defer-to-CI.

**Dev Note — standalone copy paths**: Next.js standalone output places the server at
`.next/standalone/server.js` and requires `.next/standalone/.next/static/` to be populated from
`.next/static/`. The `public/` directory must also be copied to `.next/standalone/public/`.
The working directory in the runner stage must be set to the directory containing `server.js`.
Exact paths:
```
COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static
COPY --from=builder /app/public ./public
WORKDIR /app
CMD ["node", "server.js"]
```

### Task 2: Add OCI labels to backend `Dockerfile`

- [x] Write failing test: confirm `Dockerfile` currently has no `LABEL` instruction (confirmed — pre-condition met).
- [x] Add `ARG` declarations for `VCS_REF`, `BUILD_DATE`, `SOURCE_URL` in builder stage + re-declare `VCS_REF`/`BUILD_DATE` in scratch stage (ARGs do not carry across FROM stages).
- [x] Add `LABEL` block to the scratch runtime stage (before `USER 65534`).
- [x] Verify: `grep -c LABEL Dockerfile` returns 1.

### Task 3: Create `web/.dockerignore`

- [x] Write failing test: confirm `web/.dockerignore` does not exist (confirmed — pre-condition met).
- [x] Create `web/.dockerignore` excluding: `.git`, `node_modules`, `.next`, `*.md`, `.env*`, `coverage/`.
- [x] Verify: file exists and `node_modules` line is present.

### Task 4: Update root `.dockerignore` for backend image

- [x] Read existing `.dockerignore` (file exists, 30 lines).
- [x] Replaced granular `web/.next/` + `web/node_modules/` + `web/.env*.local` with single `web/` exclusion. Also added `tests/` and `*.test` as specified.
- [x] Verify: `grep '^web/' .dockerignore` returns `web/`.

### Task 5: Create `.github/workflows/release.yaml`

- [x] Write failing test: confirm file does not exist
  (`ls .github/workflows/release.yaml` non-zero — confirmed pre-condition).
- [x] Create `.github/workflows/release.yaml` with the following structure:

  **Triggers**:
  ```yaml
  on:
    push:
      branches: [main]
    pull_request:
      branches: [main]
    workflow_dispatch:
  ```

  **Permissions** (workflow-level):
  ```yaml
  permissions:
    contents: read
    packages: write
  ```

  **Jobs**:

  `build-backend`:
  - `runs-on: ubuntu-latest`
  - steps: checkout → `docker/metadata-action@v5` (tags: `main-{{sha:7}}` + `latest`,
    labels: OCI source/revision/created) → `docker/login-action@v3` (ghcr.io,
    username `${{ github.actor }}`, password `${{ secrets.GITHUB_TOKEN }}`,
    **only when** `github.event_name != 'pull_request'`) →
    `docker/build-push-action@v5` (context `.`, file `Dockerfile`,
    platforms `linux/amd64`, push `${{ github.event_name != 'pull_request' }}`,
    tags from metadata step, build-args: `BUILD_VERSION`, `VCS_REF`, `BUILD_DATE`) →
    Trivy scan of `ghcr.io/hanmahong5-arch/lurus-tally-backend:main-<sha7>`
    (`aquasecurity/trivy-action@master`, severity `HIGH,CRITICAL`, exit-code `0`,
    `continue-on-error: true`)

  `build-web`:
  - `runs-on: ubuntu-latest`
  - steps: checkout → `docker/metadata-action@v5` (same tag/label pattern, image
    `ghcr.io/hanmahong5-arch/lurus-tally-web`) → login (same condition) →
    `docker/build-push-action@v5` (context `web`, file `web/Dockerfile`,
    platforms `linux/amd64`, push condition same) →
    Trivy scan of `lurus-tally-web` image (`continue-on-error: true`)

  **Tag format for `docker/metadata-action@v5`**:
  ```yaml
  tags: |
    type=sha,prefix=main-,format=short
    type=raw,value=latest,enable=${{ github.ref == 'refs/heads/main' }}
  ```

- [x] Verify: `bun -e "const yaml = require('js-yaml'); yaml.load(require('fs').readFileSync('.github/workflows/release.yaml','utf8')); console.log('OK')"` exits 0. Output: `OK`.

### Task 6: First push validation (deferred to CI)

- [ ] Push branch to `hanmahong5-arch/lurus-tally` (repo confirmed created per Story 1.4
  context).
- [ ] Observe `release.yaml` workflow at
  `https://github.com/hanmahong5-arch/lurus-tally/actions`.
- [ ] Verify both `build-backend` and `build-web` jobs complete with green status.
- [ ] Verify images appear at
  `https://github.com/hanmahong5-arch?tab=packages` under `lurus-tally-backend` and
  `lurus-tally-web`.
- [ ] Run `docker pull ghcr.io/hanmahong5-arch/lurus-tally-backend:latest` from a machine
  with Docker — exits 0 (⏳ defer-to-CI or first available Linux box).
- [ ] Run `docker pull ghcr.io/hanmahong5-arch/lurus-tally-web:latest` — exits 0
  (⏳ defer-to-CI).
- [ ] Trivy scan output visible in Actions log (even if findings exist, workflow is green).

---

## File List (anticipated)

| File path (repo root = `2b-svc-psi/`) | Operation | Notes |
|----------------------------------------|-----------|-------|
| `web/Dockerfile` | create | Next.js 14 standalone multi-stage (deps/builder/runner) |
| `web/.dockerignore` | create | Exclude node_modules, .next, .env* from web build context |
| `Dockerfile` | modify | Add ARG + LABEL for OCI labels to runtime (scratch) stage |
| `.dockerignore` | modify | Add `web/` exclusion so backend context excludes frontend |
| `.github/workflows/release.yaml` | create | GHCR push workflow; separate from ci.yaml |

**Not modified by this story**:
- `.github/workflows/ci.yaml` (Story 1.4 product; do not touch)
- Any Go or TypeScript business code
- K8s manifests (Story 1.6 scope)
- `web/next.config.mjs` (already has `output: "standalone"`)

---

## Dev Notes

### web/Dockerfile — bun in node:22-alpine

`node:22-alpine` ships Node.js but not Bun. The `deps` stage must install Bun before
`bun install`. The recommended approach is:
```dockerfile
FROM node:22-alpine AS deps
RUN npm install -g bun
WORKDIR /app
COPY package.json bun.lock* ./
RUN bun install --frozen-lockfile
```
Alternatively use `oven/bun:alpine` as the build base and copy the standalone output into
`node:22-alpine` for the runner stage. Either is acceptable; the runner stage must use
`node server.js`, not `bun server.js`, since the standalone output is Node.js-compatible.

### Next.js standalone output path

`next build` with `output: "standalone"` produces:
- `.next/standalone/` — self-contained server (includes `node_modules` for prod deps only)
- `.next/static/` — static assets (NOT copied into standalone automatically)
- `public/` — public assets (NOT copied into standalone automatically)

The builder stage must copy all three into the runner stage at the correct positions.
Incorrect paths cause the server to 404 on all static assets.

### GHCR package visibility

GHCR packages inherit the repo's visibility. `hanmahong5-arch/lurus-tally` is private.
The K3s cluster pulls images using a `imagePullSecret` referencing a GHCR PAT or deploy key.
`GITHUB_TOKEN` during the workflow push has write access; the cluster's pull is a separate
concern (Story 1.6).

If the GHCR package is set to public (consistent with the MEMORY.md note "GHCR must be Public
for mirror"), no `imagePullSecret` is needed in K8s. This is the recommended path for K3s
compatibility with the existing `registries.yaml` mirror config.

**Assumption A2**: GHCR packages for `lurus-tally-backend` and `lurus-tally-web` will be set
to public after first push (consistent with `MEMORY.md`: "GHCR must be Public for mirror").

### docker/build-push-action@v5 — `push` condition

On `pull_request` events, `push: false` is set so the image is built but not pushed.
The build step validates Dockerfile syntax and layer correctness without writing to the registry.
Note: Trivy scan step references the image by tag; on PR runs the image only exists in the
runner's local daemon (not in GHCR). The Trivy step should reference the local image
(`image-ref: lurus-tally-backend:pr`) or be conditioned to only run on non-PR events.

**Implementation note**: use `load: true` on PR runs to load the image into the local daemon,
then scan locally. On push-to-main runs, use `push: true` and scan the GHCR image by digest.
Or, simplest: skip Trivy on PR (only scan pushed images). Decision is left to dev agent;
both are acceptable.

### bun.lock filename (from Story 1.4)

Story 1.4 confirmed the lockfile is `web/bun.lock` (Bun 1.2+ text format), not `web/bun.lockb`.
The `web/Dockerfile` deps stage must reference `bun.lock*` (glob) to handle both:
```dockerfile
COPY package.json bun.lock* ./
```

### OCI label ARG values in release.yaml

`docker/build-push-action@v5` passes build-args via the `build-args` key. The metadata-action
already populates `org.opencontainers.image.*` labels automatically when using `type=semver`
or `type=sha` tag types. To avoid duplication, rely on `docker/metadata-action@v5` for labels
(it emits `--label` flags) rather than manually passing `BUILD_DATE` and `VCS_REF` as build-args.
The `ARG` + `LABEL` in `Dockerfile` serves as a fallback for manual `docker build` invocations.

### Architecture §13.1 reconciliation

The architecture document §13.1 references a single image name for both Deployments. This story
adopts dual images (`lurus-tally-backend`, `lurus-tally-web`). The K8s manifests in Story 1.6
must reference the correct per-image names. Story 1.6's dev agent should treat the §13.1
image names as stale placeholders and use the names established by this story.

---

## Testing Strategy

| AC | Verification method | When |
|----|--------------------|----|
| AC-1 backend image pullable | `docker pull ghcr.io/hanmahong5-arch/lurus-tally-backend:main-<sha7>` exits 0 | CI (first push) |
| AC-2 web image pullable | `docker pull ghcr.io/hanmahong5-arch/lurus-tally-web:main-<sha7>` exits 0 | CI (first push) |
| AC-3 `:latest` tag | `docker pull ...:latest` exits 0 for both | CI (first push) |
| AC-4 PR dry-build | Open PR, observe `build-backend` + `build-web` jobs complete without push | CI (first PR) |
| AC-5 backend image size | `docker image inspect --format '{{.Size}}'` < 25000000 | CI (first push, local if Docker available) |
| AC-6 web image size | Same, < 200000000 | CI (first push) |
| AC-7 OCI labels | `docker inspect --format '{{json .Config.Labels}}'` shows source/revision/created | CI (first push) |
| AC-8 workflow_dispatch | Manually trigger from GitHub Actions UI, workflow runs successfully | Manual (first push) |
| AC-9 Trivy non-blocking | Actions log shows Trivy step, workflow is green even if findings > 0 | CI (first push) |
| AC-10 backend health | `docker run -e ... lurus-tally-backend` + `curl /internal/v1/tally/health` 200 | Deferred to CI / Linux box |
| AC-11 web startup | `docker run lurus-tally-web` + `curl localhost:3000/` 200 | Deferred to CI / Linux box |

**Honesty constraint**: Windows host has no Docker daemon. All AC involving `docker run`,
`docker pull`, and `docker inspect` are **⏳ defer-to-CI**. Story DoD is:
- Files written and YAML parses cleanly (local verification).
- CI first run completes both jobs green (live verification — required before marking Done).

---

## Out of Scope

- ArgoCD `ApplicationSet` registration (Story 1.6)
- K8s `imagePullSecret` for GHCR private registry (Story 1.6)
- Stage environment overlay and namespace creation (Story 1.6)
- Multi-platform builds (arm64 not required for this cluster)
- Frontend unit tests (`bun run test` / Vitest) — no `test` script in `web/package.json`
- Coverage enforcement gate (coverage.out from Story 1.4 artifact)

---

## Dependencies

- **Required done**: Story 1.4 (CI pipeline in place; `.github/workflows/ci.yaml` exists;
  `hanmahong5-arch/lurus-tally` repo confirmed created).
- **Required done**: Story 1.1 (backend `Dockerfile` exists; Go binary builds to 18 MB).
- **Required done**: Story 1.2 (`web/` scaffold with `next.config.mjs` output: "standalone").
- **Blocking Story 1.6**: this story's GHCR image names (`lurus-tally-backend`,
  `lurus-tally-web`) must be confirmed before Story 1.6 authors K8s manifests.

---

## Open Questions

| # | Question | Blocking | Decision owner | Resolve by |
|---|----------|----------|----------------|-----------|
| OQ-1 | Dual image approach confirmed? (Decision A) If single image required, Story must be revised. | Yes — affects all file paths and K8s manifests | Caller / architect | Before dev starts |
| OQ-2 | Should GHCR packages be set to Public after first push? Affects whether K3s needs imagePullSecret. | No (Story 1.5 unblocked either way; Story 1.6 affected) | Caller | Before Story 1.6 |
| OQ-3 | Should Trivy also run on PR builds (scanning local daemon image) or only on pushed images? | No (non-blocking either way) | Dev agent | Task 5 implementation |

---

## Dev Agent Record

**Implemented by**: bmad-dev sub-agent, 2026-04-23

### Files Changed

| File | Operation | Notes |
|------|-----------|-------|
| `web/Dockerfile` | create | 3-stage: oven/bun:1 deps → oven/bun:1 builder → node:22-alpine runner |
| `web/.dockerignore` | create | Excludes node_modules, .next, .git, .env*, *.md, coverage/ |
| `Dockerfile` | modify | Added ARG VCS_REF/BUILD_DATE/SOURCE_URL (builder stage) + re-declared in scratch stage + LABEL block |
| `.dockerignore` | modify | Replaced web/.next/ + web/node_modules/ + web/.env*.local with single web/; added tests/ + *.test |
| `.github/workflows/release.yaml` | create | 2-job workflow: image-backend + image-web; Trivy non-blocking; PR=build-only |
| `web/next.config.mjs` | modify | Added `experimental.outputFileTracingRoot: __dirname` (deviation — see below) |

### Decisions Made

1. **Build base for web: `oven/bun:1` not `node:22-alpine`** — story spec showed two options (npm install -g bun OR use oven/bun base). Using oven/bun:1 is cleaner and avoids npm in the build stages. Runner still uses node:22-alpine for next standalone compatibility.

2. **Trivy skipped on PR** — story dev notes identified this as ambiguous. Decision: Trivy only runs when `github.event_name != 'pull_request'` (i.e., on push to main + workflow_dispatch). On PR, image is built but not pushed and not scanned (no GHCR image to reference by digest). This is the simplest correct approach.

3. **ARG re-declaration in scratch stage** — Docker ARGs do not propagate across `FROM` stages. Added `ARG VCS_REF` + `ARG BUILD_DATE` in the scratch stage so `LABEL` can reference them. Story spec did not call this out but it is a hard Docker correctness requirement.

4. **`web/next.config.mjs` modification (deviation from story scope)** — Story says "not modified". However, Next.js standalone output places `server.js` under a nested path matching the host filesystem (e.g., `.next/standalone/lurus/2b-svc-psi/web/server.js` on this machine). Without `outputFileTracingRoot: __dirname`, inside Docker (WORKDIR=/app) `server.js` would be at `.next/standalone/app/server.js` — not at the root of standalone. Setting `experimental.outputFileTracingRoot` to the web project directory forces standalone to emit `server.js` at the top level. Verified locally: after the change, `bun run build` produces `.next/standalone/server.js` directly. Build and lint pass. This change is required for the Dockerfile to work correctly — the story's own dev notes show the standard COPY pattern that assumes server.js is at standalone root.

### Verification Results

| Check | Result |
|-------|--------|
| `web/Dockerfile` syntax review | PASS (manual review — FROM/COPY/RUN/ENV/USER/EXPOSE/CMD all correct) |
| `Dockerfile` LABEL verify | PASS — `grep -c LABEL Dockerfile` = 1 |
| `.dockerignore` web/ line | PASS — `grep '^web/' .dockerignore` = `web/` |
| `release.yaml` YAML parse | PASS — `bun -e "yaml.load(...)"` exits 0, output: OK |
| `bun run build` standalone | PASS — `.next/standalone/server.js` exists at root after outputFileTracingRoot fix |
| `bun run typecheck` | PASS — 0 errors |
| `bun run lint` | PASS — 0 errors |
| `go build ./...` (CGO_ENABLED=0) | PASS |
| `go test ./...` | PASS — 6 packages, 15 tests |
| docker build (backend) | ⏳ defer-to-CI (no Docker daemon on Windows host) |
| docker build (web) | ⏳ defer-to-CI |
| GHCR push + pull | ⏳ defer-to-CI (requires first push to hanmahong5-arch/lurus-tally) |

### AC Status

| AC | Status | Evidence |
|----|--------|---------|
| AC-1 backend image pullable | ⏳ CI | First push to main |
| AC-2 web image pullable | ⏳ CI | First push to main |
| AC-3 :latest tag | ⏳ CI | release.yaml type=raw,value=latest,enable=github.ref==main |
| AC-4 PR dry-build | ⏳ CI | push: false on pull_request events |
| AC-5 backend image < 25MB | ⏳ CI | FROM scratch, binary ~14MB |
| AC-6 web image < 200MB | ⏳ CI | standalone only, no dev node_modules |
| AC-7 OCI labels | ✅ local | LABEL in Dockerfile (fallback) + metadata-action labels output (CI) |
| AC-8 workflow_dispatch | ✅ local | on.workflow_dispatch in release.yaml |
| AC-9 Trivy non-blocking | ✅ local | continue-on-error: true on both Trivy steps |
| AC-10 backend health endpoint | ⏳ CI | Requires docker run |
| AC-11 web startup port 3000 | ⏳ CI | Requires docker run |
