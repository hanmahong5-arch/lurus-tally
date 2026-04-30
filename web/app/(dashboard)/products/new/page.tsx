"use client"

import { useRouter } from "next/navigation"
import { ProductForm } from "@/components/product-form"
import { createProduct, type CreateProductInput } from "@/lib/api/products"
import { useDraft } from "@/hooks/useDraft"
import { DraftBadge } from "@/components/draft/DraftBadge"
import { DraftRestoreToast } from "@/components/draft/DraftRestoreToast"

/**
 * New product page.
 *
 * Story 2.1 TODO: replace devTenantId with tenantId from session.
 */
const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID

const PRODUCT_INITIAL: Partial<CreateProductInput> = {}

export default function NewProductPage() {
  const router = useRouter()

  const draft = useDraft<Partial<CreateProductInput>>(
    "draft:product:new",
    PRODUCT_INITIAL
  )

  async function handleSubmit(input: CreateProductInput) {
    await createProduct(input, devTenantId)
    await draft.markSubmitted()
    router.push("/products")
    router.refresh()
  }

  return (
    <div className="p-6 max-w-2xl">
      <div className="mb-6">
        <div className="flex items-center gap-3">
          <h1 className="text-xl font-semibold">新建商品</h1>
          <DraftBadge status={draft.status} />
        </div>
        <p className="text-sm text-muted-foreground mt-0.5">
          填写基本信息并保存
        </p>
      </div>

      <DraftRestoreToast
        restoredAt={draft.restoredAt}
        onDiscard={draft.discardDraft}
      />

      <ProductForm
        initial={draft.value}
        onSubmit={handleSubmit}
        onCancel={() => router.back()}
        tenantId={devTenantId}
        onChange={draft.setValue}
      />
    </div>
  )
}
