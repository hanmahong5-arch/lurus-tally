#!/usr/bin/env bash
# extract-routes.sh — regenerate the UAT coverage denominator from the DEPLOYED
# commit's source (not the working tree). The deployed STAGE image tag is
# main-da39944 => commit da399443. When STAGE is upgraded, change DEPLOY_COMMIT
# (or pass it as $1), re-run, and commit the refreshed routes-deployed.txt.
#
# Output: scripts/uat/routes-deployed.txt — one "METHOD PATH" per line, sorted.
# Exit 1 when the regenerated list differs from the committed file (forces an
# audit instead of silently shifting the denominator under a running UAT).
#
# Route sources (verified against the commit by hand on 2026-06-10):
#   1. router.go direct registrations (health/ready/metrics + products/units groups)
#   2. every handler RegisterRoutes() that the router or lifecycle wires
#   3. lifecycle-registered extras: telemetry, shopify webhooks, shopify admin
set -euo pipefail

cd "$(dirname "$0")/../.."
DEPLOY_COMMIT="${1:-da399443}"
OUT="scripts/uat/routes-deployed.txt"
EXPECTED_COUNT=101 # hand-audited 2026-06-10; the plan's "103" was a miscount

# emit FILE PREFIX — print "METHOD PREFIX<path>" for each route registration.
emit() {
  local file="$1" prefix="$2"
  git show "$DEPLOY_COMMIT:$file" \
    | grep -oE '\.(GET|POST|PUT|DELETE|PATCH)\("[^"]*"' \
    | sed -E 's|^\.([A-Z]+)\("([^"]*)"$|\1 '"$prefix"'\2|'
}

TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT

{
  # router.go — only the directly-registered lines (the api.* lines are
  # nil-handler stubs that duplicate the real handlers below).
  git show "$DEPLOY_COMMIT:internal/adapter/handler/router/router.go" \
    | grep -E '^[[:space:]]*(internal|products|units)\.(GET|POST|PUT|DELETE)\(|r\.GET\("/internal/v1/metrics"' \
    | grep -oE '(internal|products|units|r)\.(GET|POST|PUT|DELETE)\("[^"]*"' \
    | sed -E \
        -e 's|^internal\.([A-Z]+)\("([^"]*)"$|\1 /internal/v1/tally\2|' \
        -e 's|^products\.([A-Z]+)\("([^"]*)"$|\1 /api/v1/products\2|' \
        -e 's|^units\.([A-Z]+)\("([^"]*)"$|\1 /api/v1/units\2|' \
        -e 's|^r\.([A-Z]+)\("([^"]*)"$|\1 \2|'

  # Handlers mounted on the /api/v1 group with absolute sub-paths.
  for f in account/handler.go auth/handler.go auth/pat_handler.go \
           bill/handler.go bill/sale_handler.go billing/handler.go \
           currency/handler.go digest/handler.go horticulture/dict_handler.go \
           importing/handler.go onboarding/handler.go payment/handler.go \
           project/handler.go replenish/handler.go reports/handler.go \
           search/handler.go shopify/handler.go stock/handler.go \
           supplier/handler.go warehouse/handler.go; do
    emit "internal/adapter/handler/$f" "/api/v1"
  done

  # Handlers that open their own sub-group.
  emit "internal/adapter/handler/ai/handler.go" "/api/v1/ai"
  emit "internal/adapter/handler/export/handler.go" "/api/v1/exports"

  # Lifecycle-registered, absolute paths.
  emit "internal/adapter/handler/telemetry/handler.go" ""
  emit "internal/adapter/handler/webhook/shopify.go" ""
} | sort -u >"$TMP"

COUNT=$(wc -l <"$TMP" | tr -d ' ')
echo "extracted $COUNT routes from $DEPLOY_COMMIT"

if [ "$COUNT" -ne "$EXPECTED_COUNT" ]; then
  echo "ERROR: route count $COUNT != expected $EXPECTED_COUNT — audit the extractor" >&2
  diff "$OUT" "$TMP" 2>/dev/null || true
  exit 1
fi

if [ -f "$OUT" ] && ! diff -q "$OUT" "$TMP" >/dev/null 2>&1; then
  echo "ERROR: regenerated list differs from committed $OUT — review the diff:" >&2
  diff "$OUT" "$TMP" >&2 || true
  exit 1
fi

cp "$TMP" "$OUT"
echo "wrote $OUT"
