"use client"

import { useRouter } from "next/navigation"
import { ProductForm } from "@/components/product-form"
import { createProduct, type CreateProductInput } from "@/lib/api/products"

/**
 * New product page.
 *
 * Story 2.1 TODO: replace devTenantId with tenantId from session.
 */
const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID

export default function NewProductPage() {
  const router = useRouter()

  async function handleSubmit(input: CreateProductInput) {
    await createProduct(input, devTenantId)
    router.push("/products")
    router.refresh()
  }

  return (
    <div className="p-6 max-w-2xl">
      <div className="mb-6">
        <h1 className="text-xl font-semibold">新建商品</h1>
        <p className="text-sm text-muted-foreground mt-0.5">
          填写基本信息并保存
        </p>
      </div>
      <ProductForm
        onSubmit={handleSubmit}
        onCancel={() => router.back()}
        tenantId={devTenantId}
      />
    </div>
  )
}
