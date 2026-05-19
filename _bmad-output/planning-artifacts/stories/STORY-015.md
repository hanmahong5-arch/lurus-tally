# Story C-3.15: Tally E2E — `e2e/portal-audit-trace.sh` black-box test

Status: ready-for-dev

**Epic:** C — 5-Product Audit Event Ledger
**Sprint:** 1 (gate)
**Owner:** tally team
**Repo:** `2b-svc-psi`
**FR:** (Sprint 1 DoD gate; no direct PRD FR)
**Estimate:** 1d
**Blockers:** STORY-014 emit wiring complete; STORY-004 correlation_id propagation in portal (not required for direct test, but required for full stitch in J-2)

## Story

As Sprint 1 reviewer,
I want a single black-box script I can run that asserts the entire emit→ledger→query loop works end-to-end with tally as the producer,
so that Sprint 1 cannot ship with a silent regression that breaks the contract for the other 3 products in Sprint 4.

## Acceptance Criteria

1. Script `2b-svc-psi/e2e/portal-audit-trace.sh` exists and is executable.
2. Script does:
   a. Generates a UUID v4 `CORRELATION_ID`.
   b. POSTs a test bill create to tally backend (`POST /api/bills`) with header `X-Correlation-Id: $CORRELATION_ID`.
   c. Polls (max 5 s, 100 ms interval) `SELECT * FROM module.audit_events WHERE product_id='tally' AND correlation_id=$CORRELATION_ID LIMIT 1`.
   d. Asserts exactly 1 row found.
   e. Asserts row has: `op="tally.bill.create"`, `target_kind="bill"`, non-empty `target_id`, non-empty `actor_id`, `actor_role="authenticated"` (or `service` if test runs as service), `result="ok"`.
   f. Cleans up the test bill (`DELETE /api/bills/<id>`).
   g. Exits 0 on success, non-zero with diagnostic on any assertion fail.
3. Script runs in CI as a job; CI failure blocks Sprint 1 merge.
4. Script documents: required env vars (`TALLY_URL`, `PG_DSN`), required test fixture (a tally user account with bill-create permission).
5. Re-running the script multiple times does not leave bills behind (idempotent cleanup).
6. Script also asserts emit lag < 2 s (poll loop tracks elapsed time; if > 2 s, exits with warning but still 0).

## Tasks / Subtasks

- [ ] Create `2b-svc-psi/e2e/portal-audit-trace.sh` (AC: 1, 2)
  - [ ] Helper: `wait_for_audit_row` polling function (AC: 2c, 6)
  - [ ] Cleanup function via trap (AC: 5, 2f)
- [ ] CI job entry in `2b-svc-psi/.github/workflows/` (AC: 3)
- [ ] Documentation header in script (AC: 4)
- [ ] Local-run smoke test on dev machine before CI submit

## Dev Notes

- Use plain `bash` + `curl` + `psql` only. No new tool dependencies. (Git Bash on Win or any Linux must run it.)
- Polling: 5 s max with 100 ms interval = 50 iterations. Bail on first match.
- Cleanup `DELETE /api/bills/$id` MUST run even on assertion failure — use `trap`.
- This is THE Sprint 1 gate. Treat it as the smoke that lights the green light, not a nice-to-have.

### Project Structure Notes

- New: `2b-svc-psi/e2e/portal-audit-trace.sh`.
- Touches: tally CI workflow yaml.

### References

- [Source: lurus/_bmad-output/planning-artifacts/sprint-plan.md#sprint-1-gate-criteria]
- [Source: lurus/_bmad-output/planning-artifacts/epics.md#story-015]
- [Source: lurus/_bmad-output/planning-artifacts/prd.md#SC-1]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
