"use client"

import { useEffect, useMemo, useState } from "react"
import { getProduct } from "@/lib/api/products"

export interface PrintProduct {
  name: string
  spec: string
}

/** Shown in the toolbar when some product lookups failed and the sheet prints a placeholder name. */
export const PRODUCT_LOOKUP_WARNING = "有商品信息没加载出来，请刷新后再打印"

/**
 * Name and spec (e.g. 胸径3cm 冠幅1.2m) per product id; a failed lookup is simply absent from
 * `products` and flips `failed` once every lookup has settled.
 */
export function usePrintProducts(ids: string[], tenantId?: string): { products: Record<string, PrintProduct>; failed: boolean } {
  const key = useMemo(() => Array.from(new Set(ids.filter(Boolean))).sort().join(","), [ids])
  const [products, setProducts] = useState<Record<string, PrintProduct>>({})
  const [failedKey, setFailedKey] = useState("")

  useEffect(() => {
    if (!key) return
    const ctrl = new AbortController()
    Promise.all(
      key.split(",").map((id) =>
        getProduct(id, tenantId, ctrl.signal).then(
          (p) => [id, { name: p.name, spec: p.spec?.trim() ?? "" }] as const,
          () => null
        )
      )
    ).then((pairs) => {
      if (ctrl.signal.aborted) return
      const next: Record<string, PrintProduct> = {}
      for (const pair of pairs) if (pair) next[pair[0]] = pair[1]
      setProducts(next)
      setFailedKey(Object.keys(next).length < key.split(",").length ? key : "")
    })
    return () => ctrl.abort()
  }, [key, tenantId])

  return { products, failed: failedKey !== "" && failedKey === key }
}
