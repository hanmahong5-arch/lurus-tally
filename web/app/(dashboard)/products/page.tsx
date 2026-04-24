"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import { listProducts, deleteProduct, type Product } from "@/lib/api/products"

/**
 * Products list page — GET /api/v1/products
 *
 * Story 2.1 TODO: remove the hardcoded devTenantId once session auth is wired.
 * Replace with tenantId from the NextAuth session.
 */
const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID

export default function ProductsPage() {
  const [products, setProducts] = useState<Product[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [q, setQ] = useState("")

  function load(query?: string) {
    setLoading(true)
    setError(null)
    listProducts({ q: query, tenantId: devTenantId })
      .then((res) => {
        setProducts(res.items ?? [])
        setTotal(res.total)
      })
      .catch((e) => setError(String(e)))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    load()
  }, [])

  async function handleDelete(id: string) {
    if (!confirm("确认删除该商品？")) return
    try {
      await deleteProduct(id, devTenantId)
      load(q || undefined)
    } catch (e) {
      alert("删除失败: " + String(e))
    }
  }

  const STRATEGY_LABELS: Record<string, string> = {
    individual: "标准件",
    weight: "按重量",
    length: "按长度",
    volume: "按体积",
    batch: "批次",
    serial: "序列号",
  }

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-semibold">商品管理</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            共 {total} 条商品
          </p>
        </div>
        <Link
          href="/products/new"
          className="rounded-lg bg-primary px-4 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 transition-colors"
        >
          + 新建商品
        </Link>
      </div>

      <div className="mb-4 flex gap-2">
        <input
          className="flex-1 rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
          placeholder="搜索商品名称、编码、助记码..."
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") load(q || undefined)
          }}
        />
        <button
          onClick={() => load(q || undefined)}
          className="rounded-lg border border-border px-4 py-1.5 text-sm hover:bg-muted transition-colors"
        >
          搜索
        </button>
      </div>

      {loading && (
        <div className="py-12 text-center text-muted-foreground">加载中...</div>
      )}
      {error && (
        <div className="rounded-md bg-destructive/10 border border-destructive/30 px-4 py-3 text-sm text-destructive">
          {error}
        </div>
      )}
      {!loading && !error && products.length === 0 && (
        <div className="py-12 text-center text-muted-foreground">
          暂无商品，
          <Link href="/products/new" className="text-primary underline">
            立即新建
          </Link>
        </div>
      )}

      {!loading && products.length > 0 && (
        <div className="overflow-hidden rounded-xl border border-border">
          <table className="w-full text-sm">
            <thead className="bg-muted/50 text-muted-foreground">
              <tr>
                <th className="px-4 py-2.5 text-left font-medium">编码</th>
                <th className="px-4 py-2.5 text-left font-medium">名称</th>
                <th className="px-4 py-2.5 text-left font-medium">品牌</th>
                <th className="px-4 py-2.5 text-left font-medium">计量策略</th>
                <th className="px-4 py-2.5 text-left font-medium">状态</th>
                <th className="px-4 py-2.5 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {products.map((p) => (
                <tr key={p.id} className="hover:bg-muted/30 transition-colors">
                  <td className="px-4 py-2.5 font-mono text-xs">{p.code}</td>
                  <td className="px-4 py-2.5 font-medium">{p.name}</td>
                  <td className="px-4 py-2.5 text-muted-foreground">
                    {p.brand || "—"}
                  </td>
                  <td className="px-4 py-2.5">
                    <span className="rounded-full bg-muted px-2 py-0.5 text-xs">
                      {STRATEGY_LABELS[p.measurement_strategy] ??
                        p.measurement_strategy}
                    </span>
                  </td>
                  <td className="px-4 py-2.5">
                    <span
                      className={`rounded-full px-2 py-0.5 text-xs ${
                        p.enabled
                          ? "bg-green-500/10 text-green-500"
                          : "bg-muted text-muted-foreground"
                      }`}
                    >
                      {p.enabled ? "启用" : "停用"}
                    </span>
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    <div className="flex justify-end gap-2">
                      <Link
                        href={`/products/${p.id}`}
                        className="text-xs text-primary hover:underline"
                      >
                        编辑
                      </Link>
                      <button
                        onClick={() => handleDelete(p.id)}
                        className="text-xs text-destructive hover:underline"
                      >
                        删除
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
