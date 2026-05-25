# Code Review — Wave-2 Swarm (1ae417a3^..HEAD)

**Effort**: high (speculative findings marked with confidence label)
**Scope**: 24 commits, 78 files, ~9,900 lines added
**Reviewer**: C2 (adversarial, read-only)

---

## Summary

| Severity | Count |
|----------|-------|
| Must-fix | 5 |
| Should-fix | 8 |
| Nit | 6 |

---

## Findings Table

| ID | Severity | File:Line | Description | Suggested Fix | Confidence |
|----|----------|-----------|-------------|---------------|------------|
| F01 | Must-fix | `internal/app/importing/usecase.go:431` | **Non-atomic create+approve+mark-seen**: bill is created, approved, stock deducted, then `MarkOrderSeen` fails → bill exists, stock is reduced, but the order is not recorded as seen. Next re-import of the same CSV will pass the dedup check, create a second bill, and double-deduct stock. The comment "non-fatal: bill was created" is wrong — this is a silent double-write bug. | Make `MarkOrderSeen` + `UpsertMapping` part of the same DB transaction as bill creation, or revert the bill on `MarkOrderSeen` failure. At minimum, treat the error as fatal and surface it (currently this path does `return nil, fmt.Errorf(...)` which is actually correct — but the comment contradicts it and suggests this will be silently swallowed by callers). Remove the misleading comment; the fatal return is correct. | high |
| F02 | Must-fix | `web/app/(dashboard)/replenish/page.tsx:21,62,132` | **Dev-mode tenant ID leaked to production path**: `devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID` is passed to both `listReplenishSuggestions` and `createPurchaseBill`. If this env var is set in staging or production (e.g., inherited from a `.env` file), all replenishment reads and batch PO writes go through a hardcoded tenant ID rather than the authenticated user's tenant context. Unlike the imports page which uses a warehouse ID env var (clearly documented as MVP-only), this one controls data isolation. | Remove `devTenantId` from this component; tenant ID should come exclusively from the auth session via the API client, not a frontend env var. | high |
| F03 | Must-fix | `internal/adapter/handler/ai/handler.go:268` | **`X-User-ID` header accepted for actor attribution**: `resolveActorID` falls back to the `X-User-ID` request header when no Zitadel subject is present. This header is not set by any trusted middleware — it is user-supplied. An authenticated tenant could pass any UUID as `X-User-ID`, causing audit logs to attribute AI plan executions (including bill creation) to an arbitrary actor. Since `actorID` falls back to `tenantID` on uuid.Nil, the fallback is safe; only the header branch is unsafe. | Remove the `X-User-ID` fallback from `resolveActorID`, or gate it on a trusted-proxy check. The `tenantID` fallback is sufficient for the stated single-operator use case. | high |
| F04 | Must-fix | `internal/app/importing/usecase.go:361-384` | **DryRun path still runs dedup check against `import_order_seen`**: an order already confirmed in the DB is reported as `Skipped: "duplicate:bill_id=..."` in preview mode. This is arguably correct behaviour but is not documented — the user sees a preview that silently omits their most-recently-imported orders without explanation. More critically, in DryRun mode the result `Imported` list includes `BillID: uuid.Nil` and `BillNo: "(preview)"` for orders that were _not_ skipped, but these appear alongside genuinely imported entries in the summary DTO — `TotalParsed = len(imported)+len(skipped)` counts preview entries the same as real ones, so the summary numbers are misleading in DryRun. | In DryRun mode skip the `IsOrderSeen` check (or report it as `would_skip` rather than `duplicate`). Separate the summary DTO for preview vs. final import. | med |
| F05 | Must-fix | `internal/app/digest/usecase.go:89-110` | **Goroutine leak on context cancellation**: three goroutines are spawned with no `ctx` cancellation guard beyond passing it through to the repo. If the parent context is cancelled before all three fire (e.g., HTTP timeout), the goroutines block on `replCh <- ...` forever because the channel has buffer=1 but the orchestrating goroutine has already returned. The channels are buffered so this is not a full deadlock, but the goroutines remain in memory until the repo call returns (possibly unbounded for a slow DB). More precisely: the issue is that a context cancel in the three repo calls causes them to return quickly (correct), but if the calling goroutine returns before draining the other two channels, and the repo calls are stuck on a long DB query, those goroutines will never be reaped during the process lifetime of a high-traffic server. | Use `errgroup.WithContext` from `golang.org/x/sync/errgroup`; cancel remaining goroutines when the first error is received. Alternatively add a `select { case replCh <- ...: default: }` guard, though `errgroup` is cleaner. | med |
| F06 | Should-fix | `internal/app/importing/usecase.go:368-372` | **`OversellRow.PlatformSKU` is always empty in preview**: the field is set to `""` with comment "resolved away; caller cross-refs by productID". The frontend DTO (`oversellDTO`) exposes `product_id` only, with no `platform_sku`. The UI receives `product_id` in UUID form but the user sees the platform SKU in their CSV — they cannot match the UUID to their order without a name. | Store `rl.row.PlatformSKU` in the `OversellRow` when building it inside the DryRun loop (the `rl` slice variable is in scope). The oversell appending happens over `convertedLines` which has lost SKU, but the resolution loop above (step 3) has the mapping — propagate platform SKU through `lineWithPrice`. | high |
| F07 | Should-fix | `internal/app/onboarding/usecase.go:261-275` | **Reimplemented `strings.Contains`**: `contains`/`indexString` are hand-rolled replacements for `strings.Contains`. The `strings` package is not imported in this file (the package avoids it to stay infrastructure-free). The implementation is correct but the rationale in the comment is "we check the error string rather than importing the pq driver here" — the real reason to not import `strings` is unclear since `strings.Contains` has no infrastructure dependency. The `contains` function body `len(s) >= len(sub) && (s == sub || len(s) > 0 && indexString(s, sub) >= 0)` has a logic quirk: when `s == sub` it returns true (correct) but the condition before `indexString` checks `len(s) > 0` which is always true when `len(s) >= len(sub)` and `sub != ""`. Not wrong, just unnecessarily convoluted. | Import `strings` and use `strings.Contains`. | low |
| F08 | Should-fix | `internal/adapter/handler/onboarding/handler.go:164-167` | **`var _ = decimal.Zero` import anchor**: the `decimal` package is imported but only used in `stockAdapter.Execute`. The `var _ = decimal.Zero` anchor is a smell indicating the import is not justified in the handler file itself — it is only used inside `stockAdapter` which needs it for the `StockInitRequest` fields. The build tag comment says "remove if decimal is referenced elsewhere" but it is not. | Move the decimal import usage into the stockAdapter method (it already is), and remove the var anchor — Go will not complain because `decimal.Decimal` in the struct field of `StockInitRequest` keeps the import live transitively. If the anchor was added to satisfy a lint warning, investigate which one. | low |
| F09 | Should-fix | `internal/app/reports/usecase.go:363-380` | **`computeROP` is dead code**: defined but only referenced by `var _ = computeROP` which suppresses the lint warning. This is the Karpathy "outside-task dead code" pattern — the function is not called by any of the four use cases in this file. | Delete the function (and its `var _` anchor) until it is actually needed. The formula is already implemented in `internal/app/replenish/usecase.go`. | med |
| F10 | Should-fix | `internal/adapter/repo/search/repo.go:40-49` | **`SearchSuppliers` queries `tally.supplier` but the schema has two supplier stores**: suppliers can be stored as `tally.partner` with `partner_type IN ('supplier','both')` (migration 000004, the legacy store) _and_ in `tally.supplier` (migration 000033, the newer dedicated table). `SearchCustomers` queries `tally.partner`; `SearchSuppliers` queries `tally.supplier`. The project appears to be migrating to `tally.supplier` (the `internal/adapter/repo/supplier/repo.go` reads from it), so this is likely correct. However there is a risk that some suppliers exist only in `tally.partner` if they were created before migration 000033 and never migrated. | Document the assumption explicitly ("assumes migration 000033 has been applied and all suppliers are in tally.supplier"). Cross-check with data migration script if any exists. | med (uncertain) |
| F11 | Should-fix | `internal/app/importing/usecase.go:425-448` | **SKU mapping persistence runs after `MarkOrderSeen` only for _this order's_ resolved lines, not for all hints**: for an order with mixed hints (some resolved from DB, some from hints), only hint-backed resolutions trigger `UpsertMapping`. But the persistence loop iterates `resolved` (all lines) and checks `hintMap[rl.row.PlatformSKU]` — correct. However, hints are only persisted for _successfully approved_ orders (the code is after `saleApprover.Approve`). If `Create` succeeds but `Approve` fails (oversell), the order is skipped but the hints that were supplied are discarded. On retry the same hints must be re-supplied. | Either persist hints before the create/approve sequence, or document this limitation explicitly. | med |
| F12 | Should-fix | `internal/lifecycle/app.go:545` | **`NewCreateSaleUseCase` instantiated twice**: once at line 277 (for the sale handler) and again at line 545 (for the import adapter). Both share the same `billRepo`. This is not a correctness bug but wastes a constructor allocation and creates an implicit duplicate registration of any in-constructor side effects. | Extract the sale UC into a named variable and reuse it. | low |
| F13 | Should-fix | `internal/app/ai/orchestrator.go:293-324` | **Revert-on-failure races with concurrent confirm**: comment says "Flip to Confirmed first — acts as a lock against concurrent double-clicks." The status is flipped to `Confirmed`, executor runs, fails, then status is reverted to `Pending`. Between the revert write and the next HTTP response, a concurrent request that had queued behind the DB lock could observe `Pending` again and re-confirm. Strictly this is a TOCTOU window. For purchase drafts: not dangerous because the executor itself is called only once before the revert. For price/stock adjustments that are not fully atomic: a revert to Pending after partial success followed by a second confirmation would re-apply the change to already-changed rows. | Document explicitly that retry after partial bulk-stock or price-change execution requires operator review. Consider adding a terminal `PartiallyFailed` status. | med (uncertain) |
| N01 | Nit | `internal/adapter/repo/replenish/repo.go:150-165` | Scanning numeric columns from PostgreSQL as `string` then calling `decimal.NewFromString` (silently ignoring the error with `_`) loses scan errors. If a column has an unexpected NULL or type mismatch, the decimal will silently be zero. | Use `sql.NullString` and surface the parse error, or scan directly into `decimal.Decimal` with a custom scanner. | med |
| N02 | Nit | `internal/adapter/repo/digest/repo.go:67,103` | Same pattern as N01 — digest repo scans numerics as strings and silently drops parse errors. | Same fix. | med |
| N03 | Nit | `web/app/(dashboard)/replenish/page.tsx:114` | `parseFloat(r.suggested_qty) > 0` duplicates the Ceil logic on the backend. If the backend returns `"0"`, items are filtered out on the frontend. This is correct but fragile — if the backend ever returns a fractional zero like `"0.0"`, `parseFloat` would still filter it correctly. Low risk but worth a comment. | Comment clarifying this mirrors the backend floor-at-zero logic. | low |
| N04 | Nit | `internal/app/importing/usecase.go:271-285` | The order grouping preserves encounter order with `orderKeys` slice + `orderMap` map. This is correct. However when `orderMap[row.PlatformOrderNo]` already has lines with a different `currency` or `orderDate`, the new row's currency/date values are silently ignored (only the first row's values are stored). Multi-currency orders (e.g., a Shopify order with USD line and CAD shipping) or date discrepancies within one order number would be silently collapsed to the first encountered values. | Validate that all rows within an order have the same currency and date; return a validation error if they differ. | med |
| N05 | Nit | `internal/adapter/handler/importing/handler.go:130-133` | `importResultDTO.Summary.TotalParsed = len(imported) + len(skipped)` counts DryRun's placeholder "imported" entries as real parsed rows. The number will be inflated by re-submitted unknown-SKU orders that hit the dedup check. | Consider `TotalParsed = total unique order numbers in the CSV` rather than `imported + skipped`. | low |
| N06 | Nit | `web/components/command-palette/Palette.tsx` (entity search) | Entity search results are flattened to a single `EntityResult[]` before display (`resp.groups.flatMap(g => g.items)`). The grouped structure from the API is collapsed, losing the type header. Keyboard selection index (`selectedIdx`) spans the flat list, which means arrow keys can cross type boundaries silently. | Keep groups intact for display; compute flat index by mapping group × item. | low |

---

## Reviewed and Clean Sections

The following areas were read in full and no significant issues found:

### `internal/app/replenish/usecase.go`
Formula (ROP + safety stock) is mathematically correct and matches the commented spec. `sort.Slice` is stable within the urgency tie-breaking constraint. Zero-velocity products correctly receive `largeScore = 999999`. The `SafetyQty` field from the raw repo row is passed through (the `stock_initial.low_safe_qty`) and kept separate from the formula-computed `SafetyStock` — no confusion between the two. `Ceil()` on suggested qty is correct for discrete unit ordering.

### `internal/adapter/repo/replenish/repo.go`
SQL CTEs are structurally correct: tenant_id filter is consistent across all five CTEs, soft-delete filters are present on `bill_head` and `partner`, `in_transit` correctly uses `status IN (0, 1)`, `last_supplier` uses `DISTINCT ON` with correct ordering by `bill_date DESC`. The `p.partner_type IN ('supplier','both')` filter is correct given the partner table semantics.

### `internal/adapter/repo/importing/repo.go`
`UpsertMapping` uses `ON CONFLICT DO UPDATE` — idempotent. `MarkOrderSeen` uses `ON CONFLICT DO NOTHING` — idempotent. `isPgUniqueViolation` has a correct three-tier check (pq interface, string match, "unique constraint" string). `GetMapping` correctly returns `(nil, nil)` for not-found via `errors.Is(sql.ErrNoRows)`.

### `migrations/000037_import_sku_map.up.sql` and `migrations/000038_product_lead_time.up.sql`
Both use `IF NOT EXISTS` / `IF NOT EXISTS` guards — safe for re-run. RLS policies are checked before creation with `DO $$ ... IF NOT EXISTS` pattern. `lead_time_days INT NOT NULL DEFAULT 7` is safe as an `ADD COLUMN IF NOT EXISTS`.

### `internal/app/sku/price.go` — `ApplyAction`
Parses `+N%`, `-N%`, `=N`, and bare `N` correctly. Result is rounded to 6 decimal places and clamped at 0. Validation runs before any writes.

### `internal/adapter/repo/sku/repo.go`
`ListDefaultSKUs` uses `DISTINCT ON (product_id)` ordered by `is_default DESC, created_at ASC` — correct for picking the primary SKU first. `uuidArrayLiteral` is safe (UUID hex + dash only, no injection vector). `UpdateRetailPrice` checks `RowsAffected == 0` and returns an explicit not-found error.

### `internal/adapter/repo/warehouse/repo.go` — `DefaultWarehouseID`
Queries `ORDER BY is_default DESC, created_at ASC LIMIT 1` — correct fallback behavior. Returns `ErrNotFound` on `sql.ErrNoRows`.

### `internal/app/ai/executor.go`
`execStockAdjust` fails fast and reports how many succeeded before failure — correct for a non-atomic bulk operation. `resolveByName` prefers exact case-insensitive match before falling back to first search result — reasonable. `decodePayload` round-trips through JSON to normalize types — correct and avoids type assertion risks.

### `internal/lifecycle/ai_executor.go`
Wiring is correct. `aiDraftCreator` correctly treats missing SKU price as non-fatal (creates draft with price=0 for operator fill-in). Audit write failures are logged but do not block the execution path — correct per the stated constraint.

### `internal/adapter/handler/digest/handler.go`
Minimal, correct. Tenant check present. Returns 500 on repo errors.

### `internal/adapter/handler/onboarding/handler.go`
`SeedDemo` and `ClearDemo` both validate `tenantID != uuid.Nil` before proceeding. Persona validation is exhaustive with explicit 400 on unknown value. `stockAdapter` correctly adapts the simplified `StockInitRequest` to `RecordMovementRequest`.

### `internal/app/onboarding/usecase.go`
`Execute` is correctly idempotent at the product-code level (skip on duplicate code). `demoCatalogue` covers all three personas with a `default` fallback. `marshalAttrs` returns `{}` for nil/empty attributes.

### `internal/adapter/repo/onboarding/repo.go`
`DeleteDemoProducts` targets `remark='DEMO' AND deleted_at IS NULL` — safe. Relies on FK cascade for stock rows, which is the correct approach.

### `internal/app/search/usecase.go`
Query fan-out is sequential (correct comment: "Sequential is fine — each is a narrow ILIKE + LIMIT 5"). Empty-query short-circuit returns empty groups, not an error.

### `internal/adapter/handler/search/handler.go`
Limit is clamped at `maxLimit=20`. Blank query returns 200 with empty groups (not 400) — consistent with the usecase behavior.

### `internal/adapter/middleware/metrics.go`
WAD counter and `aiPlanExecuted` counter are registered correctly. `perTenantEnabled` conditional label branching is consistent between `IncAIPlanExecuted` and `IncWAD`. `webTelemetry` is always registered (no tenant label — correct per comment).

### `internal/adapter/handler/ai/handler.go` — `ConfirmPlan`
`actorID` falls back to `tenantID` when uuid.Nil — documented and acceptable for single-operator dev. The response correctly conditionally emits `bill_id`/`bill_no` and fires WAD metric only on purchase drafts.

### `web/components/ai-assistant/PlanCard.tsx`
Double-click guard via `inFlightRef` is correct (synchronous latch before async starts). `settled` state correctly derives from either local `outcome` or upstream `plan.status`. Undo stack registration for `ai_purchase_draft` is correct.

### `web/lib/undo/undo-stack.ts`
New `ai_purchase_draft` union member added correctly.

### `web/lib/telemetry.ts`
`void fetch(...)` idiom is correct to suppress unhandled promise rejections. Double guard (`try/catch` + `.catch`) is defensive for environments where `fetch` may not be available.

### `web/app/pricing/page.tsx`
Static server component, no auth required, no data fetching. `TALLY_PLANS` sourced from `lib/billing/plans` constant — correct pattern.

### `web/app/setup/page.tsx`
Wizard redirect flow is clear. Server action `submitProfile` redirects to `?step=seed&persona=...` on success. Profile-present + no step → `/dashboard` correctly avoids infinite redirect.

---

## Additional Notes

**Partial-success batch in `handleGenerateDrafts` (replenish page)**: the frontend iterates suppliers, creates one draft per supplier group, and continues past errors. Partial failures are reported but the UI shows a generic toast. No undo mechanism is attached to these batch PO drafts (unlike AI plan drafts which go on the undo stack). This is an intentional V1 tradeoff but worth noting.

**`contains` function shadowing**: the `contains` and `indexString` helpers in `internal/app/onboarding/usecase.go` are package-level unexported functions. If another file in the same package ever imports `strings`, they would silently coexist. Low risk but worth cleaning up (F07 above).

**`computeROP` in `reports` usecase vs. `replenish` usecase**: the formula is duplicated between the two packages with slightly different signatures. This is intentional per the code comment ("reports package stays independent of the ai package") but creates drift risk. A shared `internal/pkg/demand` package for the formula would be the DRY solution, deferred to a future refactor.
