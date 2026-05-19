# Story C-3.14: Tally — emit audit on 15 write handlers (`product_id="tally"`)

Status: ready-for-dev

**Epic:** C — 5-Product Audit Event Ledger
**Sprint:** 1 (pilot — proves the contract for kova/lucrum/lutu in Sprint 4)
**Owner:** tally team
**Repo:** `2b-svc-psi`
**FR:** FR-17
**Estimate:** 3d
**Blockers:** STORY-013 `lurus-audit-go` library available (partial OK — Emit + scrub must be in)

## Story

As an operations engineer post-incident,
I want to query "what did tenant X do in tally during the 2-hour window",
so that I can reconstruct customer impact without scrolling through structured logs that have already rolled out of the pod.

## Acceptance Criteria

1. `2b-svc-psi/go.mod` adds `lurus-audit-go` dependency.
2. Single shared `auditClient` singleton initialized at app boot; `Close(ctx)` called on graceful shutdown (5s timeout).
3. The following 15 write handlers emit exactly one `audit_events` row each on successful execution. Each row has `product_id="tally"`, correct `op`, `target_kind`, `target_id`, `actor_id` (from session), `actor_role` (`authenticated`/`service`), `correlation_id` (from request `X-Correlation-Id` header or generated).
   - `bill.create`, `bill.update`, `bill.approve`, `bill.reject`, `bill.delete`
   - `expense.create`, `expense.delete`
   - `supplier.create`, `supplier.update`, `supplier.delete`
   - `category.create`, `category.update`, `category.delete`
   - `report.export`, `report.delete`
4. Failure path emits with `result="fail"` and `error="<reason>"` — but only AFTER the failure is determined (after authz, before retry).
5. Tally request p95 latency does NOT regress > 5 ms post-deploy (R-3 mitigation; measured via existing latency dashboard).
6. Existing handler unit tests still green. New tests assert audit emit happens (use audit-go test-mock).
7. Audit emit is async — handler returns to client BEFORE audit row is persisted (NFR-18 tolerates RPC unavailability).
8. If exact 15-site list mismatches reality, owner updates list in this story file and notifies PRD/architecture owners.

## Tasks / Subtasks

- [ ] Add `lurus-audit-go` dep + verify it resolves (AC: 1)
- [ ] Wire `auditClient` singleton at app init + close on shutdown (AC: 2)
- [ ] Per-handler edit (15 handlers; AC: 3, 4, 7):
  - [ ] bill.* (5 sites)
  - [ ] expense.* (2 sites)
  - [ ] supplier.* (3 sites)
  - [ ] category.* (3 sites)
  - [ ] report.* (2 sites)
- [ ] Update handler unit tests to assert emit was called with correct shape (AC: 6)
- [ ] Pre-deploy: snapshot tally p95 latency baseline (AC: 5)
- [ ] Post-deploy: 24-hour latency comparison; document in PR comment (AC: 5)
- [ ] If list count != 15, update FR-17 in PRD via PR (AC: 8)

## Dev Notes

- Tally has REAL B-END CUSTOMERS. Latency regression is a customer-visible event. Lean heavily on R-3 mitigations.
- `lurus-audit-go.Client.Emit` is async by contract — never block the handler. If you find yourself wanting `EmitBlocking`, push back to platform team.
- `correlation_id` — extract from `X-Correlation-Id` header; generate UUID v4 if absent. Same ID propagates through any downstream call chain.
- Use existing tally test harness (table-driven, `_test.go` colocated).
- DO NOT bypass scrub: if a `params` field contains password/token/etc, the lib scrubs it; do not add manual scrub at handler.
- Coordinate with STORY-015 owner — that story is the E2E gate that exercises this story end-to-end.

### Project Structure Notes

- Touches: 15 files under `2b-svc-psi/internal/handler/` (exact paths owner-determined from current code layout).
- Adds: app init wiring in main / bootstrap.
- No DB migrations; no contract changes.

### References

- [Source: lurus/_bmad-output/planning-artifacts/prd.md#FR-17]
- [Source: lurus/_bmad-output/planning-artifacts/architecture.md#J-4]
- [Source: lurus/_bmad-output/planning-artifacts/risk-register.md#R-3]
- [Source: lurus/doc/coord/contracts.md#audit-emit-rpc-new]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
