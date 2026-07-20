#!/usr/bin/env bash
# J4 — Billing contract on STAGE (deployed commit da399443).
#
# Two auth modes are exercised:
#   (A) Pure PAT  — the harness's real auth. The PAT path injects tenant_id but
#       NO idp_subject (auth.go:96-112). billing/handler.go callerSub returns ""
#       → every billing endpoint 401 "sign-in required". This is the PROVEN-INTENDED
#       contract (handler.go:67-72 Subscribe, :100-104 Overview), same root cause as /me.
#   (B) PAT + X-IDP-Subject testability hook — the deployed handler honours the
#       X-IDP-Subject header unconditionally (callerSub fallback, billing/handler.go:108-116;
#       comment: "honoured so the integration is testable without a real OIDC flow").
#       This lets us reach the body-validation + platform paths to cover the
#       invalid-plan 4xx and the platform-error contract.
#
# Contract sources (all `git show da399443:<path>`):
#   internal/adapter/handler/billing/handler.go:62-95   Subscribe: ""sub→401; bad body→400; else platform
#   internal/adapter/handler/billing/handler.go:97-106  Overview:  ""sub→401; else platform
#   internal/adapter/handler/billing/handler.go:118-... writePlatformError mapping:
#       insufficient_balance→402, not_found→404, invalid_parameter→400,
#       unauthorized→502 platform_auth_failed, unavailable→502 platform_unavailable.
#
# OBSERVED ON STAGE (author dry-run): with the hook + a syntactic-but-unmapped sub,
# platform rejects Tally's internal credential → 502 platform_auth_failed for BOTH
# overview and a valid-plan subscribe. The happy-path checkout URL is therefore
# UNREACHABLE on STAGE without a real platform-mapped IdP subject for a UAT tenant —
# a resource we do not have. Per anti-shortcut policy we DO NOT fabricate a checkout
# URL; we assert the documented platform-error contract and flag the gap in the report.
set -u
CASE_ID=J4
# shellcheck disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"

use_primary

# A syntactic sub that is intentionally NOT mapped to any platform account.
BOGUS_SUB="uat-${RUN_ID}-unmapped-sub"

# Documented non-success platform-error statuses the handler can emit.
PLATFORM_ERR_SET="400 402 404 502"

# === Mode A: pure PAT — proven-intended 401 contract ===========================
http overview-pat GET /api/v1/billing/overview
expect_status 401
check "overview (PAT) 401 sign-in required" \
  jq -e '.error == "unauthorized" and (.detail | test("sign-in required"))' "$HTTP_BODY_FILE"

# Subscribe under pure PAT: the sub gate precedes body binding, so even a valid
# body yields 401 (handler.go:67-72).
http subscribe-pat POST /api/v1/billing/subscribe \
  -H 'Content-Type: application/json' \
  -d '{"plan_code":"pro","billing_cycle":"monthly","payment_method":"alipay"}'
expect_status 401
check "subscribe (PAT) 401 sign-in required" \
  jq -e '.error == "unauthorized"' "$HTTP_BODY_FILE"

# === Mode B: PAT + X-IDP-Subject testability hook ==============================

# B1: invalid subscribe body (missing plan_code) → 400 bad_request.
#     This is the spec's "POST /billing/subscribe with an invalid plan → 4xx".
http subscribe-invalid POST /api/v1/billing/subscribe \
  -H 'Content-Type: application/json' \
  -H "X-IDP-Subject: $BOGUS_SUB" \
  -d '{"billing_cycle":"monthly"}'
expect_status 400
check "subscribe invalid body → bad_request (plan_code required)" \
  jq -e '.error == "bad_request"' "$HTTP_BODY_FILE"

# B2: overview reaches platform; unmapped/credential-rejected → documented error.
http overview-hook GET /api/v1/billing/overview \
  -H "X-IDP-Subject: $BOGUS_SUB"
expect_status_in "$PLATFORM_ERR_SET"
check "overview platform error is a stable coded error (not a leaked 500/stack)" \
  jq -e '.error | type == "string" and (test("internal_error") | not)' "$HTTP_BODY_FILE"

# B3: valid-plan subscribe reaches platform. Spec wants a checkout/redirect URL;
#     on STAGE platform rejects Tally's internal auth → 502 platform_auth_failed,
#     so NO checkout URL is issued. We assert the documented platform-error
#     contract and explicitly verify NO pay_url leaked. DO NOT follow any URL.
http subscribe-valid POST /api/v1/billing/subscribe \
  -H 'Content-Type: application/json' \
  -H "X-IDP-Subject: $BOGUS_SUB" \
  -d '{"plan_code":"pro","billing_cycle":"monthly","payment_method":"alipay","return_url":"https://tally-stage.lurus.cn/uat"}'
if [ "$HTTP_STATUS" = "200" ]; then
  # Happy path (only reachable with a real platform-mapped sub): assert the
  # response carries a checkout/redirect URL — and STOP. Never fetch it.
  check "subscribe 200 carries a non-empty pay_url string (NOT followed)" \
    jq -e '(.pay_url // "") | type == "string" and (startswith("http"))' "$HTTP_BODY_FILE"
else
  # STAGE reality: platform credential rejected → documented platform error.
  expect_status_in "$PLATFORM_ERR_SET"
  check "subscribe valid-plan platform error is stable-coded (no pay_url leaked)" \
    jq -e '(.error | type == "string") and (has("pay_url") | not)' "$HTTP_BODY_FILE"
fi

finish
