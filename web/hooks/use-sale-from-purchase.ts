"use client"

import { useEffect, useRef } from "react"
import { getPurchaseBill } from "@/lib/api/purchase"
import { getProduct } from "@/lib/api/products"
import { extractApiError } from "@/lib/api/errors"
import { linesFromPurchase, type SaleFromPurchase } from "@/lib/sale-from-purchase"

/** Loads an approved purchase into a new sale draft. A restored local draft wins. */
export function useSaleFromPurchase(
  fromId: string | null,
  tenantId: string | undefined,
  restored: boolean,
  onApply: (built: SaleFromPurchase) => void,
  onError: (message: string) => void
) {
  const restoredRef = useRef(restored)
  restoredRef.current = restored
  const applyRef = useRef(onApply)
  applyRef.current = onApply
  const errorRef = useRef(onError)
  errorRef.current = onError

  useEffect(() => {
    if (!fromId) return
    let cancelled = false
    getPurchaseBill(fromId, tenantId)
      .then(async (detail) => {
        if (cancelled || restoredRef.current) return
        if (detail.head.status !== 2 || detail.head.sub_type !== "采购") {
          errorRef.current("只有已审核的进货单才能开销售单")
          return
        }
        const names: Record<string, string> = {}
        await Promise.all(
          Array.from(new Set(detail.items.map((it) => it.product_id))).map((pid) =>
            getProduct(pid, tenantId).then(
              (p) => {
                names[pid] = p.name
              },
              () => {}
            )
          )
        )
        if (cancelled || restoredRef.current) return
        applyRef.current(linesFromPurchase(detail, names))
      })
      .catch((e) => {
        if (!cancelled) errorRef.current(extractApiError(e).message)
      })
    return () => {
      cancelled = true
    }
  }, [fromId, tenantId])
}
