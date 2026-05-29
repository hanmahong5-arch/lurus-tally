"use client"

import { useCallback, useState } from "react"
import Link from "next/link"
import { toast } from "sonner"
import type { ColumnDef } from "@tanstack/react-table"
import { listProducts, deleteProduct, restoreProduct, type Product } from "@/lib/api/products"
import { globalUndoStack } from "@/lib/undo/undo-stack"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useTenantId } from "@/hooks/use-tenant-id"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { DataTable } from "@/components/ui/data-table"
import { Badge } from "@/components/ui/badge"
import { Button, buttonVariants } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { EmptyState } from "@/components/ui/empty-state"

/**
 * Products list page — GET /api/v1/products
 */
const STRATEGY_LABELS: Record<string, string> = {
  individual: "标准件",
  weight: "按重量",
  length: "按长度",
  volume: "按体积",
  batch: "批次",
  serial: "序列号",
}

export default function ProductsPage() {
  const [products, setProducts] = useState<Product[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [q, setQ] = useState("")
  const tenantId = useTenantId()

  const load = useCallback((query?: string, signal?: AbortSignal, isCancelled?: () => boolean) => {
    setLoading(true)
    setError(null)
    listProducts({ q: query, tenantId, signal, retry: 2 })
      .then((res) => {
        if (isCancelled?.()) return
        setProducts(res.items ?? [])
        setTotal(res.total)
      })
      .catch((e) => {
        if (isCancelled?.() || signal?.aborted) return
        setError(String(e))
      })
      .finally(() => {
        if (isCancelled?.()) return
        setLoading(false)
      })
  }, [tenantId])

  useAbortableEffect((signal, isCancelled) => {
    load(undefined, signal, isCancelled)
  }, [load])

  async function handleDelete(p: Product) {
    // Push undo entry BEFORE the delete call so the entry is never lost if delete fails.
    globalUndoStack.push({
      type: "delete_product",
      id: p.id,
      name: p.name,
      revert: async () => {
        await restoreProduct(p.id, tenantId)
        load(q || undefined)
      },
    })

    try {
      await deleteProduct(p.id, tenantId)
      load(q || undefined)
    } catch (e) {
      // Delete failed — remove the entry we just pushed so undo doesn't fire.
      globalUndoStack.pop()
      toast.error("删除失败：" + String(e))
    }
  }

  const columns: ColumnDef<Product>[] = [
    {
      id: "code",
      header: "编码",
      cell: ({ row }) => <span className="font-mono text-xs">{row.original.code}</span>,
    },
    {
      id: "name",
      header: "名称",
      cell: ({ row }) => <span className="font-medium">{row.original.name}</span>,
    },
    {
      id: "brand",
      header: "品牌",
      cell: ({ row }) => (
        <span className="text-muted-foreground">{row.original.brand || "—"}</span>
      ),
    },
    {
      id: "strategy",
      header: "计量策略",
      cell: ({ row }) => (
        <Badge tone="neutral">
          {STRATEGY_LABELS[row.original.measurement_strategy] ?? row.original.measurement_strategy}
        </Badge>
      ),
    },
    {
      id: "status",
      header: "状态",
      cell: ({ row }) => (
        <Badge tone={row.original.enabled ? "ok" : "neutral"}>
          {row.original.enabled ? "启用" : "停用"}
        </Badge>
      ),
    },
    {
      id: "actions",
      header: "操作",
      meta: { align: "right" },
      cell: ({ row }) => (
        <div className="flex justify-end gap-2">
          <Link
            href={`/products/${row.original.id}`}
            className="text-xs text-primary hover:underline"
          >
            编辑
          </Link>
          <button
            type="button"
            onClick={() => handleDelete(row.original)}
            className="text-xs text-destructive hover:underline"
          >
            删除
          </button>
        </div>
      ),
    },
  ]

  return (
    <PageContainer width="wide">
      <PageHeader
        title="商品管理"
        subtitle={`共 ${total} 条商品`}
        actions={
          <Link href="/products/new" className={buttonVariants()}>
            + 新建商品
          </Link>
        }
      />

      <div className="mb-4 flex gap-2">
        <Input
          aria-label="搜索商品"
          className="flex-1"
          placeholder="搜索商品名称、编码、助记码..."
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") load(q || undefined)
          }}
        />
        <Button variant="outline" onClick={() => load(q || undefined)}>
          搜索
        </Button>
      </div>

      <DataTable
        columns={columns}
        data={products}
        loading={loading}
        error={error}
        getRowId={(p) => p.id}
        animateRows
        skeletonRows={5}
        empty={
          <EmptyState
            title="暂无商品"
            description="创建第一个商品以开始管理库存"
            action={
              <Link href="/products/new" className="text-sm text-primary hover:underline">
                立即新建
              </Link>
            }
          />
        }
      />
    </PageContainer>
  )
}
