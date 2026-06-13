#!/usr/bin/env bash
# J5 — Identity & RLS security.
# STAGE = commit da399443. Verified handler contracts (read at authoring time):
#   * middleware/auth.go: PAT path injects tenant_id only, NEVER a Zitadel sub.
#     Missing/forged/malformed bearer => 401. No X-Tenant-ID header fallback.
#   * auth/handler.go GetMe: 401 when sub == "" (always true on the PAT path).
#   * auth/pat_handler.go: Create=201 (+plaintext token once), List=200 {items},
#     Revoke=204 (idempotent; revoked token then 401 at middleware).
#   * product/handler.go: List/Get tenant-scoped via middleware.GetTenantID only;
#     Get of a foreign id => 404 (repoproduct.ErrNotFound).
#   * account/handler.go: sessions/profile/avatar all require BOTH tenantID AND
#     a non-empty userID (GetZitadelSub) => 401 under PAT. audit-log + RevokeSession
#     require ONLY tenantID => reachable under PAT (200 / 204).
#
# Deployed route lines exercised here (verbatim from routes-deployed.txt):
#   GET    /api/v1/products
#   GET    /api/v1/me
#   POST   /api/v1/auth/pats
#   GET    /api/v1/auth/pats
#   DELETE /api/v1/auth/pats/:id
#   POST   /api/v1/products
#   GET    /api/v1/products/:id
#   GET    /api/v1/stock/snapshots
#   GET    /api/v1/suppliers
#   GET    /api/v1/account/sessions
#   DELETE /api/v1/account/sessions/:id
#   GET    /api/v1/account/audit-log
#   GET    /api/v1/account/profile
#   PUT    /api/v1/account/profile
#   POST   /api/v1/account/avatar
#   GET    /api/v1/account/avatar
set -u
CASE_ID=J5
# shellcheck source=../lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib.sh"

PRIMARY_TENANT="$UAT_TENANT_PRIMARY"

# ---------------------------------------------------------------------------
# (a) Unauthenticated GET /products → 401.
# ---------------------------------------------------------------------------
use_none
http a_no_auth GET /api/v1/products
expect_status 401

# ---------------------------------------------------------------------------
# (b) Well-formed but fake PAT (tally_pat_ + 8-char prefix + 32-char secret,
#     URL-safe alphabet) → 401. Shape matches domain/auth/pat.go so the PAT
#     resolver runs (not just a prefix-shape reject) and still fails the hash.
# ---------------------------------------------------------------------------
_rand_urlsafe() { # $1 = char count
  LC_ALL=C tr -dc 'A-Za-z0-9_-' </dev/urandom | head -c "$1"
}
FAKE_PAT="tally_pat_$(_rand_urlsafe 8)$(_rand_urlsafe 32)"
use_token "$FAKE_PAT"
http b_fake_pat GET /api/v1/products
expect_status 401

# ---------------------------------------------------------------------------
# (c) Forged X-Tenant-ID header with NO bearer must NOT authenticate → 401.
#     The middleware reads tenant_id only from the verified token, never a header.
# ---------------------------------------------------------------------------
use_none
http c_forged_header GET /api/v1/products -H "X-Tenant-ID: $PRIMARY_TENANT"
expect_status 401

# ---------------------------------------------------------------------------
# (d) GET /me under a valid primary PAT → 401 (no sub on PAT path; expected
#     contract, documented in lib.sh gate comment + auth/handler.go).
# ---------------------------------------------------------------------------
use_primary
http d_me_under_pat GET /api/v1/me
expect_status 401

# ---------------------------------------------------------------------------
# (e) PAT lifecycle: create → use → list → revoke → revoked-token-rejected.
# ---------------------------------------------------------------------------
use_primary
http e1_create_pat POST /api/v1/auth/pats \
  -H 'Content-Type: application/json' \
  --data "{\"name\":\"UAT-${RUN_ID}-temp\"}"
expect_status 201
NEW_PAT="$(body_json -r '.token // empty')"
NEW_PAT_ID="$(body_json -r '.id // empty')"
check "create returned a plaintext token" test -n "$NEW_PAT"
check "create returned an id" test -n "$NEW_PAT_ID"

# The freshly minted token authenticates a tenant-scoped read.
use_token "$NEW_PAT"
http e2_use_new_pat GET /api/v1/products
expect_status 200

# It appears in the tenant's PAT list (read under the primary PAT).
use_primary
http e3_list_pats GET /api/v1/auth/pats
expect_status 200
check "new PAT id present in list" \
  bash -c "jq -e --arg id '$NEW_PAT_ID' '(.items//[])|map(.id)|index(\$id)!=null' '$HTTP_BODY_FILE' >/dev/null"

# Revoke it (204; idempotent per handler).
http e4_revoke_pat DELETE "/api/v1/auth/pats/$NEW_PAT_ID"
expect_status_in "200 204"

# The revoked token is now rejected at the middleware.
use_token "$NEW_PAT"
http e5_revoked_rejected GET /api/v1/products
expect_status 401

# ---------------------------------------------------------------------------
# (f) Cross-tenant RLS isolation.
#     primary creates a probe product; secondary must not see it (list omits it
#     AND GET :id → 404). Also secondary's stock/suppliers must not surface
#     primary rows by id. Then reverse-probe: secondary creates, primary blind.
# ---------------------------------------------------------------------------
use_primary
http f1_primary_create_product POST /api/v1/products \
  -H 'Content-Type: application/json' \
  --data "{\"name\":\"UAT-${RUN_ID}-rls-probe\",\"code\":\"UAT-${RUN_ID}-RLS\"}"
expect_status 201
PRIMARY_PROD_ID="$(body_json -r '.id // empty')"
check "primary product id captured" test -n "$PRIMARY_PROD_ID"

# secondary list must not contain the primary id.
use_secondary
http f2_secondary_list GET "/api/v1/products?limit=200"
expect_status 200
check "secondary product list does NOT contain primary id" \
  bash -c "jq -e --arg id '$PRIMARY_PROD_ID' '(.items//[])|map(.id)|index(\$id)==null' '$HTTP_BODY_FILE' >/dev/null"

# secondary GET of the exact primary id → 404 (RLS hides it; handler maps NotFound).
if [ -n "$PRIMARY_PROD_ID" ]; then
  http f3_secondary_get_primary GET "/api/v1/products/$PRIMARY_PROD_ID"
  expect_status 404
else
  check "primary product id present for cross-tenant GET probe" false
fi

# secondary stock snapshots / suppliers must not surface primary's probe product.
http f4_secondary_stock GET /api/v1/stock/snapshots
expect_status 200
check "secondary stock snapshots omit primary probe product id" \
  bash -c "jq -e --arg id '$PRIMARY_PROD_ID' '[(.items//[])[]|(.product_id//.ProductID//empty)]|index(\$id)==null' '$HTTP_BODY_FILE' >/dev/null"

http f5_secondary_suppliers GET "/api/v1/suppliers?limit=200"
expect_status 200
# Probe by the unique UAT name marker — primary's run-scoped rows must be absent.
check "secondary supplier list contains no primary UAT-${RUN_ID} rows" \
  bash -c "jq -e --arg m 'UAT-${RUN_ID}-' '[(.items//[])[]|(.name//\"\")|select(startswith(\$m))]|length==0' '$HTTP_BODY_FILE' >/dev/null"

# Reverse probe: secondary creates a supplier; primary must not see it.
http f6_secondary_create_supplier POST /api/v1/suppliers \
  -H 'Content-Type: application/json' \
  --data "{\"name\":\"UAT-${RUN_ID}-rls-rev-supplier\"}"
expect_status_in "200 201"
SEC_SUPPLIER_ID="$(body_json -r '.id // .ID // empty')"

use_primary
http f7_primary_list_suppliers GET "/api/v1/suppliers?limit=200"
expect_status 200
if [ -n "$SEC_SUPPLIER_ID" ]; then
  check "primary supplier list does NOT contain secondary id" \
    bash -c "jq -e --arg id '$SEC_SUPPLIER_ID' '(.items//[])|map(.id//.ID)|index(\$id)==null' '$HTTP_BODY_FILE' >/dev/null"
else
  # Fall back to the name marker if the create envelope omitted an id.
  check "primary supplier list contains no secondary reverse-probe row" \
    bash -c "jq -e --arg n 'UAT-${RUN_ID}-rls-rev-supplier' '[(.items//[])[]|(.name//\"\")|select(.==\$n)]|length==0' '$HTTP_BODY_FILE' >/dev/null"
fi

# ---------------------------------------------------------------------------
# (g) Account center under PAT — assert each endpoint's INTENDED contract.
#     sub-requiring (sessions list, profile GET/PUT, avatar up/down) → 401.
#     tenant-only (audit-log, RevokeSession) → reachable (200 / 204).
# ---------------------------------------------------------------------------
use_primary

# GET sessions — needs sub → 401.
http g1_sessions_list GET /api/v1/account/sessions
expect_status 401

# DELETE sessions/:id — needs only tenant → reaches the use case. With a random
# (non-existent) id the repo Revoke is idempotent → 204. (No sub gate here.)
RANDOM_SESSION_ID="$(cat /proc/sys/kernel/random/uuid 2>/dev/null || python - <<'PY' 2>/dev/null
import uuid;print(uuid.uuid4())
PY
)"
[ -n "$RANDOM_SESSION_ID" ] || RANDOM_SESSION_ID="00000000-0000-4000-8000-000000000000"
http g2_session_revoke DELETE "/api/v1/account/sessions/$RANDOM_SESSION_ID"
expect_status 204

# GET audit-log — tenant-only → 200 with envelope.
http g3_audit_log GET /api/v1/account/audit-log
expect_status 200
check "audit-log envelope has items array" \
  bash -c "jq -e 'has(\"items\") and (.items|type==\"array\")' '$HTTP_BODY_FILE' >/dev/null"

# GET profile — needs sub → 401.
http g4_profile_get GET /api/v1/account/profile
expect_status 401

# PUT profile — needs sub → 401 (gate runs before body bind).
http g5_profile_put PUT /api/v1/account/profile \
  -H 'Content-Type: application/json' \
  --data "{\"display_name\":\"UAT-${RUN_ID}-x\",\"phone\":\"\"}"
expect_status 401

# POST avatar (multipart) — needs sub → 401. Documented error contract under PAT.
# Write the fixture into the CURRENT working dir and reference it RELATIVELY:
# the MSYS curl cannot open absolute /c/... or /tmp/... paths via @, but a
# cwd-relative path works. Cases are invoked from scripts/uat (cwd).
AVATAR_TMP="./_uat-${RUN_ID}-avatar.png"
printf '\x89PNG\r\n\x1a\n' >"$AVATAR_TMP"
http g6_avatar_upload POST /api/v1/account/avatar -F "file=@$AVATAR_TMP;type=image/png"
expect_status 401
rm -f "$AVATAR_TMP"

# GET avatar — needs sub → 401.
http g7_avatar_get GET /api/v1/account/avatar
expect_status 401

finish
