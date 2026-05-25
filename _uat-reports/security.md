# Security Review — Lurus Tally (2b-svc-psi)
**Commits reviewed**: `12dc0001` through `1ae417a3` (11 commits, this session)
**Reviewer**: C1 (automated security review agent)
**Date**: 2026-05-25

---

## Executive Summary

| Severity | Count |
|----------|-------|
| Critical | 0 |
| High     | 1 |
| Medium   | 2 |
| Low      | 2 |
| Info     | 1 |

No SQL injection. No cross-tenant data read via crafted URL. No secrets in localStorage. No formula injection vector surviving to DB. The single High finding is a missing warehouse-to-tenant ownership check that could allow a user to write stock movements into a warehouse belonging to another tenant.

---

## Findings

| ID | Severity | File:Line | Description | Suggested Fix |
|----|----------|-----------|-------------|---------------|
| S-01 | **High** | `internal/adapter/handler/importing/handler.go:81-91` | The `warehouse_id` form field is parsed from user input and used directly as the destination warehouse for stock deduction. The importing use case passes this UUID to `RecordMovementUseCase` without verifying that the warehouse belongs to the authenticated tenant. An attacker could supply a warehouse UUID from another tenant and write stock movements into it. The stock repo advisory lock does filter by `tenant_id + product_id + warehouse_id`, but the warehouse ownership check (`WHERE tenant_id = $1 AND id = $2`) is never executed before accepting the import. | Before calling `uc.Execute`, look up the warehouse via a repo call that enforces `WHERE id = $warehouseID AND tenant_id = $tenantID`; reject with 400 if not found. The `warehouse/repo.go:GetByID` query already has this filter. |
| S-02 | **Medium** | `internal/adapter/handler/ai/handler.go:268-273` | `resolveActorID` reads `X-User-ID` header as a fallback for the Zitadel sub. This header is attacker-controlled on any request that reaches the backend (e.g. direct calls bypassing the Next.js proxy). The actor ID is written into audit logs and bill `creator_id`. An adversary authenticated as tenant A can forge `X-User-ID` to attribute writes to any user UUID. | Remove the `X-User-ID` fallback entirely. The PAT path in `AuthMiddleware` already allows M2M access without a sub. When no sub is present, fall back to `tenantID` only (as the existing comment documents for single-operator deployments), not to an arbitrary user-supplied header. |
| S-03 | **Medium** | `web/app/(dashboard)/replenish/page.tsx:21` and ~20 other client pages | `NEXT_PUBLIC_DEV_TENANT_ID` is exposed to the browser bundle. If set to a real tenant UUID in a non-dev environment, any user can read that value from the page source and use it to construct requests with an explicit `X-Tenant-ID` override on the `apiFetch` client. The backend does not read `X-Tenant-ID` from HTTP headers in production (the auth middleware injects `tenant_id` from the JWT), but the pattern is misleading and a misconfiguration risk. In a future environment where backend auth is relaxed, this creates a direct privilege escalation vector. | Audit that `NEXT_PUBLIC_DEV_TENANT_ID` is not set in any production or staging `.env`; enforce this in CI. Consider removing the `tenantId` parameter from `apiFetch` / client.ts for new endpoints introduced in this session (replenish, reports, search, importing, digest), as they all rely on the server-side proxy and do not need it. |
| S-04 | **Low** | `web/components/ai-assistant/Drawer.tsx:14-31` | Conversation history (including the text of user questions and assistant responses) is persisted to `localStorage` under `tally_ai_history`, trimmed to the last 50 messages. This data could include business-sensitive inventory details. It is not encrypted at rest and is accessible to any JS running on the same origin. | Document this in the privacy notice. Cap stored message content to a shorter limit (e.g. 200 chars per message). Offer a "clear history" button. This is low severity because same-origin JS already has full session access; it is a data-hygiene rather than a security boundary concern. |
| S-05 | **Low** | `web/components/onboarding/OnboardingWizard.tsx:38,107` | `SIGNUP_TS_KEY` (`tally_signup_ts`) is written to localStorage. This is a timestamp, not sensitive data. However, it is read and sent via telemetry as `tenant_age_minutes`, and a user could manipulate it to distort the onboarding funnel metric. | Low business risk. Accept as-is or validate server-side using tenant `created_at` from the JWT claim. |
| S-06 | **Info** | `internal/adapter/middleware/metrics.go:12-13` | `TALLY_METRICS_PER_TENANT` defaults to `true` (per-tenant label enabled). The `tally_ai_plan_executed_total{type, tenant_id}` and `tally_wad_total{tenant_id}` counters include `tenant_id` as a Prometheus label. For the current scale (tens of tenants) this is safe. At hundreds of tenants the label cardinality could strain Prometheus. | Monitor cardinality. The env knob `TALLY_METRICS_PER_TENANT=false` is correctly in place as a mitigation. No action required now. |

---

## Areas Reviewed and Found Clean

### 1. AuthN/AuthZ — All New Handlers

All seven handlers (`replenish`, `reports`, `search`, `importing`, `digest`, `onboarding`, `ai`) call `middleware.GetTenantID(c)` as their first action and abort with 401 on `uuid.Nil`. The router mounts all these routes under `api := r.Group("/api/v1")` with `authMW` applied before any handler runs (`router.go:88-96`). The middleware is a real JWT/PAT verifier; it does not read tenant from HTTP headers. Finding S-01 is a second-layer check (warehouse ownership), not a bypass of auth.

### 2. SQL Injection — All New Queries

Every new query in `repo/search/`, `repo/replenish/`, `repo/reports/`, `repo/digest/`, `repo/importing/` uses the `database/sql` parameterised query API (`$1`, `$2`, `$3` placeholders). User-controlled strings (`q`, `platform`, `sku`) are passed as bind parameters, never via string concatenation. The search ILIKE pattern (`"%"+q+"%"`) is passed as a parameter, not interpolated into the SQL string — this is safe. No SQL injection found.

### 3. Tenant Isolation / RLS

Every new repo query hard-codes `WHERE tenant_id = $1` with the middleware-validated UUID. Soft-delete is applied where relevant (`deleted_at IS NULL`). Manual cross-check: a request from tenant A with a crafted `plan_id` belonging to tenant B would hit `GetPlan(ctx, tenantA, planB_ID)` which generates the Redis key `tally:ai:plan:<tenantA>:<planB_ID>` — this key will not exist, returning nil → 404. Cross-tenant plan access is blocked by key structure.

### 4. CSV Import — Formula Injection, Size, and Encoding

The CSV parser (`app/importing/usecase.go:parseAmazonCSV`, `parseShopifyCSV`) uses the standard library `encoding/csv`. The upload is bounded to 10 MB at the handler layer (`io.LimitReader(f, maxUploadBytes)`), and again at multipart parse (`ParseMultipartForm(maxUploadBytes)`). Platform SKU strings are stored to the database only via parameterised queries. CSV field values are never written to a spreadsheet client-side; the only output is JSON over the API. Formula injection (`=CMD`, `+CMD`, etc.) has no execution path — no spreadsheet is generated server-side. The `qty` and `unit_price` fields are parsed with `decimal.NewFromString` and validated for non-negative, non-zero values. Malformed UTF-8 in CSV fields: the Go `csv.Reader` does not enforce UTF-8, but malformed bytes would reach only the database (parameterised insert), which will reject invalid UTF-8 if the DB collation is UTF-8. This is an edge case, not a security boundary.

### 5. AI Executor — Tenant Boundary on Execution

`DefaultPlanExecutor.Execute` (`app/ai/executor.go`) receives a `*domainai.Plan` that was fetched from the plan store using the tenant-scoped key. `plan.TenantID` is the field used to scope all downstream writes: `CreatePurchaseDraft(ctx, plan.TenantID, actorID, lines)`, `ApplyPriceChange(ctx, plan.TenantID, ...)`, `AdjustStock(ctx, plan.TenantID, ...)`. Product resolution (`resolveByName`) calls `SearchProducts(ctx, plan.TenantID, name)` — tenant-scoped. A crafted plan payload cannot target another tenant's products because the executor always uses `plan.TenantID` (not user input) for downstream scoping.

### 6. Audit Trail Integrity

`aiAuditWriter.Write` (`lifecycle/ai_executor.go:53`) accepts `rec.TenantID` and `rec.ActorID` from the plan object, not from request body. The actor ID is resolved via Zitadel sub or falls back to `tenantID` (with the `X-User-ID` weakness noted in S-02, but bounded to the authenticated tenant's scope — it cannot cross-tenant). Audit writes are best-effort (failures logged, not surfaced), which is appropriate since the side effect already committed.

### 7. Frontend Secrets / Token Leak

The Next.js proxy (`web/app/api/proxy/[...path]/route.ts`) injects the `accessToken` from the server-side NextAuth session into the `Authorization` header. The token is never sent to the browser in response bodies or written to localStorage or sessionStorage. `web/components/ai-assistant/Drawer.tsx` stores conversation history (not auth tokens) in localStorage. No token leak found in new files.

### 8. Server Actions — CSRF and Input Validation

`web/app/setup/page.tsx:submitProfile` is a Next.js Server Action (`"use server"`). Next.js Server Actions are automatically protected against CSRF via the framework's built-in same-origin enforcement (the `Content-Type: application/x-www-form-urlencoded` or `multipart/form-data` constraint). Input validation is performed at line 81: only `cross_border`, `retail`, and `horticulture` are accepted; anything else redirects to `/setup?error=invalid`. Session is re-fetched server-side (`await auth()`), so the token cannot be forged by the client.

### 9. Prometheus Label Cardinality

`tally_web_telemetry_total{event=...}` uses `event` as a label. The event value is validated against `AllowedWebTelemetryEvents` (7 entries) in `internal/adapter/handler/telemetry/handler.go` before `IncWebTelemetry` is called. The label cardinality is bounded at 7 — no blowup risk. The frontend allow-list in `web/app/api/otel-events/route.ts` mirrors the backend list, providing defence in depth.

### 10. NATS Subject Leakage

Business events use flat subjects (`PSI_EVENTS.bill.created`, etc.) with `tenant_id` embedded in the payload, not in the subject. This means all tenants' events share the same subject — which is intentional for a single JetStream stream. Downstream consumers must filter by `tenant_id` in the payload. This is the existing architecture; no regression introduced in this session. Web telemetry uses `PSI_TELEMETRY.web.<event_name>` (7 subjects); tenant is in the payload. No tenant_id in the subject is architecturally consistent but requires consumers to be tenant-aware.

---

_Report generated from source-code static review only. No dynamic testing was performed._
