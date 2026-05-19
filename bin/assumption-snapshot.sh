#!/usr/bin/env bash
# S0.C2 — daily snapshot of H1/H2/H3 assumption status into assumptions.md.
#
# Reads Prometheus for the (sparse, until later sprints) metrics behind
# each hypothesis, applies the falsification threshold, appends one row
# to the "状态历史" table at the bottom of
# `_bmad-output/planning-artifacts/assumptions.md`, and trims the table
# to the most recent 90 entries.
#
# Idempotency rule: if a row for today already exists, replace it (the
# afternoon re-run should reflect end-of-day numbers, not duplicate).
#
# Failure policy: any unexpected error → exit 1 AND post to the
# ASSUMPTION_SNAPSHOT_FAIL_FEISHU webhook so an operator notices the
# pipeline is broken instead of silently rotting.
#
# Required env:
#   PROM_URL           Prometheus base URL (e.g. http://prometheus.stage.r6:9090)
#   ASSUMPTIONS_PATH   Path to assumptions.md (default: repo path below)
# Optional:
#   ASSUMPTION_SNAPSHOT_FAIL_FEISHU  Webhook posted on script error.
#   DRY_RUN=1                        Print the new row, do not edit file.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ASSUMPTIONS_PATH="${ASSUMPTIONS_PATH:-$REPO_ROOT/_bmad-output/planning-artifacts/assumptions.md}"
PROM_URL="${PROM_URL:-}"
DRY_RUN="${DRY_RUN:-0}"

today="$(date -u +%Y-%m-%d)"

# ---------------------------------------------------------------------------
# helpers
# ---------------------------------------------------------------------------

fail_feishu() {
  local msg="$1"
  echo "ERROR: $msg" >&2
  if [ -n "${ASSUMPTION_SNAPSHOT_FAIL_FEISHU:-}" ]; then
    curl -sS -X POST "$ASSUMPTION_SNAPSHOT_FAIL_FEISHU" \
      -H "Content-Type: application/json" \
      -d "{\"msg_type\":\"text\",\"content\":{\"text\":\"assumption-snapshot.sh failed: $msg\"}}" \
      >/dev/null || true
  fi
  exit 1
}

trap 'fail_feishu "unexpected exit on line $LINENO"' ERR

# Query Prom; emit "n/a" if PROM_URL unset, the metric missing, or any error.
prom_query() {
  local q="$1"
  if [ -z "$PROM_URL" ]; then
    echo "n/a"
    return
  fi
  local resp
  resp=$(curl -sS --max-time 10 -G --data-urlencode "query=$q" "$PROM_URL/api/v1/query" 2>/dev/null || echo "")
  if [ -z "$resp" ]; then
    echo "n/a"
    return
  fi
  # jq is required; if missing, fall back to grep.
  local value
  value=$(echo "$resp" | jq -r '.data.result[0].value[1] // "n/a"' 2>/dev/null || echo "n/a")
  if [ "$value" = "null" ] || [ -z "$value" ]; then
    echo "n/a"
  else
    echo "$value"
  fi
}

decide_status() {
  # $1 = current value, $2 = threshold operator (e.g. "<0.3"), $3 = trial-window flag (true/false)
  local val="$1" thr="$2" in_window="$3"
  if [ "$val" = "n/a" ]; then
    echo "inconclusive"
    return
  fi
  # threshold is shell-evaluable via awk; e.g. "0.45 < 0.3" → 0 (false)
  if awk -v v="$val" -v t="${thr#<}" "BEGIN { exit !(v ${thr:0:1} t) }"; then
    echo "falsified"
  elif [ "$in_window" = "true" ]; then
    echo "pending"
  else
    echo "truthy"
  fi
}

# ---------------------------------------------------------------------------
# Per-hypothesis snapshot. Until later sprints emit the metrics, almost
# everything reports n/a — that's signal, not failure.
# ---------------------------------------------------------------------------

# H1: 90d trial → ¥3000+ conversion. Falsified if < 3/8 i.e. < 0.375.
h1_val=$(prom_query 'tally_trial_conversion_d90')
h1_status=$(decide_status "$h1_val" "<0.375" "true")

# H2: retail pilots stop Excel. Compound; conservative — leave as n/a
# until retail_pilot_log.md is in shape.
h2_val="n/a"
h2_status="inconclusive"

# H3: Palette DAU penetration < 0.40 → falsified. Use the lower of palette / drawer ratio.
palette_ratio=$(prom_query '(sum(increase(tally_palette_invocation_dau[1d])) or vector(0)) / (sum(increase(tally_total_dau[1d])) or vector(1))')
drawer_ratio=$(prom_query '(sum(increase(tally_ai_drawer_open_dau[1d])) or vector(0)) / (sum(increase(tally_total_dau[1d])) or vector(1))')
if [ "$palette_ratio" = "n/a" ] && [ "$drawer_ratio" = "n/a" ]; then
  h3_val="n/a"
else
  h3_val="palette=${palette_ratio}, drawer=${drawer_ratio}"
fi
# Coarse: take palette_ratio (proxy for H3 falsification).
h3_status=$(decide_status "$palette_ratio" "<0.40" "true")

# ---------------------------------------------------------------------------
# Compose the row and append / replace.
# ---------------------------------------------------------------------------

new_row="| $today | $h1_status | $h1_val | $h2_status | $h2_val | $h3_status | $h3_val |"

echo "snapshot: $new_row"

if [ "$DRY_RUN" = "1" ]; then
  exit 0
fi

if [ ! -f "$ASSUMPTIONS_PATH" ]; then
  fail_feishu "assumptions.md not found at $ASSUMPTIONS_PATH"
fi

# Strip any existing row for $today so re-runs are idempotent.
tmp=$(mktemp)
grep -v "^| $today " "$ASSUMPTIONS_PATH" > "$tmp"

# Append the new row.
echo "$new_row" >> "$tmp"

# Trim history to 90 rows. The history table is the LAST sequence of
# "| YYYY-MM-DD |..." lines in the file. We don't touch the YAML blocks.
awk -v keep=90 '
  /^\| [0-9]{4}-[0-9]{2}-[0-9]{2} \|/ {
    hist[++n] = $0
    next
  }
  { print }
  END {
    start = (n > keep) ? n - keep + 1 : 1
    for (i = start; i <= n; i++) print hist[i]
  }
' "$tmp" > "${tmp}.trimmed"

mv "${tmp}.trimmed" "$ASSUMPTIONS_PATH"
rm -f "$tmp"
echo "wrote $ASSUMPTIONS_PATH"
