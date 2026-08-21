#!/usr/bin/env bash
# S0.C3 — Monday 09:00 Beijing health post to Feishu.
#
# Reports the 5 anti-metrics that gate Sprint exit (roadmap-v1.5
# "Anti-metric 升级" section). Each metric source is independently
# defensive: missing → "n/a". Single Feishu message; failures fall back
# to OPS_ALERT_FEISHU.
#
# Required env:
#   HEALTH_REPORT_FEISHU_URL   Primary Feishu webhook.
# Optional:
#   PROM_URL                   Prometheus base URL.
#   GH_OWNER, GH_REPO          GitHub owner/repo for issue counts (default
#                              hanmahong5-arch/lurus-tally).
#   GH_TOKEN                   Token for gh API (or use gh CLI logged in).
#   OPS_ALERT_FEISHU           Fallback webhook on send failure.
#   ASSUMPTIONS_PATH           Path to assumptions.md.
#   DRY_RUN=1                  Print message, do not send.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PROM_URL="${PROM_URL:-}"
ASSUMPTIONS_PATH="${ASSUMPTIONS_PATH:-$REPO_ROOT/_bmad-output/planning-artifacts/assumptions.md}"
GH_OWNER="${GH_OWNER:-hanmahong5-arch}"
GH_REPO="${GH_REPO:-lurus-tally}"
DRY_RUN="${DRY_RUN:-0}"

iso_week="$(date -u +%G-W%V)"

# ---------------------------------------------------------------------------
# helpers
# ---------------------------------------------------------------------------

post_feishu() {
  local url="$1" payload="$2"
  curl -sS -X POST "$url" -H "Content-Type: application/json" -d "$payload" >/dev/null
}

ops_alert() {
  local msg="$1"
  echo "ERROR: $msg" >&2
  if [ -n "${OPS_ALERT_FEISHU:-}" ]; then
    post_feishu "$OPS_ALERT_FEISHU" \
      "{\"msg_type\":\"text\",\"content\":{\"text\":\"health-report.sh failed: $msg\"}}" || true
  fi
  exit 1
}

trap 'ops_alert "unexpected exit on line $LINENO"' ERR

gh_issue_count() {
  local label="$1" state="$2" since_iso="${3:-}"
  local query="repo:$GH_OWNER/$GH_REPO label:$label state:$state"
  if [ -n "$since_iso" ]; then
    query="$query closed:>=$since_iso"
  fi
  if ! command -v gh >/dev/null; then
    echo "n/a"
    return
  fi
  # A label that was never defined makes search/issues return a perfectly
  # healthy `total_count: 0` — indistinguishable from "the label exists and
  # nothing matched". Both `feature` and `regression` were in fact undefined
  # on this repo (18 issues, neither label), so two of the five anti-metrics
  # had been reporting a fabricated 0 since the report was written. Probe the
  # label first and degrade to n/a, per this script's "missing → n/a" rule.
  if ! gh api "repos/$GH_OWNER/$GH_REPO/labels/$label" >/dev/null 2>&1; then
    echo "n/a(no '$label' label)"
    return
  fi
  gh api -X GET search/issues --field q="$query" --jq '.total_count' 2>/dev/null || echo "n/a"
}

prom_scalar() {
  local q="$1"
  if [ -z "$PROM_URL" ]; then
    echo "n/a"
    return
  fi
  if ! command -v jq >/dev/null; then
    echo "n/a"
    return
  fi
  curl -sS --max-time 10 -G --data-urlencode "query=$q" "$PROM_URL/api/v1/query" 2>/dev/null \
    | jq -r '.data.result[0].value[1] // "n/a"'
}

assumption_evidence_gap_days() {
  # Days since the last row that actually CONCLUDED something (truthy or
  # falsified), not since the last row of any kind.
  #
  # Counting every row measured "did the snapshot bot run yesterday" rather
  # than "when did we last learn something". bin/assumption-snapshot.sh
  # appends a row daily, so the gap sat pinned at 0d — permanently "≤ 7d,
  # on target" — while 100% of the 90-day retained history read
  # `inconclusive | n/a` for all five hypotheses. That is a self-validating
  # loop: the act of measuring produced the evidence of health. This file's
  # own scoring table says a hypothesis left inconclusive is 视同 falsified,
  # so an all-inconclusive history is the opposite of healthy and must not
  # be reported as 0d.
  if [ ! -f "$ASSUMPTIONS_PATH" ]; then
    echo "n/a"
    return
  fi
  local last_date rows
  rows=$(grep -cE '^\| [0-9]{4}-[0-9]{2}-[0-9]{2} \|' "$ASSUMPTIONS_PATH" || echo 0)
  last_date=$(grep -E '^\| [0-9]{4}-[0-9]{2}-[0-9]{2} \|' "$ASSUMPTIONS_PATH" \
    | grep -E '\| *(truthy|falsified) *\|' | tail -n 1 | awk '{print $2}' || echo "")
  if [ -z "$last_date" ]; then
    echo "🔴 NEVER (${rows} rows, 0 conclusive)"
    return
  fi
  local last_ts now_ts
  last_ts=$(date -u -d "$last_date" +%s 2>/dev/null || echo "")
  now_ts=$(date -u +%s)
  if [ -z "$last_ts" ]; then
    echo "n/a"
    return
  fi
  echo "$(( (now_ts - last_ts) / 86400 ))d"
}

# True only for a bare number — every "unavailable" marker this script emits
# ("n/a", "n/a(no 'feature' label)", "🔴 NEVER …") must fail this test so it
# can never be silently arithmetic'd into a derived metric.
is_num() {
  case "$1" in
    ''|*[!0-9.]*) return 1 ;;
    *) return 0 ;;
  esac
}

# ---------------------------------------------------------------------------
# Gather metrics. Tolerate gh / prom absence — n/a is itself signal.
# ---------------------------------------------------------------------------

last_monday=$(date -u -d 'last monday' +%Y-%m-%d 2>/dev/null || echo "1970-01-01")

features_shipped=$(gh_issue_count "feature" "closed" "$last_monday")
bugs_open=$(gh_issue_count "regression" "open" "")
bugs_closed_week=$(gh_issue_count "regression" "closed" "$last_monday")
bugs_delta="${bugs_open:-n/a}/${bugs_closed_week:-n/a}"

gap_days=$(assumption_evidence_gap_days)

lint_warnings=$(prom_scalar 'sum(tally_lint_warnings_total)')
wad_weekly=$(prom_scalar 'sum(increase(tally_plan_accept_total[7d]))')

ratio="n/a"
if is_num "$features_shipped" && is_num "$wad_weekly" \
   && [ "$features_shipped" != "0" ] && [ "$wad_weekly" != "0" ]; then
  ratio=$(awk -v f="$features_shipped" -v w="$wad_weekly" 'BEGIN { printf "%.3f", f/w }')
fi

# ---------------------------------------------------------------------------
# Compose Feishu markdown payload.
# ---------------------------------------------------------------------------

text=$(cat <<EOM
📊 Tally V1.5 Health · Week $iso_week

• features_shipped (last 7d): $features_shipped (target ≤ 3/月 ≈ 0.7/周)
• bugs_open / closed-7d: $bugs_delta (target open ≤ 5)
• assumption_evidence_gap: ${gap_days} (target ≤ 7d, 只认 truthy/falsified)
• lint_warnings_total: $lint_warnings (单调递减目标)
• features_shipped / WAD_weekly: $ratio (趋势必降)

红线：任一连 2 周违 → @founder retrospective
EOM
)

# Prefer jq for proper JSON escaping. Fall back to a hand-rolled payload
# only when text has no embedded quotes / backslashes / newlines that would
# need escaping (the template above is safe).
if command -v jq >/dev/null; then
  payload=$(jq -n --arg t "$text" '{msg_type:"text", content:{text:$t}}')
else
  escaped=$(printf '%s' "$text" | awk 'BEGIN{ORS="\\n"} {gsub(/"/,"\\\""); print}')
  payload="{\"msg_type\":\"text\",\"content\":{\"text\":\"$escaped\"}}"
fi

echo "----- health report -----"
echo "$text"
echo "-------------------------"

# The report must have a destination that does not depend on the Feishu
# channel being configured, or "webhook unset" silently means "no report".
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  {
    printf '## Tally V1.5 Health · Week %s\n\n' "$iso_week"
    printf '```\n%s\n```\n' "$text"
  } >> "$GITHUB_STEP_SUMMARY"
fi

if [ "$DRY_RUN" = "1" ]; then
  echo "(DRY_RUN=1, not sending)"
  exit 0
fi

if [ -z "${HEALTH_REPORT_FEISHU_URL:-}" ]; then
  # An unconfigured channel is an infra gap, not a health-report failure.
  # Failing here made "the webhook secret was never set" indistinguishable
  # from "a metric breached its red line": the scheduled run had been red
  # for weeks and the red carried no information. The report itself is on
  # stdout and in the job summary above, so nothing is lost by exiting 0 —
  # and the warning keeps the gap visible in the run's annotations.
  # Set REQUIRE_CHANNEL=1 (once the secret exists) to restore hard-fail so a
  # later *removal* of the secret does become a real failure again.
  echo "::warning title=Health report not delivered::HEALTH_REPORT_FEISHU_URL is unset — the report was generated (see job summary) but not sent. Owner-gated infra task, not a code regression."
  if [ "${REQUIRE_CHANNEL:-0}" = "1" ]; then
    ops_alert "HEALTH_REPORT_FEISHU_URL not set while REQUIRE_CHANNEL=1"
  fi
  exit 0
fi

if ! post_feishu "$HEALTH_REPORT_FEISHU_URL" "$payload"; then
  ops_alert "primary Feishu webhook returned non-zero"
fi
echo "sent OK"
