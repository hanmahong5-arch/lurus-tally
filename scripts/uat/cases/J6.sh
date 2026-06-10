#!/usr/bin/env bash
# J6 — CSV order import + Shopify shop binding + public webhooks.
# STAGE = commit da399443. Verified handler contracts (read at authoring time):
#
#   importing/handler.go  POST /imports/orders  — multipart/form-data:
#       file (CSV, required) · platform=amazon|shopify (required) ·
#       warehouse=<uuid> (required, no server default) · hints (optional JSON).
#       ?preview=true → dry-run; commit → 201/200.
#       *** VERIFIED ON STAGE: under PAT auth this endpoint ALWAYS 422s with
#       "creator_id is required" (usecase.go:371) BEFORE the CSV is parsed,
#       because the handler derives creator_id from the Zitadel sub which the
#       PAT path never injects. The full import vertical therefore requires a
#       JWT and is asserted here only via its PAT error contract. ***
#       Shopify CSV columns (for reference / future JWT run): Name, Lineitem sku,
#       Lineitem quantity, Lineitem price, Currency, Created at (usecase.go:1122);
#       buildRow accepts order_date "2006-01-02", qty>0, price>=0 (usecase.go:1198);
#       targetCurrency = CNY (lifecycle/app.go:599).
#
#   importing/usecase.go:385 — whChecker.BelongsToTenant guards cross-tenant
#       warehouse_id. This is the FIX for finding S-01 (UAT-REPORT.md:92:
#       "CSV import 收 caller warehouse_id,无 tenant 归属校验 → 跨租户写库存").
#       lifecycle/app.go:597 wires importWarehouseChecker in production, so a
#       foreign warehouse uuid MUST be rejected (Execute returns an error →
#       handler 422 import_failed). Case (d) below re-probes S-01 explicitly.
#
#   shopify/handler.go  POST /shopify/shops — body {shop_domain, warehouse_id};
#       domain must match ^...\.myshopify\.com$ (usecase.go:33); warehouse must
#       be owned (else 422 ErrWarehouseNotOwned). 201 on success, GET lists,
#       DELETE :id → 204.
#
#   webhook/shopify.go — public, no auth. readAndVerify: HMAC verifySignature
#       returns false whenever h.secret == "" (shopify.go:349); with the secret
#       unset the BACKEND would 401 invalid_signature. *** VERIFIED ON STAGE:
#       the request never reaches the backend — the Next.js edge shadows
#       /webhooks/* and 307-redirects to the FE login page. We assert the
#       observed 307 (and flag the routing defect). ***
#
# Deployed route lines exercised here (verbatim from routes-deployed.txt):
#   POST   /api/v1/warehouses
#   POST   /api/v1/products
#   POST   /api/v1/imports/orders
#   GET    /api/v1/imports/mappings
#   GET    /api/v1/sale-bills
#   POST   /api/v1/shopify/shops
#   GET    /api/v1/shopify/shops
#   DELETE /api/v1/shopify/shops/:id
#   POST   /webhooks/shopify/orders
#   POST   /webhooks/shopify/refunds
set -u
CASE_ID=J6
# shellcheck source=../lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib.sh"

use_primary

# ---------------------------------------------------------------------------
# Fixtures: a warehouse + a product owned by the primary tenant.
# ---------------------------------------------------------------------------
http s1_create_warehouse POST /api/v1/warehouses \
  -H 'Content-Type: application/json' \
  --data "{\"name\":\"UAT-${RUN_ID}-wh\"}"
expect_status 201
WH_ID="$(body_json -r '.id // .ID // empty')"
check "warehouse id captured" test -n "$WH_ID"

http s2_create_product POST /api/v1/products \
  -H 'Content-Type: application/json' \
  --data "{\"name\":\"UAT-${RUN_ID}-import-prod\",\"code\":\"UAT-${RUN_ID}-SKU1\"}"
expect_status 201
PROD_ID="$(body_json -r '.id // empty')"
check "product id captured" test -n "$PROD_ID"

# Small single-order Shopify CSV (CNY → no FX row needed). One SKU we map below.
# Write into the CURRENT working dir and reference RELATIVELY: the MSYS curl
# cannot open absolute /c/... or /tmp/... paths via @, only cwd-relative ones.
# Cases are invoked from scripts/uat (cwd).
CSV_FILE="./_uat-${RUN_ID}-order.csv"
{
  printf 'Name,Lineitem sku,Lineitem quantity,Lineitem price,Currency,Created at\n'
  printf 'UAT-%s-ORDER-1,UAT-%s-SKU1,2,15.00,CNY,2026-06-01\n' "$RUN_ID" "$RUN_ID"
} >"$CSV_FILE"

# hints maps the platform SKU → our product so the order resolves (not unknown).
HINTS="[{\"platform_sku\":\"UAT-${RUN_ID}-SKU1\",\"product_id\":\"$PROD_ID\"}]"

# ---------------------------------------------------------------------------
# (a) PAT-PATH CONTRACT for CSV import.
#     IMPORTANT FINDING (verified on STAGE, da399443): the import use case
#     rejects with 422 {"error":"import_failed","detail":"importing: creator_id
#     is required"} BEFORE any CSV parse, because usecase.go:371 requires a
#     non-nil CreatorID and the handler derives it from the Zitadel sub
#     (importing/handler.go actorID → middleware.GetZitadelSub). The PAT path
#     injects NO sub, so CreatorID is always uuid.Nil under a PAT.
#
#     => The CSV import vertical (preview → commit → sale bill → persisted
#        mapping) is UNREACHABLE under PAT auth. It needs a Zitadel JWT, which
#        the UAT harness does not have. We therefore assert the documented
#        error contract (422 + creator_id detail) as the coverage outcome for
#        BOTH preview and commit, rather than fabricating a JWT.
# ---------------------------------------------------------------------------
http a1_import_preview POST "/api/v1/imports/orders?preview=true" \
  -F "file=@$CSV_FILE;type=text/csv" \
  -F "platform=shopify" \
  -F "warehouse=$WH_ID" \
  -F "hints=$HINTS"
expect_status 422
check "preview reject is the creator_id contract (no sub on PAT path)" \
  bash -c "jq -e '(.error==\"import_failed\") and ((.detail//\"\")|contains(\"creator_id\"))' '$HTTP_BODY_FILE' >/dev/null"

http a2_import_commit POST /api/v1/imports/orders \
  -F "file=@$CSV_FILE;type=text/csv" \
  -F "platform=shopify" \
  -F "warehouse=$WH_ID" \
  -F "hints=$HINTS"
expect_status 422
check "commit reject is the creator_id contract (no sub on PAT path)" \
  bash -c "jq -e '(.error==\"import_failed\") and ((.detail//\"\")|contains(\"creator_id\"))' '$HTTP_BODY_FILE' >/dev/null"

# ---------------------------------------------------------------------------
# (a cont.) GET /imports/mappings — tenant-scoped read; reachable under PAT
#     (no sub required, handler keys on middleware.GetTenantID). The mapping
#     is NOT persisted (commit never ran), so we assert only the envelope.
# ---------------------------------------------------------------------------
http a4_mappings GET "/api/v1/imports/mappings?platform=shopify"
expect_status 200
check "mappings envelope present" \
  bash -c "jq -e 'has(\"items\")' '$HTTP_BODY_FILE' >/dev/null"

# /sale-bills reachable under PAT (tenant-scoped) — coverage for the read path.
http a5_sale_bills GET "/api/v1/sale-bills?limit=50"
expect_status 200
check "sale-bills envelope present" \
  bash -c "jq -e 'has(\"items\")' '$HTTP_BODY_FILE' >/dev/null"

# ---------------------------------------------------------------------------
# (d) S-01 re-probe — DEVIATION: the cross-tenant warehouse guard (usecase.go:385,
#     finding S-01 from UAT-REPORT.md:92) CANNOT be exercised under PAT, because
#     the creator_id check (usecase.go:371) fires FIRST and 422s before the
#     warehouse-ownership check is reached. Asserting 422 here would be a FALSE
#     positive (right status, wrong reason). We therefore explicitly DO NOT claim
#     S-01 is verified — we record that it is blocked behind the PAT creator_id
#     gate and assert only that the request is still rejected (never 200/201, which
#     would be an unambiguous S-01 regression regardless of reason).
# ---------------------------------------------------------------------------
use_secondary
http d1_sec_warehouse POST /api/v1/warehouses \
  -H 'Content-Type: application/json' \
  --data "{\"name\":\"UAT-${RUN_ID}-sec-wh\"}"
expect_status 201
SEC_WH_ID="$(body_json -r '.id // .ID // empty')"

use_primary
if [ -n "$SEC_WH_ID" ]; then
  http d2_cross_tenant_import POST /api/v1/imports/orders \
    -F "file=@$CSV_FILE;type=text/csv" \
    -F "platform=shopify" \
    -F "warehouse=$SEC_WH_ID" \
    -F "hints=$HINTS"
  # Never a write success. 200/201 = unambiguous S-01 regression. 422/400/404 =
  # rejected (here specifically by the creator_id gate, NOT the warehouse guard —
  # S-01 itself remains UNVERIFIED under PAT; needs a JWT-authenticated re-run).
  check "cross-tenant import is NOT a write success (200/201 would be S-01 regression; rejection here is by creator_id gate, S-01 UNVERIFIED under PAT)" \
    bash -c "[ '$HTTP_STATUS' = '422' ] || [ '$HTTP_STATUS' = '400' ] || [ '$HTTP_STATUS' = '404' ]"
else
  check "secondary warehouse id available for S-01 probe" false
fi

rm -f "$CSV_FILE"

# ---------------------------------------------------------------------------
# (b) Shopify shop binding CRUD (authenticated, primary tenant + owned WH).
# ---------------------------------------------------------------------------
SHOP_DOMAIN="uat-${RUN_ID}.myshopify.com"
# Shopify slug must be lowercase [a-z0-9-]; RUN_ID may contain other chars, so
# sanitise to keep the domain regex-valid (usecase.go:33).
SHOP_SLUG="$(printf 'uat-%s' "$RUN_ID" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9-' '-' | sed -E 's/-+/-/g; s/^-//; s/-$//')"
SHOP_DOMAIN="${SHOP_SLUG}.myshopify.com"

http b1_bind_shop POST /api/v1/shopify/shops \
  -H 'Content-Type: application/json' \
  --data "{\"shop_domain\":\"$SHOP_DOMAIN\",\"warehouse_id\":\"$WH_ID\"}"
expect_status 201
SHOP_ID="$(body_json -r '.id // empty')"
check "shop binding id captured" test -n "$SHOP_ID"

http b2_list_shops GET /api/v1/shopify/shops
expect_status 200
check "bound shop appears in list" \
  bash -c "jq -e --arg d '$SHOP_DOMAIN' '[(.items//[])[]|select(.shop_domain==\$d)]|length>=1' '$HTTP_BODY_FILE' >/dev/null"

if [ -n "$SHOP_ID" ]; then
  http b3_unbind_shop DELETE "/api/v1/shopify/shops/$SHOP_ID"
  expect_status_in "200 204"
else
  check "shop id available to unbind" false
fi

# ---------------------------------------------------------------------------
# (c) Public webhooks (no auth, use_none).
#     FINDING (verified on STAGE): the public hostname tally-stage.lurus.cn is
#     fronted by the Next.js app, whose auth middleware INTERCEPTS /webhooks/*
#     and returns 307 → https://test-tally.lurus.cn/login?callbackUrl=... The
#     request NEVER reaches the Go webhook handler, so the intended backend
#     contract (401 invalid_signature when SHOPIFY_WEBHOOK_SECRET is unset,
#     webhook/shopify.go:349) is UNREACHABLE from the public edge.
#
#     This is a real STAGE routing defect: a genuine Shopify webhook delivery
#     would be bounced to a login redirect instead of being verified/ingested.
#     The frontend route matcher must EXCLUDE /webhooks/* (as it already does
#     for /api). We assert the observed edge behaviour (307 to the login page)
#     and flag it; the backend reject is asserted as a documented expectation in
#     the comment, not fabricated.
# ---------------------------------------------------------------------------
use_none

http c1_webhook_orders POST /webhooks/shopify/orders \
  -H 'Content-Type: application/json' \
  -H "X-Shopify-Topic: orders/create" \
  -H "X-Shopify-Shop-Domain: $SHOP_DOMAIN" \
  -H "X-Shopify-Hmac-Sha256: Zm9v" \
  --data '{"id":1,"name":"#UAT","line_items":[]}'
# STAGE edge shadows the route → 307 redirect to the FE login page.
# (If the FE matcher is fixed to exclude /webhooks, this becomes 401 from Go —
#  update the assertion at that point.)
expect_status_in "307 401"
check "FINDING: orders webhook is shadowed by FE auth (307 login redirect) OR backend-rejected (401)" \
  bash -c "[ '$HTTP_STATUS' = '307' ] || [ '$HTTP_STATUS' = '401' ]"
if [ "$HTTP_STATUS" = "307" ]; then
  check "307 target is the FE login redirect (confirms edge shadowing of /webhooks)" \
    bash -c "grep -qi 'location:.*/login' '$HTTP_HEADERS_FILE'"
else
  check "401 body is the backend invalid_signature reject" \
    bash -c "jq -e '.error==\"invalid_signature\"' '$HTTP_BODY_FILE' >/dev/null"
fi

http c2_webhook_refunds POST /webhooks/shopify/refunds \
  -H 'Content-Type: application/json' \
  -H "X-Shopify-Topic: refunds/create" \
  -H "X-Shopify-Shop-Domain: $SHOP_DOMAIN" \
  -H "X-Shopify-Hmac-Sha256: Zm9v" \
  --data '{"id":1,"order_id":1,"refund_line_items":[]}'
expect_status_in "307 401"
check "FINDING: refunds webhook is shadowed by FE auth (307 login redirect) OR backend-rejected (401)" \
  bash -c "[ '$HTTP_STATUS' = '307' ] || [ '$HTTP_STATUS' = '401' ]"
if [ "$HTTP_STATUS" = "307" ]; then
  check "307 target is the FE login redirect (confirms edge shadowing of /webhooks)" \
    bash -c "grep -qi 'location:.*/login' '$HTTP_HEADERS_FILE'"
else
  check "401 body is the backend invalid_signature reject" \
    bash -c "jq -e '.error==\"invalid_signature\"' '$HTTP_BODY_FILE' >/dev/null"
fi

finish
