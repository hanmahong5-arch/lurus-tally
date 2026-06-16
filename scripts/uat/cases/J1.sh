#!/usr/bin/env bash
# J1 — New-tenant journey on STAGE (deployed commit da399443).
#
# Flow: /me 401 contract → create warehouse → seed-demo → create unit /
# supplier / product (+ read each back, field round-trip) → weekly-summary
# → clear-demo → cleanup of UAT-created masters.
#
# Contract sources (all `git show da399443:<path>`):
#   internal/adapter/middleware/auth.go:96-112       PAT path sets tenant_id only, never zitadel_sub
#   internal/adapter/handler/auth/handler.go:126-134 GetMe: empty sub → 401 (expected under PAT)
#   internal/adapter/handler/onboarding/handler.go:104-145 SeedDemo: {persona, warehouse_id} → 200 {products_created}
#   internal/adapter/handler/onboarding/handler.go:147-162 ClearDemo: no body → 204 No Content
#       (NOTE: deployed contract returns NO deletion counts — 204 empty body is the intended shape)
#   internal/adapter/handler/warehouse/handler.go:39-46 createRequest {code,name,...} → 201 DTO
#   internal/adapter/handler/supplier/handler.go:40-48  createRequest {code,name,...} → 201 DTO
#   internal/adapter/handler/unit/handler.go:33-64      createRequest {code,name,unit_type} → 201; list = {items}
#       (no GET /units/:id route exists — read-back is via GET /api/v1/units)
#   internal/adapter/handler/product/handler.go:47-115  createRequest → 201 product JSON
#   internal/adapter/handler/digest/handler.go:67-95    weekly-summary → 200 {replenish{count,amount_cny},oversell,dead_stock,generated_at}
set -u
CASE_ID=J1
# shellcheck disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"

use_primary
P="UAT-${RUN_ID}" # mandatory prefix for every entity we name
# unit_def.code is VARCHAR(20); full P is too long. Derive a ≤20-char unit code:
# "UAT-" (4) + 12 chars of RUN_ID + "-U" (2) = 18 chars total for the final code.
UC="UAT-${RUN_ID:0:12}"

# --- Step 1: GET /me must 401 under PAT (no sub injected on PAT path) -------
http me-401 GET /api/v1/me
expect_status 401
check "/me 401 body has error=unauthorized" \
  jq -e '.error == "unauthorized"' "$HTTP_BODY_FILE"

# --- Step 2: create warehouse (needed by seed-demo) --------------------------
http wh-create POST /api/v1/warehouses \
  -H 'Content-Type: application/json' \
  -d "{\"code\":\"${P}-WH\",\"name\":\"${P}-warehouse\",\"address\":\"${P} addr\"}"
expect_status 201
WH_ID=$(body_json -r '.id')
check "warehouse create returned uuid id" \
  jq -e '.id | test("^[0-9a-f-]{36}$")' "$HTTP_BODY_FILE"

http wh-get GET "/api/v1/warehouses/$WH_ID"
expect_status 200
check "warehouse round-trip: code" jq -e --arg v "${P}-WH" '.code == $v' "$HTTP_BODY_FILE"
check "warehouse round-trip: name" jq -e --arg v "${P}-warehouse" '.name == $v' "$HTTP_BODY_FILE"

# --- Step 3: seed demo data ---------------------------------------------------
# persona must be cross_border|retail|horticulture (onboarding/handler.go:120-127).
# Seeded product codes are server-defined (DEMO-RT-*, app/onboarding/usecase.go:130+,
# remark="DEMO") — clear-demo below removes exactly those rows.
#
# PRODUCT BUG (STAGE da399443): the intended contract is 200 {products_created},
# but seed-demo returns 500. Root cause is NOT a test artifact — the onboarding
# stock adapter (onboarding/handler.go:36-48) sets ReferenceType=RefInit but
# never sets ReferenceID, so the stock_movement insert (repo/stock/repo.go:226-234)
# writes NULL into the NOT NULL reference_id column (SQLSTATE 23502). The product
# row IS created first, the stock movement then fails and aborts. We assert the
# INTENDED 200 contract so this stays RED as evidence; we do NOT green-wash it.
http seed-demo POST /api/v1/onboarding/seed-demo \
  -H 'Content-Type: application/json' \
  -d "{\"persona\":\"retail\",\"warehouse_id\":\"$WH_ID\"}"
expect_status 200
if [ "$HTTP_STATUS" = "200" ]; then
  # Idempotent at product-code level: 3 on first run for this tenant, 0..3 on re-run.
  check "seed-demo products_created in 0..3" \
    jq -e '.products_created | type == "number" and . >= 0 and . <= 3' "$HTTP_BODY_FILE"
fi

# --- Step 4: unit create + read back (list — no GET /units/:id route) --------
# unit_def.code is VARCHAR(20); use UC (≤20 chars) instead of the full P prefix.
http unit-create POST /api/v1/units \
  -H 'Content-Type: application/json' \
  -d "{\"code\":\"${UC}-U\",\"name\":\"${P}-box\",\"unit_type\":\"count\"}"
expect_status 201
UNIT_ID=$(body_json -r '.id')
check "unit create returned uuid id" \
  jq -e '.id | test("^[0-9a-f-]{36}$")' "$HTTP_BODY_FILE"

http unit-list GET /api/v1/units
expect_status 200
check "unit round-trip via list: code+name" \
  jq -e --arg id "$UNIT_ID" --arg c "${UC}-U" --arg n "${P}-box" \
    '.items | map(select(.id == $id)) | length == 1 and .[0].code == $c and .[0].name == $n' \
    "$HTTP_BODY_FILE"

# --- Step 5: supplier create + read back -------------------------------------
http sup-create POST /api/v1/suppliers \
  -H 'Content-Type: application/json' \
  -d "{\"code\":\"${P}-SUP\",\"name\":\"${P}-supplier\",\"contact\":\"uat\",\"phone\":\"000\"}"
expect_status 201
SUP_ID=$(body_json -r '.id')

http sup-get GET "/api/v1/suppliers/$SUP_ID"
expect_status 200
check "supplier round-trip: code"    jq -e --arg v "${P}-SUP" '.code == $v' "$HTTP_BODY_FILE"
check "supplier round-trip: name"    jq -e --arg v "${P}-supplier" '.name == $v' "$HTTP_BODY_FILE"
check "supplier round-trip: contact" jq -e '.contact == "uat"' "$HTTP_BODY_FILE"

# --- Step 6: product create + read back --------------------------------------
http prod-create POST /api/v1/products \
  -H 'Content-Type: application/json' \
  -d "{\"code\":\"${P}-PRD\",\"name\":\"${P}-product\",\"brand\":\"${P}\",\"remark\":\"${P} uat row\"}"
expect_status 201
PRD_ID=$(body_json -r '.id')

http prod-get GET "/api/v1/products/$PRD_ID"
expect_status 200
check "product round-trip: code"  jq -e --arg v "${P}-PRD" '.code == $v' "$HTTP_BODY_FILE"
check "product round-trip: name"  jq -e --arg v "${P}-product" '.name == $v' "$HTTP_BODY_FILE"
check "product round-trip: brand" jq -e --arg v "$P" '.brand == $v' "$HTTP_BODY_FILE"

# --- Step 7: weekly summary ----------------------------------------------------
http weekly GET /api/v1/weekly-summary
expect_status 200
check "weekly-summary shape: replenish.count number" \
  jq -e '.replenish.count | type == "number"' "$HTTP_BODY_FILE"
check "weekly-summary shape: replenish.amount_cny string" \
  jq -e '.replenish.amount_cny | type == "string"' "$HTTP_BODY_FILE"
check "weekly-summary shape: oversell + dead_stock counts" \
  jq -e '(.oversell.count|type=="number") and (.dead_stock.count|type=="number")' "$HTTP_BODY_FILE"
check "weekly-summary shape: generated_at present" \
  jq -e '.generated_at | type == "string" and length > 0' "$HTTP_BODY_FILE"

# --- Step 8: clear demo --------------------------------------------------------
# Deployed contract is 204 No Content with EMPTY body (onboarding/handler.go:161
# c.Status(http.StatusNoContent)) — the deployed API returns no deletion counts.
http clear-demo POST /api/v1/onboarding/clear-demo
expect_status 204
check "clear-demo body is empty (deployed contract has no deletion counts)" \
  [ ! -s "$HTTP_BODY_FILE" ]

# Verify demo products are actually gone: list filtered by demo code prefix.
http prod-list-demo GET "/api/v1/products?q=DEMO-RT-"
expect_status 200
check "demo products deleted (q=DEMO-RT- returns none)" \
  jq -e '(.items // []) | length == 0' "$HTTP_BODY_FILE"

# --- Step 9: cleanup of UAT-created masters (soft deletes) --------------------
http prod-delete DELETE "/api/v1/products/$PRD_ID"
expect_status 204
http sup-delete DELETE "/api/v1/suppliers/$SUP_ID"
expect_status 204
http unit-delete DELETE "/api/v1/units/$UNIT_ID"
expect_status 204
http wh-delete DELETE "/api/v1/warehouses/$WH_ID"
expect_status 204

finish
