/**
 * API wrapper for the Shopify shop-binding endpoints.
 * Follows the same apiFetch pattern as suppliers.ts.
 */
import { apiFetch } from "./client"

export interface ShopItem {
  id: string
  shop_domain: string
  warehouse_id: string
  creator_id: string
}

export interface ShopListResult {
  items: ShopItem[]
}

export interface BindShopInput {
  shop_domain: string
  warehouse_id: string
}

export async function listShops(signal?: AbortSignal): Promise<ShopListResult> {
  return apiFetch<ShopListResult>("/shopify/shops", { signal, retry: 2 })
}

export async function bindShop(input: BindShopInput): Promise<ShopItem> {
  return apiFetch<ShopItem>("/shopify/shops", {
    method: "POST",
    body: JSON.stringify(input),
  })
}

export async function unbindShop(id: string): Promise<void> {
  await apiFetch<void>(`/shopify/shops/${id}`, { method: "DELETE" })
}
