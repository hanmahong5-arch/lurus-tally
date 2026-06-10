#!/usr/bin/env bash
# J8 — Edge & hardening probes (STAGE, commit da399443).
#
#  a) Idempotency replay  — same Idempotency-Key twice on POST /suppliers:
#     2nd response must carry "Idempotent-Replay: true" + byte-identical body
#     (internal/adapter/middleware/idempotency.go caches per tenant+key).
#  b) Pagination cap      — GET /products?limit=99999 → items length <= 500
#     (middleware.DefaultMaxPageLimit = 500 silently clamps).
#  c) CSV exports         — stock.csv / payments.csv → 200 + text/csv + BOM.
#  d) Search              — GET /search?q=<name> finds a created supplier
#     (handler: internal/adapter/handler/search/handler.go, param "q",
#      response {groups:[{type,items:[{type,id,label,sublabel}]}]}).
#  e) Pagination paging   — DEVIATION: the deployed commit has NO cursor
#     pagination anywhere (grep "cursor" over handlers/app = zero hits); all
#     lists are limit/offset or page/size. We exercise offset paging instead:
#     /suppliers?limit=1&offset=0 vs offset=1 must return different rows.
#  f) Malformed input     — invalid UUID path params → 400 per handler
#     contract; invalid JSON body → 400.
set -u
UAT_CASES_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CASE_ID="J8"
# shellcheck disable=SC1091
source "$UAT_CASES_DIR/../lib.sh"

TS="$(date +%s)"
P="UAT-${RUN_ID}-${TS}"
use_primary

# ---------------------------------------------------------------------------
# a) Idempotency replay
# ---------------------------------------------------------------------------
IDEM_KEY="UAT-${RUN_ID}-${TS}-J8-replay"
SUP_BODY="{\"code\":\"$P-S1\",\"name\":\"$P-supplier-idem\"}"

http idem-first POST "/api/v1/suppliers" \
  -H 'Content-Type: application/json' -H "Idempotency-Key: $IDEM_KEY" \
  -d "$SUP_BODY"
expect_status 201
FIRST_BODY="$HTTP_BODY_FILE"
SUP1_ID=$(body_json -r '.id')
check "first response has no Idempotent-Replay header" \
  bash -c "! grep -qi '^idempotent-replay:' '$HTTP_HEADERS_FILE'"

http idem-replay POST "/api/v1/suppliers" \
  -H 'Content-Type: application/json' -H "Idempotency-Key: $IDEM_KEY" \
  -d "$SUP_BODY"
expect_status 201
check "replay carries header Idempotent-Replay: true (evidence: raw headers file)" \
  grep -qi '^idempotent-replay: true' "$HTTP_HEADERS_FILE"
check "replay body is byte-identical to first body (same supplier id, no dup row)" \
  cmp -s "$FIRST_BODY" "$HTTP_BODY_FILE"

# corroborate no duplicate was created
http idem-count GET "/api/v1/suppliers?q=$P-supplier-idem"
expect_status 200
check "exactly one supplier named $P-supplier-idem exists" \
  jq -e --arg n "$P-supplier-idem" '[.items[] | select(.name==$n)] | length==1' "$HTTP_BODY_FILE"

# ---------------------------------------------------------------------------
# b) Pagination cap: limit=99999 clamped to <= 500
# ---------------------------------------------------------------------------
http products-cap GET "/api/v1/products?limit=99999"
expect_status 200
check "products items length <= 500 despite limit=99999" \
  jq -e '(.items | length) <= 500' "$HTTP_BODY_FILE"

# ---------------------------------------------------------------------------
# c) CSV exports: stock.csv / payments.csv
# ---------------------------------------------------------------------------
http export-stock GET "/api/v1/exports/stock.csv"
expect_status 200
check "stock.csv content-type is text/csv" \
  grep -qi '^content-type: text/csv' "$HTTP_HEADERS_FILE"
BOM_HEX=$(od -An -tx1 -N3 "$HTTP_BODY_FILE" | tr -d ' \n')
check "stock.csv starts with UTF-8 BOM efbbbf (got $BOM_HEX)" [ "$BOM_HEX" = "efbbbf" ]

http export-payments GET "/api/v1/exports/payments.csv"
expect_status 200
check "payments.csv content-type is text/csv" \
  grep -qi '^content-type: text/csv' "$HTTP_HEADERS_FILE"
BOM_HEX=$(od -An -tx1 -N3 "$HTTP_BODY_FILE" | tr -d ' \n')
check "payments.csv starts with UTF-8 BOM efbbbf (got $BOM_HEX)" [ "$BOM_HEX" = "efbbbf" ]

# ---------------------------------------------------------------------------
# d) Search finds a created entity (supplier from step a)
# ---------------------------------------------------------------------------
http search GET "/api/v1/search?q=$P-supplier-idem"
expect_status 200
check "search groups contain supplier $P-supplier-idem with matching id" \
  jq -e --arg n "$P-supplier-idem" --arg id "$SUP1_ID" \
    '[.groups[] | select(.type=="supplier") | .items[] | select(.label==$n and .id==$id)] | length==1' \
    "$HTTP_BODY_FILE"

# ---------------------------------------------------------------------------
# e) Pagination paging (offset-based — no cursor pagination exists at da399443)
# ---------------------------------------------------------------------------
http create-supplier2 POST "/api/v1/suppliers" \
  -H 'Content-Type: application/json' \
  -d "{\"code\":\"$P-S2\",\"name\":\"$P-supplier-page\"}"
expect_status 201

http page-0 GET "/api/v1/suppliers?limit=1&offset=0"
expect_status 200
PAGE0_ID=$(body_json -r '.items[0].id')
check "page 0 returns exactly 1 item" jq -e '(.items|length)==1' "$HTTP_BODY_FILE"
check "page 0 total >= 2" jq -e '.total >= 2' "$HTTP_BODY_FILE"

http page-1 GET "/api/v1/suppliers?limit=1&offset=1"
expect_status 200
check "page 1 returns exactly 1 item" jq -e '(.items|length)==1' "$HTTP_BODY_FILE"
check "page 1 item differs from page 0 item (offset paging advances)" \
  jq -e --arg p0 "$PAGE0_ID" '.items[0].id != $p0' "$HTTP_BODY_FILE"

# ---------------------------------------------------------------------------
# f) Malformed input probes
# ---------------------------------------------------------------------------
# invalid UUID in path → 400 (product handler: "invalid product id: must be a UUID")
http bad-uuid-product GET "/api/v1/products/not-a-uuid"
expect_status 400

# invalid UUID in path → 400 (bill handler: "invalid bill id")
http bad-uuid-bill GET "/api/v1/purchase-bills/not-a-uuid"
expect_status 400

# invalid UUID pair on snapshot path → 400 (stock handler: "invalid product_id")
http bad-uuid-snapshot GET "/api/v1/stock/snapshots/not-a-uuid/also-not-a-uuid"
expect_status 400

# invalid JSON body → 400 (ShouldBindJSON failure)
http bad-json-supplier POST "/api/v1/suppliers" \
  -H 'Content-Type: application/json' \
  --data-raw '{"name": "broken'
expect_status 400

http bad-json-bill POST "/api/v1/purchase-bills" \
  -H 'Content-Type: application/json' \
  --data-raw '{not json at all'
expect_status 400

finish
