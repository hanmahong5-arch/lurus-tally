#!/usr/bin/env bash
# lib.sh — shared harness for STAGE UAT case scripts. Source me, don't run me.
#
# Contract for case scripts:
#   RUN_ID=<id> CASE_ID=<J1> source lib.sh   (CASE_ID defaults to script name)
#   http <name> <method> <path> [curl args…]  → evidence JSON + body on disk,
#       sets HTTP_STATUS / HTTP_BODY_FILE / HTTP_HEADERS_FILE / EVIDENCE_FILE
#   check <description> <shell test…>         → records pass/fail assertion
#   finish                                    → writes case result JSON, exits
#       non-zero if any assertion failed
#
# Safety rails (HARD, not advisory):
#   * uat_gate runs at source time: the PAT must resolve to a tenant whose PAT
#     list contains the seeded uat-* token name. Wrong tenant → refuse to run.
#   * ai_call_guard: at most $UAT_AI_CALL_LIMIT (default 3) LLM-touching calls
#     per RUN_ID across ALL cases (file-based counter).
#   * Never completes a billing payment; never mutates k8s; raw SQL only in
#     bootstrap-stage.sh.
set -u

UAT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$UAT_DIR/../.." && pwd)"
ENV_FILE="$UAT_DIR/.uat.env"

[ -f "$ENV_FILE" ] || { echo "FATAL: $ENV_FILE missing — run bootstrap-stage.sh first" >&2; exit 2; }
# shellcheck disable=SC1090
source "$ENV_FILE"

: "${UAT_BASE:?set in .uat.env}"
: "${UAT_PAT_PRIMARY:?set in .uat.env}"
: "${UAT_PAT_SECONDARY:?set in .uat.env}"
: "${UAT_TENANT_PRIMARY:?set in .uat.env}"
: "${UAT_TENANT_SECONDARY:?set in .uat.env}"
: "${RUN_ID:?caller must export RUN_ID}"

CASE_ID="${CASE_ID:-$(basename "${0:-case}" .sh)}"
EVID_DIR="$REPO_ROOT/_uat-reports/evidence/$RUN_ID/$CASE_ID"
mkdir -p "$EVID_DIR"

UAT_AI_CALL_LIMIT="${UAT_AI_CALL_LIMIT:-3}"
AI_COUNTER_FILE="$REPO_ROOT/_uat-reports/evidence/$RUN_ID/.ai_calls"

_SEQ=0
_PASS=0
_FAIL=0
_FAILED_CHECKS=()

# AUTH defaults to the primary tenant PAT; cases flip with use_secondary/use_primary/use_none.
_AUTH_HEADER="Authorization: Bearer $UAT_PAT_PRIMARY"
use_primary()   { _AUTH_HEADER="Authorization: Bearer $UAT_PAT_PRIMARY"; }
use_secondary() { _AUTH_HEADER="Authorization: Bearer $UAT_PAT_SECONDARY"; }
use_none()      { _AUTH_HEADER=""; }
use_token()     { _AUTH_HEADER="Authorization: Bearer $1"; } # e.g. forged PAT

# http NAME METHOD PATH [extra curl args…]
# PATH may be absolute (/internal/…) or /api/v1/… — passed through verbatim.
http() {
  local name="$1" method="$2" path="$3"; shift 3
  _SEQ=$((_SEQ + 1))
  local nn; nn=$(printf '%02d' "$_SEQ")
  local base="$EVID_DIR/$nn-$name"
  HTTP_BODY_FILE="$base.body"
  HTTP_HEADERS_FILE="$base.headers"
  EVIDENCE_FILE="$base.json"

  local -a args=(-sS -o "$HTTP_BODY_FILE" -D "$HTTP_HEADERS_FILE" -w '%{http_code}' -X "$method")
  [ -n "$_AUTH_HEADER" ] && args+=(-H "$_AUTH_HEADER")
  args+=("$@" "$UAT_BASE$path")

  HTTP_STATUS=$(curl "${args[@]}" 2>"$base.curlerr" || echo "000")
  [ -s "$base.curlerr" ] || rm -f "$base.curlerr"

  # Evidence JSON — never embeds the bearer token.
  jq -n \
    --arg case "$CASE_ID" --arg run "$RUN_ID" --arg name "$name" \
    --arg method "$method" --arg url "$UAT_BASE$path" --arg status "$HTTP_STATUS" \
    --arg auth "$(echo "$_AUTH_HEADER" | sed -E 's/(tally_pat_.{8}).*/\1***REDACTED***/')" \
    --rawfile headers "$HTTP_HEADERS_FILE" \
    --arg body_file "$(basename "$HTTP_BODY_FILE")" \
    --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '{run_id:$run, case:$case, seq:$name, method:$method, url:$url,
      auth:$auth, status:($status|tonumber? // $status), ts:$ts,
      response_headers:$headers, body_file:$body_file}' >"$EVIDENCE_FILE"
  echo "  [$CASE_ID/$nn] $method $path -> $HTTP_STATUS"
}

# body_json — convenience jq over the last response body.
body_json() { jq "$@" "$HTTP_BODY_FILE"; }

# check DESCRIPTION CMD… — run CMD; record pass/fail. The last http() evidence
# file path is attached so the supervisor can re-open the raw proof.
check() {
  local desc="$1"; shift
  if "$@"; then
    _PASS=$((_PASS + 1))
    echo "    PASS: $desc"
  else
    _FAIL=$((_FAIL + 1))
    _FAILED_CHECKS+=("$desc (evidence: ${EVIDENCE_FILE:-none})")
    echo "    FAIL: $desc (evidence: ${EVIDENCE_FILE:-none})" >&2
  fi
}

# expect_status N — assert last http() returned exactly N.
expect_status() { check "status == $1 (got $HTTP_STATUS)" [ "$HTTP_STATUS" = "$1" ]; }
# expect_status_in "401 403" — any of the listed codes passes.
expect_status_in() {
  local want="$1" hit=1 s
  for s in $want; do [ "$HTTP_STATUS" = "$s" ] && hit=0; done
  check "status in [$want] (got $HTTP_STATUS)" [ "$hit" = "0" ]
}

# ai_call_guard — call IMMEDIATELY BEFORE any request that reaches the LLM
# (POST /ai/chat). Aborts the whole case when the run-wide budget is spent.
ai_call_guard() {
  local n=0
  [ -f "$AI_COUNTER_FILE" ] && n=$(cat "$AI_COUNTER_FILE")
  if [ "$n" -ge "$UAT_AI_CALL_LIMIT" ]; then
    echo "FATAL: AI call budget exhausted ($n/$UAT_AI_CALL_LIMIT) — refusing LLM call" >&2
    finish
  fi
  echo $((n + 1)) >"$AI_COUNTER_FILE"
  echo "  [ai_call_guard] LLM call $((n + 1))/$UAT_AI_CALL_LIMIT"
}

# finish — write the case summary and exit (0 only if every check passed).
finish() {
  local failed_json="[]"
  if [ "${#_FAILED_CHECKS[@]}" -gt 0 ]; then
    failed_json=$(printf '%s\n' "${_FAILED_CHECKS[@]}" | jq -R . | jq -s .)
  fi
  jq -n --arg case "$CASE_ID" --arg run "$RUN_ID" \
    --argjson pass "$_PASS" --argjson fail "$_FAIL" --argjson failed "$failed_json" \
    --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '{run_id:$run, case:$case, pass:$pass, fail:$fail, failed_checks:$failed, finished_at:$ts}' \
    >"$EVID_DIR/result.json"
  echo "[$CASE_ID] done: $_PASS pass / $_FAIL fail (evidence: $EVID_DIR)"
  [ "$_FAIL" -eq 0 ] && exit 0 || exit 1
}

# ---------------------------------------------------------------------------
# Tenant safety gate — runs at source time. GET /me is NOT usable here: the
# deployed auth middleware injects no `sub` on the PAT path (auth.go:67) so
# /me always 401s under a PAT. Instead we list the tenant's PATs (tenant-scoped
# read) and require the seeded uat-* token name to be present. A PAT pointing
# at any non-UAT tenant cannot satisfy this.
# ---------------------------------------------------------------------------
_gate_one() {
  local label="$1" pat="$2" want_name="$3"
  local body status
  body=$(mktemp)
  status=$(curl -sS -o "$body" -w '%{http_code}' \
    -H "Authorization: Bearer $pat" "$UAT_BASE/api/v1/auth/pats" || echo 000)
  if [ "$status" != "200" ] || ! jq -e --arg n "$want_name" \
      '(.items // .) | map(.name) | index($n) != null' "$body" >/dev/null 2>&1; then
    echo "FATAL: safety gate failed for $label (status=$status) — PAT does not" \
         "resolve to a seeded UAT tenant. Refusing to run." >&2
    rm -f "$body"; exit 3
  fi
  rm -f "$body"
}
if [ "${UAT_SKIP_GATE:-}" != "1" ]; then
  _gate_one primary "$UAT_PAT_PRIMARY" "uat-primary"
  _gate_one secondary "$UAT_PAT_SECONDARY" "uat-secondary"
fi
