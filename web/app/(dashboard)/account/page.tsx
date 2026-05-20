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
  return <AccountCenter />
}
