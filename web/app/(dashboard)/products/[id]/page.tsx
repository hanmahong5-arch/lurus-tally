"use client"

import { useCallback, useState } from "react"
import { useRouter, useParams } from "next/navigation"
import {
  getProduct,
  updateProduct,
  type Product,
  type CreateProductInput,
} from "@/lib/api/products"
import { ProductForm } from "@/components/product-form"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { ErrorBanner } from "@/components/ui/error-banner"
import Link from "next/link"

const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID

export default function ProductDetailPage() {
  const router = useRouter()
  const params = useParams()
  const id = params?.id as string

  const [product, setProduct] = useState<Product | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback((signal: AbortSignal, isCancelled: () => boolean) => {
    if (!id) return
    setLoading(true)
    setError(null)
    getProduct(id, devTenantId, signal)
      .then((p) => {
        if (isCancelled()) return
        setProduct(p)
      })
      .catch((e) => {
        if (isCancelled() || signal.aborted) return
        setError(String(e))
      })
      .finally(() => {
        if (isCancelled()) return
        setLoading(false)
      })
  }, [id])

  useAbortableEffect(load, [load])

  async function handleSubmit(input: CreateProductInput) {
    await updateProduct(id, input, devTenantId)
    router.push("/products")
    router.refresh()
  }

  if (loading) {
    return (
      <div className="p-6 text-muted-foreground text-sm">加载中...</div>
    )
  }

  if (error || !product) {
    return (
      <div className="p-6 space-y-4">
        <ErrorBanner hint="请刷新页面重试">{error ?? "商品不存在"}</ErrorBanner>
        <Link href="/products" className="text-sm text-primary hover:underline">
          返回商品列表
        </Link>
      </div>
    )
  }

  return (
    <div className="p-6 max-w-2xl">
      <div className="mb-6">
        <h1 className="text-xl font-semibold">编辑商品</h1>
        <p className="text-sm text-muted-foreground mt-0.5">
          {product.code} · {product.name}
        </p>
      </div>
      <ProductForm
        initial={product}
        onSubmit={handleSubmit}
        onCancel={() => router.back()}
        tenantId={devTenantId}
      />
    </div>
  )
}
