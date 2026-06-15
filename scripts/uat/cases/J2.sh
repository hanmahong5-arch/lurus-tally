#!/usr/bin/env bash
# J2 — Full purchase→sale loop with exact stock math (STAGE, commit da399443).
#
# Flow: unit + warehouse + supplier + 2 products → purchase draft (with
# Idempotency-Key) → PUT update draft → approve → assert snapshots/movements
# with numeric-exact deltas → quick-checkout sale → low-stock alert probe →
# payment record + list → gross-margin / sales-top → bills.csv export.
#
# KNOWN GAP (deployed commit da399443): no REST endpoint writes
# tally.stock_initial.low_safe_qty (only readers exist: stock repo ListLowStock,
# digest repo, replenish repo). The low-stock threshold therefore CANNOT be set
# through the API, so the "product appears in /stock/alerts/low-stock" check is
# expected to FAIL until a threshold-setting endpoint ships. The check is kept
# (not weakened) so the gap stays visible in every run.
set -u
UAT_CASES_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CASE_ID="J2"
# shellcheck disable=SC1091
source "$UAT_CASES_DIR/../lib.sh"

TS="$(date +%s)"                 # uniqueness inside one RUN_ID re-execution
P="UAT-${RUN_ID}-${TS}"          # mandated prefix for every created entity

# Billed quantities (integers → float math in jq stays exact)
PB_QTY1_DRAFT=60   # product 1 qty in the initial draft
PB_QTY1=55         # product 1 qty after PUT update (this is what approval books)
PB_QTY2=30         # product 2 qty (unchanged by update)
SALE_QTY1=50       # quick-checkout qty for product 1 → leaves 5 on hand
LOW_THRESHOLD=10   # intended low-stock threshold (NOT settable via API, see header)
PAY_AMOUNT=100     # payment recorded against the purchase bill

use_primary

# --- helper: set ON_HAND from the LAST http response --------------------------
# 200 → numeric on_hand_qty; 404 → 0 (snapshot does not exist before the first
# movement); anything else → ERR. Must be called right after http(); deliberately
# NOT wrapped in $() so http() keeps mutating harness state in this shell.
read_on_hand() {
  case "$HTTP_STATUS" in
    200) ON_HAND=$(body_json -r '.on_hand_qty | tonumber') ;;
    404) ON_HAND=0 ;;
    *)   ON_HAND="ERR" ;;
  esac
}

# ---------------------------------------------------------------------------
# 1. Master data
# ---------------------------------------------------------------------------
# unit_def.code is VARCHAR(20) in the DB (migration 000014) while the handler
# accepts up to 128 chars — the full "UAT-${RUN_ID}-…" prefix does not fit, so
# the code keeps a truncated "UAT-<run_id:9>-<ts:5>" marker (name carries the
# full mandated prefix). Oversized codes surface as 400 with a raw SQLSTATE.
UCODE=$(printf 'UAT-%.9s-%s' "$RUN_ID" "${TS: -5}")
http create-unit POST "/api/v1/units" \
  -H 'Content-Type: application/json' \
  -d "{\"code\":\"$UCODE\",\"name\":\"$P-unit\",\"unit_type\":\"count\"}"
expect_status 201
UNIT_ID=$(body_json -r '.id')
check "unit id is a UUID" jq -e '.id | test("^[0-9a-f-]{36}$")' "$HTTP_BODY_FILE"

http create-warehouse POST "/api/v1/warehouses" \
  -H 'Content-Type: application/json' \
  -d "{\"code\":\"$P-WH\",\"name\":\"$P-warehouse\"}"
expect_status 201
WH_ID=$(body_json -r '.id')
check "warehouse id is a UUID" jq -e '.id | test("^[0-9a-f-]{36}$")' "$HTTP_BODY_FILE"

http create-supplier POST "/api/v1/suppliers" \
  -H 'Content-Type: application/json' \
  -d "{\"code\":\"$P-SUP\",\"name\":\"$P-supplier\"}"
expect_status 201
SUP_ID=$(body_json -r '.id')

http create-product1 POST "/api/v1/products" \
  -H 'Content-Type: application/json' \
  -d "{\"code\":\"$P-P1\",\"name\":\"$P-product-1\",\"remark\":\"UAT J2\"}"
expect_status 201
PROD1_ID=$(body_json -r '.id')

http create-product2 POST "/api/v1/products" \
  -H 'Content-Type: application/json' \
  -d "{\"code\":\"$P-P2\",\"name\":\"$P-product-2\",\"remark\":\"UAT J2\"}"
expect_status 201
PROD2_ID=$(body_json -r '.id')

# ---------------------------------------------------------------------------
# 2. Purchase bill: DRAFT (with Idempotency-Key) → PUT update → approve
# ---------------------------------------------------------------------------
# KNOWN-BUG probe: bill_head.partner_id has an FK to tally.partner, but the only
# REST-creatable directory entity is tally.supplier (POST /api/v1/suppliers) and
# no /partners route exists. Linking a purchase bill to a supplier id therefore
# 500s with a raw FK violation instead of a 4xx validation error.
http create-purchase-with-supplier POST "/api/v1/purchase-bills" \
  -H 'Content-Type: application/json' \
  -d "$(jq -n --arg sup "$SUP_ID" --arg wh "$WH_ID" --arg p1 "$PROD1_ID" '
    {partner_id:$sup, warehouse_id:$wh, remark:"UAT J2 partner probe",
     items:[{product_id:$p1, line_no:1, qty:"1", unit_price:"1"}]}')"
check "KNOWN-BUG purchase bill accepts supplier id as partner_id (expect 201, got $HTTP_STATUS — 500 = FK bill_head_partner_id_fkey → tally.partner; suppliers live in tally.supplier and no /partners API exists)" \
  [ "$HTTP_STATUS" = "201" ]

# Real bill: omit partner_id (optional) so the flow can proceed.
PB_BODY=$(jq -n --arg wh "$WH_ID" \
  --arg p1 "$PROD1_ID" --arg p2 "$PROD2_ID" \
  --arg q1 "$PB_QTY1_DRAFT" --arg q2 "$PB_QTY2" '
  {warehouse_id:$wh, remark:"UAT J2 draft",
   items:[{product_id:$p1, line_no:1, qty:$q1, unit_price:"10"},
          {product_id:$p2, line_no:2, qty:$q2, unit_price:"20"}]}')
http create-purchase-draft POST "/api/v1/purchase-bills" \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: UAT-${RUN_ID}-${TS}-J2-pb" \
  -d "$PB_BODY"
expect_status 201
BILL_ID=$(body_json -r '.bill_id')
BILL_NO=$(body_json -r '.bill_no')
check "create returned bill_id + bill_no" \
  jq -e '(.bill_id|length)==36 and (.bill_no|length)>0' "$HTTP_BODY_FILE"

# PUT update while still draft: qty1 60 → 55, new remark
PB_UPDATE=$(jq -n --arg wh "$WH_ID" \
  --arg p1 "$PROD1_ID" --arg p2 "$PROD2_ID" \
  --arg q1 "$PB_QTY1" --arg q2 "$PB_QTY2" '
  {warehouse_id:$wh, remark:"UAT J2 updated",
   items:[{product_id:$p1, line_no:1, qty:$q1, unit_price:"10"},
          {product_id:$p2, line_no:2, qty:$q2, unit_price:"20"}]}')
http update-purchase-draft PUT "/api/v1/purchase-bills/$BILL_ID" \
  -H 'Content-Type: application/json' -d "$PB_UPDATE"
expect_status 200
check "update kept draft (status==0), remark applied, total == 1150 (55×10+30×20) exactly" \
  jq -e '.status==0 and .remark=="UAT J2 updated" and (.total_amount|tonumber)==1150' "$HTTP_BODY_FILE"

# Baseline on-hand BEFORE approval (404 → 0)
http snap-p1-before GET "/api/v1/stock/snapshots/$PROD1_ID/$WH_ID"
read_on_hand; BEFORE1=$ON_HAND
check "p1 baseline snapshot readable (got $BEFORE1)" [ "$BEFORE1" != "ERR" ]
http snap-p2-before GET "/api/v1/stock/snapshots/$PROD2_ID/$WH_ID"
read_on_hand; BEFORE2=$ON_HAND
check "p2 baseline snapshot readable (got $BEFORE2)" [ "$BEFORE2" != "ERR" ]

http approve-purchase POST "/api/v1/purchase-bills/$BILL_ID/approve" \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: UAT-${RUN_ID}-${TS}-J2-apprv" \
  -d '{}'
expect_status 200
check "approve response status==approved" jq -e '.status=="approved"' "$HTTP_BODY_FILE"

# ---------------------------------------------------------------------------
# 3. Stock math after approval — exact deltas
# ---------------------------------------------------------------------------
http snap-p1-after GET "/api/v1/stock/snapshots/$PROD1_ID/$WH_ID"
expect_status 200
read_on_hand; AFTER1=$ON_HAND
check "p1 on-hand delta == $PB_QTY1 exactly (before=$BEFORE1 after=$AFTER1)" \
  jq -ne --argjson b "$BEFORE1" --argjson a "$AFTER1" --argjson q "$PB_QTY1" '($a - $b) == $q'

http snap-p2-after GET "/api/v1/stock/snapshots/$PROD2_ID/$WH_ID"
expect_status 200
read_on_hand; AFTER2=$ON_HAND
check "p2 on-hand delta == $PB_QTY2 exactly (before=$BEFORE2 after=$AFTER2)" \
  jq -ne --argjson b "$BEFORE2" --argjson a "$AFTER2" --argjson q "$PB_QTY2" '($a - $b) == $q'

# list endpoint agrees with the per-SKU endpoint
http list-snapshots GET "/api/v1/stock/snapshots?product_id=$PROD1_ID&warehouse_id=$WH_ID"
expect_status 200
check "snapshots list shows p1 on_hand == $AFTER1" \
  jq -e --arg p "$PROD1_ID" --argjson a "$AFTER1" \
    '.items | map(select(.product_id==$p)) | length==1 and (.[0].on_hand_qty|tonumber)==$a' \
    "$HTTP_BODY_FILE"

# movement rows: one inbound per line, qty_base exact, reference_id == bill
http list-movements-p1 GET "/api/v1/stock/movements?product_id=$PROD1_ID&warehouse_id=$WH_ID"
expect_status 200
check "p1 movement: direction=in, qty_base==$PB_QTY1, reference_id==bill_id" \
  jq -e --arg ref "$BILL_ID" --argjson q "$PB_QTY1" \
    '[.items[] | select(.reference_id==$ref and .direction=="in" and (.qty_base|tonumber)==$q)] | length==1' \
    "$HTTP_BODY_FILE"

http list-movements-p2 GET "/api/v1/stock/movements?product_id=$PROD2_ID&warehouse_id=$WH_ID"
expect_status 200
check "p2 movement: direction=in, qty_base==$PB_QTY2, reference_id==bill_id" \
  jq -e --arg ref "$BILL_ID" --argjson q "$PB_QTY2" \
    '[.items[] | select(.reference_id==$ref and .direction=="in" and (.qty_base|tonumber)==$q)] | length==1' \
    "$HTTP_BODY_FILE"

# ---------------------------------------------------------------------------
# 4. Quick-checkout sale: drop p1 below the intended threshold
# ---------------------------------------------------------------------------
SALE_BODY=$(jq -n --arg p1 "$PROD1_ID" --arg wh "$WH_ID" --arg q "$SALE_QTY1" '
  {customer_name:"UAT J2 walk-in", payment_method:"cash", paid_amount:"1500",
   items:[{product_id:$p1, warehouse_id:$wh, line_no:1, qty:$q, unit_price:"30"}]}')
http quick-checkout POST "/api/v1/sale-bills/quick-checkout" \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: UAT-${RUN_ID}-${TS}-J2-qc" \
  -d "$SALE_BODY"
expect_status 201
SALE_BILL_NO=$(body_json -r '.bill_no')
SALE_BILL_ID=$(body_json -r '.bill_id')
check "quick-checkout total_amount == 1500 (50×30)" \
  jq -e '(.total_amount|tonumber)==1500' "$HTTP_BODY_FILE"

http snap-p1-after-sale GET "/api/v1/stock/snapshots/$PROD1_ID/$WH_ID"
expect_status 200
read_on_hand; AFTER_SALE1=$ON_HAND
check "p1 on-hand delta after sale == -$SALE_QTY1 exactly (was $AFTER1, now $AFTER_SALE1)" \
  jq -ne --argjson a "$AFTER1" --argjson n "$AFTER_SALE1" --argjson q "$SALE_QTY1" '($a - $n) == $q'

# outbound movement for the sale
http list-movements-sale GET "/api/v1/stock/movements?product_id=$PROD1_ID&warehouse_id=$WH_ID"
expect_status 200
check "sale movement: direction=out, qty_base==$SALE_QTY1, reference_id==sale bill" \
  jq -e --arg ref "$SALE_BILL_ID" --argjson q "$SALE_QTY1" \
    '[.items[] | select(.reference_id==$ref and .direction=="out" and (.qty_base|tonumber)==$q)] | length==1' \
    "$HTTP_BODY_FILE"

# ---------------------------------------------------------------------------
# 5. Low-stock alert — KNOWN GAP, see file header. on-hand (5) < threshold (10)
#    but stock_initial.low_safe_qty is unsettable via the deployed API, so the
#    join in ListLowStock can never match a UAT-created product.
# ---------------------------------------------------------------------------
http low-stock GET "/api/v1/stock/alerts/low-stock"
expect_status 200
check "low-stock items is an array" jq -e '.items | type=="array"' "$HTTP_BODY_FILE"
check "KNOWN-GAP p1 (on-hand $AFTER_SALE1 < intended threshold $LOW_THRESHOLD) listed in low-stock — no API at da399443 writes stock_initial.low_safe_qty, expected FAIL" \
  jq -e --arg p "$PROD1_ID" '[.items[] | select(.product_id==$p)] | length>=1' "$HTTP_BODY_FILE"

# ---------------------------------------------------------------------------
# 6. Payment against the purchase bill
#
# KNOWN-BUG (deployed commit da399443): POST /api/v1/payments always 422s.
# internal/adapter/repo/payment/repo.go:128 SumByBill runs
#   SELECT COALESCE(SUM(amount),0) … FOR UPDATE
# which PostgreSQL rejects (SQLSTATE 0A000 "FOR UPDATE is not allowed with
# aggregate functions"), so RecordPaymentUseCase can never commit. The two
# checks below are kept failing on purpose until the fix ships. The positive
# path is still proven via the quick-checkout payment (recorded through a
# different code path) appearing in GET /payments.
# ---------------------------------------------------------------------------
http record-payment POST "/api/v1/payments" \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: UAT-${RUN_ID}-${TS}-J2-pay" \
  -d "{\"bill_id\":\"$BILL_ID\",\"amount\":\"$PAY_AMOUNT\",\"payment_method\":\"bank\",\"remark\":\"UAT J2 payment\"}"
check "KNOWN-BUG POST /payments records (expect 201, got $HTTP_STATUS — 422 = SumByBill FOR UPDATE+SUM, SQLSTATE 0A000, repo.go:128)" \
  [ "$HTTP_STATUS" = "201" ]
check "KNOWN-BUG payment response status==recorded (cascade of the 422 above)" \
  jq -e '.status=="recorded"' "$HTTP_BODY_FILE"

http list-payments GET "/api/v1/payments?bill_id=$BILL_ID"
expect_status 200
check "KNOWN-BUG payment list contains amount==$PAY_AMOUNT for purchase bill (cascade: record 422s, so list stays empty)" \
  jq -e --argjson a "$PAY_AMOUNT" --arg b "$BILL_ID" \
    '[.items[] | select(.bill_id==$b and (.amount|tonumber)==$a)] | length>=1' "$HTTP_BODY_FILE"

# Positive payment-list path: quick-checkout recorded a cash payment of 1500
# through its own transaction (not affected by the SumByBill bug).
http list-payments-sale GET "/api/v1/payments?bill_id=$SALE_BILL_ID"
expect_status 200
check "payments list shows quick-checkout cash payment of 1500 on sale bill" \
  jq -e --arg b "$SALE_BILL_ID" \
    '[.items[] | select(.bill_id==$b and .pay_type=="cash" and (.amount|tonumber)==1500)] | length==1' \
    "$HTTP_BODY_FILE"

# ---------------------------------------------------------------------------
# 7. Reports
# ---------------------------------------------------------------------------
http gross-margin GET "/api/v1/reports/gross-margin"
expect_status 200
check "gross-margin has overall_margin/top10/bottom10/days" \
  jq -e 'has("overall_margin") and has("top10") and has("bottom10") and .days==30' "$HTTP_BODY_FILE"

http sales-top GET "/api/v1/reports/sales-top?metric=qty&days=7&limit=100"
expect_status 200
check "sales-top(qty,7d) lists $P-product-1" \
  jq -e --arg n "$P-product-1" '[.top_products[] | select(.name==$n)] | length==1' "$HTTP_BODY_FILE"
check "sales-top score for $P-product-1 == $SALE_QTY1 exactly" \
  jq -e --arg n "$P-product-1" --argjson q "$SALE_QTY1" \
    '[.top_products[] | select(.name==$n and (.score|tonumber)==$q)] | length==1' "$HTTP_BODY_FILE"

# ---------------------------------------------------------------------------
# 8. CSV export
# ---------------------------------------------------------------------------
http export-bills GET "/api/v1/exports/bills.csv"
expect_status 200
check "bills.csv content-type is text/csv" \
  grep -qi '^content-type: text/csv' "$HTTP_HEADERS_FILE"
BOM_HEX=$(od -An -tx1 -N3 "$HTTP_BODY_FILE" | tr -d ' \n')
check "bills.csv starts with UTF-8 BOM efbbbf (got $BOM_HEX)" [ "$BOM_HEX" = "efbbbf" ]
check "bills.csv contains purchase bill_no $BILL_NO" grep -q "$BILL_NO" "$HTTP_BODY_FILE"
check "bills.csv contains sale bill_no $SALE_BILL_NO" grep -q "$SALE_BILL_NO" "$HTTP_BODY_FILE"

finish
