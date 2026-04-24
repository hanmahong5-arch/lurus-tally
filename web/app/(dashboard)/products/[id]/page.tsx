"use client"

import { useEffect, useState } from "react"
import { useRouter, useParams } from "next/navigation"
import {
  getProduct,
  updateProduct,
  type Product,
  type CreateProductInput,
} from "@/lib/api/products"
import { ProductForm } from "@/components/product-form"

/**
 * Product detail / edit page.
 *
 * Story 2.1 TODO: replace devTenantId with tenantId from session.
 */
const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID

export default function ProductDetailPage() {
  const router = useRouter()
  const params = useParams()
  const id = params?.id as string

  const [product, setProduct] = useState<Product | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!id) return
    setLoading(true)
    getProduct(id, devTenantId)
      .then(setProduct)
      .catch((e) => setError(String(e)))
      .finally(() => setLoading(false))
  }, [id])

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

  if (error) {
    return (
      <div className="p-6">
        <div className="rounded-md bg-destructive/10 border border-destructive/30 px-4 py-3 text-sm text-destructive">
          {error}
        </div>
      </div>
    )
  }

  if (!product) {
    return (
      <div className="p-6 text-muted-foreground text-sm">商品不存在</div>
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
