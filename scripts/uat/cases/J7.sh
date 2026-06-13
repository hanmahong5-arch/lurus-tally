#!/usr/bin/env bash
# J7.sh — AI assistant journey: chat -> plan -> confirm -> verify effect ->
# revert -> verify rollback, then a second chat -> plan -> cancel.
#
# HARD BUDGET: at most 2 POST /ai/chat calls, each behind ai_call_guard
# (run-wide cap enforced by lib.sh). Set UAT_J7_SECOND_CHAT=0 to skip phase 2
# and spend only 1 LLM call (used during authoring dry-runs).
#
# Design notes (all read from the DEPLOYED commit da399443):
#   * Plan types: price_change / create_purchase_draft / bulk_stock_adjust.
#     We drive bulk_stock_adjust because its effect is observable over REST
#     (GET /stock/snapshots on_hand_qty); SKU retail prices touched by
#     price_change are NOT exposed by any deployed GET endpoint.
#   * /ai/chat responds with an SSE stream: event chunk/plan/done/error.
#     The plan event data is the full Plan JSON ({id, type, status, ...}).
#   * Revert window: 30s measured from plan CreatedAt (revert.go:145 uses
#     CreatedAt as the execution-time proxy). LLM streaming latency can eat
#     the window, so a 409 revert_window_closed is the documented contract,
#     not a failure — the script branches on it.
#   * Stock adjust lands in the tenant's default warehouse (DefaultWarehouseID
#     falls back to the oldest live warehouse when none is flagged default).
#   * LLM nondeterminism: when chat yields no plan event we fall back to
#     GET /ai/plans?status=pending filtered by our unique product name; if
#     still nothing, the raw SSE body is the evidence and every dependent
#     check is reported as BLOCKED (skipped, not failed).
#
# Invoke: RUN_ID=<id> bash scripts/uat/cases/J7.sh
set -u
CASE_ID="J7"
# shellcheck disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"

PFX="UAT-${RUN_ID}-J7"
WIDGET="${PFX}-WIDGET"
BOGUS_UUID="00000000-0000-0000-0000-00000000dead"
JSON=(-H 'Content-Type: application/json')
_BLOCKED=0

blocked() {
  # Dependent step cannot run because the LLM produced no plan (or got rate
  # limited). Recorded in stdout + evidence dir, but NOT counted as a failure.
  echo "    BLOCKED: $1 (see raw SSE evidence in $EVID_DIR)"
  echo "$1" >>"$EVID_DIR/blocked.txt"
}

# sse_plan_id FILE — extract the first plan-event payload's .id from an SSE body.
sse_plan_id() {
  grep -A1 '^event: plan' "$1" | sed -n 's/^data: //p' | head -1 | jq -r '.id // empty' 2>/dev/null
}

# pending_plan_for_widget — fallback: newest pending plan whose preview
# description references our unique product name.
pending_plan_for_widget() {
  jq -r --arg w "$WIDGET" \
    '[.items[] | select(.preview.description | contains($w))] | sort_by(.created_at) | last | .id // empty' \
    "$HTTP_BODY_FILE" 2>/dev/null
}

###############################################################################
# Step 1 — deterministic target fixture: a uniquely named product, plus a
# warehouse if the tenant has none (stock adjust needs a default warehouse).
###############################################################################
http wh-list GET /api/v1/warehouses
expect_status 200
WH_TOTAL=$(body_json -r '.total // 0')
if [ "${WH_TOTAL:-0}" = "0" ]; then
  http wh-create POST /api/v1/warehouses "${JSON[@]}" \
    -d "{\"code\":\"${PFX}-WH\",\"name\":\"${PFX}-warehouse\"}"
  expect_status 201
fi

http product-create POST /api/v1/products "${JSON[@]}" \
  -d "{\"code\":\"${PFX}\",\"name\":\"$WIDGET\",\"remark\":\"UAT AI journey target\"}"
expect_status 201
PROD_ID=$(body_json -r '.id // empty')
check "product create returned id" [ -n "$PROD_ID" ]

# Baseline stock for the widget (fresh product -> expect no snapshots / qty 0).
http stock-baseline GET "/api/v1/stock/snapshots?product_id=$PROD_ID"
expect_status 200
BASE_QTY=$(body_json -r '[.items[]?.on_hand_qty // "0" | tonumber] | add // 0')
check "baseline on-hand qty is 0 for fresh product (got $BASE_QTY)" [ "$BASE_QTY" = "0" ]

###############################################################################
# Step 2 — LLM call 1: ask for a bulk stock adjustment plan on the widget.
# Prompt pins the tool, the exact filter and the delta so the plan is
# deterministic in CONTENT even though the LLM run itself is not.
###############################################################################
ai_call_guard
http ai-chat-1 POST /api/v1/ai/chat "${JSON[@]}" --max-time 180 \
  -d "{\"message\":\"这是自动化验收测试。请立刻调用 propose_bulk_stock_adjust 工具,参数 filter 设为 \\\"$WIDGET\\\",delta 设为 5。不要追问,不要调用其他工具,调用一次后直接结束回答。\"}"

PLAN_ID=""
if [ "$HTTP_STATUS" = "429" ]; then
  # Per-tenant LLM budget hit: assert-and-record the documented contract, no retry loop.
  check "chat rate-limited contract: 429 llm_rate_limited" \
    bash -c "jq -e '.error == \"llm_rate_limited\"' '$HTTP_BODY_FILE' >/dev/null"
  blocked "plan lifecycle (confirm/revert) — chat was rate limited"
elif [ "$HTTP_STATUS" != "200" ]; then
  # 5xx/503 from the gateway: record, don't retry the LLM.
  check "chat returned 200 SSE stream (got $HTTP_STATUS — gateway/LLM failure, recorded)" false
  blocked "plan lifecycle (confirm/revert) — chat HTTP $HTTP_STATUS"
else
  expect_status 200
  check "SSE stream terminated with done or error event" \
    bash -c "grep -qE '^event: (done|error)' '$HTTP_BODY_FILE'"
  PLAN_ID=$(sse_plan_id "$HTTP_BODY_FILE")
fi

###############################################################################
# Step 3 — GET /ai/plans: find the plan, assert pending.
###############################################################################
http plans-pending GET "/api/v1/ai/plans?status=pending"
expect_status 200
if [ -z "$PLAN_ID" ]; then
  PLAN_ID=$(pending_plan_for_widget)
fi

if [ -z "$PLAN_ID" ]; then
  _BLOCKED=1
  blocked "no plan produced by chat 1 (LLM nondeterminism) — confirm/effect/revert checks skipped; raw SSE saved"
else
  check "plan $PLAN_ID is pending with type bulk_stock_adjust" \
    bash -c "jq -e --arg id '$PLAN_ID' '.items[] | select(.id == \$id) | (.status == \"pending\") and (.type == \"bulk_stock_adjust\")' '$HTTP_BODY_FILE' >/dev/null"
  check "plan targets exactly 1 product (unique filter)" \
    bash -c "jq -e --arg id '$PLAN_ID' '.items[] | select(.id == \$id) | .preview.affected_count == 1' '$HTTP_BODY_FILE' >/dev/null"
fi

###############################################################################
# Step 4 — confirm, verify the stock effect, revert, verify rollback.
# The 30s undo window runs from plan CreatedAt, so no sleeps in this block.
###############################################################################
if [ "$_BLOCKED" = "0" ] && [ -n "$PLAN_ID" ]; then
  http plan-confirm POST "/api/v1/ai/plans/$PLAN_ID/confirm"
  expect_status 200
  check "confirm reports status confirmed, affected_count 1" \
    bash -c "jq -e '(.status == \"confirmed\") and (.affected_count == 1)' '$HTTP_BODY_FILE' >/dev/null"

  http stock-after-confirm GET "/api/v1/stock/snapshots?product_id=$PROD_ID"
  expect_status 200
  QTY=$(body_json -r '[.items[]?.on_hand_qty // "0" | tonumber] | add // 0')
  check "on-hand qty is 5 after confirm (got $QTY)" [ "$QTY" = "5" ]

  http plan-revert POST "/api/v1/ai/plans/$PLAN_ID/revert"
  if [ "$HTTP_STATUS" = "200" ]; then
    check "revert returned 200 within undo window" [ "$HTTP_STATUS" = "200" ]
    http stock-after-revert GET "/api/v1/stock/snapshots?product_id=$PROD_ID"
    expect_status 200
    QTY=$(body_json -r '[.items[]?.on_hand_qty // "0" | tonumber] | add // 0')
    check "on-hand qty rolled back to 0 after revert (got $QTY)" [ "$QTY" = "0" ]

    # Idempotency guard: a second revert must 409 already_reverted.
    http plan-revert-again POST "/api/v1/ai/plans/$PLAN_ID/revert"
    expect_status 409
  elif [ "$HTTP_STATUS" = "409" ]; then
    # Documented contract: the 30s window is measured from plan CreatedAt and
    # LLM streaming latency can consume it before we reach this step.
    check "revert window closed (409 revert_window_closed) — documented contract, stock left at +5" \
      bash -c "jq -e '.error == \"revert_window_closed\"' '$HTTP_BODY_FILE' >/dev/null"
    blocked "rollback verification — undo window elapsed before revert (timing, not a product bug)"
  else
    check "revert returned 200 or documented 409 (got $HTTP_STATUS)" false
  fi
fi

###############################################################################
# Step 5 — parameter error paths (no LLM spend).
###############################################################################
http confirm-bogus POST "/api/v1/ai/plans/$BOGUS_UUID/confirm"
expect_status 404

http confirm-badid POST /api/v1/ai/plans/not-a-uuid/confirm
expect_status 400

http cancel-badid POST /api/v1/ai/plans/not-a-uuid/cancel
expect_status 400

http revert-bogus POST "/api/v1/ai/plans/$BOGUS_UUID/revert"
expect_status 404

###############################################################################
# Step 6 — LLM call 2: second plan, then cancel it.
# UAT_J7_SECOND_CHAT=0 skips this phase (authoring dry-runs spend 1 call max).
###############################################################################
if [ "${UAT_J7_SECOND_CHAT:-1}" = "1" ]; then
  ai_call_guard
  http ai-chat-2 POST /api/v1/ai/chat "${JSON[@]}" --max-time 180 \
    -d "{\"message\":\"这是自动化验收测试。请立刻调用 propose_bulk_stock_adjust 工具,参数 filter 设为 \\\"$WIDGET\\\",delta 设为 3。不要追问,不要调用其他工具,调用一次后直接结束回答。\"}"

  PLAN2_ID=""
  if [ "$HTTP_STATUS" = "200" ]; then
    PLAN2_ID=$(sse_plan_id "$HTTP_BODY_FILE")
  elif [ "$HTTP_STATUS" = "429" ]; then
    check "chat 2 rate-limited contract: 429 llm_rate_limited" \
      bash -c "jq -e '.error == \"llm_rate_limited\"' '$HTTP_BODY_FILE' >/dev/null"
  else
    check "chat 2 returned 200 SSE stream (got $HTTP_STATUS — recorded)" false
  fi

  if [ -z "$PLAN2_ID" ] && [ "$HTTP_STATUS" = "200" ]; then
    http plans-pending-2 GET "/api/v1/ai/plans?status=pending"
    expect_status 200
    PLAN2_ID=$(pending_plan_for_widget)
  fi

  if [ -z "$PLAN2_ID" ]; then
    blocked "cancel flow — chat 2 produced no plan (LLM nondeterminism or rate limit); raw SSE saved"
  else
    http plan2-cancel POST "/api/v1/ai/plans/$PLAN2_ID/cancel"
    expect_status 200
    check "cancel returns status cancelled" \
      bash -c "jq -e '.status == \"cancelled\"' '$HTTP_BODY_FILE' >/dev/null"

    http plans-cancelled GET "/api/v1/ai/plans?status=cancelled"
    expect_status 200
    check "plan $PLAN2_ID listed as cancelled" \
      bash -c "jq -e --arg id '$PLAN2_ID' '.items[] | select(.id == \$id) | .status == \"cancelled\"' '$HTTP_BODY_FILE' >/dev/null"
  fi
else
  echo "  [J7] UAT_J7_SECOND_CHAT=0 — phase 2 (second chat + cancel) skipped to conserve LLM budget"
fi

finish
