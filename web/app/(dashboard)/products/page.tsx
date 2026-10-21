"use client"

import { useCallback, useEffect, useState } from "react"
import Link from "next/link"
import { toast } from "sonner"
import type { ColumnDef } from "@tanstack/react-table"
import { listProducts, deleteProduct, restoreProduct, type Product } from "@/lib/api/products"
import { extractApiError } from "@/lib/api/errors"
import { globalUndoStack } from "@/lib/undo/undo-stack"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useTenantId } from "@/hooks/use-tenant-id"
import { useRole } from "@/hooks/use-role"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { DataTable, statusCell } from "@/components/ui/data-table"
import { DataCard } from "@/components/ui/data-card"
import { formatCNY } from "@/lib/format"
import { Badge } from "@/components/ui/badge"
import { Button, buttonVariants } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { EmptyState } from "@/components/ui/empty-state"
import { ProductImportDialog } from "@/components/product-import-dialog"
import { BarcodeScanner } from "@/components/barcode-scanner"

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

// 列表接口当前不返回条码字段时显示占位。
function barcodeOf(p: Product): string {
  const b = (p as Product & { barcode?: string }).barcode
  return b || "—"
}

export default function ProductsPage() {
  const [products, setProducts] = useState<Product[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [q, setQ] = useState("")
  const [importOpen, setImportOpen] = useState(false)
  const tenantId = useTenantId()
  // Adding, importing, editing and deleting need product.write; others only look.
  // An unknown role hides nothing (the backend decides).
  const { known, can } = useRole()
  const mayWrite = !known || can("product.write")

  // /products?import=1 (dashboard "去导入") opens the import dialog straight away.
  useEffect(() => {
    const url = new URL(window.location.href)
    if (url.searchParams.get("import") !== "1") return
    if (mayWrite) setImportOpen(true)
    // Drop the flag so a refresh doesn't pop the dialog again.
    url.searchParams.delete("import")
    window.history.replaceState(window.history.state, "", url.pathname + url.search + url.hash)
  }, [mayWrite])

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
        setError(extractApiError(e).message)
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
      toast.error("删除失败：" + extractApiError(e).message)
    }
  }

  const allColumns: ColumnDef<Product>[] = [
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
  const columns = mayWrite ? allColumns : allColumns.filter((c) => c.id !== "actions")

  return (
    <PageContainer width="wide">
      <PageHeader
        title="商品管理"
        subtitle={`共 ${total} 条商品`}
        actions={
          mayWrite ? (
            <>
              <Button variant="outline" onClick={() => setImportOpen(true)}>
                批量导入
              </Button>
              <Link href="/products/new" className={buttonVariants()}>
                + 新建商品
              </Link>
            </>
          ) : undefined
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
        <BarcodeScanner
          onScan={(code) => {
            setQ(code)
            load(code)
          }}
        />
        <Button variant="outline" onClick={() => load(q || undefined)}>
          搜索
        </Button>
      </div>

      <ProductImportDialog
        open={importOpen}
        onOpenChange={setImportOpen}
        tenantId={tenantId}
        onImported={() => load(q || undefined)}
      />

      <DataTable
        columns={columns}
        data={products}
        loading={loading}
        error={error}
        getRowId={(p) => p.id}
        mobileCard={(p) => (
          <DataCard
            title={p.name}
            subtitle={p.code}
            aside={statusCell(p.enabled ? "启用" : "停用", p.enabled ? "ok" : "neutral")}
            fields={[
              { label: "售价", value: p.retail_price ? formatCNY(p.retail_price) : "—" },
              { label: "条码", value: barcodeOf(p) },
              { label: "品牌", value: p.brand || "—" },
            ]}
            actions={
              mayWrite ? (
                <>
                  <Link href={`/products/${p.id}`} className="text-xs text-primary hover:underline">
                    编辑
                  </Link>
                  <button
                    type="button"
                    onClick={() => handleDelete(p)}
                    className="text-xs text-destructive hover:underline"
                  >
                    删除
                  </button>
                </>
              ) : undefined
            }
          />
        )}
        animateRows
        skeletonRows={5}
        empty={
          <EmptyState
            title="暂无商品"
            description={mayWrite ? "新建第一个商品，或把表格里的商品一次导入" : "还没有商品，等老板或店长添加后这里就会出现"}
            action={
              mayWrite ? (
                <div className="flex gap-4">
                  <Link href="/products/new" className="text-sm text-primary hover:underline">
                    立即新建
                  </Link>
                  <button type="button" onClick={() => setImportOpen(true)} className="text-sm text-primary hover:underline">
                    批量导入
                  </button>
                </div>
              ) : undefined
            }
          />
        }
      />
    </PageContainer>
  )
}
