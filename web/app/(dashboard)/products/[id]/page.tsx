"use client"

import { useCallback, useState } from "react"
import { useRouter, useParams } from "next/navigation"
import Link from "next/link"
import {
  getProduct,
  updateProduct,
  type Product,
  type CreateProductInput,
} from "@/lib/api/products"
import { ProductForm } from "@/components/product-form"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { ErrorBanner } from "@/components/ui/error-banner"
import { Skeleton } from "@/components/ui/skeleton"

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
      <PageContainer width="narrow">
        <PageHeader title="编辑商品" />
        <div className="space-y-4">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Skeleton className="h-9" />
            <Skeleton className="h-9" />
          </div>
          <Skeleton className="h-9" />
          <Skeleton className="h-24" />
        </div>
      </PageContainer>
    )
  }

  if (error || !product) {
    return (
      <PageContainer width="narrow">
        <div className="space-y-4">
          <ErrorBanner hint="请刷新页面重试">{error ?? "商品不存在"}</ErrorBanner>
          <Link href="/products" className="text-sm text-primary hover:underline">
            返回商品列表
          </Link>
        </div>
      </PageContainer>
    )
  }

  return (
    <PageContainer width="narrow">
      <PageHeader title="编辑商品" subtitle={`${product.code} · ${product.name}`} />
      <ProductForm
        initial={product}
        onSubmit={handleSubmit}
        onCancel={() => router.back()}
        tenantId={devTenantId}
      />
    </PageContainer>
  )
}
