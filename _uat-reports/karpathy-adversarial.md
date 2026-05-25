# Karpathy Four-Question + Adversarial Edge-Case Review
Generated: 2026-05-25 · Reviewer: C3 (read-only, retry run)

---

## Req 1: AI Plan Exec
**4Q**:
1. FAIL — `PlanType` is a string alias (`domainai.PlanType`), not an enum; unsupported plan types reach `Execute()` as a runtime error (`executor.go:101`). At type-level nothing prevents an unknown string from being stored in Redis and replayed.
2. FAIL — `maxToolRounds=6` caps the LLM loop (`orchestrator.go:108`) but **no cap** exists on `payload.Items` length in `execPurchase` (`executor.go:116-133`); a malicious plan with 10k items triggers 10k DB round-trips per confirm.
3. PASS — `ConfirmPlan` returns `ErrPlanNotFound`, `ErrPlanExpired`, or a wrapped error; comment at line 276 documents retry vs re-flow semantics clearly.
4. PASS — `Orchestrator` depends on `PlanStore` (interface) + `PlanExecutor` (interface) + `AuditWriter` (interface); all three are nil-safe; `orchestrator_test.go` exercises the core loop with stubs.

**EC**:
- EC1 FAIL@executor.go:186-189 — `execStockAdjust` iterates products serially; on partial failure it returns a non-nil `ExecutionResult` **and** a non-nil error simultaneously; most callers only check `err != nil` and will drop the `affected_count`.
- EC2 NEEDS-CHECK@orchestrator.go:302-321 — status is flipped to `Confirmed` before `Execute`; on execute failure it reverts to `Pending`. If the revert itself fails the plan is stuck `Confirmed` with no side-effects and the comment (line 275) acknowledges this. The handler's idempotency story for a stuck plan is absent.
- EC3 PASS@orchestrator.go:288-297 — `ExpiresAt.IsZero()` guard prevents the zero-value `time.Time` from triggering a spurious expiry.

---

## Req 2: Audit + Undo
**4Q**:
1. PASS — `AuditRecord` is a concrete struct; `AuditWriter` is an interface; `aiAuditWriter` is the only production impl (`lifecycle/ai_executor.go:44`). Types are sufficient.
2. PASS — audit writes are fire-and-forget; `Write` is called once per confirm; no loops.
3. PASS — `recordAudit` swallows errors by design (comment at `orchestrator.go:330`); caller never sees audit failure; responsibility is on the writer to log (`ai_executor.go:62`).
4. PASS — `aiAuditWriter` wraps `AppendAuditLog` which takes a `context.Context`; unit-testable by stubbing `AuditRepo`. Frontend `UndoStack` is a pure in-memory class (`web/lib/undo/undo-stack.ts`) fully testable without DOM.

**EC**:
- EC1 FAIL@web/lib/undo/undo-stack.ts:25-27 — stack is a **module singleton**; in SSR (Next.js server component render) `globalUndoStack` would be shared across concurrent requests. The `typeof window === "undefined"` guard in `telemetry.ts` is present but absent from undo-stack. The `revert` functions call fetch — if invoked server-side they will throw.
- EC2 NEEDS-CHECK@lifecycle/ai_executor.go:53-69 — `Write` returns the error but `recordAudit` at `orchestrator.go:333` uses `_ = o.audit.Write(...)`, so a persistent audit-backend failure is silent beyond the `slog.Warn`. No alerting hook.
- EC3 PASS@undo-stack.ts:29-32 — 30s expiry + MAX_DEPTH=10 prevents unbounded memory growth.

---

## Req 3: Replenish Page
**4Q**:
1. PASS — `SuggestionRepo` is an interface; `ListSuggestionsUseCase` depends only on the interface; illegal input (zero tenantID) propagates to the SQL query as a zero UUID which returns an empty set, not a crash.
2. NEEDS-CHECK@app/replenish/usecase.go:97 — no row-count cap on `ListSuggestions`; a tenant with 50k active products allocates a 50k-row slice in Go memory. The SQL query at `repo/replenish/repo.go:43-128` has no `LIMIT`.
3. PASS — errors from `repo.ListSuggestions` are wrapped and returned; caller (handler) maps to 500; no silent swallow.
4. PASS — formula in `usecase.go:97-191` is pure math over `[]RawRow`; `usecase_test.go` exists and exercises the formula without a DB.

**EC**:
- EC1 FAIL@repo/replenish/repo.go:43 — `listSuggestionsQuery` has no `LIMIT`; on large catalogs this can return unbounded rows and cause OOM. No max is enforced anywhere in the call chain.
- EC2 NEEDS-CHECK@usecase.go:119 — `leadTime <= 0 → 7` default is applied in Go, but the SQL `lead_time_days` column is not verified to be non-negative at the DB level; a negative value from a buggy migration would not be caught at type level.
- EC3 PASS@usecase.go:148-153 — zero-velocity urgency is handled explicitly as `999999`, preventing division by zero.

---

## Req 4: ROP + Lead-Time
**4Q**:
1. FAIL — `sigmaFactor=0.3` and `z=1.65` are untyped package-level `const float64` (`usecase.go:80-85`). Formula correctness is enforced only by the constant values; no type prevents a future dev from swapping the wrong constant.
2. PASS — `math.Sqrt(float64(leadTime))` at line 126 has no edge case problem since `leadTime` is always `>= 1` after the default guard.
3. PASS — formula errors surface as wrong numbers, not panics; `decimal.Decimal` arithmetic is overflow-safe.
4. PASS — `ListSuggestionsUseCase.Execute` is a pure transformation over `[]RawRow`; the formula test in `usecase_test.go` runs without a DB.

**EC**:
- EC1 NEEDS-CHECK@usecase.go:92 — the formula for `target` uses `avgDailySales × 7 × weeks`; `weeks` comes from the handler query param with a `<= 0 → defaultWeeks` fallback but no upper bound. A caller passing `weeks=365` produces a target 26× a normal order, silently suggesting a year of stock.
- EC2 PASS@usecase.go:137-141 — `suggested` is floored at zero and then `Ceil()`ed; negative result is handled.
- EC3 NEEDS-CHECK@repo/replenish/repo.go:160-164 — decimal parse errors are silently dropped (`_, _ = decimal.NewFromString(...)`); a corrupt `available_qty` in the DB returns `0` with no error, making a product look well-stocked.

---

## Req 5: CSV Import
**4Q**:
1. PASS — `Platform` is a typed string with `Validate()` (`usecase.go:42-49`); `ImportRequest` has nil-UUID guards at lines 240-253; `qty <= 0` and `price < 0` rejected at parse time (`buildRow:640`).
2. FAIL — `CSVData []byte` is unbounded; no max file size check before `csv.NewReader`. A 500 MB upload is parsed into memory without a limit.
3. PASS — `Execute` returns a typed `ImportResult` with `Skipped` + `UnknownSKUs`; partial success is surfaced; `MarkOrderSeen` failure returns an error (line 428) so the caller knows the bill exists but is de-dup-unsafe.
4. PASS — `ImportOrdersUseCase` depends entirely on interfaces (`ImportRepo`, `SaleCreator`, etc.); `usecase_test.go` tests the full flow with stubs.

**EC**:
- EC1 FAIL@usecase.go:428 — `MarkOrderSeen` failure after bill creation returns a hard error, aborting the entire import result. The bill is created but the order is not marked seen, so a re-import will try to create a duplicate bill (the dedup check will miss it). This is a data-integrity risk.
- EC2 NEEDS-CHECK@usecase.go:433-443 — hint mappings are persisted **after** `MarkOrderSeen`; if `UpsertMapping` fails the mapping is lost but the bill was already created. Subsequent imports will again see `UnknownSKU` for the same platform SKU.
- EC3 PASS@usecase.go:585-586 — Shopify blank-SKU lines are explicitly skipped, preventing zero-SKU orders from creating malformed bills.

---

## Req 6: ⌘K Entity Search
**4Q**:
1. PASS — `EntityType` is a typed string constant (`usecase.go:12`); `SearchRequest.Q == ""` returns empty without hitting the DB (`usecase.go:73-75`).
2. FAIL — four sequential `ILIKE` queries are fired per keystroke after a 150 ms debounce (`Palette.tsx:26`). Each repo call has a `limit` param but the repo interface allows a caller to pass `limit=0`, which falls back to `5` in the use case (`usecase.go:79`). No cap on how many concurrent debounced requests can be in-flight — the AbortController (`Palette.tsx:80`) cancels previous requests on the client, but the server still executes the cancelled one.
3. PASS — any single entity-type error aborts the entire search and returns an error to the handler (`usecase.go:109-111`); the handler maps to 500; no partial-result masking.
4. PASS — `SearchEntitiesUseCase` depends on `EntityRepo` interface; fully testable with stubs.

**EC**:
- EC1 NEEDS-CHECK@usecase.go:86-106 — search runs sequentially, not concurrently; four DB round-trips on every keystroke. The comment says "Sequential is fine" but under load (cold PG cache) this can exceed 200 ms end-to-end, breaking the 200 ms SLA from the product red-lines.
- EC2 NEEDS-CHECK — no LIMIT enforcement at the SQL layer was read (the repo adapter was not found within the file set). The interface accepts `limit int` but if the SQL repo ignores it, unbounded results are possible.
- EC3 PASS@Palette.tsx:55-56 — `AbortController` cancels in-flight fetch on new keystrokes; prevents response interleaving.

---

## Req 7: Onboarding
**4Q**:
1. PASS — `Persona` is validated against an explicit switch in `handler.go:121-126`; unknown values return 400. `warehouseID` is UUID-parsed before use (line 113).
2. PASS — seed demo creates a fixed set of demo products per persona; not driven by user-supplied size; bounded by the persona catalog definition.
3. PASS — seed failure returns 500 with `err.Error()`; clear-demo failure returns 500; both are actionable by the operator.
4. PASS — `SeedDemoUseCase` depends on `ProductCreator` + `StockInitializer` interfaces; `NewForTest` constructor at `handler.go:160` enables handler-level unit tests without HTTP stack.

**EC**:
- EC1 NEEDS-CHECK@handler.go:128 — `h.seed.Execute` is called with `c.Request.Context()` which is cancelled when the HTTP connection drops. If the seed partially completes and the context is cancelled mid-flight, some products are created without stock, leaving the tenant in a partial-demo state with no rollback.
- EC2 NEEDS-CHECK@handler.go:100 — no idempotency guard on `seed-demo`; calling it twice creates duplicate demo products. The `import_order_seen` pattern used in CSV import is not mirrored here.
- EC3 PASS@handler.go:101-104 — `tenantID == uuid.Nil` guard prevents cross-tenant seed if middleware misbehaves.

---

## Req 8: Telemetry
**4Q**:
1. PASS — `TelemetryEvent` is a TypeScript union type (`telemetry.ts:13`); `trackEvent<E extends TelemetryEvent>` is generic; calling with an unlisted event name is a compile-time error.
2. PASS — metrics are Prometheus counters registered once in `init()` (`metrics.go:65`); no unbounded accumulation; label cardinality is bounded by the `perTenantEnabled` guard.
3. PASS — `trackEvent` is fire-and-forget; errors are swallowed by design; telemetry failures do not propagate to UI. Backend `IncXxx` functions are call-and-forget with no error return.
4. PASS — `metrics.go` has no external dependencies beyond `prometheus`; backend counters are unit-testable by calling `IncXxx` and asserting `prometheus.GatherAndCompare`. Frontend `trackEvent` is a pure function (conditionally skipped on SSR).

**EC**:
- EC1 NEEDS-CHECK@metrics.go:65 — all metrics use `prometheus.MustRegister` inside `init()`. If the binary is loaded twice in the same process (e.g. integration test suite without registry reset), `MustRegister` will panic. Standard mitigation is `prometheus.NewRegistry()` per test, but the test file (`metrics_test.go`) was not read — marking NEEDS-CHECK.
- EC2 NEEDS-CHECK@telemetry.ts:87 — `trackEvent` posts to `/api/otel-events` (a Next.js API route), not to the backend `/internal/v1/telemetry/web`. The backend allow-list referenced in the comment is at a different path; if the Next.js route is the only gate, a mismatch between the two allow-lists could cause silent drops without error.
- EC3 PASS@telemetry.ts:82-84 — SSR guard prevents server-side `fetch` call; `typeof window === "undefined"` check is correct.

---

## Req 9: Monday Digest
**4Q**:
1. PASS — `DigestRepo` interface is narrow (three methods); `tenantID uuid.UUID` is the only input; no stringly-typed filter paths.
2. PASS — three goroutines are spawned with buffered channels (capacity 1) (`usecase.go:81-95`); no goroutine leak on error since channels are buffered and goroutines always send exactly one value.
3. PASS — first repo error aborts and returns; errors are wrapped and returned to the handler; the pattern is "fail fast, don't suppress".
4. PASS — `WeeklySummaryUseCase` depends solely on `DigestRepo` interface; pure aggregation math after the repo calls; `usecase_test.go` exists.

**EC**:
- EC1 NEEDS-CHECK@usecase.go:96-108 — goroutines receive `ctx` from the caller. If the caller's context is cancelled (e.g. HTTP client disconnects), all three goroutines may return errors; the use case returns the first error but does not drain the remaining two channels. Since the channels are buffered (cap 1), goroutines complete without blocking and the GC can reclaim them — PASS on goroutine leak, but error from second/third goroutine is silently dropped.
- EC2 NEEDS-CHECK@usecase.go:113-118 — `ReplenishAmountCNY` formula uses `avgDailySales × coverageDays − available`. When `available` is negative (oversell condition), this inflates the suggested amount. The oversell case is counted separately (`OversellCount`) but the amount calculation does not filter it out.
- EC3 PASS@usecase.go:114-117 — `suggested.IsNegative()` guard floors at zero before multiplying by `unitCost`; no negative amount can be emitted.

---

## Req 10: Reports
**4Q**:
1. PASS — `SQLRepo` is the only implementation; `appreports.Repo` is the interface; queries use parameterised `$1`/`$2` placeholders; no injection surface.
2. FAIL — `ListRecentSaleLines` has `LIMIT 10000` (`repo.go:65`) hardcoded in SQL but no cap in the use case layer or handler; 10k rows of `(uuid, string, decimal×3, time)` is ~2-4 MB per request, repeated per dashboard load. `ListStockSnapshots` has **no LIMIT** at all (`repo.go:133`).
3. PASS — both queries return `rows.Err()` at the end of scanning; `fmt.Errorf` wrapping is consistent; handler receives a typed error.
4. PASS — `SQLRepo` depends on the narrow `DB` interface (`repo.go:19`); testable with any `DB` stub.

**EC**:
- EC1 FAIL@repo/reports/repo.go:154 — `s.LeadTimeDays = 7` is hardcoded for every stock row regardless of what is in `tally.product.lead_time_days`. The SQL does not join `product.lead_time_days`, so the per-product lead-time configured by the operator is silently ignored in the reports layer (though the replenish repo does use it).
- EC2 FAIL@repo/reports/repo.go:91-158 — `ListStockSnapshots` has no LIMIT; on a tenant with 10k+ products it returns all rows in a single unbounded query; no pagination is enforced.
- EC3 NEEDS-CHECK@repo/reports/repo.go:103 — `COALESCE(lm.last_moved_at, now() - interval '200 days')` fabricates a "last moved" timestamp for products that have never moved. This makes dead-stock detection in the reports UI look like a product last moved 200 days ago rather than "never", which could mislead the user.

---

## §4.1 Reflexive-Doubt Notes

- **Perfect 0% error rate on `decimal.NewFromString` ignores**: every repo that scans decimal columns as strings silently discards parse errors with `_, _ = decimal.NewFromString(...)` (replenish, reports, digest). A column returning a non-numeric value (e.g. schema drift) produces `decimal.Zero` — the inventory looks fine, numbers are quietly wrong.
- **Partial execution is not atomic for bulk stock adjust**: `execStockAdjust` returns `(result, err)` on mid-batch failure (`executor.go:188`); result is non-nil meaning "some succeeded", but the caller's error branch in `ConfirmPlan` discards `result` entirely and reverts the plan to Pending — the already-adjusted products are not reverted.
- **Telemetry route mismatch**: `telemetry.ts` fires at `/api/otel-events` (Next.js route); the backend metrics file references `/internal/v1/telemetry/web`. Without reading the Next.js route file the exact forwarding behaviour is unverified — both the allow-list and auth may differ.
- **No upper bound on `weeks` param in replenish**: a caller passing `weeks=9999` generates a suggested order for ~27 years of stock with no server-side rejection.

---

## Summary Table

| Req | Title | 4Q Score | EC Pass Rate | Verdict |
|-----|-------|----------|--------------|---------|
| 1 | AI Plan Exec | 2/4 | 1/3 | NEEDS-WORK |
| 2 | Audit + Undo | 4/4 | 1/3 | NEEDS-WORK |
| 3 | Replenish Page | 3/4 | 1/3 | NEEDS-WORK |
| 4 | ROP + Lead-Time | 3/4 | 1/3 | NEEDS-WORK |
| 5 | CSV Import | 3/4 | 1/3 | NEEDS-WORK |
| 6 | ⌘K Entity Search | 3/4 | 1/3 | NEEDS-WORK |
| 7 | Onboarding | 4/4 | 1/3 | NEEDS-WORK |
| 8 | Telemetry | 4/4 | 1/3 | NEEDS-WORK |
| 9 | Monday Digest | 4/4 | 1/3 | NEEDS-WORK |
| 10 | Reports | 3/4 | 0/3 | BLOCKER |

**Legend**: EC pass rate counts only EC rows marked PASS (NEEDS-CHECK = not PASS).

### Top 3 Blockers to Fix Before Ship

1. **Unbounded SQL** (Req 3, 10): `ListSuggestions` and `ListStockSnapshots` have no LIMIT; OOM risk on growing catalogs.
2. **Partial stock-adjust atomicity** (Req 1): `execStockAdjust` partial failure leaves inventory mutated without the plan reverting those rows; retry confirms a second partial mutation.
3. **`MarkOrderSeen` failure after bill creation** (Req 5): bill exists but dedup record is missing; re-import creates a duplicate bill silently.
