"use client"

import { useCallback, useEffect, useRef } from "react"
import { getLastPrices, type LastPrice, type LastPriceKind } from "@/lib/api/last-prices"

/** What the grey hint under a pre-filled price shows. */
export interface LastPriceHint {
  /** The price as quoted (in `unit_name`). */
  price: string
  date: string
  source: "last" | "default"
  unit_name?: string
  /** Per-base-unit price, set when the filled price differs from the quoted one. */
  base_price?: string
  /** Price per base unit, always set; lets a manual unit change reprice the line. */
  base_value?: string
}

/** Runtime-only fields a bill line carries for the last-price feature. */
export interface LastPriceLine {
  product_id: string
  qty: string
  unit_price: string
  unit_id?: string
  unit_name?: string
  _subtotal?: string
  /** Where the pre-filled price came from; drives the grey hint. */
  _last_price?: LastPriceHint
  /** The user typed the price (or it came from a copied bill): never overwrite it. */
  _price_touched?: boolean
  /** The price before it was auto-filled, restored if the new partner has no history. */
  _prev_price?: string
  /** The unit before it was auto-filled (lines with a unit column only). */
  _prev_unit?: { id?: string; name?: string }
}

function subtotal(qty: string, unitPrice: string): string {
  return ((parseFloat(qty) || 0) * (parseFloat(unitPrice) || 0)).toFixed(4)
}

/**
 * The line patch for one price hit. A line with a unit column takes the unit the price
 * was quoted in; a line without one is billed in the base unit, so it takes the
 * per-base-unit price instead. A quoted price never lands on a different unit.
 */
export function applyLastPrice<T extends LastPriceLine>(it: T, hit: LastPrice, withUnits: boolean): T {
  const useUnit = withUnits && hit.source === "last" && !!hit.unit_id
  const price = useUnit || hit.source === "default" ? hit.unit_price : hit.base_unit_price
  const converted = !useUnit && hit.source === "last" && hit.base_unit_price !== hit.unit_price
  const first = !it._last_price
  return {
    ...it,
    unit_price: price,
    ...(useUnit
      ? { unit_id: hit.unit_id, unit_name: hit.unit_name }
      : withUnits
        ? // The price is per base unit: put the line back on the unit it had before any
          // auto-fill, or clear it so the editor re-applies the base unit.
          { unit_id: it._prev_unit?.id, unit_name: it._prev_unit?.name }
        : {}),
    _prev_price: first ? it.unit_price : it._prev_price,
    _prev_unit: it._prev_unit ?? (useUnit ? { id: it.unit_id, name: it.unit_name } : undefined),
    _last_price: {
      price: hit.unit_price,
      date: hit.bill_date,
      source: hit.source,
      unit_name: hit.unit_name || undefined,
      base_price: converted ? hit.base_unit_price : undefined,
      base_value: hit.base_unit_price,
    },
    _subtotal: subtotal(it.qty, price),
  }
}

function restore<T extends LastPriceLine>(it: T): T {
  const restored = it._prev_price ?? it.unit_price
  const unit = it._prev_unit ? { unit_id: it._prev_unit.id, unit_name: it._prev_unit.name } : {}
  return {
    ...it,
    ...unit,
    unit_price: restored,
    _prev_price: undefined,
    _prev_unit: undefined,
    _last_price: undefined,
    _subtotal: subtotal(it.qty, restored),
  }
}

export interface LastPriceFill<T> {
  /** Look up and fill the prices of these products on the current lines. */
  fill: (productIds: string[]) => void
  /**
   * Patch every line of a product against the *latest* lines, including changes made
   * by another response that landed before React re-rendered. Return null to skip.
   */
  patchProduct: (productId: string, fn: (it: T) => T | null) => void
}

/**
 * Fills each line's unit price with what the chosen customer / supplier was last billed
 * for that product, or the product's default price when there is no history. Re-runs on
 * the lines still untouched when the partner changes.
 * `withUnits`: the lines have a unit column, so the unit the old price was quoted in is
 * applied too.
 */
export function useLastPriceFill<T extends LastPriceLine>(
  items: T[],
  onChange: (next: T[]) => void,
  partnerId: string | undefined,
  kind: LastPriceKind,
  tenantId?: string,
  enabled = true,
  withUnits = false
): LastPriceFill<T> {
  // Always the newest lines we know of: every emit below updates it synchronously so two
  // responses arriving before a re-render build on each other, not on a stale snapshot.
  const itemsRef = useRef(items)
  itemsRef.current = items
  const onChangeRef = useRef(onChange)
  onChangeRef.current = onChange
  const partnerRef = useRef(partnerId)
  partnerRef.current = partnerId

  const emit = useCallback((next: T[]) => {
    itemsRef.current = next
    onChangeRef.current(next)
  }, [])

  const patchProduct = useCallback(
    (productId: string, fn: (it: T) => T | null) => {
      let changed = false
      const next = itemsRef.current.map((it) => {
        if (it.product_id !== productId) return it
        const patched = fn(it)
        if (!patched) return it
        changed = true
        return patched
      })
      if (changed) emit(next)
    },
    [emit]
  )

  const fill = useCallback(
    (productIds: string[]) => {
      const ids = Array.from(new Set(productIds.filter(Boolean)))
      const pid = partnerId
      if (!enabled || ids.length === 0) return
      getLastPrices({ partnerId: pid, productIds: ids, type: kind }, { tenantId }).then(
        (res) => {
          if (partnerRef.current !== pid) return
          const byProduct = new Map(res.items.map((x) => [x.product_id, x]))
          let changed = false
          const next = itemsRef.current.map((it) => {
            if (!ids.includes(it.product_id) || it._price_touched) return it
            const hit = byProduct.get(it.product_id)
            if (hit) {
              changed = true
              return applyLastPrice(it, hit, withUnits)
            }
            if (!it._last_price) return it
            changed = true
            return restore(it)
          })
          if (changed) emit(next)
        },
        () => {
          // Best effort: the user can always type the price.
        }
      )
    },
    [partnerId, kind, tenantId, enabled, withUnits, emit]
  )

  // Partner changed (or cleared: default prices): refresh every line the user has not priced by hand.
  useEffect(() => {
    if (!enabled) return
    const ids = itemsRef.current.filter((it) => it.product_id && !it._price_touched).map((it) => it.product_id)
    fill(ids)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [partnerId, kind, enabled])

  return { fill, patchProduct }
}
