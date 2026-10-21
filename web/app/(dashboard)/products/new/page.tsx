"use client"

import { useRouter } from "next/navigation"
import { ProductForm } from "@/components/product-form"
import { createProduct, type CreateProductInput } from "@/lib/api/products"
import { useDraft } from "@/hooks/useDraft"
import { useTenantId } from "@/hooks/use-tenant-id"
import { useRole } from "@/hooks/use-role"
import { EmptyState } from "@/components/ui/empty-state"
import { DraftBadge } from "@/components/draft/DraftBadge"
import { DraftRestoreToast } from "@/components/draft/DraftRestoreToast"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"

/**
 * New product page.
 */
const PRODUCT_INITIAL: Partial<CreateProductInput> = {}

export default function NewProductPage() {
  const router = useRouter()
  const tenantId = useTenantId()
  const { known, can } = useRole()

  const draft = useDraft<Partial<CreateProductInput>>("draft:product:new", PRODUCT_INITIAL)

  async function handleSubmit(input: CreateProductInput) {
    await createProduct(input, tenantId)
    await draft.markSubmitted()
    router.push("/products")
    router.refresh()
  }

  if (known && !can("product.write")) {
    return (
      <PageContainer width="narrow">
        <PageHeader title="新建商品" />
        <EmptyState title="你没有新建商品的权限" description="商品只有老板、店长、店员可以新建，请找老板或店长" />
      </PageContainer>
    )
  }

  return (
    <PageContainer width="narrow">
      <PageHeader
        title={
          <span className="flex items-center gap-3">
            新建商品
            <DraftBadge status={draft.status} />
          </span>
        }
        subtitle="填写基本信息并保存"
      />

      <DraftRestoreToast restoredAt={draft.restoredAt} onDiscard={draft.discardDraft} />

      <ProductForm
        initial={draft.value}
        onSubmit={handleSubmit}
        onCancel={() => router.back()}
        tenantId={tenantId}
        onChange={draft.setValue}
      />
    </PageContainer>
  )
}
