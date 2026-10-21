"use client"

import { useEffect, useState } from "react"
import { getSupplier } from "@/lib/api/suppliers"
import { getCustomer } from "@/lib/api/customers"
import { getWarehouse } from "@/lib/api/warehouses"

interface PartyRef {
  kind: "supplier" | "customer"
  partnerId?: string
  partnerName?: string
  warehouseId?: string
  warehouseName?: string
  tenantId?: string
}

/**
 * useBillPartyNames resolves the counterparty and warehouse names for a bill
 * header. Server-provided names win; otherwise the ids are looked up. A failed
 * lookup leaves the name empty so the caller can say "已指定" instead of
 * showing a raw id.
 */
export function useBillPartyNames(ref: PartyRef): { partnerName: string; warehouseName: string } {
  const { kind, partnerId, warehouseId, tenantId } = ref
  const [looked, setLooked] = useState<{
    partnerId?: string
    warehouseId?: string
    partner?: string
    warehouse?: string
  }>({})
  const needPartner = Boolean(partnerId) && !ref.partnerName
  const needWarehouse = Boolean(warehouseId) && !ref.warehouseName

  useEffect(() => {
    let cancelled = false
    if (needPartner && partnerId) {
      const get = kind === "supplier" ? getSupplier : getCustomer
      get(partnerId, tenantId).then(
        (p) => !cancelled && setLooked((s) => ({ ...s, partnerId, partner: p.name })),
        () => undefined
      )
    }
    if (needWarehouse && warehouseId) {
      getWarehouse(warehouseId, tenantId).then(
        (w) => !cancelled && setLooked((s) => ({ ...s, warehouseId, warehouse: w.name })),
        () => undefined
      )
    }
    return () => {
      cancelled = true
    }
  }, [kind, partnerId, warehouseId, tenantId, needPartner, needWarehouse])

  return {
    // Looked-up names are keyed by id so a stale one never shows for a new bill.
    partnerName: ref.partnerName || (looked.partnerId === partnerId ? looked.partner : "") || "",
    warehouseName:
      ref.warehouseName || (looked.warehouseId === warehouseId ? looked.warehouse : "") || "",
  }
}
