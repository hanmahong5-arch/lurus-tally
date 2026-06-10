#!/usr/bin/env bash
# J3 — Replenishment journey on STAGE (deployed commit da399443), deployed
# subset only: GET /replenish/suggestions + POST /replenish/draft-batch.
# (GET /replenish/scorecard is NOT in routes-deployed.txt — intentionally not called.)
#
# Contract sources (all `git show da399443:<path>`):
#   internal/adapter/handler/replenish/handler.go:77-121  GetSuggestions → 200 {items,count,weeks}
#   internal/adapter/handler/replenish/handler.go:150-226 PostDraftBatch body {lines:[{product_id,qty,supplier_id?}]}
#   internal/adapter/handler/replenish/handler.go:231-247 resolveCreatorID: PAT path has no
#       zitadel_sub in context → CreatorID = uuid.Nil ("the use case validates non-nil and
#       rejects gracefully")
#   internal/app/bill/create_purchase.go:69-71            CreatorID == uuid.Nil → ErrValidation
#       "creator_id is required"; replenish handler maps ANY use-case error to 500
#       (handler.go:201-204). So under PAT the deployed contract for draft-batch is
#       500 internal_error + detail containing "creator_id is required".
#   internal/adapter/handler/bill/handler.go:285-319      GET /purchase-bills → 200 {items,total}
#   internal/adapter/handler/bill/handler.go:220-250      POST /purchase-bills/:id/cancel → 200 {"status":"cancelled"}
set -u
CASE_ID=J3
# shellcheck disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"

use_primary
P="UAT-${RUN_ID}"

# --- Step 1: suggestions list + shape -----------------------------------------
http suggestions GET /api/v1/replenish/suggestions
expect_status 200
check "suggestions shape: items array" jq -e '.items | type == "array"' "$HTTP_BODY_FILE"
check "suggestions shape: count == items length" \
  jq -e '.count == (.items | length)' "$HTTP_BODY_FILE"
check "suggestions shape: weeks defaults to 2" jq -e '.weeks == 2' "$HTTP_BODY_FILE"
check "suggestions rows carry required fields (vacuously true when empty)" \
  jq -e '[.items[] | has("product_id") and has("suggested_qty") and has("urgency_score")] | all' \
  "$HTTP_BODY_FILE"

# --- Step 2: fixtures for draft-batch (tenant-scoped, UAT-prefixed) -----------
http sup-create POST /api/v1/suppliers \
  -H 'Content-Type: application/json' \
  -d "{\"code\":\"${P}-J3SUP\",\"name\":\"${P}-J3-supplier\"}"
expect_status 201
SUP_ID=$(body_json -r '.id')

http prod-create POST /api/v1/products \
  -H 'Content-Type: application/json' \
  -d "{\"code\":\"${P}-J3PRD\",\"name\":\"${P}-J3-product\",\"remark\":\"${P} uat row\"}"
expect_status 201
PRD_ID=$(body_json -r '.id')

# --- Step 3: draft-batch -------------------------------------------------------
# Two contracts are possible depending on auth mode; under PAT the deployed code
# path is deterministic (no sub → creator uuid.Nil → 500 "creator_id is required",
# see header comment). If a future deploy supplies a creator on the PAT path the
# 200 branch verifies + cancels the created drafts instead.
http draft-batch POST /api/v1/replenish/draft-batch \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: ${P}-J3-draft" \
  -d "{\"lines\":[{\"product_id\":\"$PRD_ID\",\"supplier_id\":\"$SUP_ID\",\"qty\":\"5\"}]}"

if [ "$HTTP_STATUS" = "200" ]; then
  check "draft-batch 200: drafts array with count" \
    jq -e '(.drafts | type == "array") and (.count == (.drafts | length)) and .count >= 1' \
    "$HTTP_BODY_FILE"
  BILL_IDS=$(body_json -r '.drafts[].bill_id')
  for BILL_ID in $BILL_IDS; do
    # Verify the draft really exists as a purchase bill of this tenant.
    http "bill-get-${BILL_ID:0:8}" GET "/api/v1/purchase-bills/$BILL_ID"
    expect_status 200
    check "created draft $BILL_ID readable via /purchase-bills/:id" \
      jq -e --arg id "$BILL_ID" '.head.id == $id or .head.ID == $id' "$HTTP_BODY_FILE"
    # Cancel to leave no open drafts behind (bill/handler.go:249 → {"status":"cancelled"}).
    http "bill-cancel-${BILL_ID:0:8}" POST "/api/v1/purchase-bills/$BILL_ID/cancel"
    expect_status 200
    check "draft $BILL_ID cancelled" jq -e '.status == "cancelled"' "$HTTP_BODY_FILE"
  done
else
  # Deployed PAT contract: validation rejection surfaced as 500 internal_error.
  expect_status 500
  check "draft-batch PAT rejection detail mentions creator_id (create_purchase.go:69)" \
    jq -e '.error == "internal_error" and (.detail | test("creator_id is required"))' \
    "$HTTP_BODY_FILE"
  # PRODUCT-BUG SUSPICION (recorded, not fudged): a missing creator under a valid
  # PAT is a caller/auth-mode condition, yet it returns 500 instead of a 4xx.
fi

# --- Step 4: purchase-bills list shape (independent coverage) -------------------
http bills-list GET "/api/v1/purchase-bills?page=1&size=5"
expect_status 200
# Deployed reality: an empty result serializes items as JSON null (nil slice),
# not []. bill/handler.go:318 returns out.Items verbatim. Accept null-or-array.
check "purchase-bills list shape {items(null|array),total}" \
  jq -e '((.items | type) as $t | $t == "array" or $t == "null") and (.total | type == "number")' \
  "$HTTP_BODY_FILE"

# --- Step 5: cleanup fixtures ----------------------------------------------------
http prod-delete DELETE "/api/v1/products/$PRD_ID"
expect_status 204
http sup-delete DELETE "/api/v1/suppliers/$SUP_ID"
expect_status 204

finish
