/**
 * API wrapper for the onboarding endpoints.
 *
 * POST /api/v1/onboarding/seed-demo  — seeds demo products + initial stock
 * POST /api/v1/onboarding/clear-demo — removes all demo-marked rows
 */
import { apiFetch } from "./client"

export type OnboardingPersona = "cross_border" | "retail" | "horticulture"

export interface SeedDemoResult {
  products_created: number
}

/**
 * seedDemo — seeds the demo catalogue for the given persona.
 * Requires a warehouse_id that already exists in the tenant (resolved by the
 * caller from the warehouse list).
 */
export async function seedDemo(
  persona: OnboardingPersona,
  warehouseId: string,
): Promise<SeedDemoResult> {
  return apiFetch<SeedDemoResult>("/onboarding/seed-demo", {
    method: "POST",
    body: JSON.stringify({ persona, warehouse_id: warehouseId }),
  })
}

/**
 * clearDemo — removes all demo-marked rows for the authenticated tenant.
 */
export async function clearDemo(): Promise<void> {
  await apiFetch<void>("/onboarding/clear-demo", { method: "POST" })
}
