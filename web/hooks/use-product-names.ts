"use client"

import { useEffect, useMemo, useState } from "react"
import { getProduct } from "@/lib/api/products"

/**
 * useProductNames resolves product ids to display names for bill detail views.
 * Bill items only carry product_id, so without this a saved bill shows raw
 * UUIDs. Unknown / failed lookups are simply absent from the map (the caller
 * falls back to the id).
 */
export function useProductNames(ids: string[], tenantId?: string): Record<string, string> {
  const key = useMemo(() => Array.from(new Set(ids.filter(Boolean))).sort().join(","), [ids])
  const [names, setNames] = useState<Record<string, string>>({})

  useEffect(() => {
    if (!key) return
    const ctrl = new AbortController()
    Promise.all(
      key.split(",").map((id) =>
        getProduct(id, tenantId, ctrl.signal).then(
          (p) => [id, p.name] as const,
          () => null
        )
      )
    ).then((pairs) => {
      if (ctrl.signal.aborted) return
      const next: Record<string, string> = {}
      for (const pair of pairs) if (pair) next[pair[0]] = pair[1]
      setNames(next)
    })
    return () => ctrl.abort()
  }, [key, tenantId])

  return names
}
