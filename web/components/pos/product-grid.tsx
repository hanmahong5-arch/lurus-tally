"use client"

import { useState } from "react"
import type { Product } from "@/lib/api/products"

type Tab = "all" | "common"

interface ProductGridProps {
  products: Product[]
  onAdd: (product: Product) => void
}

/**
 * ProductGrid displays products in a 4-column grid with tab filtering.
 * Tabs: 全部 (all) / 常用 (is_common===true).
 *
 * Each card is at least 80px tall with touch-friendly tap targets.
 */
export function ProductGrid({ products, onAdd }: ProductGridProps) {
  const [activeTab, setActiveTab] = useState<Tab>("all")

  const commonProducts = products.filter(
    (p) => p.attributes?.is_common === true
  )

  const visibleProducts = activeTab === "common" ? commonProducts : products

  return (
    <div className="flex flex-col gap-3">
      {/* Tab bar */}
      <div className="flex gap-1 border-b border-border pb-2">
        {(
          [
            { key: "all", label: "全部" },
            { key: "common", label: "常用" },
          ] as { key: Tab; label: string }[]
        ).map((tab) => (
          <button
            key={tab.key}
            onClick={() => setActiveTab(tab.key)}
            className={`rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
              activeTab === tab.key
                ? "bg-primary text-primary-foreground"
                : "text-muted-foreground hover:bg-muted hover:text-foreground"
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* Product grid */}
      {visibleProducts.length === 0 ? (
        <div className="py-8 text-center text-sm text-muted-foreground">
          {activeTab === "common"
            ? "暂无常用商品，在商品设置中勾选 is_common 即可"
            : "暂无商品，请先添加商品"}
        </div>
      ) : (
        <div className="grid grid-cols-4 gap-2 sm:grid-cols-3 overflow-y-auto max-h-[calc(100vh-280px)]">
          {visibleProducts.map((product) => (
            <button
              key={product.id}
              onClick={() => onAdd(product)}
              className="flex min-h-[80px] flex-col items-center justify-center gap-1 rounded-lg border border-border bg-card px-2 py-3 text-center transition-colors hover:bg-muted active:scale-95"
              style={{ minWidth: 80 }}
            >
              <span className="line-clamp-2 text-sm font-medium leading-tight">
                {product.name}
              </span>
              {product.code && (
                <span className="font-mono text-xs text-muted-foreground">
                  {product.code}
                </span>
              )}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
