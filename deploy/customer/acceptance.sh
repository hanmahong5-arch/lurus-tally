#!/usr/bin/env bash
# Lurus Tally — on-premise acceptance check (self-contained, no repo tooling).
#
# Turns INSTALL.md "Verification" from prose into an executable check with
# three-state semantics:
#   PASS    — probe ran and the expected condition held
#   FAIL    — probe ran and the condition did NOT hold
#   UNKNOWN — probe could not determine the condition (unreachable, missing
#             tooling, missing credentials). UNKNOWN NEVER counts as success:
#             recording "could not test" as "passed" is exactly how broken
#             deployments go unnoticed.
#
# Checks (each with the failure mode it exists to catch):
#   1. /internal/v1/tally/health returns 200 with the right body, and a bogus
#      path does NOT 200 (server actually routes, not a proxy answering 200
#      to everything).
#   2. /internal/v1/tally/ready returns 200 with status "ok" — "degraded"
#      (e.g. Redis unreachable) is a FAIL, not a PASS.
#   3. web /login is reachable and / does not 500.
#   4. the auth gate actually engages: an unauthenticated request to a
#      business API is rejected.
#   5. a real write -> read-back roundtrip, asserting the CONTENT comes back
#      (a 200 with no matching row is a failure, not a pass). Requires either
#      TALLY_DEV_MODE=true (read from --env-file) or TALLY_ACCEPT_BEARER set.
#   6. data survives container RE-CREATION of the whole stack
#      (`up -d --force-recreate`, including postgres). A mere `restart` keeps
#      the container filesystem, so data would "survive" even with no volume
#      mounted — recreation is the only probe that actually proves the
#      volume config is live. Only runs if checks 1 and 5 both PASS.
#   7. exactly the five expected services are running (catches a dependency
#      — e.g. redis — silently failing while the app containers stay up).
#
# Usage (from the repository root, after INSTALL.md "First-time setup"):
#   bash deploy/customer/acceptance.sh [backend_url] [web_url]
# Both URLs default to values derived from TALLY_BACKEND_PORT / TALLY_WEB_PORT
# in deploy/customer/.env (or http://localhost:<port> if that file is absent).
#
# Exit code: 0 = all PASS, otherwise = number of FAIL + UNKNOWN lines below.
# A timestamped report is written next to this script (acceptance-*.txt) so
# "when did this last pass" is answerable later.

set -u

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
COMPOSE_FILE="${COMPOSE_FILE:-$SCRIPT_DIR/docker-compose.customer.yml}"
ENV_FILE="${ENV_FILE:-$SCRIPT_DIR/.env}"
# Compose project name must match what `up` used. Without pinning it, the
# --force-recreate in check 6 would start a SECOND stack under the default
# name (port clash, stray volumes) — exactly what happened on 2026-09-05 when
# the install had used `-p`. COMPOSE_PROJECT_NAME is what compose itself
# honours; the default below is what compose derives when nothing is set.
PROJECT="${COMPOSE_PROJECT_NAME:-$(basename "$(dirname "$COMPOSE_FILE")")}"
REPORT="$SCRIPT_DIR/acceptance-$(date +%Y%m%d-%H%M%S).txt"

env_val() {
  # env_val <KEY> — last matching line in $ENV_FILE, empty if absent/missing.
  [ -f "$ENV_FILE" ] || return 0
  sed -n "s/^$1=//p" "$ENV_FILE" | tail -1
}

port_to_url() {
  # A port value that already contains ":" (e.g. 127.0.0.1:18200) is used
  # as-is with an http:// prefix; a bare port number becomes localhost:<n>.
  case "$1" in
    *:*) printf 'http://%s' "$1" ;;
    *)   printf 'http://localhost:%s' "$1" ;;
  esac
}

DEFAULT_BACKEND_PORT="$(env_val TALLY_BACKEND_PORT)"; DEFAULT_BACKEND_PORT="${DEFAULT_BACKEND_PORT:-18200}"
DEFAULT_WEB_PORT="$(env_val TALLY_WEB_PORT)"; DEFAULT_WEB_PORT="${DEFAULT_WEB_PORT:-3000}"
BACKEND_URL="${1:-$(port_to_url "$DEFAULT_BACKEND_PORT")}"
WEB_URL="${2:-$(port_to_url "$DEFAULT_WEB_PORT")}"

# --- three-state accounting -------------------------------------------------
fail=0; unknown=0
say()  { printf '%-52s %s\n' "$1" "$2" | tee -a "$REPORT"; }
PASS() { say "$1" "PASS"; }
FAIL() { say "$1" "FAIL  -- $2"; fail=$((fail+1)); }
UNK()  { say "$1" "UNKNOWN($2)"; unknown=$((unknown+1)); }

command -v curl >/dev/null 2>&1 || { echo "curl is required" >&2; exit 2; }

{
  echo "# tally acceptance run"
  echo "# date:    $(date -u +%FT%TZ)"
  echo "# backend: $BACKEND_URL"
  echo "# web:     $WEB_URL"
  echo "# project: $PROJECT"
} >> "$REPORT"

# http <method> <url> [extra curl args...] — body to $BODY_FILE, echoes status.
# Empty status (curl failed / unreachable / DNS error) is "", callers must
# treat as UNKNOWN, never compare it as if it were a code.
BODY_FILE="$(mktemp)"
trap 'rm -f "$BODY_FILE"' EXIT
http() {
  local method="$1" url="$2"; shift 2
  curl -sS -o "$BODY_FILE" -w '%{http_code}' -X "$method" \
       --connect-timeout 5 --max-time 30 "$url" "$@" 2>/dev/null
}

# --- 1. backend health + negative control -----------------------------------
code="$(http GET "$BACKEND_URL/internal/v1/tally/health")"
health_ok=0
if [ -z "$code" ] || [ "$code" = "000" ]; then
  UNK "1. backend /health reachable+200" "no HTTP response from $BACKEND_URL"
elif [ "$code" = "200" ] && grep -q '"service":"lurus-tally"' "$BODY_FILE" && grep -q '"status":"ok"' "$BODY_FILE"; then
  neg="$(http GET "$BACKEND_URL/internal/v1/tally/definitely-not-$$")"
  if [ -n "$neg" ] && [ "$neg" != "200" ] && [ "$neg" != "000" ]; then
    PASS "1. backend /health 200, correct body (bogus path: $neg)"
    health_ok=1
  else
    FAIL "1. backend /health" "bogus path returned '$neg' instead of 4xx — is a proxy answering for the server?"
  fi
else
  FAIL "1. backend /health" "HTTP $code or unexpected body"
fi

# --- 2. ready (no degraded dependency) ---------------------------------------
code="$(http GET "$BACKEND_URL/internal/v1/tally/ready")"
if [ -z "$code" ] || [ "$code" = "000" ]; then
  UNK "2. backend /ready 200, not degraded" "no HTTP response"
elif [ "$code" = "200" ] && grep -q 'degraded' "$BODY_FILE"; then
  FAIL "2. backend /ready" "200 but body reports degraded: $(cat "$BODY_FILE")"
elif [ "$code" = "200" ] && grep -q '"status":"ok"' "$BODY_FILE"; then
  PASS "2. backend /ready 200, status ok"
else
  FAIL "2. backend /ready" "HTTP $code or unexpected body"
fi

# --- 3. web reachable ---------------------------------------------------------
root_code="$(http GET "$WEB_URL/")"
login_code="$(http GET "$WEB_URL/login")"
# The session endpoint is the real probe: next-auth answers it 500 when the
# web container is misconfigured (UntrustedHost without AUTH_TRUST_HOST,
# InvalidEndpoints without OIDC_ISSUER/OIDC_CLIENT_ID) while / still 307s —
# to /setup, because the 500 body gets mistaken for a session. A status-only
# check passed that state on 2026-09-05; this one does not.
session_code="$(http GET "$WEB_URL/api/auth/session")"
root_redirect="$(curl -sS -o /dev/null -w '%{redirect_url}' --connect-timeout 5 --max-time 30 "$WEB_URL/" 2>/dev/null)"
if [ -z "$root_code" ] || [ "$root_code" = "000" ] || [ -z "$login_code" ] || [ "$login_code" = "000" ] || [ -z "$session_code" ] || [ "$session_code" = "000" ]; then
  UNK "3. web reachable (/, /login, /api/auth/session)" "no HTTP response from $WEB_URL"
elif [ "$session_code" != "200" ]; then
  FAIL "3. web auth configured" "/api/auth/session=$session_code (want 200): set AUTH_TRUST_HOST=true and OIDC_ISSUER/OIDC_CLIENT_ID, see INSTALL.md section 5"
elif [ "$login_code" = "200" ] && { [ "$root_code" = "200" ] || { case "$root_code" in 302|307|308) true ;; *) false ;; esac && case "$root_redirect" in *login*) true ;; *) false ;; esac; }; }; then
  PASS "3. web reachable (/=$root_code -> ${root_redirect:-no-redirect}, /login=$login_code, session=200)"
else
  FAIL "3. web reachable" "/=$root_code -> ${root_redirect:-none} /login=$login_code session=$session_code (anonymous / must land on /login)"
fi

# --- 4. auth gate engages (no credentials -> rejected) -----------------------
code="$(http GET "$BACKEND_URL/api/v1/units")"
if [ -z "$code" ] || [ "$code" = "000" ]; then
  UNK "4. auth gate rejects unauthenticated request" "no HTTP response"
elif [ "$code" = "401" ] || [ "$code" = "403" ]; then
  PASS "4. auth gate rejects unauthenticated request ($code)"
else
  FAIL "4. auth gate" "expected 401/403, got $code — auth is NOT engaging"
fi

# --- credentials for checks 5/6 -----------------------------------------------
DEV_MODE="$(env_val TALLY_DEV_MODE)"
AUTH_ARGS=""
AUTH_DESC=""
if [ "$DEV_MODE" = "true" ]; then
  AUTH_ARGS="-H X-IDP-Subject:acceptance -H X-Tenant-ID:0000acce-0000-4000-8000-000000000001"
  AUTH_DESC="TALLY_DEV_MODE trusted headers"
elif [ -n "${TALLY_ACCEPT_BEARER:-}" ]; then
  AUTH_ARGS="-H Authorization:Bearer $TALLY_ACCEPT_BEARER"
  AUTH_DESC="TALLY_ACCEPT_BEARER"
fi

# Kept short on purpose: this becomes the "code" column value, which the
# schema caps at varchar(20) — "acc-" (4) + up to 5 epoch digits + "-" (1) +
# up to 5 pid digits stays under that even with a large PID.
TOKEN="acc-$(($(date +%s) % 100000))-$(($$ % 100000))"
UNIT_ID=""
write_read_ok=0
if [ -z "$AUTH_ARGS" ]; then
  UNK "5. write->read roundtrip" "no credentials: set TALLY_DEV_MODE=true in $ENV_FILE or export TALLY_ACCEPT_BEARER"
else
  code="$(http POST "$BACKEND_URL/api/v1/units" \
          -H 'Content-Type: application/json' $AUTH_ARGS \
          -d "{\"code\":\"$TOKEN\",\"name\":\"acceptance $TOKEN\",\"unit_type\":\"count\"}")"
  if [ -z "$code" ] || [ "$code" = "000" ]; then
    UNK "5. write->read roundtrip" "POST got no HTTP response"
  elif [ "$code" != "201" ]; then
    FAIL "5. write->read roundtrip" "POST returned HTTP $code (auth: $AUTH_DESC)"
  elif ! grep -q '"id"' "$BODY_FILE"; then
    FAIL "5. write->read roundtrip" "POST 201 but body has no \"id\""
  else
    UNIT_ID="$(sed -n 's/.*"id":"\{0,1\}\([a-zA-Z0-9-]*\)"\{0,1\}.*/\1/p' "$BODY_FILE" | head -1)"
    code="$(http GET "$BACKEND_URL/api/v1/units" $AUTH_ARGS)"
    if [ "$code" = "200" ] && grep -q "$TOKEN" "$BODY_FILE"; then
      PASS "5. write->read roundtrip (content found, auth: $AUTH_DESC)"
      write_read_ok=1
    elif [ "$code" = "200" ]; then
      FAIL "5. write->read roundtrip" "GET 200 but result does not contain the written code"
    else
      FAIL "5. write->read roundtrip" "GET returned HTTP ${code:-<none>}"
    fi
  fi
fi

# --- 6. persistence across full-stack re-creation -----------------------------
if [ "$health_ok" -ne 1 ] || [ "$write_read_ok" -ne 1 ]; then
  UNK "6. persistence across re-create (all services)" "check 1 or 5 did not PASS; skipping to avoid a no-op recreate"
elif ! command -v docker >/dev/null 2>&1 || [ ! -f "$COMPOSE_FILE" ]; then
  UNK "6. persistence across re-create (all services)" "docker/compose file not available from here"
elif [ "$(docker compose -p "$PROJECT" -f "$COMPOSE_FILE" --env-file "$ENV_FILE" ps --status running -q 2>/dev/null | grep -c .)" -eq 0 ]; then
  # Refuse rather than recreate: a project with nothing running means the
  # name is wrong, and `up` here would start a second copy of the stack.
  UNK "6. persistence across re-create (all services)" "compose project '$PROJECT' has no running services — if you started the stack with -p NAME, run: COMPOSE_PROJECT_NAME=NAME bash deploy/customer/acceptance.sh"
else
  # env -u: values exported in this shell would override the --env-file
  # values during interpolation and silently re-key/re-point the stack on
  # recreation (compose shell-env precedence over --env-file).
  if ! env -u POSTGRES_PASSWORD -u POSTGRES_USER -u POSTGRES_DB -u AUTH_SECRET \
         -u TALLY_BACKEND_IMAGE -u TALLY_WEB_IMAGE -u TALLY_BACKEND_PORT \
         -u TALLY_WEB_PORT -u TALLY_WEB_PUBLIC_URL \
         docker compose -p "$PROJECT" -f "$COMPOSE_FILE" --env-file "$ENV_FILE" \
         up -d --force-recreate --pull never --no-build >/dev/null 2>&1; then
    UNK "6. persistence across re-create (all services)" "up --force-recreate command failed"
  else
    up=""
    for _ in $(seq 1 60); do
      sleep 2
      hc="$(http GET "$BACKEND_URL/internal/v1/tally/health")"
      rc="$(http GET "$BACKEND_URL/internal/v1/tally/ready")"
      [ "$hc" = "200" ] && [ "$rc" = "200" ] && { up=1; break; }
    done
    if [ -z "$up" ]; then
      FAIL "6. persistence across re-create (all services)" "health/ready did not both return 200 within 120s of re-creation"
    else
      code="$(http GET "$BACKEND_URL/api/v1/units" $AUTH_ARGS)"
      if [ "$code" = "200" ] && grep -q "$TOKEN" "$BODY_FILE"; then
        PASS "6. data survives full-stack re-creation"
      else
        FAIL "6. persistence across re-create" "written unit not found after re-creation (HTTP ${code:-<none>}) — volume config not live?"
      fi
    fi
  fi
fi

# --- 7. exactly the expected services are running -----------------------------
if ! command -v docker >/dev/null 2>&1 || [ ! -f "$COMPOSE_FILE" ]; then
  UNK "7. exactly 5 services running" "docker/compose file not available from here"
else
  running="$(docker compose -p "$PROJECT" -f "$COMPOSE_FILE" --env-file "$ENV_FILE" ps --status running --format '{{.Service}}' 2>/dev/null | sort | tr '\n' ' ')"
  if [ "$running" = "backend nats postgres redis web " ]; then
    PASS "7. exactly 5 services running"
  else
    FAIL "7. exactly 5 services running" "got: '${running:-<none>}'"
  fi
fi

# --- best-effort cleanup of the canary row (not a check) ----------------------
if [ -n "$UNIT_ID" ] && [ -n "$AUTH_ARGS" ]; then
  http DELETE "$BACKEND_URL/api/v1/units/$UNIT_ID" $AUTH_ARGS >/dev/null
  echo "# info: best-effort DELETE of canary unit $UNIT_ID" >> "$REPORT"
fi

# --- verdict -----------------------------------------------------------------
result=PASS
[ "$unknown" -gt 0 ] && result=UNKNOWN
[ "$fail" -gt 0 ] && result=FAIL
say "ACCEPTANCE" "$result"
echo "report: $REPORT"
exit $((fail + unknown))
