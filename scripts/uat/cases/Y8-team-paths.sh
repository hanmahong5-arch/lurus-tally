#!/usr/bin/env bash
# Y8 - round-12 team and shared-till paths, LOCAL dev mode only (not STAGE).
#
#   1. Y8-team-paths.py  - own-shop invite link (i=), no cashier in a nursery shop
#                          (needs a server with TALLY_DEV_MODE=true)
#   2. vitest            - other tabs jump to /login?switch=1 after a sign-out;
#                          a failed sign-out puts the cart and refund reminders back
#
#   UAT_DEV_BASE=http://localhost:18200 RUN_ID=<id> bash scripts/uat/cases/Y8-team-paths.sh
#   UAT_SKIP_FRONTEND=1 skips step 2. Step 2 only runs bunx vitest (no build, no install).
set -u
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEB="$(cd "$DIR/../../../web" && pwd)"
export RUN_ID="${RUN_ID:-local-$(date +%s)}"
export UAT_DEV_BASE="${UAT_DEV_BASE:-http://localhost:18200}"
rc=0

PY="${PYTHON:-python3}"
command -v "$PY" >/dev/null 2>&1 || PY=python
"$PY" -I "$DIR/Y8-team-paths.py" || rc=1

if [ "${UAT_SKIP_FRONTEND:-}" != "1" ]; then
  echo "== frontend: sign-out broadcast, cart restore and team tab =="
  (cd "$WEB" && bunx vitest run --maxWorkers=2 \
    hooks/use-signout-broadcast.test.ts \
    components/account/switch-user.test.ts \
    components/pos/cart-storage.test.ts \
    "app/(dashboard)/account/team-tab.test.tsx") || rc=1
fi

[ "$rc" -eq 0 ] && echo "[Y8] all green" || echo "[Y8] FAILED" >&2
exit "$rc"
