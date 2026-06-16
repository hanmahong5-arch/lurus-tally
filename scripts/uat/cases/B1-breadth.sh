#!/usr/bin/env bash
# B1-breadth.sh — breadth sweep of every deployed endpoint NOT covered by the
# J1-J8 journey cases. One expected-success path per endpoint (or the documented
# expected-error when success is unreachable under a PAT) plus one error path
# (400/404) wherever the route takes parameters.
#
# Contracts asserted here were read from the DEPLOYED commit da399443
# (git show da399443:internal/adapter/handler/...), NOT the working tree:
#   * PUT /api/v1/sale-bills/:id            -> 501 not_implemented (Story 7.2 stub)
#   * POST /api/v1/auth/logout              -> 200 {"status":"logged out"} (server-side stub, no sub needed)
#   * POST /api/v1/tenant/profile           -> 401 under PAT (requires Zitadel sub; PAT path injects none)
#   * GET/POST /api/v1/account/avatar       -> 401 under PAT (requires Zitadel sub)
#   * GET /internal/v1/metrics              -> 401 without the INTERNAL_API_KEY bearer
#   * POST /internal/v1/telemetry/web       -> 401 without the PLATFORM_INTERNAL_KEY bearer
#   * DELETE on units/projects/suppliers/warehouses/products/nursery-dict -> 204
#   * POST .../restore                      -> 200 (entity body or {"status":"draft"} for purchase bills)
#
# Invoke: RUN_ID=<id> bash scripts/uat/cases/B1-breadth.sh
set -u
CASE_ID="B1-breadth"
# shellcheck disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"

PFX="UAT-${RUN_ID}-B1"
# Entity NAMES carry the full UAT-${RUN_ID}- prefix (safety rule); CODE columns
# are width-limited in the schema (unit.code VARCHAR(20), project VARCHAR(50))
# so codes use a short run-derived token instead of the raw RUN_ID.
# The epoch seconds are mixed in so that a re-run with the same RUN_ID still
# generates unique codes (avoiding 409 duplicate-key conflicts).
SHORT=$(printf '%s-%s' "$RUN_ID" "$(date +%s)" | cksum | cut -d' ' -f1)
BOGUS_UUID="00000000-0000-0000-0000-00000000dead"
JSON=(-H 'Content-Type: application/json')

###############################################################################
# Section 0 — public internal surface (no auth)
#   GET /internal/v1/tally/health
#   GET /internal/v1/tally/ready
#   GET /internal/v1/metrics
###############################################################################
use_none

http health GET /internal/v1/tally/health
expect_status 200

http ready GET /internal/v1/tally/ready
expect_status 200

# Metrics is gated by a bearer key the UAT harness intentionally does not hold.
http metrics-nokey GET /internal/v1/metrics
expect_status 401

http metrics-wrongkey GET /internal/v1/metrics -H 'Authorization: Bearer not-the-internal-key'
expect_status 401

###############################################################################
# Section 1 — POST /internal/v1/telemetry/web (PLATFORM_INTERNAL_KEY gate)
###############################################################################
http telemetry-noauth POST /internal/v1/telemetry/web "${JSON[@]}" \
  -d '{"event":"page_view"}'
expect_status 401

http telemetry-wrongkey POST /internal/v1/telemetry/web "${JSON[@]}" \
  -H 'Authorization: Bearer not-the-internal-key' -d '{"event":"page_view"}'
expect_status 401

use_primary

###############################################################################
# Section 2 — units
#   GET /api/v1/units · POST /api/v1/units · DELETE /api/v1/units/:id
###############################################################################
http unit-create POST /api/v1/units "${JSON[@]}" \
  -d "{\"code\":\"UB1-${SHORT}\",\"name\":\"${PFX}-unit\",\"unit_type\":\"count\"}"
expect_status 201
UNIT_ID=$(body_json -r '.id // empty')
check "unit create returned id" [ -n "$UNIT_ID" ]

# Disposable second unit for the delete cycle (keep UNIT_ID alive as a fixture).
http unit-create2 POST /api/v1/units "${JSON[@]}" \
  -d "{\"code\":\"UB2-${SHORT}\",\"name\":\"${PFX}-unit-del\",\"unit_type\":\"count\"}"
expect_status 201
UNIT2_ID=$(body_json -r '.id // empty')

http unit-list GET "/api/v1/units?unit_type=count"
expect_status 200
check "unit list contains ${PFX}-unit" \
  bash -c "jq -e --arg n '${PFX}-unit' '.items | map(.name) | index(\$n) != null' '$HTTP_BODY_FILE' >/dev/null"

http unit-delete DELETE "/api/v1/units/$UNIT2_ID"
expect_status 204

http unit-delete-again DELETE "/api/v1/units/$UNIT2_ID"
expect_status 404

http unit-delete-badid DELETE /api/v1/units/not-a-uuid
expect_status 400

###############################################################################
# Section 3 — currencies + exchange rates
#   GET /api/v1/currencies · POST /api/v1/exchange-rates
#   GET /api/v1/exchange-rates · GET /api/v1/exchange-rates/history
###############################################################################
http currencies GET /api/v1/currencies
expect_status 200

http rate-create POST /api/v1/exchange-rates "${JSON[@]}" \
  -d "{\"from_currency\":\"USD\",\"to_currency\":\"CNY\",\"rate\":\"7.25\",\"effective_at\":\"$(date -u +%Y-%m-%d)\"}"
expect_status 201

http rate-create-bad POST /api/v1/exchange-rates "${JSON[@]}" \
  -d '{"from_currency":"USD","to_currency":"CNY","rate":"-1"}'
expect_status 400

http rate-get GET "/api/v1/exchange-rates?from=USD&to=CNY"
expect_status 200

http rate-get-noparams GET /api/v1/exchange-rates
expect_status 400

http rate-history GET "/api/v1/exchange-rates/history?from=USD&to=CNY"
expect_status 200

http rate-history-noparams GET /api/v1/exchange-rates/history
expect_status 400

###############################################################################
# Section 4 — nursery-dict full CRUD + restore
#   GET/POST /api/v1/nursery-dict · GET/PUT/DELETE /api/v1/nursery-dict/:id
#   POST /api/v1/nursery-dict/:id/restore
###############################################################################
http dict-create POST /api/v1/nursery-dict "${JSON[@]}" \
  -d "{\"name\":\"${PFX}-${SHORT}-dict\",\"latin_name\":\"Uatus testus\",\"family\":\"Testaceae\",\"genus\":\"Uatus\",\"type\":\"shrub\",\"is_evergreen\":true,\"climate_zones\":[\"7\"],\"best_season\":[3,5],\"remark\":\"UAT breadth fixture\"}"
expect_status 201
DICT_ID=$(body_json -r '.id // empty')
check "dict create returned id" [ -n "$DICT_ID" ]

http dict-list GET /api/v1/nursery-dict
expect_status 200

http dict-get GET "/api/v1/nursery-dict/$DICT_ID"
expect_status 200
check "dict get name round-trips" \
  bash -c "jq -e --arg n '${PFX}-${SHORT}-dict' '.name == \$n' '$HTTP_BODY_FILE' >/dev/null"

http dict-update PUT "/api/v1/nursery-dict/$DICT_ID" "${JSON[@]}" \
  -d '{"remark":"UAT breadth fixture (updated)"}'
expect_status 200
check "dict update remark applied" \
  bash -c "jq -e '.remark == \"UAT breadth fixture (updated)\"' '$HTTP_BODY_FILE' >/dev/null"

http dict-delete DELETE "/api/v1/nursery-dict/$DICT_ID"
expect_status 204

http dict-get-after-delete GET "/api/v1/nursery-dict/$DICT_ID"
expect_status 404

http dict-restore POST "/api/v1/nursery-dict/$DICT_ID/restore"
expect_status 200

http dict-get-after-restore GET "/api/v1/nursery-dict/$DICT_ID"
expect_status 200

http dict-get-bogus GET "/api/v1/nursery-dict/$BOGUS_UUID"
expect_status 404

http dict-get-badid GET /api/v1/nursery-dict/not-a-uuid
expect_status 400

###############################################################################
# Section 5 — projects full CRUD + restore
#   GET/POST /api/v1/projects · GET/PUT/DELETE /api/v1/projects/:id
#   POST /api/v1/projects/:id/restore
###############################################################################
http proj-create POST /api/v1/projects "${JSON[@]}" \
  -d "{\"code\":\"UB1-${SHORT}-PJ\",\"name\":\"${PFX}-project\",\"manager\":\"UAT bot\",\"remark\":\"UAT breadth fixture\"}"
expect_status 201
PROJ_ID=$(body_json -r '.id // empty')
check "project create returned id" [ -n "$PROJ_ID" ]

http proj-list GET /api/v1/projects
expect_status 200

http proj-get GET "/api/v1/projects/$PROJ_ID"
expect_status 200

http proj-update PUT "/api/v1/projects/$PROJ_ID" "${JSON[@]}" \
  -d '{"manager":"UAT bot updated"}'
expect_status 200

http proj-delete DELETE "/api/v1/projects/$PROJ_ID"
expect_status 204

http proj-restore POST "/api/v1/projects/$PROJ_ID/restore"
expect_status 200

http proj-get-after-restore GET "/api/v1/projects/$PROJ_ID"
expect_status 200

http proj-get-bogus GET "/api/v1/projects/$BOGUS_UUID"
expect_status 404

http proj-update-badid PUT /api/v1/projects/not-a-uuid "${JSON[@]}" -d '{"name":"x"}'
expect_status 400

###############################################################################
# Section 6 — suppliers (GET list / GET :id / PUT / DELETE / restore)
#   POST /api/v1/suppliers is the fixture step.
###############################################################################
http supp-create POST /api/v1/suppliers "${JSON[@]}" \
  -d "{\"code\":\"UB1-${SHORT}-SUP\",\"name\":\"${PFX}-${SHORT}-supplier\",\"contact\":\"UAT bot\",\"remark\":\"UAT breadth fixture\"}"
expect_status 201
SUPP_ID=$(body_json -r '.id // empty')
check "supplier create returned id" [ -n "$SUPP_ID" ]

http supp-list GET /api/v1/suppliers
expect_status 200

http supp-get GET "/api/v1/suppliers/$SUPP_ID"
expect_status 200

http supp-update PUT "/api/v1/suppliers/$SUPP_ID" "${JSON[@]}" \
  -d '{"contact":"UAT bot updated"}'
expect_status 200

http supp-delete DELETE "/api/v1/suppliers/$SUPP_ID"
expect_status 204

http supp-restore POST "/api/v1/suppliers/$SUPP_ID/restore"
expect_status 200

http supp-get-bogus GET "/api/v1/suppliers/$BOGUS_UUID"
expect_status 404

http supp-update-badid PUT /api/v1/suppliers/not-a-uuid "${JSON[@]}" -d '{"name":"x"}'
expect_status 400

###############################################################################
# Section 7 — warehouses (GET list / GET :id / PUT / DELETE / restore)
#   WH1 stays alive as the bill fixture; WH2 runs the delete/restore cycle.
###############################################################################
http wh-create POST /api/v1/warehouses "${JSON[@]}" \
  -d "{\"code\":\"UB1-${SHORT}-WH\",\"name\":\"${PFX}-${SHORT}-warehouse\",\"remark\":\"UAT breadth fixture\"}"
expect_status 201
WH_ID=$(body_json -r '.id // empty')
check "warehouse create returned id" [ -n "$WH_ID" ]

http wh-create2 POST /api/v1/warehouses "${JSON[@]}" \
  -d "{\"code\":\"UB1-${SHORT}-WH2\",\"name\":\"${PFX}-${SHORT}-warehouse-del\"}"
expect_status 201
WH2_ID=$(body_json -r '.id // empty')

http wh-list GET /api/v1/warehouses
expect_status 200

http wh-get GET "/api/v1/warehouses/$WH_ID"
expect_status 200

http wh-update PUT "/api/v1/warehouses/$WH_ID" "${JSON[@]}" \
  -d '{"manager":"UAT bot"}'
expect_status 200

http wh-delete DELETE "/api/v1/warehouses/$WH2_ID"
expect_status 204

http wh-restore POST "/api/v1/warehouses/$WH2_ID/restore"
expect_status 200

http wh-get-bogus GET "/api/v1/warehouses/$BOGUS_UUID"
expect_status 404

http wh-update-badid PUT /api/v1/warehouses/not-a-uuid "${JSON[@]}" -d '{"name":"x"}'
expect_status 400

###############################################################################
# Section 8 — products PUT / DELETE / restore
#   PROD1 stays alive as the bill fixture; PROD2 runs the delete/restore cycle.
###############################################################################
http prod-create POST /api/v1/products "${JSON[@]}" \
  -d "{\"code\":\"UB1-${SHORT}-P1\",\"name\":\"${PFX}-product\",\"default_unit_id\":\"$UNIT_ID\",\"remark\":\"UAT breadth fixture\"}"
expect_status 201
PROD_ID=$(body_json -r '.id // empty')
check "product create returned id" [ -n "$PROD_ID" ]

http prod-update PUT "/api/v1/products/$PROD_ID" "${JSON[@]}" \
  -d "{\"name\":\"${PFX}-product\",\"brand\":\"UAT\",\"remark\":\"updated by breadth sweep\"}"
expect_status 200

http prod-get-after-update GET "/api/v1/products/$PROD_ID"
expect_status 200
check "product update brand applied" \
  bash -c "jq -e '.brand == \"UAT\"' '$HTTP_BODY_FILE' >/dev/null"

http prod-create2 POST /api/v1/products "${JSON[@]}" \
  -d "{\"code\":\"UB1-${SHORT}-P2\",\"name\":\"${PFX}-product-del\",\"default_unit_id\":\"$UNIT_ID\"}"
expect_status 201
PROD2_ID=$(body_json -r '.id // empty')

http prod-delete DELETE "/api/v1/products/$PROD2_ID"
expect_status 204

http prod-get-after-delete GET "/api/v1/products/$PROD2_ID"
expect_status 404

http prod-restore POST "/api/v1/products/$PROD2_ID/restore"
expect_status 200

http prod-get-after-restore GET "/api/v1/products/$PROD2_ID"
expect_status 200

http prod-update-bogus PUT "/api/v1/products/$BOGUS_UUID" "${JSON[@]}" -d '{"name":"x"}'
expect_status 404

http prod-restore-badid POST /api/v1/products/not-a-uuid/restore
expect_status 400

###############################################################################
# Section 9 — purchase bill restore
#   POST /api/v1/purchase-bills/:id/restore (target). Fixture steps: create a
#   draft, cancel it, restore it back to draft. A second draft is approved to
#   put stock on hand for the sale-bill section.
###############################################################################
PB_BODY="{\"warehouse_id\":\"$WH_ID\",\"remark\":\"${PFX} purchase\",\"items\":[{\"product_id\":\"$PROD_ID\",\"line_no\":1,\"qty\":\"10\",\"unit_price\":\"5.00\"}]}"

http pbill-create POST /api/v1/purchase-bills "${JSON[@]}" -d "$PB_BODY"
expect_status 201
PBILL_ID=$(body_json -r '.bill_id // empty')
check "purchase bill create returned bill_id" [ -n "$PBILL_ID" ]

http pbill-cancel POST "/api/v1/purchase-bills/$PBILL_ID/cancel"
expect_status 200

http pbill-restore POST "/api/v1/purchase-bills/$PBILL_ID/restore"
expect_status 200
check "purchase restore flips status back to draft" \
  bash -c "jq -e '.status == \"draft\"' '$HTTP_BODY_FILE' >/dev/null"

http pbill-restore-bogus POST "/api/v1/purchase-bills/$BOGUS_UUID/restore"
expect_status 404

http pbill-restore-badid POST /api/v1/purchase-bills/not-a-uuid/restore
expect_status 400

# Approve the restored draft so the sale section below has 10 units on hand.
# Idempotency-Key is mandatory on approve routes (hardening: aaeef7c1).
http pbill-approve POST "/api/v1/purchase-bills/$PBILL_ID/approve" \
  -H "Idempotency-Key: b1-pbapprove-${SHORT}"
expect_status 200

# Restoring an APPROVED bill must be refused (409, cannot_restore_approved_bill).
http pbill-restore-approved POST "/api/v1/purchase-bills/$PBILL_ID/restore"
expect_status 409

###############################################################################
# Section 10 — sale bills: PUT /:id + approve/cancel on our own drafts
#   PUT /api/v1/sale-bills/:id · POST /api/v1/sale-bills/:id/approve
#   POST /api/v1/sale-bills/:id/cancel
###############################################################################
SB_BODY="{\"warehouse_id\":\"$WH_ID\",\"remark\":\"${PFX} sale\",\"items\":[{\"product_id\":\"$PROD_ID\",\"warehouse_id\":\"$WH_ID\",\"line_no\":1,\"qty\":\"2\",\"unit_price\":\"9.00\"}]}"

http sbill-create POST /api/v1/sale-bills "${JSON[@]}" -d "$SB_BODY"
expect_status 201
SBILL_ID=$(body_json -r '.bill_id // empty')
check "sale bill create returned bill_id" [ -n "$SBILL_ID" ]

# Deployed contract (da399443 sale_handler.go:144): Update is a 501 stub
# ("sale bill update coming in Story 7.2"). Assert the stub, not a fantasy 200.
http sbill-update PUT "/api/v1/sale-bills/$SBILL_ID" "${JSON[@]}" -d "$SB_BODY"
expect_status 501

# GET by id — success + bogus-id 404 (closes the one pure coverage gap found
# in the denominator simulation; every other uncovered route is a known bug).
http sbill-get GET "/api/v1/sale-bills/$SBILL_ID"
expect_status 200
check "sale bill GET :id round-trips bill_id" \
  bash -c "jq -e --arg id '$SBILL_ID' '(.head.id // .bill_id // .id) == \$id' '$HTTP_BODY_FILE' >/dev/null"

http sbill-get-bogus GET "/api/v1/sale-bills/$BOGUS_UUID"
expect_status 404

# Idempotency-Key is mandatory on approve routes (hardening: aaeef7c1).
http sbill-approve POST "/api/v1/sale-bills/$SBILL_ID/approve" "${JSON[@]}" -d '{}' \
  -H "Idempotency-Key: b1-sbapprove-${SHORT}"
expect_status 200
check "sale approve returns approved" \
  bash -c "jq -e '.status == \"approved\"' '$HTTP_BODY_FILE' >/dev/null"

# Second draft exercises cancel.
http sbill-create2 POST /api/v1/sale-bills "${JSON[@]}" -d "$SB_BODY"
expect_status 201
SBILL2_ID=$(body_json -r '.bill_id // empty')

http sbill-cancel POST "/api/v1/sale-bills/$SBILL2_ID/cancel"
expect_status 200
check "sale cancel returns cancelled" \
  bash -c "jq -e '.status == \"cancelled\"' '$HTTP_BODY_FILE' >/dev/null"

# Idempotency-Key must be present even for the bogus-id 404 path; middleware
# validates the header before the handler looks up the resource (hardening: aaeef7c1).
http sbill-approve-bogus POST "/api/v1/sale-bills/$BOGUS_UUID/approve" "${JSON[@]}" -d '{}' \
  -H "Idempotency-Key: b1-sbapprove-bogus-${SHORT}"
expect_status 404

http sbill-cancel-badid POST /api/v1/sale-bills/not-a-uuid/cancel
expect_status 400

###############################################################################
# Section 11 — reports
#   GET /api/v1/reports/abc · GET /api/v1/reports/dead-stock
###############################################################################
http report-abc GET /api/v1/reports/abc
expect_status 200

http report-deadstock GET "/api/v1/reports/dead-stock?days=90"
expect_status 200

# Error path: the only failure mode these read-only reports expose is the
# auth gate (no parameters to corrupt — days is clamped, never rejected).
use_none
http report-abc-noauth GET /api/v1/reports/abc
expect_status 401
http report-deadstock-noauth GET /api/v1/reports/dead-stock
expect_status 401
use_primary

###############################################################################
# Section 12 — GET /api/v1/weekly-summary
###############################################################################
http weekly GET /api/v1/weekly-summary
expect_status 200

use_none
http weekly-noauth GET /api/v1/weekly-summary
expect_status 401
use_primary

###############################################################################
# Section 13 — POST /api/v1/tenant/profile
#   PAT path injects no Zitadel sub -> 401 is the documented contract.
###############################################################################
http tenant-profile-pat POST /api/v1/tenant/profile "${JSON[@]}" \
  -d '{"profile_type":"retail"}'
expect_status 401

use_none
http tenant-profile-noauth POST /api/v1/tenant/profile "${JSON[@]}" \
  -d '{"profile_type":"retail"}'
expect_status 401
use_primary

###############################################################################
# Section 14 — POST /api/v1/auth/logout
#   Deployed contract: server-side stub, returns 200 {"status":"logged out"}
#   for any authenticated caller (session clearing is NextAuth's job).
###############################################################################
http logout-pat POST /api/v1/auth/logout
expect_status 200
check "logout returns logged out" \
  bash -c "jq -e '.status == \"logged out\"' '$HTTP_BODY_FILE' >/dev/null"

use_none
http logout-noauth POST /api/v1/auth/logout
expect_status 401
use_primary

###############################################################################
# Section 15 — GET /api/v1/account/avatar
#   DownloadAvatar requires both tenant_id AND Zitadel sub; PAT injects no sub
#   -> 401 is the documented contract (counts as PAT-path coverage).
###############################################################################
http avatar-get-pat GET /api/v1/account/avatar
expect_status 401

use_none
http avatar-get-noauth GET /api/v1/account/avatar
expect_status 401
use_primary

finish
