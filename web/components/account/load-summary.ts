import { apiFetch } from "@/lib/api/client"
import { fetchAccountSummary, type AccountIdentity, type AccountSummary } from "@/lib/api/account"

/**
 * loadAccountSummary reads the identity and, only for someone who may manage
 * billing, the plan and wallet. Everyone else never asks the billing service,
 * which would refuse them.
 */
export async function loadAccountSummary(withBilling: boolean): Promise<AccountSummary> {
  if (withBilling) return fetchAccountSummary()
  const identity = await apiFetch<AccountIdentity>("/me", { cache: "no-store", silent: true })
  return { identity, billing: null, billingError: null }
}
