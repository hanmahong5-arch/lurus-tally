#!/usr/bin/env python3
"""Y8 - team paths of round 12 on a LOCAL dev-mode server (header auth).

Covers:
  a) an invite link carries the invite id (`i=`); opening it as the shop's own
     owner answers 409 already_member_here; a person without a shop joins fine
  b) a nursery (horticulture) shop cannot invite or promote a cashier
     (422 role_not_for_shop); a retail shop can

Needs TALLY_DEV_MODE=true on the server. Never run against STAGE/PROD: it
creates shops through the dev header identities.
  UAT_DEV_BASE=http://localhost:18200 RUN_ID=<id> python Y8-team-paths.py
Chinese request bodies go through urllib on purpose (no shell quoting issues).
"""
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

BASE = os.environ.get("UAT_DEV_BASE", "http://localhost:18200").rstrip("/")
RUN = os.environ.get("RUN_ID") or "local-%d" % int(time.time())

passed = 0
failed = []


def check(desc, ok):
    global passed
    if ok:
        passed += 1
        print("    PASS: " + desc)
    else:
        failed.append(desc)
        print("    FAIL: " + desc, file=sys.stderr)


def call(sub, method, path, body=None):
    data = json.dumps(body, ensure_ascii=False).encode("utf-8") if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header("X-IDP-Subject", sub)
    req.add_header("X-Email", sub + "@uat.example")
    req.add_header("X-Display-Name", sub)
    if data is not None:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=20) as r:
            raw, code = r.read(), r.status
    except urllib.error.HTTPError as e:
        raw, code = e.read(), e.code
    try:
        js = json.loads(raw) if raw else {}
    except ValueError:
        js = {}
    print("  %s %s -> %s" % (method, path, code))
    return code, js


def open_shop(sub, profile):
    code, _ = call(sub, "POST", "/api/v1/tenant/profile", {"profile_type": profile})
    check("%s opens a %s shop (200)" % (sub, profile), code == 200)
    # The team page is what creates the shop's team space; the owner opens it first.
    code, js = call(sub, "GET", "/api/v1/team/members")
    check("%s can open the team page (200)" % sub, code == 200)
    return js


def invite(sub, role):
    return call(sub, "POST", "/api/v1/team/invites", {"role": role})


def link_parts(link):
    q = urllib.parse.parse_qs(urllib.parse.urlparse(link).query)
    return {k: v[0] for k, v in q.items()}


def main():
    pfx = "uat-y8-%s" % RUN
    owner_r, staff_r, owner_h, staff_h = (pfx + s for s in ("-owner-r", "-staff-r", "-owner-h", "-staff-h"))

    print("== a) invite link with i= ==")
    js = open_shop(owner_r, "retail")
    check("retail shop allows cashier (cashier_allowed true)", js.get("cashier_allowed") is True)

    code, js = invite(owner_r, "clerk")
    check("clerk invite created (201)", code == 201)
    parts = link_parts(js.get("link", ""))
    check("link has token", bool(parts.get("token")))
    check("link has i= equal to invite_id", parts.get("i") == str(js.get("invite_id")) and bool(parts.get("i")))

    code, js2 = call(owner_r, "POST", "/api/v1/team/join",
                     {"token": parts.get("token", ""), "invite_id": parts.get("i", "")})
    check("owner opening own-shop link -> 409 (got %s)" % code, code == 409)
    check("error code already_member_here", js2.get("error") == "already_member_here")
    check("message says it is the person's own shop link", "自己店铺的邀请链接" in js2.get("message", ""))

    # A cashier opening the shop's own link gets the same answer.
    code, jc = invite(owner_r, "cashier")
    check("cashier invite created (201, got %s)" % code, code == 201)
    cparts = link_parts(jc.get("link", ""))
    code, _ = call(owner_r + "-cash", "POST", "/api/v1/team/join",
                   {"token": cparts.get("token", ""), "invite_id": cparts.get("i", "")})
    check("cashier invite joins in a retail shop (200, got %s)" % code, code == 200)
    code, jc2 = invite(owner_r, "clerk")
    c2 = link_parts(jc2.get("link", ""))
    code, jj = call(owner_r + "-cash", "POST", "/api/v1/team/join",
                    {"token": c2.get("token", ""), "invite_id": c2.get("i", "")})
    check("cashier opening the own-shop link again -> 409 (got %s)" % code, code == 409)
    check("cashier error code already_member_here", jj.get("error") == "already_member_here")
    check("cashier message says it is the own shop link", "自己店铺的邀请链接" in jj.get("message", ""))

    # The same link still works for a person without a shop (the owner's attempt used nothing up).
    code, js3 = call(staff_r, "POST", "/api/v1/team/join",
                     {"token": parts.get("token", ""), "invite_id": parts.get("i", "")})
    check("a person without a shop joins with that link (200, got %s)" % code, code == 200)
    check("joined as clerk", js3.get("role") == "clerk" or js3.get("role_label") == "店员")

    print("== b) retail shop may have a cashier ==")
    code, js = invite(owner_r, "cashier")
    check("cashier invite in a retail shop (201)", code == 201)

    print("== c) nursery shop has no cashier ==")
    js = open_shop(owner_h, "horticulture")
    check("nursery shop: cashier_allowed false", js.get("cashier_allowed") is False)
    code, js = invite(owner_h, "cashier")
    check("cashier invite in a nursery shop -> 422 (got %s)" % code, code == 422)
    check("error code role_not_for_shop", js.get("error") == "role_not_for_shop")
    check("message tells to pick clerk", "请选店员" in js.get("message", ""))

    code, js = invite(owner_h, "clerk")
    check("clerk invite in a nursery shop (201)", code == 201)
    parts = link_parts(js.get("link", ""))
    code, _ = call(staff_h, "POST", "/api/v1/team/join", {"token": parts.get("token", ""), "invite_id": parts.get("i", "")})
    check("nursery clerk joins (200)", code == 200)

    code, js = call(owner_h, "GET", "/api/v1/team/members")
    staff = [m for m in js.get("items", []) if m.get("is_self") is False]
    check("nursery shop lists the clerk", len(staff) == 1)
    if staff:
        acct = staff[0].get("account_id")
        code, js = call(owner_h, "PATCH", "/api/v1/team/members/%s" % acct, {"role": "cashier"})
        check("promoting the clerk to cashier in a nursery shop -> 422 (got %s)" % code, code == 422)
        check("PATCH error code role_not_for_shop", js.get("error") == "role_not_for_shop")

    print("[Y8-team-paths] done: %d pass / %d fail" % (passed, len(failed)))
    for f in failed:
        print("  - " + f, file=sys.stderr)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
