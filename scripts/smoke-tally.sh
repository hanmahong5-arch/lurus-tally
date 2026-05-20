#!/usr/bin/env bash
# Smoke test — verify W0-W5 hardening is actually deployed and working.
# Run after deploying to STAGE / PROD; non-zero exit = at least one check failed.
#
# Usage:
#   BASE=https://tally-stage.lurus.cn TENANT=<uuid> AUTH="Bearer <pat>" ./scripts/smoke-tally.sh
#   BASE=http://localhost:18200 TENANT=<uuid> AUTH="Bearer dev-token" ./scripts/smoke-tally.sh
#
# Requires: curl, jq, psql (optional, for DB checks)

set -u

BASE="${BASE:-http://localhost:18200}"
TENANT="${TENANT:-}"
AUTH="${AUTH:-}"
PSQL="${PSQL:-}"           # e.g. "psql -h R6_IP -U tally -d tally"
NATS_CMD="${NATS_CMD:-}"   # e.g. "kubectl -n lurus-tally"

PASS=0
FAIL=0
SKIP=0

color() {
  case "$1" in
    green)  printf "\033[32m%s\033[0m" "$2" ;;
    red)    printf "\033[31m%s\033[0m" "$2" ;;
    yellow) printf "\033[33m%s\033[0m" "$2" ;;
    *)      printf "%s" "$2" ;;
  esac
}

ok()   { PASS=$((PASS+1)); echo "  $(color green PASS) $1"; }
bad()  { FAIL=$((FAIL+1)); echo "  $(color red   FAIL) $1"; }
skip() { SKIP=$((SKIP+1)); echo "  $(color yellow SKIP) $1"; }

echo "== Tally smoke @ $BASE =="

# ---------------------------------------------------------------------------
# W0.A1 — X-Tenant-ID header MUST be rejected (no fallback)
# ---------------------------------------------------------------------------
echo "[W0.A1] tenant header fallback removed"
STATUS=$(curl -sS -o /dev/null -w "%{http_code}" \
  -H "X-Tenant-ID: 00000000-0000-0000-0000-000000000001" \
  "$BASE/api/v1/products?limit=1" || echo "000")
if [ "$STATUS" = "401" ] || [ "$STATUS" = "403" ]; then
  ok "GET /products with forged X-Tenant-ID rejected ($STATUS)"
else
  bad "GET /products with forged X-Tenant-ID returned $STATUS (expected 401/403)"
fi

# ---------------------------------------------------------------------------
# W0.A4 — pagination limit hard cap (500)
# ---------------------------------------------------------------------------
echo "[W0.A4] pagination limit cap"
if [ -n "$AUTH" ]; then
  RESP=$(curl -sS -H "Authorization: $AUTH" \
    "$BASE/api/v1/products?limit=99999" || echo '{}')
  COUNT=$(echo "$RESP" | jq -r '.items // [] | length' 2>/dev/null || echo 0)
  if [ "$COUNT" -le 500 ]; then
    ok "limit=99999 clamped to $COUNT (<=500)"
  else
    bad "limit=99999 returned $COUNT rows (cap broken)"
  fi
else
  skip "AUTH not set, cannot exercise authenticated endpoint"
fi

# ---------------------------------------------------------------------------
# W0.A5 — AI chat rate limit (best-effort, doesn't burn quota in prod)
# ---------------------------------------------------------------------------
echo "[W0.A5] LLM rate limit"
skip "rate-limit verification requires 60+ requests; run manually if needed"

# ---------------------------------------------------------------------------
# W4.E2 — /ready returns 200 even if cache degraded
# ---------------------------------------------------------------------------
echo "[W4.E2] health degraded mode"
READY=$(curl -sS -w "\n%{http_code}" "$BASE/internal/v1/tally/ready" || echo "000")
STATUS=$(echo "$READY" | tail -n1)
BODY=$(echo "$READY" | sed '$d')
if [ "$STATUS" = "200" ]; then
  DEGRADED=$(echo "$BODY" | jq -r '.degraded // [] | length' 2>/dev/null || echo "?")
  ok "/ready returned 200 (degraded=$DEGRADED)"
elif [ "$STATUS" = "503" ]; then
  bad "/ready returned 503 — service unhealthy: $BODY"
else
  bad "/ready returned unexpected $STATUS"
fi

# ---------------------------------------------------------------------------
# W4.E1 — event_outbox table exists + has expected columns
# ---------------------------------------------------------------------------
echo "[W4.E1] event outbox table"
if [ -n "$PSQL" ]; then
  COLS=$($PSQL -tAc "SELECT count(*) FROM information_schema.columns WHERE table_schema='tally' AND table_name='event_outbox' AND column_name IN ('id','subject','payload','attempts','created_at','published_at')" 2>/dev/null || echo 0)
  if [ "$COLS" = "6" ]; then
    ok "tally.event_outbox has 6 expected columns"
    PENDING=$($PSQL -tAc "SELECT COUNT(*) FROM tally.event_outbox WHERE published_at IS NULL" 2>/dev/null || echo "?")
    echo "       pending events: $PENDING"
  else
    bad "tally.event_outbox missing columns (found $COLS of 6)"
  fi
else
  skip "PSQL not set, cannot verify event_outbox schema"
fi

# ---------------------------------------------------------------------------
# W1.B1/B2 — bill_head.revision + paid_amount CHECK + payment FK
# ---------------------------------------------------------------------------
echo "[W1] state machine + constraints"
if [ -n "$PSQL" ]; then
  REV=$($PSQL -tAc "SELECT data_type FROM information_schema.columns WHERE table_schema='tally' AND table_name='bill_head' AND column_name='revision'" 2>/dev/null || echo "")
  if [ -n "$REV" ]; then
    ok "bill_head.revision column exists ($REV)"
  else
    bad "bill_head.revision missing — migration 034 not applied?"
  fi

  CHK=$($PSQL -tAc "SELECT count(*) FROM information_schema.check_constraints WHERE constraint_schema='tally' AND check_clause LIKE '%paid_amount%total_amount%'" 2>/dev/null || echo 0)
  if [ "$CHK" -gt 0 ]; then
    ok "paid_amount <= total_amount CHECK present"
  else
    bad "paid_amount CHECK constraint missing"
  fi
else
  skip "PSQL not set"
fi

# ---------------------------------------------------------------------------
# W5.F3 — CSV export endpoints return valid CSV
# ---------------------------------------------------------------------------
echo "[W5.F3] CSV export"
if [ -n "$AUTH" ]; then
  CT=$(curl -sS -o /dev/null -w "%{content_type}" \
    -H "Authorization: $AUTH" \
    "$BASE/api/v1/exports/stock.csv" || echo "")
  if echo "$CT" | grep -q "text/csv\|application/csv"; then
    ok "stock.csv returns CSV content-type ($CT)"
  else
    bad "stock.csv content-type: $CT (expected text/csv)"
  fi

  # UTF-8 BOM check — Excel relies on this for Chinese
  FIRST3=$(curl -sS -H "Authorization: $AUTH" \
    "$BASE/api/v1/exports/stock.csv" | head -c 3 | od -An -tx1 | tr -d ' \n')
  if [ "$FIRST3" = "efbbbf" ]; then
    ok "stock.csv has UTF-8 BOM (Excel-friendly)"
  else
    bad "stock.csv missing UTF-8 BOM (got: $FIRST3)"
  fi
else
  skip "AUTH not set, cannot fetch CSV"
fi

# ---------------------------------------------------------------------------
# Observability — /internal/v1/metrics exposes business counters
# ---------------------------------------------------------------------------
echo "[obs] Prometheus metrics"
METRICS=$(curl -sS "$BASE/internal/v1/metrics" 2>/dev/null || echo "")
if [ -z "$METRICS" ]; then
  bad "/internal/v1/metrics returned empty (metrics endpoint down or gated)"
else
  for METRIC in tally_idempotency_skipped_total tally_llm_rate_limit_dropped_total tally_http_request_duration_seconds; do
    if echo "$METRICS" | grep -q "^# HELP $METRIC\|^$METRIC"; then
      ok "metric $METRIC present"
    else
      bad "metric $METRIC missing"
    fi
  done
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "== Result: $(color green "$PASS pass") / $(color red "$FAIL fail") / $(color yellow "$SKIP skip") =="
[ "$FAIL" -eq 0 ] && exit 0 || exit 1
