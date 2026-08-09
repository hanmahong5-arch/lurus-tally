import { AccountCenter } from "./account-center"

/**
 * /account — Tier 3 of the account-center progression. Multi-tab vertical
 * layout: profile / subscription / wallet / security / audit / api-keys /
 * team / notifications. Tab is preserved across reload via ?tab= query.
 *
 * Phase 1 ships 4 working tabs (profile / subscription / wallet / api-keys);
 * the other 4 render an "即将上线" placeholder so the nav contract is stable
 * before Phase 3 backend lands.
 */
export default function AccountPage() {
  // The security tab links out to the SSO console for password / 2FA. Derive
  // the host from the SAME OIDC_ISSUER the sign-in flow uses (deploy/k8s/base/
  // configmap-web.yaml, patched to test-auth.lurus.cn by the stage overlay)
  // rather than hardcoding a host: a literal sent STAGE users to the PROD
  // console, and the literal in use was `auth.lurus.cn` — a 301 alias being
  // torn down 2026-08-19, after which the button would have led nowhere.
  // Server component, so the non-public env var is readable here and handed
  // to the client component as a prop.
  return <AccountCenter ssoConsoleURL={process.env.OIDC_ISSUER || "https://identity.lurus.cn"} />
}
