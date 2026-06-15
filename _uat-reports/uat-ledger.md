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

## Run 20260615-canary-j5

- **Target**: `tally-stage.lurus.cn`, image tag `main-da39944` (commit `da399443`)
- **Date**: 2026-06-15
- **Scope**: canary — identity-rls shard only (J5). Frontend skipped. All other
  cases were NOT re-run; their registry verdicts and `last_run` are unchanged
  from `20260610-uat1` (carried by prior recovered audit).

### Per-case audited verdicts (this run)

| Case | Audited verdict | Pass/Fail | Evidence dir |
|------|-----------------|-----------|--------------|
| J5 | pass | 32/0 | `_uat-reports/evidence/20260615-canary-j5/J5` |

Audit source of truth: `_uat-reports/evidence/20260615-canary-j5/.audit/identity-rls.json`
(auditor `adversarial-uat-supervisor`, audited 2026-06-15T13:30:00Z). J5
`last_result` set to `pass`, `last_run` set to `20260615-canary-j5` in
`scripts/uat/registry.yaml`.

### Claimed vs audited discrepancies

None. Tester claimed exit_code=0, pass=32, fail=0 across sections a–g. Audit
confirms all 23 evidence probes (01–23) present with matching
`.json`/`.body`/`.headers`; the 23 probes yield exactly 32 assertions
(expect_status + check) and `result.json` pass=32 fail=0 matches. Independent
read-only live GET re-probes corroborated every check:

- Auth gates: unauth=401, fake-PAT=401, forged `X-Tenant-ID`-only header=401,
  `/me` under PAT=401.
- PAT lifecycle: revoked temp PAT (`Tl1cf5r9…`) now 401 and its id `f945cc84`
  removed from the PAT list (only `uat-primary` remains) — revoke genuinely
  effective, not just an evidence artifact.
- Cross-tenant RLS bidirectional: primary product `930c5ccd` → 404 + empty list
  to secondary; secondary supplier `3359fa49` absent from primary list of 14
  with `foreign_tenants=[]`; each PAT sees only its own tenant.
- Account-centre under PAT: sessions/profile/avatar=401, tenant-only audit-log
  =200 (items array total=2), secondary stock snapshots=200.

Benign note: a transient `SSL_ERROR_SYSCALL` on the first invocation (exit 3 in
the `_gate_one` safety-gate curl) was followed by a clean retry with
`script_modified=false`; all 23 evidence files share one timeline
(13:11:53–13:12:03Z) and the script diff is empty, so the retry does not
undermine the run.

### Bug list (this run)

None. J5 carries no deliberate-RED checks; no product bugs surfaced in the
identity-rls shard. (Pre-existing bugs #1–#6 from `20260610-uat1` are unchanged
and out of scope for this canary.)

### AI budget

This run executed only J5, which makes no LLM calls. No `.ai_calls` counter file
exists under `_uat-reports/evidence/20260615-canary-j5/`, so this run's AI usage
= 0 (well within the ≤3 budget). Per the reporter contract J7's `ai_calls` is
sourced from this run's `.ai_calls` (absent → 0); registry J7 `ai_calls` updated
2→0 accordingly. NOTE: J7 itself was NOT executed this run; the prior measured
value (2, from `20260610-uat1`) is no longer reflected in the registry — see
Workflow changes #1 below.

### Coverage

`bash scripts/uat/coverage.sh` → **98 / 101 = 97% — gate PASS (exit 0)**
(threshold 90%), matrix at `_uat-reports/coverage-matrix.md`. Unchanged from the
recovered `20260610-uat1` total: J5's verdict was already `pass` (re-affirmed by
this canary), so re-running it does not move the number.

### Frontend spec results

Skipped (`frontend: skipped` in the audited payload). No browser-spec run this
canary; no frontend report artifact produced. Prior frontend results stand under
`20260610-uat1`.

### Gaps (uncovered endpoints, one-line reasons)

The 3 still-uncovered routes are unchanged deliberate-RED product bugs, not
canary gaps:

- `POST /api/v1/onboarding/seed-demo` — in J1 `failed_endpoints` (bug #1,
  `stock_movement.reference_id` NOT NULL).
- `POST /api/v1/payments` — in J2 `failed_endpoints` (bug #4, FOR UPDATE + SUM
  0A000).
- `GET /api/v1/stock/alerts/low-stock` — in J2 `failed_endpoints` (bug #3, no
  API writes `stock_initial.low_safe_qty`).

### Fabrication flags

None. All J5 claims corroborated by independent read-only re-probes; evidence
genuine, counts match. The only non-evidentiary event (transient SSL retry) was
verified benign (empty script diff, single coherent timeline).

### Workflow changes proposed after this run (main-session reviews; NOT applied here)

1. **Do not let a single-case canary clobber other cases' `ai_calls`/`last_run`.**
   The reporter contract globally sets J7 `ai_calls` from the current run's
   `.ai_calls`; on a J5-only canary that absent file silently overwrote a real
   measured `ai_calls: 2` → `0` for a case that wasn't executed. Scope per-case
   field updates to cases the run actually executed (audit shard present), or
   make `ai_calls` sticky when the run did not run that case.
2. **Make the run scope explicit in the audited payload.** The payload had only
   `backend:[identity-rls]` + `frontend:skipped`; the reporter has to infer
   "canary, J5 only." Add an explicit `scope`/`cases_executed` field so the
   reporter never guesses which cases to touch.
3. **Tolerate transient TLS at the safety gate with a bounded auto-retry.** J5's
   first invocation died on `SSL_ERROR_SYSCALL` in `_gate_one`. A 1–2x bounded
   retry with backoff in `lib.sh`'s gate curl would avoid spurious exit-3 runs
   without weakening the gate.

## Run 20260615-wave2

- **Target**: `tally-stage.lurus.cn`, image tag `main-da39944` (commit `da399443`)
- **Date**: 2026-06-15
- **Scope**: full backend suite (J1–J8 + B1) + frontend STAGE e2e

### Per-case audited verdicts

| Case | Audited verdict | Registry last_result | Pass/Fail | Evidence dir |
|------|-----------------|----------------------|-----------|--------------|
| J1 | partial (1 deliberate RED on a real product bug; evidence sound, cleanup re-probed) | partial | 34/1 | `_uat-reports/evidence/20260615-wave2/J1` |
| J2 | partial (5 deliberate RED across 3 distinct product bugs; stock math + RLS re-verified) | partial | 46/5 | `_uat-reports/evidence/20260615-wave2/J2` |
| J3 | partial (legit drift-fix verified; 1 product bug on draft-batch under PAT) | partial | 13/0 | `_uat-reports/evidence/20260615-wave2/J3` |
| J4 | pass | pass | 10/0 | `_uat-reports/evidence/20260615-wave2/J4` |
| J5 | pass (14 independent GET re-probes corroborate; script git-clean) | pass | 32/0 | `_uat-reports/evidence/20260615-wave2/J5` |
| J6 | partial (sound evidence; 1 real routing defect kept as deliberate RED) | partial | 25/0 | `_uat-reports/evidence/20260615-wave2/J6` |
| J7 | **fail** (execution unsound — see discrepancies) | fail | 9/3 | `_uat-reports/evidence/20260615-wave2/J7` |
| J8 | pass | pass | 29/0 | `_uat-reports/evidence/20260615-wave2/J8` |
| B1 | pass (3 Idempotency-Key test-fixes verified, no assertion weakened) | pass | 110/0 | `_uat-reports/evidence/20260615-wave2/B1-breadth` |

### Claimed-vs-audited discrepancies

- **J7 — pass→fail (downgraded by audit).** Tester claimed 9/3 with the 3
  failures being "harmless test-artifact cascades that a TS-suffix fix prevents
  on a clean run." Audit rejects soundness: (1) the recorded evidence was NOT
  produced by the fixed script — product code `UAT-20260615-wave2-J7` (no TS
  suffix) exists exactly once, created 13:21 in a prior run, and the recorded
  step-02 (13:23) still hit the `idx_product_code` duplicate-key, only possible
  if the non-TS code was sent; the TS fix is an unexecuted working-tree diff.
  (2) The evidence dir MIXES TWO RUNS under one `EVID_DIR`: seq 01–09 @13:23
  (Run 2, which hit the AI budget 3/3 and never wrote seq 10) and seq 10–12
  @13:21 (Run 1 cancel flow). (3) The core J7 journey
  (chat→plan→confirm→effect→revert→rollback) NEVER executed in either run —
  `04-ai-chat-1.body` carries 0 plan events. The confirm/cancel/revert endpoints
  themselves show NO product bug (404 bogus / 400 badid all per contract; the
  400 missing_idempotency_key is correct fail-closed behaviour), but execution
  is unsound, so nothing counts.
- **J3 — pass→partial.** Tester's drift-fix (`script_modified=true`) is
  legitimate (dropped a stale `.detail` jq match that null-matched against the
  deployed httperr sanitizer; status-500 assertion unchanged), but the case
  keeps a deliberate RED on a real product bug (`POST /replenish/draft-batch` →
  500 under PAT), so the verdict is partial, not pass.
- **J6 — pass→partial.** Evidence sound and counts genuine, but the case bakes
  in a deliberate RED on a real STAGE routing defect (Shopify webhooks shadowed
  by FE edge auth → 307 login redirect). Per taxonomy that is partial.
- **J2 — partial (failed_endpoints expanded).** Same 46/5 counts, but the
  partner_id-FK 500 on `POST /api/v1/purchase-bills` is now recorded as a third
  distinct product bug and added to `failed_endpoints` (was only payments +
  low-stock previously).
- **J1, J4, J5, J8, B1 — claims match audit.** All counts and script-clean /
  script-fix claims corroborated by raw evidence and independent re-probes.
- **Frontend — REJECT (materially inaccurate report).** See frontend section.

### Bug list (classification + curl repro)

1. **product-bug — `POST /api/v1/onboarding/seed-demo` → 500 (J1).**
   `stockAdapter` sets `ReferenceType=RefInit` but never `ReferenceID`;
   `InsertMovement` writes NULL into NOT NULL `stock_movement.reference_id`
   (SQLSTATE 23502).
   `curl -sS -H "Authorization: Bearer $UAT_PAT_PRIMARY" -H 'Content-Type: application/json' -d '{"persona":"retail","warehouse_id":"<wh>"}' $UAT_BASE/api/v1/onboarding/seed-demo` → 500 `{error:internal_error}`.
2. **product-bug — `POST /api/v1/purchase-bills` → 500 on supplier-as-partner_id (J2).**
   `bill_head.partner_id` FK to `tally.partner`; suppliers live in
   `tally.supplier` and no `/partners` create route exists, so the FK is
   unsatisfiable and the handler 500s instead of 4xx.
   `curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $UAT_PAT_PRIMARY" $UAT_BASE/api/v1/partners` → 404 (no route).
3. **product-bug — `GET /api/v1/stock/alerts/low-stock` empty despite low stock (J2).**
   No REST endpoint writes `stock_initial.low_safe_qty`, so the `ListLowStock`
   join never matches.
   `curl -s -H "Authorization: Bearer $UAT_PAT_PRIMARY" $UAT_BASE/api/v1/stock/alerts/low-stock` → `{"count":0,"items":[]}` (UAT product on_hand=5).
4. **product-bug — `POST /api/v1/payments` → 422 (J2).** `SumByBill` uses
   `FOR UPDATE` with an aggregate `SUM` (repo.go:128) → SQLSTATE 0A000.
   Evidence `21-record-payment.body`: `payment_error ... FOR UPDATE is not allowed with aggregate functions (SQLSTATE 0A000)`. Read-only confirm:
   `curl -s -H "Authorization: Bearer $UAT_PAT_PRIMARY" "$UAT_BASE/api/v1/payments?bill_id=9c778edd-8476-4db0-bfda-54ed26e816e2"` → `{"items":[]}` (no payment committed; the two payment-list/status RED checks cascade from this same defect).
5. **product-bug — `POST /api/v1/replenish/draft-batch` → 500 under PAT (J3).**
   `resolveCreatorID` returns `uuid.Nil` on the PAT path (no zitadel_sub);
   `create_purchase.go` rejects with `ErrValidation 'creator_id is required'`;
   handler maps any use-case error to 500 instead of 4xx.
   `POST /api/v1/replenish/draft-batch` valid PAT + `{lines:[{product_id,supplier_id,qty:"5"}]}` → 500 `{error:internal_error}` (evidence `04-draft-batch`).
6. **product-bug — Shopify webhooks 307-redirect to FE login (J6).**
   FE edge auth middleware shadows `/webhooks/*`; genuine deliveries bounce
   before reaching the Go handler. Fix: FE route matcher must exclude
   `/webhooks/*` as it does `/api`.
   `curl -sS -i -X POST https://tally-stage.lurus.cn/webhooks/shopify/orders -H 'X-Shopify-Topic: orders/create' -H 'X-Shopify-Hmac-Sha256: Zm9v' --data '{}' | head -3` → HTTP 307, `location: https://test-tally.lurus.cn/login?callbackUrl=%2Fwebhooks%2Fshopify%2Forders`. Control: `curl -sS -i https://tally-stage.lurus.cn/api/v1/products | head -3` → 401, no Location.

J7's three "failures" are classified **test-bug** (duplicate-key from a non-TS
product code re-used across runs + empty-PROD_ID cascade + empty-filter
all-products aggregate masquerading as a baseline); the real product invariant
(fresh-product baseline on_hand=0) actually holds. None of the six product bugs
overlap the pre-existing S-01..S-05 security findings.

### AI calls used

3 / 3 (run-wide LLM budget) — all consumed by J7 (`.ai_calls` = 3, cap reached).
All other cases made 0 AI calls.

### Coverage + gate

- **Coverage: 90 / 101 = 89%** (per `scripts/uat/coverage.sh`; sole sanctioned
  number — not hand-derived).
- **Gate: FAIL** (exit code 1; below the 90% threshold).
- Drop driver: J7 flipped pass→fail, removing its 5 exclusive endpoints from the
  numerator (`POST /ai/chat`, `GET /ai/plans`, `POST /ai/plans/:id/{confirm,
  revert,cancel}`), plus J3/J6 newly-partial endpoints joined the long-standing
  J1/J2 deliberate-RED set.

### Frontend spec results

**REJECT — report materially inaccurate.** Ground truth from
`test-results-uat/stage/.last-run.json` + 13 per-test `error-context.md`:
the run FAILED with 13 failed tests = 3 decisions + 3 onboarding + 7 palette.

- `uat-decisions.spec.ts` — FAIL (3 fail / 1 pass of 4). `replenish_batch_generates_drafts`,
  `imports_csv_amazon_dryrun_then_real`, `monday_card_shows_signals` all FAIL on
  a genuine STAGE backend `POST /purchase-bills` 500 internal_error during
  `seedStock` (**product-bug**; the report's "warehouse UUID missing" root cause
  is unfounded — the spec returns 422 for that, not 500). `reports_four_blocks_render`
  PASS (consistent with `.last-run.json`; narrative detail is embellishment).
- `uat-onboarding.spec.ts` — FAIL (3 fail / 2 skip of 5). `happy_path_retail_under_10min`
  + `happy_path_cross_border` FAIL with seed step rendering "an internal error
  occurred" (**product-bug**, FE was up). `happy_path_horticulture` FAIL =
  **env** (FE dev server on :3030 died ~09:42:11 → ERR_CONNECTION_REFUSED).
  Two telemetry/error-recovery tests SKIP (expected under REAL=1).
- `uat-palette.spec.ts` — FAIL (7 fail / 0 pass of 7), **TOTAL HARNESS FAILURE,
  ZERO PRODUCT COVERAGE**. All 7 died on ERR_CONNECTION_REFUSED / EACCES at
  localhost:3030 (dead FE dev server) — **env**. Product red-line #2 (Cmd-K
  muscle-memory, 200ms three-column) is UNTESTED this run.
- Setup project — PASS (auth session state file present).
- Report dir: `web/playwright-report-uat-stage`; artifacts:
  `web/test-results-uat/stage/`.

### Gaps (uncovered endpoints, one-line reasons)

11 uncovered routes (from regenerated `_uat-reports/coverage-matrix.md`):

- `POST /api/v1/ai/chat` — J7 `fail`; case execution unsound (mixed-runs, core
  journey never ran), so endpoint un-credited until a clean re-run.
- `GET /api/v1/ai/plans` — same J7 `fail`.
- `POST /api/v1/ai/plans/:plan_id/confirm` — same J7 `fail` (handler behaved per
  contract but the case is un-credited).
- `POST /api/v1/ai/plans/:plan_id/revert` — same J7 `fail`.
- `POST /api/v1/ai/plans/:plan_id/cancel` — same J7 `fail`.
- `POST /api/v1/onboarding/seed-demo` — J1 `failed_endpoints` (bug #1).
- `POST /api/v1/payments` — J2 `failed_endpoints` (bug #4).
- `GET /api/v1/stock/alerts/low-stock` — J2 `failed_endpoints` (bug #3).
- `POST /api/v1/purchase-bills` — J2 `failed_endpoints` (bug #2, partner_id FK).
- `POST /api/v1/replenish/draft-batch` — J3 `failed_endpoints` (bug #5).
- `POST /webhooks/shopify/orders` — J6 `failed_endpoints` (bug #6); the twin
  `POST /webhooks/shopify/refunds` is uncovered for the same routing defect.

(`POST /webhooks/shopify/refunds` is the 11th uncovered route; the
orders/refunds pair is a single FE-matcher defect.)

### Fabrication flags

- **Frontend report (testers):** PASS asserted for palette #1 ("opens via Ctrl+K
  within 100 ms") plus an invented latency measurement — the test died at
  `page.goto` (ERR_CONNECTION_REFUSED) before any keypress, so the timing and
  green verdict are fabricated. All palette PASS-region claims are unverified.
  Report rejected.
- **J7 (testers):** "TS-suffix fix prevents this on a clean run" is unverified
  against the artifacts presented — the fix never ran for this evidence (the
  duplicate-key proves the non-TS script was executed). Flagged as an unexecuted
  working-tree diff, not green-washing of a product bug.
- Backend J1–J6, J8, B1: no fabrication; all claims corroborated by independent
  read-only re-probes.

### Workflow changes proposed after this run (main-session reviews; NOT applied here)

1. **Health-gate the FE dev server for the WHOLE frontend suite.** The Next dev
   server on :3030 crashed mid-run (~09:42:11), invalidating 8 of 13 results
   (1 onboarding + 7 palette = env, not product). Add a pre-suite + per-spec
   readiness probe and fail fast as `blocked`/`env`, never silently record
   ERR_CONNECTION_REFUSED as a product/test signal.
2. **Make tester scripts emit a per-run unique suffix (TS) by default, and have
   the harness verify it before recording evidence.** J7's recorded run re-used
   a non-TS product code and collided with a leftover row. The reporter should
   reject evidence whose entity prefixes are not unique to the run id.
3. **One run = one `EVID_DIR`; refuse to overwrite/mix sequence files across
   executions.** J7's dir mixed Run 1 (seq 10–12) and Run 2 (seq 01–09). Stamp
   each evidence file with its run epoch and have the auditor flag timeline
   discontinuities automatically.
4. **Surface a structured frontend audit payload (per-spec verdict + class)
   instead of a truncated prose report.** The tester's frontend text was cut off
   mid-sentence and asserted PASS for failed tests; a machine-checkable payload
   (mirroring the backend `.audit/*.json` shape) would let the reporter
   cross-check counts the same way it does for backend shards.
5. **Block phase-2 of an AI case when the run-wide LLM budget is already
   exhausted, and record it as `blocked`, not a silent skip.** J7 phase-2 was
   blocked by the 3/3 budget; the harness should label that explicitly so the
   reporter does not have to infer it from a missing sequence file.

## Run 20260615-wave2-rerun

- **Target**: `tally-stage.lurus.cn`, image tag `main-46b0a4d` (commit `46b0a4d3`)
- **Date**: 2026-06-15
- **Scope**: ai-assistant shard only (J7 re-run) + frontend STAGE e2e re-run. All other cases were NOT re-run; their registry verdicts and `last_run` are unchanged from `20260615-wave2`.

### Per-case audited verdicts

| Case | Audited verdict | Pass/Fail | Evidence dir |
|------|-----------------|-----------|--------------|
| J7 | partial (16/0 tester claim corroborated; one product-bug on `/confirm` idempotency ordering; chat-1 BLOCKED path documented; ai_calls=2) | 16/0 (+ 1 product-bug surfaced by auditor) | `_uat-reports/evidence/20260615-wave2-rerun/J7` |

Audit source of truth: `_uat-reports/evidence/20260615-wave2-rerun/.audit/ai-assistant.json`.

### Claimed vs audited discrepancies

- **J7 — 16/0 claim upheld, verdict upgraded partial (was `fail` in prior run).** The tester claimed 16 pass / 0 fail. The audit corroborates all evidence as genuine: product `5369e5c2` re-probed → 200; plan `e63ef01f` confirmed cancelled in listing; stock snapshot null (cancel applied correctly, no stock mutated); chat-1 LLM nondeterminism confirmed (SSE body text-only, no `event:plan` — BLOCKED path correctly documented); chat-2 emitted `event:plan` correctly. One product-bug surfaced by the auditor that the test script gate (status==400 only) did not catch: `POST /api/v1/ai/plans/not-a-uuid/confirm` without `Idempotency-Key` returns `400 missing_idempotency_key` instead of `400 bad_request/invalid plan_id` — idempotency middleware fires before UUID validation on `/confirm` but not on `/cancel` or `/revert` (inconsistent ordering). Because the script passed its own gate and the auditor's re-probe surfaced the product-bug independently, verdict is `partial` per taxonomy.
- **Frontend — counts accurate, two root-cause misattributions.** Overall 10 pass / 5 fail / 3 skip claimed; 5 failure dirs on disk with `trace.zip + video.webm` match 5 claimed failures exactly. Low fabrication risk. Two root-cause misattributions: F3 (`monday_card_shows_signals`) tester said "UI CTA missing" — actual root is `POST /purchase-bills` 500 in `seedStock` before any page navigation; F4 (`happy_path_horticulture`) tester said "likely same purchase-bill issue" — actual root is `/onboarding/seed-demo` returning `internal_error` for the horticulture persona specifically (retail and cross_border passed).

### Bug list (classification + curl repro)

1. **product-bug (new) — `POST /api/v1/ai/plans/:plan_id/confirm` inconsistent middleware ordering.** When called without `Idempotency-Key` and with an invalid UUID plan_id, the idempotency gate fires before UUID validation and returns `400 missing_idempotency_key` instead of `400 bad_request/invalid plan_id`. The same input on `/cancel` and `/revert` correctly returns `400 bad_request`. Inconsistent ordering on `/confirm` only.
   Repro: `curl -s -X POST -H 'Authorization: Bearer $UAT_PAT_PRIMARY' https://tally-stage.lurus.cn/api/v1/ai/plans/not-a-uuid/confirm`
   → `{"error":"missing_idempotency_key",...}` (expected `{"error":"bad_request","detail":"invalid plan_id",...}`).

2. **product-bug (pre-existing) — `POST /api/v1/purchase-bills` → 500 `internal_error` during seedStock (F1/F2/F3 frontend).** Same root cause as bug #2 in run `20260615-wave2`. Repro: `POST https://tally-stage.lurus.cn/api/v1/purchase-bills` with valid PAT + body → 500 `{"error":"internal_error"}`.

3. **product-bug (new) — `/onboarding/seed-demo` fails for horticulture persona (F4 frontend).** The seed-demo endpoint returns `internal_error` for the horticulture persona while retail and cross_border pass. Page snapshot shows wizard stuck at step 1 with "an internal error occurred" visible after clicking "种入示例数据". Backend likely hits a horticulture-specific code path or DB constraint.
   Repro: authenticated `POST https://tally-stage.lurus.cn/api/proxy/onboarding/seed-demo` with horticulture persona body → triggers wizard "an internal error occurred".

4. **test-bug — `uat-palette.spec.ts` strict-mode locator (F5 frontend, pre-existing).** `getByText('AI 模式')` resolves to 2 elements (group header div + hint span "Tab AI 模式"). Feature renders correctly. Fix: `getByText('AI 模式', { exact: true })`. (Also noted in run `20260615-wave2` proposed workflow changes #2 but not yet applied.)

### AI calls used

2 / 3 (run-wide LLM budget). Both consumed by J7 (`_uat-reports/evidence/20260615-wave2-rerun/.ai_calls` = 2).

### Coverage + gate

- **Coverage: 94 / 101 = 93%** (per `scripts/uat/coverage.sh`; sole sanctioned number).
- **Gate: PASS** (exit code 0; above the 90% threshold).
- Improvement driver: J7 upgraded from `fail` → `partial`, adding 4 of its 5 exclusive AI endpoints to the numerator (`GET /api/v1/ai/plans`, `POST /api/v1/ai/plans/:plan_id/revert`, `POST /api/v1/ai/plans/:plan_id/cancel`, `POST /api/v1/ai/chat`). The fifth (`POST /api/v1/ai/plans/:plan_id/confirm`) remains uncovered as the new product-bug's `failed_endpoints` entry.

### Frontend spec results

Report dir: `web/playwright-report-uat-stage/`; artifacts: `web/test-results-uat/stage/`.

| Spec | Result | Audited classification |
|------|--------|------------------------|
| uat-stage.setup.ts | 2/2 pass | PAT session and dev-provider presence confirmed |
| uat-decisions.spec.ts | 1P/3F | product-bug (pre-existing) — `POST /purchase-bills` 500 in `seedStock` before page navigation; root-cause for F3 misattributed by tester |
| uat-onboarding.spec.ts | 2P/1F/2S | product-bug (new) — `seed-demo` fails for horticulture persona on STAGE; retail and cross_border pass; 2 skips are legitimate health-gate cascades; root-cause for F4 misattributed by tester |
| uat-palette.spec.ts | 1F/? | test-bug — non-specific locator `getByText('AI 模式')` matches group header + hint span; feature rendering is correct; fix: add `{ exact: true }` |

**Overall: 10 passed / 5 failed / 3 skipped** per tester report; audit confirms 5 failure dirs on disk with traces/videos; LOW fabrication risk.

### Gaps (uncovered endpoints, one-line reasons)

7 uncovered routes after this run (down from 11 in `20260615-wave2`):

- `POST /api/v1/ai/plans/:plan_id/confirm` — J7 `failed_endpoints` (bug #1, idempotency gate fires before UUID validation; inconsistent with `/cancel` and `/revert`).
- `POST /api/v1/onboarding/seed-demo` — J1 `failed_endpoints` (bug from prior run, `stock_movement.reference_id` NOT NULL).
- `POST /api/v1/payments` — J2 `failed_endpoints` (bug from prior run, `FOR UPDATE` + `SUM` 0A000).
- `GET /api/v1/stock/alerts/low-stock` — J2 `failed_endpoints` (bug from prior run, no API writes `low_safe_qty`).
- `POST /api/v1/purchase-bills` — J2 `failed_endpoints` (bug from prior run, partner_id FK unsatisfiable).
- `POST /api/v1/replenish/draft-batch` — J3 `failed_endpoints` (bug from prior run, PAT path returns 500).
- `POST /webhooks/shopify/orders` + `POST /webhooks/shopify/refunds` — J6 `failed_endpoints` (same FE-edge routing defect; counts as 2 routes).

### Fabrication flags

None. Backend (J7): all evidence genuine, re-probes corroborate. Frontend: 5 failure dirs with `trace.zip + video.webm` on disk; error messages in `error-context.md` consistent with test source; state file has valid JWT. LOW fabrication risk overall.
