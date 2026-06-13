# Tally STAGE UAT Ledger

Append-only run history. Each section is written once by the UAT report phase
and never edited afterwards. Verdicts here are POST-AUDIT (adversarially
re-probed), not tester self-claims.

## Run 20260610-uat1

- **Target**: `tally-stage.lurus.cn`, image tag `main-da39944` (commit `da399443`)
- **Date**: 2026-06-10

### Per-case audited verdicts

| Case | Audited verdict | Registry last_result | Pass/Fail | Evidence dir |
|------|-----------------|----------------------|-----------|--------------|
| J1 | fail (5 failed checks = pre-annotated product bug; evidence sound) | partial | 34/1 | `_uat-reports/evidence/20260610-uat1/J1` |
| J2 | fail (5 failed checks = pre-annotated product bugs; evidence sound) | partial | 46/5 | `_uat-reports/evidence/20260610-uat1/J2` |
| J3 | pass | pass | 13/0 | `_uat-reports/evidence/20260610-uat1/J3` |
| J4 | pass | pass | 10/0 | `_uat-reports/evidence/20260610-uat1/J4` |
| J5 | **AUDIT MISSING** (tester claim 32/0; not counted) | null | 32/0 (unaudited) | `_uat-reports/evidence/20260610-uat1/J5` |
| J6 | **AUDIT MISSING** (tester claim 25/0; not counted) | null | 25/0 (unaudited) | `_uat-reports/evidence/20260610-uat1/J6` |
| J7 | **AUDIT MISSING** (tester claim 26/0; not counted) | null | 26/0 (unaudited) | `_uat-reports/evidence/20260610-uat1/J7` |
| J8 | pass | pass | 29/0 | `_uat-reports/evidence/20260610-uat1/J8` |
| B1 | **AUDIT MISSING** (tester claim 110/0; not counted) | null | 110/0 (unaudited) | `_uat-reports/evidence/20260610-uat1/B1-breadth` |

Verdict mapping note: the audit vocabulary is pass/fail; the registry contract
reserves `fail` for "execution/evidence unsound — nothing counts" and defines
`partial` for "ran to completion, deliberate RED checks on real product bugs".
J1/J2 audits explicitly confirmed evidence soundness and classified every
failure as product-bug already listed in `failed_endpoints`, so they are
recorded as `partial` (failed endpoints excluded from coverage).

**Data-loss flag**: the audited-results payload delivered to the report phase
was truncated mid-J8; audit verdicts for the identity-security shard (J5, J6),
ai-assistant shard (J7), breadth shard (B1) and the frontend spec run never
arrived. Per anti-fabrication policy their tester claims were NOT promoted to
verdicts; registry left `null` and their endpoints excluded from coverage.

### Claimed vs audited discrepancies

None among audited cases. All five audited result.json files matched
independent recounts and git state:

- J1: claimed 34/1 matched; independent recount found 35 executed assertions
  (one conditional check skipped on the 500 branch) — bookkeeping nuance, not a
  discrepancy. `script_modified=true` verified: exactly lines 73+83 of
  `cases/J1.sh`, unit code `${P}-UNIT` → `${P}-U` to fit `unit_def.code`
  VARCHAR(20); stale truncated unit in evidence independently confirms root cause.
- J2: 51 assertion calls counted in script, 46/5 matches, git tree clean.
- J3/J4/J8: counts exact, scripts unmodified, evidence byte-corroborated;
  read-only re-probes (snapshots, payments list, cross-tenant 404s, billing
  401/502) all matched evidence.

### Bug list (audited, with classification)

1. **product-bug (new)** — `POST /api/v1/onboarding/seed-demo` → 500
   SQLSTATE 23502: onboarding stock adapter sets ReferenceType but never
   ReferenceID on `stock_movement` (NOT NULL after migration 34). Same
   root-cause class as UAT-REPORT P0 #1 (ai_executor) but a distinct,
   previously unreported code site (not in S-01..S-05).
   Repro: `curl -X POST -H "Authorization: Bearer $UAT_PAT_PRIMARY" -H 'Content-Type: application/json' -d '{"persona":"retail","warehouse_id":"<wh-uuid>"}' https://tally-stage.lurus.cn/api/v1/onboarding/seed-demo` → 500
2. **product-bug** — purchase bill accepts supplier id as partner_id → 500:
   suppliers live in `tally.supplier`, FK `bill_head_partner_id_fkey` points to
   `tally.partner` (SQLSTATE 23503); no `/partners` API exists.
   Repro: `curl -X POST $UAT_BASE/api/v1/purchase-bills -H "Authorization: Bearer $UAT_PAT_PRIMARY" -d '{...,"partner_id":"<supplier_id>"}'` → 500
3. **product-bug (feature gap)** — no API at da399443 writes
   `stock_initial.low_safe_qty`, so low-stock alerts can never fire.
   Repro: `GET $UAT_BASE/api/v1/stock/alerts/low-stock` → `{"count":0,"items":[]}` with p1 on-hand 5 below intended threshold 10.
4. **product-bug** — `POST /api/v1/payments` → 422 SQLSTATE 0A000: payment
   repo `SumByBill` combines FOR UPDATE with SUM aggregate
   (internal/adapter/repo payment repo.go:128). Two further J2 RED checks
   (status==recorded, list amount==100) are cascades of this one.
   Repro: `curl -X POST $UAT_BASE/api/v1/payments -H "Authorization: Bearer $UAT_PAT_PRIMARY" -d '{"bill_id":"<purchase_bill>","amount":"100",...}'` → 422 payment_error
5. **product-bug (minor, observed not failing)** — PAT-authenticated
   `POST /api/v1/replenish/draft-batch` rejects missing creator (uuid.Nil path,
   create_purchase.go:69) as 500 `internal_error` "creator_id is required"
   instead of a 4xx. Asserted as deployed contract in J3.

Failed-check tally by classification: product-bug 6 (J1×1 + J2×5, of which 2
cascades); script-bug 0; env-blocked 0; fabricated 0.

### AI budget

`.ai_calls` counter at `_uat-reports/evidence/20260610-uat1/.ai_calls` = **2**
(both in J7), within the ≤3 budget. Recorded as `ai_calls: 2` on J7.

### Coverage

`bash scripts/uat/coverage.sh` → **38/101 = 37%**, gate **FAIL (exit 1)**,
matrix at `_uat-reports/coverage-matrix.md`. The shortfall is dominated by the
missing audits: J5/J6/J7/B1 endpoints (account/*, auth/pats, ai/*, shopify/*,
imports/*, webhooks, internal/*, nursery-dict, projects, currencies,
exchange-rates, sale-bills CRUD, restore endpoints, reports abc/dead-stock,
tenant/profile, logout) all executed green per tester result.json but count as
uncovered until audited.

### Frontend spec results

**Not received.** No frontend report artifact exists under `_uat-reports/` for
this run and the frontend shard result was lost in the same payload truncation.
No browser-spec verdicts are claimed.

### Gaps (uncovered endpoints, one-line reasons)

- All J5/J6/J7/B1-only endpoints (≈55 routes incl. `GET /api/v1/account/*`,
  `POST /api/v1/auth/pats`, `POST /api/v1/ai/chat` + plans lifecycle,
  `POST /api/v1/imports/orders`, shopify shops + webhooks, nursery-dict,
  projects, currencies/exchange-rates, sale-bills CRUD/approve/cancel, all
  `:id/restore`, `GET /internal/v1/*`, `POST /internal/v1/telemetry/web`,
  `POST /api/v1/tenant/profile`, `POST /api/v1/auth/logout`,
  `GET /api/v1/warehouses`, `GET /api/v1/sale-bills`,
  `GET /api/v1/reports/{abc,dead-stock}`) — audit verdicts lost in truncated
  payload; registry null, nothing counts.
- `POST /api/v1/onboarding/seed-demo` — in J1 failed_endpoints (bug #1).
- `POST /api/v1/payments`, `GET /api/v1/stock/alerts/low-stock` — in J2
  failed_endpoints (bugs #3/#4).
- `PUT /api/v1/{products,suppliers,warehouses,sale-bills,projects,nursery-dict}/:id` —
  only exercised by B1 (audit missing).

### Fabrication flags

None in the five audited cases — all claims corroborated by independent
re-probes. Flag instead: **audit pipeline data loss** (truncated shard payload)
prevented verdicts for 4 of 9 cases; treat their green tester claims as
unverified, not as failures.

## Run 20260610-uat1 — supplement (lost audit payload recovered)

The original report phase received a truncated audit payload (inline JSON
sliced at 12000 chars by the workflow script) and correctly refused to promote
unaudited tester claims. The full audit verdicts WERE produced by the
Supervise phase and are recorded in the workflow return value; the main
session recovered them verbatim and completes the record here. The workflow
script has been fixed to use file-based audit transport (see commit).

### Recovered audited verdicts

| Case | Audited verdict | Registry last_result | Pass/Fail | Notes |
|------|-----------------|----------------------|-----------|-------|
| J5 | pass | pass | 32/0 | 14 independent re-probes corroborate; auditor design note: `DELETE /account/sessions/:id` is tenant-only — returns 204 under PAT for any id (review-worthy, not a failing check) |
| J6 | pass | pass | 25/0 | Webhook-shadowing confirmed independently: `POST /webhooks/shopify/orders` → 307 to `test-tally.lurus.cn/login` — Next.js edge middleware must exclude `/webhooks/*`; NEW routing defect, not in S-01..S-05. S-01 cross-tenant import guard UNVERIFIED under PAT (creator_id gate fires first) — honestly disclaimed |
| J7 | pass | pass | 26/0 | Exactly 2 LLM calls (within ≤3 budget); confirm(+5)→revert(0) stock math re-probed; semantic note: reverted plan surfaces as status=cancelled (no distinct reverted status) |
| B1 | pass | pass | 110/0 | 93 evidence steps, statuses re-checked 0 mismatches; script fixes verified to not weaken assertions; tester-noted transient pbill-approve 500 on empty-id cascade (robustness ticket candidate, outside final checks) |

### Frontend (Playwright vs STAGE) — audited

Artifacts genuine (report.json stats {total:18, expected:8, unexpected:7,
skipped:3} match claims; traces/videos present). Per-spec:

| Spec | Result | Audited classification |
|------|--------|------------------------|
| uat-stage.setup.ts | 2 pass | login via dev-provider PAT session works end-to-end |
| uat-decisions.spec.ts | 1P/3F | **test-bug** — hardcoded `TEST_WAREHOUSE_ID` not adapted for REAL mode; + minor product-bug: backend maps FK violation to 500 instead of 422 |
| uat-onboarding.spec.ts | 3F/2S | **product-bug (same as Bug 1)** — seed-demo 500 (`stock_movement.reference_id` NOT NULL since migration 34); the auditor corrected the tester's wrong root-cause guess using the run's own error-context snapshot |
| uat-palette.spec.ts | 5P/1F/1S | **test-bug** — ambiguous locator (strict-mode violation); palette p50=110ms/p95=138ms measured via proxy+WAN (informational, 200ms gate not asserted in REAL mode) |

### Corrected coverage

`coverage.sh` after applying recovered verdicts: **98 / 101 = 97% — gate PASS**
(threshold 90%). The 3 uncovered endpoints are the deliberate-RED product
bugs: `POST /onboarding/seed-demo`, `POST /payments`,
`GET /stock/alerts/low-stock`.

### Additional bug (frontend-surfaced, backend-rooted)

6. **product-bug (minor)** — purchase-bill create with nonexistent warehouse
   FK violation surfaces as 500 `internal_error` instead of 422
   (uat-decisions REAL-mode failure; spec comment itself expects 422).

### Workflow changes applied after this run (main-session reviewed)

1. Audit shards now write verdict JSON to
   `_uat-reports/evidence/<run>/.audit/<shard>.json`; the reporter reads from
   disk — inline-payload truncation can no longer lose verdicts.
2. Auditors emit the registry taxonomy directly (pass/partial/fail/blocked).
3. Report phase hard-fails when any executed case lacks an audit verdict.
4. Reporter prompt field-order text synced with the actual registry contract
   (failed_endpoints included).
