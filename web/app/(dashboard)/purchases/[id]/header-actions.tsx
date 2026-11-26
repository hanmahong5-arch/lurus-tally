"use client"

import type { ReactNode } from "react"

import { Button } from "@/components/ui/button"

export function PurchaseHeaderActions(props: {
  priceBlind: boolean
  isDraft: boolean
  isApproved: boolean
  isReturnDoc: boolean
  mayEdit: boolean
  mayCancel: boolean
  mayApprove: boolean
  canReturn: boolean
  acting: boolean
  returning: boolean
  printButton: ReactNode
  copyButton: ReactNode
  onEdit: () => void
  onCancel: () => void
  onApprove: () => void
  onReturn: () => void
  onOpenSale: () => void
}) {
  const {
    priceBlind, isDraft, isApproved, isReturnDoc, mayEdit, mayCancel, mayApprove, canReturn,
    acting, returning, printButton, copyButton, onEdit, onCancel, onApprove, onReturn, onOpenSale,
  } = props
  if (priceBlind) {
    const edit = isDraft && mayEdit ? (
      <Button variant="outline" disabled={acting} onClick={onEdit}>
        修改
      </Button>
    ) : null
    const sell = isApproved && !isReturnDoc && mayEdit ? (
      <Button variant="outline" onClick={onOpenSale}>
        开销售单
      </Button>
    ) : null
    if (!edit && !sell) return null
    return <>{edit}{sell}</>
  }
  if (isReturnDoc) return <>{printButton}</>
  if (canReturn) {
    return (
      <>
        {printButton}
        {copyButton}
        <Button variant="outline" disabled={acting || returning} onClick={onReturn}>
          退货
        </Button>
      </>
    )
  }
  if (isDraft) {
    return (
      <>
        {printButton}
        {copyButton}
        {mayEdit && (
          <Button variant="outline" disabled={acting} onClick={onEdit}>
            修改
          </Button>
        )}
        {mayCancel && (
          <Button variant="outline" disabled={acting} onClick={onCancel}>
            取消单据
          </Button>
        )}
        {mayApprove && (
          <Button disabled={acting} onClick={onApprove}>
            审核通过
          </Button>
        )}
      </>
    )
  }
  return (
    <>
      {printButton}
      {copyButton}
      {mayEdit && (
        <Button variant="outline" onClick={onOpenSale}>
          开销售单
        </Button>
      )}
    </>
  )
}
