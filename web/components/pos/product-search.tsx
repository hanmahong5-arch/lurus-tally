"use client"

import React, { useState, useEffect, useRef, useCallback } from "react"
import { listProducts, type Product } from "@/lib/api/products"
import { BarcodeScanner } from "@/components/barcode-scanner"
import { NetworkError } from "@/lib/api/errors"
import { catalogItemToProduct, searchCatalogWithMeta } from "@/lib/catalog-cache"

/** 缓存时间转「x 分钟前」。 */
function agoText(updatedAt: number): string {
  const min = Math.max(0, Math.round((Date.now() - updatedAt) / 60_000))
  if (min < 1) return "刚刚"
  if (min < 60) return `${min} 分钟前`
  const h = Math.round(min / 60)
  return h < 24 ? `${h} 小时前` : `${Math.round(h / 24)} 天前`
}

interface ProductSearchProps {
  onSelect: (product: Product) => void
  tenantId?: string
  lastAddedProductId?: string
}

const BARCODE_RE = /^[A-Za-z0-9-]{6,}$/

/**
 * ProductSearch provides a debounced input that supports both barcode scanning
 * (alphanumeric input of 6+ chars → attribute_filter lookup, falling back to name / code search
 * when no barcode matches) and name search (free text → q param).
 *
 * Single barcode match → auto-adds to cart without user confirmation.
 * Multiple name results → shows a dropdown (up to 8 items), keyboard navigable.
 */
export const ProductSearch = React.forwardRef<HTMLInputElement, ProductSearchProps>(
  function ProductSearch({ onSelect, tenantId }, ref) {
    const [query, setQuery] = useState("")
    const [results, setResults] = useState<Product[]>([])
    const [highlighted, setHighlighted] = useState(0)
    const [loading, setLoading] = useState(false)
    const [notFound, setNotFound] = useState(false)
    // 结果来自离线缓存时记下缓存时间，列表里标「缓存价」。
    const [cachedAt, setCachedAt] = useState<number | null>(null)

    const internalRef = useRef<HTMLInputElement>(null)
    // Merge the forwarded ref with the internal ref so we can use both
    const inputRef = (ref as React.RefObject<HTMLInputElement>) ?? internalRef
    const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
    // Tracks the in-flight search so a quickly-typed query can abort the prior
    // request before its response races back and clobbers the newer state.
    const abortRef = useRef<AbortController | null>(null)

    const runSearch = useCallback(
      async (q: string, signal: AbortSignal) => {
        if (!q.trim()) {
          setResults([])
          setNotFound(false)
          return
        }

        const isBarcode = BARCODE_RE.test(q.trim())
        setLoading(true)

        // 断网或请求没发出去：改查本机目录缓存；条码唯一命中仍自动加购。
        const searchCache = async () => {
          const hit = tenantId ? await searchCatalogWithMeta(tenantId, q.trim()) : null
          if (signal.aborted) return
          const items = (hit?.items ?? []).map(catalogItemToProduct)
          if (isBarcode && items.length === 1 && hit?.items[0].barcode === q.trim()) {
            onSelect(items[0])
            setQuery("")
            setResults([])
            setNotFound(false)
            setCachedAt(null)
            return
          }
          setResults(items)
          setNotFound(items.length === 0)
          setCachedAt(hit && items.length > 0 ? hit.updated_at : null)
        }

        try {
          if (typeof navigator !== "undefined" && navigator.onLine === false) {
            await searchCache()
            return
          }
          setCachedAt(null)
          if (isBarcode) {
            const hit = await listProducts({
              attributes_filter: { barcode: q.trim() },
              limit: 2,
              tenantId,
              signal,
            })
            if (signal.aborted) return
            const exact = hit.items ?? []
            if (exact.length === 1) {
              // Auto-select on unambiguous barcode scan
              onSelect(exact[0])
              setQuery("")
              setResults([])
              setNotFound(false)
              return
            }
            if (exact.length > 1) {
              // Two products share the barcode: let the cashier pick instead of guessing.
              setResults(exact.slice(0, 8))
              setNotFound(false)
              return
            }
          }
          // Not a barcode (or a number that is no barcode, e.g. a spec such as "304"):
          // look it up by name / code.
          const res = await listProducts({ q: q.trim(), limit: 20, tenantId, signal })
          if (signal.aborted) return
          const items = res.items ?? []
          setResults(items.slice(0, 8))
          setNotFound(items.length === 0)
        } catch (err) {
          if (signal.aborted) return
          if (err instanceof NetworkError) {
            await searchCache()
          } else {
            setResults([])
          }
        } finally {
          if (!signal.aborted) setLoading(false)
        }
      },
      [onSelect, tenantId]
    )

    useEffect(() => {
      if (!query.trim()) {
        setResults([])
        setNotFound(false)
        setLoading(false)
        if (debounceRef.current) clearTimeout(debounceRef.current)
        abortRef.current?.abort()
        abortRef.current = null
        return
      }

      if (debounceRef.current) clearTimeout(debounceRef.current)
      debounceRef.current = setTimeout(() => {
        abortRef.current?.abort()
        const ctrl = new AbortController()
        abortRef.current = ctrl
        runSearch(query, ctrl.signal)
      }, 200)

      return () => {
        if (debounceRef.current) clearTimeout(debounceRef.current)
        abortRef.current?.abort()
      }
    }, [query, runSearch])

    const handleSelect = useCallback(
      (product: Product) => {
        onSelect(product)
        setQuery("")
        setResults([])
        setHighlighted(0)
        inputRef.current?.focus()
      },
      [onSelect, inputRef]
    )

    const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === "Enter") {
        if (results.length > 0) {
          e.preventDefault()
          handleSelect(results[highlighted])
        } else if (query.trim()) {
          // Immediate barcode lookup on Enter without waiting for debounce
          if (debounceRef.current) clearTimeout(debounceRef.current)
          abortRef.current?.abort()
          const ctrl = new AbortController()
          abortRef.current = ctrl
          runSearch(query, ctrl.signal)
        }
      } else if (e.key === "ArrowDown") {
        e.preventDefault()
        setHighlighted((h) => Math.min(h + 1, results.length - 1))
      } else if (e.key === "ArrowUp") {
        e.preventDefault()
        setHighlighted((h) => Math.max(h - 1, 0))
      } else if (e.key === "Escape") {
        setResults([])
        setQuery("")
      }
    }

    return (
      <div className="relative w-full">
        <div className="flex items-stretch gap-2">
          <input
            ref={inputRef}
            type="text"
            role="textbox"
            autoFocus
            value={query}
            onChange={(e) => {
              setQuery(e.target.value)
              setHighlighted(0)
            }}
            onKeyDown={handleKeyDown}
            placeholder="商品名 / 编码 / 扫码 (F1)"
            className="w-full rounded-lg border border-border bg-background px-4 py-3 text-xl outline-none focus:ring-2 focus:ring-ring placeholder:text-muted-foreground"
          />
          <BarcodeScanner
            className="h-auto self-stretch"
            onScan={(code) => {
              setQuery(code)
              setHighlighted(0)
            }}
          />
        </div>

        {loading && (
          <div className="absolute right-24 top-3 text-sm text-muted-foreground">搜索中...</div>
        )}

        {notFound && (
          <div className="absolute left-0 right-0 top-full z-20 mt-1 rounded-lg border border-border bg-background p-3 text-sm text-muted-foreground shadow-lg">
            未找到商品
          </div>
        )}

        {results.length > 0 && (
          <ul
            role="listbox"
            className="absolute left-0 right-0 top-full z-20 mt-1 max-h-72 overflow-y-auto rounded-lg border border-border bg-background shadow-lg"
          >
            {results.map((product, idx) => (
              <li
                key={product.id}
                role="option"
                aria-selected={idx === highlighted}
                onClick={() => handleSelect(product)}
                onMouseEnter={() => setHighlighted(idx)}
                className={`cursor-pointer px-4 py-2.5 text-sm transition-colors ${
                  idx === highlighted ? "bg-muted" : "hover:bg-muted/50"
                }`}
              >
                <span className="font-medium">{product.name}</span>
                {product.code && (
                  <span className="ml-2 text-xs text-muted-foreground font-mono">
                    {product.code}
                  </span>
                )}
                {cachedAt !== null && (
                  <span className="ml-2 text-xs text-amber-700">
                    缓存价（{agoText(cachedAt)}）
                    {product.retail_price ? ` ¥${product.retail_price}` : ""}
                  </span>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>
    )
  }
)
