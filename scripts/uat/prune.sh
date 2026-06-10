#!/usr/bin/env bash
# prune.sh — optional cleanup of UAT-created data, API-level ONLY (no SQL).
# Deletes entities whose name/code starts with "UAT-" inside the two UAT
# tenants. Bills are cancelled, not deleted (the API has no bill delete).
# Demo data from /onboarding/seed-demo is removed via /onboarding/clear-demo.
#
# Usage: RUN_ID=prune bash prune.sh [name-prefix]   (default prefix: UAT-)
set -u

PREFIX="${1:-UAT-}"
export RUN_ID="${RUN_ID:-prune}"
export CASE_ID="prune"
UAT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck disable=SC1091
source "$UAT_DIR/lib.sh"

prune_tenant() {
  local label="$1"
  echo "== pruning $label tenant (prefix $PREFIX) =="

  # Cancel any draft/approved UAT bills first (frees stock references).
  for kind in purchase-bills sale-bills; do
    http "list-$kind" GET "/api/v1/$kind?limit=500"
    [ "$HTTP_STATUS" = "200" ] || continue
    for id in $(body_json -r --arg p "$PREFIX" \
        '(.items // .) | map(select((.remark // .bill_no // "") | startswith($p) or contains($p))) | .[].id' 2>/dev/null); do
      http "cancel-$kind-$id" POST "/api/v1/$kind/$id/cancel"
    done
  done

  # Entity collections with DELETE-by-id; list field name is "items" or array.
  local col
  for col in products projects suppliers warehouses nursery-dict units; do
    http "list-$col" GET "/api/v1/$col?limit=500"
    [ "$HTTP_STATUS" = "200" ] || continue
    for id in $(body_json -r --arg p "$PREFIX" \
        '(.items // .) | map(select(((.name // "") | startswith($p)) or ((.code // "") | startswith($p)))) | .[].id' 2>/dev/null); do
      http "del-$col-$id" DELETE "/api/v1/$col/$id"
    done
  done

  # Shopify shop bindings.
  http "list-shops" GET "/api/v1/shopify/shops"
  if [ "$HTTP_STATUS" = "200" ]; then
    for id in $(body_json -r --arg p "$PREFIX" \
        '(.items // .) | map(select((.shop_domain // .name // "") | contains($p))) | .[].id' 2>/dev/null); do
      http "del-shop-$id" DELETE "/api/v1/shopify/shops/$id"
    done
  fi

  # Demo dataset, if a seed-demo was left behind.
  http "clear-demo" POST "/api/v1/onboarding/clear-demo"

  # Revoke leftover temp PATs created by J5 (never the seeded uat-* anchors).
  http "list-pats" GET "/api/v1/auth/pats"
  if [ "$HTTP_STATUS" = "200" ]; then
    for id in $(body_json -r --arg p "$PREFIX" \
        '(.items // .) | map(select(.name | startswith($p))) | .[].id' 2>/dev/null); do
      http "revoke-pat-$id" DELETE "/api/v1/auth/pats/$id"
    done
  fi
}

use_primary;   prune_tenant primary
use_secondary; prune_tenant secondary

echo "prune complete (evidence: $EVID_DIR)"
exit 0
