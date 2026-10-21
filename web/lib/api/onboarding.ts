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

export interface ConvertedProduct {
  id: string
  code: string
  name: string
}

export interface ClearDemoResult {
  cleared_products: number
  /** Sample products an approved bill still uses; kept as real products. */
  converted_products: ConvertedProduct[]
  /** Follow-up for the owner; present when converted_products is not empty. */
  hint?: string
}

/**
 * clearDemo — clears the sample data for the authenticated tenant. Sample
 * products that approved bills still use are kept as real products and listed
 * in the result.
 */
export async function clearDemo(): Promise<ClearDemoResult> {
  return apiFetch<ClearDemoResult>("/onboarding/clear-demo", { method: "POST" })
}
