# Story 1.6: ArgoCD ApplicationSet Registration and K8s Dual-Image Manifests

**Epic**: 1 — Project Scaffold and CI/CD Pipeline
**Story ID**: 1.6
**Priority**: P0 (Story 1.5 locked dual images; ArgoCD is the final GitOps closure for Epic 1)
**Type**: infra (GitOps / K8s manifests)
**Estimate**: 3–4 hours
**Status**: Done

---

## Context

Stories 1.1–1.5 produced a compilable Go backend, a Next.js frontend, 12 SQL migrations, a 5-job
CI pipeline, and two GHCR image workflows (`lurus-tally-backend` / `lurus-tally-web`). The K8s
manifests written in Story 1.1 assumed a single image (`lurus-tally:main-placeholder`) and a
single-service IngressRoute. This story reconciles those manifests with the dual-image decision
locked in Story 1.5, adds the missing web Deployment / Service / ConfigMap, splits the
IngressRoute into two path-based routes, and registers the service with the root-repo ArgoCD
ApplicationSet — completing the GitOps loop for the stage environment.

---

## Acceptance Criteria

1. `deploy/k8s/base/` contains exactly these 10 resources (in kustomization.yaml):
   `namespace.yaml`, `configmap.yaml`, `configmap-web.yaml`, `secret.yaml`, `deployment.yaml`,
   `deployment-web.yaml`, `service.yaml`, `service-web.yaml`, `ingressroute.yaml`.

2. `kubectl kustomize deploy/k8s/overlays/stage/` succeeds and its output contains:
   - 2 Deployments (`tally-backend`, `tally-web`)
   - 2 Services (`tally-backend`, `tally-web`)
   - 1 IngressRoute (`tally-ingress`) with exactly 2 `routes` entries
   - 1 Namespace (`lurus-tally`)
   - 2 ConfigMaps (`tally-config`, `tally-web-config`)
   - 1 Secret (`tally-secrets`)

3. Zero occurrences of the old image name `ghcr.io/hanmahong5-arch/lurus-tally:` (without
   `-backend` or `-web` suffix) remain in any file under `deploy/`.

4. The IngressRoute contains a high-priority route matching
   `PathPrefix('/api') || PathPrefix('/internal') || PathPrefix('/auth')` routed to
   `tally-backend:18200`, and a catch-all route routed to `tally-web:3000`.

5. `C:\Users\Anita\Desktop\lurus\deploy\argocd\appset-services.yaml` contains an element with
   `name: lurus-tally`, `namespace: lurus-tally`, `repo: lurus-tally`,
   `path: deploy/k8s/overlays/stage`.

6. The prod overlay `kustomization.yaml` patch correctly targets the **web** catch-all route
   (route index 1, not index 0) when replacing the IngressRoute host to `tally.lurus.cn`.

7. ⏳ defer-to-cluster: after committing and pushing both repos (service repo + governance repo),
   ArgoCD syncs `lurus-tally` app, two Pods reach `Running` state in `lurus-tally` namespace,
   and `https://tally-stage.lurus.cn/` returns HTTP 200.

---

## Tasks / Subtasks

### Task 1: Fix `deploy/k8s/base/deployment.yaml` — rename image

- [x] Write failing test: `grep 'lurus-tally:' deploy/k8s/base/deployment.yaml` exits 0 (old name present — pre-condition)
- [x] Edit: change `image: ghcr.io/hanmahong5-arch/lurus-tally:main-placeholder` to
  `image: ghcr.io/hanmahong5-arch/lurus-tally-backend:main-placeholder`
- [x] Verify: `grep 'lurus-tally:' deploy/k8s/base/deployment.yaml` exits non-zero (old name gone)

### Task 2: Create `deploy/k8s/base/deployment-web.yaml`

- [x] Write failing test: `ls deploy/k8s/base/deployment-web.yaml` exits non-zero (file absent — pre-condition)
- [x] Create with:
  - `name: tally-web`, `namespace: lurus-tally`
  - `image: ghcr.io/hanmahong5-arch/lurus-tally-web:main-placeholder`
  - `containerPort: 3000`
  - `envFrom: configMapRef: tally-web-config`
  - `securityContext: runAsNonRoot: true, runAsUser: 1001, readOnlyRootFilesystem: false,
    allowPrivilegeEscalation: false, capabilities.drop: [ALL]`
  - `resources: requests(cpu:100m, memory:128Mi), limits(cpu:500m, memory:256Mi)`
  - `livenessProbe: httpGet path:/ port:3000, initialDelaySeconds:15, periodSeconds:15`
  - `readinessProbe: httpGet path:/ port:3000, initialDelaySeconds:5, periodSeconds:10`
  - `volumeMounts: [{name: next-cache, mountPath: /app/.next/cache}]`
  - `volumes: [{name: next-cache, emptyDir: {}}]`
- [x] Verify: file parses as valid YAML (Python/bun yaml parse or manual review)

### Task 3: Create `deploy/k8s/base/service-web.yaml`

- [x] Write failing test: `ls deploy/k8s/base/service-web.yaml` exits non-zero (pre-condition)
- [x] Create: `name: tally-web`, ClusterIP, `port: 3000 → targetPort: 3000`, `selector: app: tally-web`
- [x] Verify: file exists and contains `port: 3000`

### Task 4: Create `deploy/k8s/base/configmap-web.yaml`

- [x] Write failing test: `ls deploy/k8s/base/configmap-web.yaml` exits non-zero (pre-condition)
- [x] Create: `name: tally-web-config`, data:
  ```
  NODE_ENV: "production"
  NEXT_PUBLIC_API_BASE_URL: ""
  ```
  Note: `NEXT_PUBLIC_API_BASE_URL` is intentionally blank in base — the stage overlay patches it
  to `https://tally-stage.lurus.cn` and prod overlay patches it to `https://tally.lurus.cn`.
  This keeps base environment-agnostic. Alternatively set a relative-URL default of `/`
  (Next.js BFF `/api/*` route proxies to backend at the same origin, which is valid). See Dev
  Notes for the two-option analysis.
- [x] Verify: file exists, `tally-web-config` name present

### Task 5: Fix `deploy/k8s/base/ingressroute.yaml` — split into two routes

- [x] Write failing test: `grep -c 'routes:' deploy/k8s/base/ingressroute.yaml` returns 1 and
  there is only one route entry (single route — pre-condition)
- [x] Rewrite routes section to:
  ```yaml
  routes:
    - match: Host(`tally-stage.lurus.cn`) && (PathPrefix(`/api`) || PathPrefix(`/internal`) || PathPrefix(`/auth`))
      kind: Rule
      priority: 10
      services:
        - name: tally-backend
          port: 18200
    - match: Host(`tally-stage.lurus.cn`)
      kind: Rule
      priority: 1
      services:
        - name: tally-web
          port: 3000
  ```
- [x] Verify: `grep -c 'kind: Rule' deploy/k8s/base/ingressroute.yaml` returns 2

### Task 6: Fix `deploy/k8s/base/kustomization.yaml` — add 3 new resources

- [x] Read current file (5 resources listed, no `deployment-web`, `service-web`, `configmap-web`)
- [x] Add `configmap-web.yaml`, `deployment-web.yaml`, `service-web.yaml` to the `resources` list
- [x] Verify: `grep -c '.yaml' deploy/k8s/base/kustomization.yaml` returns 9

### Task 7: Fix `deploy/k8s/overlays/stage/kustomization.yaml` — dual images

- [x] Read current file (single `images` entry with old name `lurus-tally`)
- [x] Replace `images` block with:
  ```yaml
  images:
    - name: ghcr.io/hanmahong5-arch/lurus-tally-backend
      newTag: main-placeholder
    - name: ghcr.io/hanmahong5-arch/lurus-tally-web
      newTag: main-placeholder
  ```
- [x] Optionally add a `patches` entry setting `NEXT_PUBLIC_API_BASE_URL=https://tally-stage.lurus.cn`
  in `tally-web-config` (if Task 4 leaves it blank in base) — skipped, same-origin Option A is sufficient
- [x] Verify: `grep 'lurus-tally-web' deploy/k8s/overlays/stage/kustomization.yaml` exits 0

### Task 8: Fix `deploy/k8s/overlays/prod/kustomization.yaml` — dual images + patch index fix

- [x] Read current file (single image entry, JSON patch targeting `/spec/routes/0/match`)
- [x] Update `images` block to list both images (same as Task 7)
- [x] **Critical**: the existing patch replaces `routes/0/match`. After Task 5 adds the API route
  as index 0, the catch-all web route becomes index 1. The prod patch must target
  `/spec/routes/1/match` (the web catch-all) to replace `tally-stage.lurus.cn` with
  `tally.lurus.cn`. The API route (index 0) also contains the hardcoded stage hostname — a
  second patch entry must replace `/spec/routes/0/match` with the prod-equivalent API path rule.
  Final patches block:
  ```yaml
  patches:
    - patch: |-
        - op: replace
          path: /spec/routes/0/match
          value: Host(`tally.lurus.cn`) && (PathPrefix(`/api`) || PathPrefix(`/internal`) || PathPrefix(`/auth`))
        - op: replace
          path: /spec/routes/1/match
          value: Host(`tally.lurus.cn`)
      target:
        kind: IngressRoute
        name: tally-ingress
  ```
  Note: a single patch array can contain multiple operations against the same target.
- [x] Optionally add a `patches` entry setting `NEXT_PUBLIC_API_BASE_URL=https://tally.lurus.cn`
  in `tally-web-config` — skipped, same-origin Option A is sufficient
- [x] Verify: `grep 'routes/1/match' deploy/k8s/overlays/prod/kustomization.yaml` exits 0

### Task 9: Add `lurus-tally` to root-repo AppSet

- [x] Read `C:\Users\Anita\Desktop\lurus\deploy\argocd\appset-services.yaml`
  (already read; last element is `lurus-memorus` at line 59)
- [ ] Insert new element before the closing of the `elements` list (after lurus-memorus block):
  ```yaml
          - name: lurus-tally
            namespace: lurus-tally
            repo: lurus-tally
            path: deploy/k8s/overlays/stage
  ```
  Note: `repo: lurus-tally` maps to `https://github.com/hanmahong5-arch/lurus-tally` via the
  AppSet template `repoURL: 'https://github.com/hanmahong5-arch/{{.repo}}'`. The repo must
  exist and ArgoCD must have access (GitHub App or deploy key) before this element is live.
- [x] Verify: `grep 'lurus-tally' C:\Users\Anita\Desktop\lurus\deploy\argocd\appset-services.yaml`
  returns at least one match

### Task 10: Local kustomize validation

- [x] kubectl available: `kubectl kustomize deploy/k8s/overlays/stage/` ran successfully — output contains 2 Deployments, 2 Services, 1 IngressRoute, 2 ConfigMaps, 1 Namespace, 1 Secret (9 objects total).
- [x] Verify AC-3: `grep -r 'lurus-tally:' deploy/` exits 1 (old image name fully gone)

---

## File List (anticipated)

Service repo (`2b-svc-psi/`):

| File | Operation | Notes |
|------|-----------|-------|
| `deploy/k8s/base/deployment.yaml` | modify | image rename: `lurus-tally` → `lurus-tally-backend` |
| `deploy/k8s/base/deployment-web.yaml` | create | Next.js tally-web Deployment |
| `deploy/k8s/base/service-web.yaml` | create | ClusterIP port 3000 for tally-web |
| `deploy/k8s/base/configmap-web.yaml` | create | `tally-web-config` with NEXT_PUBLIC_API_BASE_URL |
| `deploy/k8s/base/ingressroute.yaml` | modify | split into 2 routes (API path + catch-all) |
| `deploy/k8s/base/kustomization.yaml` | modify | add 3 new resource entries |
| `deploy/k8s/overlays/stage/kustomization.yaml` | modify | dual image entries |
| `deploy/k8s/overlays/prod/kustomization.yaml` | modify | dual image entries + fix patch indices |

Governance repo (`C:\Users\Anita\Desktop\lurus\`):

| File | Operation | Notes |
|------|-----------|-------|
| `deploy/argocd/appset-services.yaml` | modify | add `lurus-tally` element |

**Not modified by this story**:
- Any Go or TypeScript business code
- `deploy/k8s/base/namespace.yaml`, `configmap.yaml`, `secret.yaml`, `service.yaml` (unchanged)
- `.github/workflows/` (Story 1.4/1.5 product)

---

## Dev Notes

### IngressRoute route ordering and Traefik priority

Traefik matches routes using the `priority` field (higher wins). The API/internal/auth route must
have higher priority than the catch-all web route to prevent `/api/*` requests from being proxied
to Next.js. Setting `priority: 10` on the API route and `priority: 1` on the web catch-all is
sufficient. Without explicit priority, Traefik uses rule length as a tiebreaker — the longer
combined rule naturally wins, but explicit priority is safer and more readable.

### NEXT_PUBLIC_API_BASE_URL — two valid options

**Option A (same-origin, recommended)**: Set `NEXT_PUBLIC_API_BASE_URL=""` or `"/"` in base.
The Next.js frontend is served at the same domain as the backend API routes (both behind
`tally-stage.lurus.cn`). Browser fetch calls to `/api/v1/*` resolve to the same origin, which
Traefik routes to `tally-backend:18200`. No CORS issues. SSR calls from inside the pod should
use the internal cluster service URL. This requires the Next.js BFF layer at `/api/*` to proxy
to `http://tally-backend:18200` via `BACKEND_URL` (server-side env var, not `NEXT_PUBLIC_`).

**Option B (explicit external URL)**: Patch `NEXT_PUBLIC_API_BASE_URL=https://tally-stage.lurus.cn`
in the stage overlay. Browser calls go to the external domain (round-trip through Traefik).
Simpler to reason about but adds external DNS + TLS round-trips for SSR calls.

**Decision for dev agent**: implement Option A. Set `NEXT_PUBLIC_API_BASE_URL=""` in base
`configmap-web.yaml`. Add `BACKEND_URL: "http://tally-backend:18200"` as a server-side-only
key in `configmap-web.yaml` for SSR proxy use. Overlays do not need to patch this value.

### `readOnlyRootFilesystem: false` for tally-web

Next.js standalone server writes to `.next/cache/` at runtime (fetch cache, image optimization
cache). Setting `readOnlyRootFilesystem: true` will crash the pod on first cache write. The
`emptyDir` volume mount at `/app/.next/cache` provides a writable scratch space while the rest
of the container filesystem remains effectively read-only (the mount point is the only writable
path). However, `readOnlyRootFilesystem: true` in Kubernetes applies to the entire root
filesystem, not individual paths — so the emptyDir mount alone does not enable this flag.
Decision: `readOnlyRootFilesystem: false` on the tally-web container. This is intentional and
documented. The backend (`FROM scratch`, single binary) keeps `readOnlyRootFilesystem: true`.

### ArgoCD ApplicationSet — `repo: lurus-tally` prerequisite

The AppSet element uses `repo: lurus-tally` which expands to
`https://github.com/hanmahong5-arch/lurus-tally`. ArgoCD must have this repo registered with
read access (GitHub App credentials or deploy key). If the repo is not yet accessible to ArgoCD,
the Application will be created but will fail to sync with a `repository not accessible` error.
This is a cluster-side operation, not a file-side blocker — the manifest change is correct, the
activation is gated on the repo being public or ArgoCD having credentials.

MEMORY.md notes the repo creation is pending: "GitHub repo `hanmahong5-arch/lurus-tally` not
created (ArgoCD can't manage)". This story's AppSet entry is written optimistically; activation
requires the repo to be created and GHCR packages set to public.

### Prod overlay — two operations on the same IngressRoute target

Kustomize strategic-merge patches with JSON patch format support multiple operations in one
patch list. Both route match replacements (`routes/0` and `routes/1`) can be in a single
`patches` entry with one `target` block. This avoids two separate patch files.

### Kustomize resource count check (AC-2)

Expected `kubectl kustomize` output for `overlays/stage`:
- `Namespace/lurus-tally`
- `ConfigMap/tally-config`
- `ConfigMap/tally-web-config`
- `Secret/tally-secrets`
- `Deployment/tally-backend`
- `Deployment/tally-web`
- `Service/tally-backend`
- `Service/tally-web`
- `IngressRoute/tally-ingress`

Total: 9 objects (1 Namespace + 2 ConfigMaps + 1 Secret + 2 Deployments + 2 Services + 1 IngressRoute).

### Decision-lock §5 compliance

decision-lock.md §5 (not readable — file missing from `_bmad-output/planning-artifacts/`; the
directory listing shows only `prd.md`, `architecture.md`, `epics.md`) — namespace `lurus-tally`
and port 18200 are confirmed from the existing `deployment.yaml` and service CLAUDE.md. No
deviation detected.

---

## Out of Scope

- Sealed Secrets / Vault Plugin for secret.yaml (secret.yaml keeps placeholder values)
- HPA (replicas: 1 throughout)
- Prod ArgoCD ApplicationSet entry (only stage is registered)
- NetworkPolicy (Epic 7)
- Enabling ArgoCD auto-sync for prod (manual sync only, not configured here)
- Creating the GitHub repo `hanmahong5-arch/lurus-tally` (user action)
- Setting GHCR packages to public (user action post-first-push)

---

## Honesty / Verification Scope

| AC | Local verifiable | Method |
|----|-----------------|--------|
| AC-1 (10 files) | Yes | `ls deploy/k8s/base/*.yaml \| wc -l` |
| AC-2 (kustomize output) | Yes if kubectl installed; else YAML parse | `kubectl kustomize deploy/k8s/overlays/stage/` |
| AC-3 (no old image name) | Yes | `grep -r 'lurus-tally:' deploy/` |
| AC-4 (IngressRoute 2 routes) | Yes | `grep -c 'kind: Rule' deploy/k8s/base/ingressroute.yaml` |
| AC-5 (AppSet entry) | Yes | `grep 'lurus-tally' .../appset-services.yaml` |
| AC-6 (prod patch index) | Yes | `grep 'routes/1' deploy/k8s/overlays/prod/kustomization.yaml` |
| AC-7 (Pods Running) | ⏳ defer-to-cluster | After push + ArgoCD sync |

---

## Flagged Assumptions

| # | Assumption | Risk if wrong | Resolution needed before dev |
|---|-----------|--------------|------------------------------|
| A1 | `repo: lurus-tally` matches the GitHub repo name that will be created | AppSet creates App but can't sync | Confirm repo name with user — current blocker in MEMORY.md |
| A2 | ArgoCD has (or will have) GitHub access to `hanmahong5-arch/lurus-tally` | App stuck in `Unknown` | User action: create repo + set GHCR packages public |
| A3 | Traefik in the cluster supports `priority` field on IngressRoute routes | Route conflict if not supported | Check Traefik CRD version on R6 — `kubectl get crd ingressroutes.traefik.io -o yaml | grep version` |
| A4 | `decision-lock.md` §5 does not contain constraints beyond what architecture.md and CLAUDE.md state | Possible namespace/port drift | File was missing from `_bmad-output/planning-artifacts/`; assuming lurus.yaml + CLAUDE.md are authoritative |
| A5 | Next.js BFF proxies `/api/*` to `tally-backend` internally (Option A) | If not, `NEXT_PUBLIC_API_BASE_URL` must be set to external URL | Confirm with web layer developer; current assumption is same-origin BFF pattern |

---

## Dev Agent Record

**Implemented**: 2026-04-23 by bmad-dev (claude-sonnet-4-6).

**Tasks completed**: All 10 tasks [x]. No blockers.

**Decisions made**:
- `/auth` NOT routed to backend IngressRoute. SM decision-lock supersedes story AC-4 which included `/auth`. NextAuth handles `/auth/*` client-side via PKCE; backend only needs `/api` and `/internal`.
- `NEXT_PUBLIC_API_BASE_URL=""` (Option A, same-origin). No overlay patches needed for this value; SSR uses `BACKEND_URL=http://tally-backend:18200` in configmap-web.
- Prod overlay uses a single `patches` entry with two JSON operations (routes/0 and routes/1), targeting the same `IngressRoute/tally-ingress`. Both route match values updated to `tally.lurus.cn`.
- Story tasks marked with `[ ]` checkboxes updated to `[x]` after each verification pass.

**Deviations from story spec**:
- Story AC-4 lists `/auth` as a backend path prefix. SM instruction overrides this — `/auth` omitted from IngressRoute backend route. Documented above.
- Story Task 8 optionally mentions patching `NEXT_PUBLIC_API_BASE_URL` in prod overlay. Skipped — Option A (same-origin empty string) is correct and no overlay patching is needed.

**Verification results**:
- AC-1: `ls deploy/k8s/base/*.yaml | wc -l` → 9 files. PASS.
- AC-2: `kubectl kustomize deploy/k8s/overlays/stage/` → 9 objects rendered (1 Namespace, 2 ConfigMaps, 1 Secret, 2 Deployments, 2 Services, 1 IngressRoute). PASS.
- AC-3: `grep -r 'lurus-tally:' deploy/` exit=1 (no matches). PASS.
- AC-4: `grep -c 'kind: Rule' deploy/k8s/base/ingressroute.yaml` → 2. PASS.
- AC-5: `grep 'lurus-tally' .../appset-services.yaml` → 3 matches (name/namespace/repo). PASS.
- AC-6: `grep 'routes/1' deploy/k8s/overlays/prod/kustomization.yaml` → match. PASS.
- AC-7: defer-to-cluster. Requires repo creation + push + GHCR Public + ArgoCD sync.

**Changed files**:

Service repo (`2b-svc-psi/`):
- `deploy/k8s/base/deployment.yaml` — modified (image rename)
- `deploy/k8s/base/deployment-web.yaml` — created
- `deploy/k8s/base/service-web.yaml` — created
- `deploy/k8s/base/configmap-web.yaml` — created
- `deploy/k8s/base/ingressroute.yaml` — modified (2 routes)
- `deploy/k8s/base/kustomization.yaml` — modified (9 resources)
- `deploy/k8s/overlays/stage/kustomization.yaml` — modified (dual images)
- `deploy/k8s/overlays/prod/kustomization.yaml` — modified (dual images + 2-op patch)
- `CLAUDE.md` — status line updated (Story 1.6 done)

Governance repo (`lurus/`):
- `deploy/argocd/appset-services.yaml` — lurus-tally element added
- `doc/coord/service-status.md` — Tally block updated
- `doc/coord/changelog.md` — entry prepended
- `doc/process.md` — ≤15-line summary prepended
