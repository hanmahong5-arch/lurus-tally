"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import { toast } from "sonner"
import { updateProduct } from "@/lib/api/products"
import { listProductUnits, type ProductUnitOption } from "@/lib/api/product-units"
import { listUnits } from "@/lib/api/units"
import { useProfile } from "@/lib/profile"

export interface LineUnitChoice {
  id: string
  name: string
  conversion_factor: string
  is_base: boolean
}

export interface LineUnitState {
  options: LineUnitChoice[]
  /** True when the list is the shop catalogue, because this product has no units of its own. */
  fromCatalog: boolean
  ready: boolean
  failed: boolean
}

const NURSERY_FIRST = ["株", "棵", "丛", "袋", "平方米"]

/**
 * Units a bill line may use. A product that already has a base unit (or conversions)
 * offers only those — any other unit would be rejected when the bill is approved.
 * A product with none, and a line that has no product yet, offers the same catalogue
 * the product form shows (the units seeded at signup plus any the shop added).
 * Picking one of those for a product writes it as that product's base unit.
 */
export function useBillUnits(productIds: string[], tenantId: string | undefined, enabled: boolean) {
  const { profileType } = useProfile()
  const [byProduct, setByProduct] = useState<Record<string, LineUnitChoice[]>>({})
  const [catalog, setCatalog] = useState<LineUnitChoice[] | null>(null)
  const [catalogFailed, setCatalogFailed] = useState(false)
  const requested = useRef(new Set<string>())

  useEffect(() => {
    if (!enabled) return
    let cancelled = false
    listUnits(undefined, tenantId).then(
      (res) => {
        if (cancelled) return
        const items = [...(res.items ?? [])].sort((a, b) => {
          if (profileType === "horticulture") {
            const rank = (name: string) => {
              const i = NURSERY_FIRST.indexOf(name)
              return i < 0 ? NURSERY_FIRST.length : i
            }
            const d = rank(a.name) - rank(b.name)
            if (d !== 0) return d
          }
          return Number(a.is_system) - Number(b.is_system)
        })
        setCatalogFailed(false)
        setCatalog(items.map((u) => ({ id: u.id, name: u.name, conversion_factor: "1", is_base: false })))
      },
      () => {
        if (cancelled) return
        setCatalogFailed(true)
        setCatalog([])
      }
    )
    return () => {
      cancelled = true
    }
  }, [enabled, tenantId, profileType])

  const productKey = Array.from(new Set(productIds.filter(Boolean))).join(",")
  useEffect(() => {
    if (!enabled) return
    for (const pid of productKey.split(",").filter(Boolean)) {
      if (requested.current.has(pid)) continue
      requested.current.add(pid)
      listProductUnits(pid, { tenantId }).then(
        (opts) => setByProduct((prev) => ({ ...prev, [pid]: opts.map(toChoice) })),
        () => setByProduct((prev) => ({ ...prev, [pid]: [] }))
      )
    }
  }, [productKey, enabled, tenantId])

  const forLine = useCallback(
    (productId: string | undefined, current?: { id?: string; name?: string }): LineUnitState => {
      const own = productId ? byProduct[productId] : undefined
      const productReady = !productId || Object.prototype.hasOwnProperty.call(byProduct, productId)
      let options: LineUnitChoice[] = []
      let fromCatalog = false
      let ready = false
      if (productReady && own && own.length > 0) {
        options = own
        ready = true
      } else if (productReady && catalog) {
        options = catalog
        fromCatalog = true
        ready = true
      }
      if (current?.id && !options.some((o) => o.id === current.id)) {
        options = [{ id: current.id, name: current.name || "当前单位", conversion_factor: "1", is_base: false }, ...options]
      }
      return { options, fromCatalog, ready, failed: fromCatalog && catalogFailed }
    },
    [byProduct, catalog, catalogFailed]
  )

  const baseOf = useCallback(
    (productId: string): LineUnitChoice | undefined => byProduct[productId]?.find((o) => o.is_base),
    [byProduct]
  )

  const remember = useCallback(
    (productId: string, unitId: string, fromCatalog: boolean) => {
      if (!fromCatalog || !productId || !unitId) return
      const name = catalog?.find((u) => u.id === unitId)?.name ?? ""
      updateProduct(productId, { default_unit_id: unitId }, tenantId).then(
        () => {
          setByProduct((prev) => ({
            ...prev,
            [productId]: [{ id: unitId, name, conversion_factor: "1", is_base: true }],
          }))
        },
        () => {
          toast.error("这个单位还没记到商品上，审核前请到商品里补上基本单位")
        }
      )
    },
    [tenantId, catalog]
  )

  return { forLine, baseOf, remember }
}

function toChoice(o: ProductUnitOption): LineUnitChoice {
  return { id: o.id, name: o.name, conversion_factor: o.conversion_factor, is_base: o.is_base }
}
