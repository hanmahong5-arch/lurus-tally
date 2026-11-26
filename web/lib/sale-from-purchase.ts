import type { BillLineItemInput } from "@/lib/api/purchase"

export function qtyText(raw: string): string {
  if (!/^\d+(\.\d+)?$/.test(raw)) return raw
  return raw.replace(/(\.\d*?)0+$/, "$1").replace(/\.$/, "")
}

export interface SaleFromPurchaseLine {
  product_id: string
  product_name?: string
  unit_id?: string
  unit_name?: string
  line_no: number
  qty: string
  unit_price: string
  source_item_id: string
}

export interface SaleFromPurchase {
  items: SaleFromPurchaseLine[]
  warehouseId: string
  note: string
}

interface PurchaseLike {
  head: { bill_no: string; partner_name?: string; warehouse_id?: string }
  items: {
    id: string
    product_id: string
    unit_id?: string
    unit_name?: string
    qty: string
    batch_remaining?: string
  }[]
}

/** Lines for a sale opened from an approved purchase. Selling price stays blank. */
export function linesFromPurchase(detail: PurchaseLike, names: Record<string, string>): SaleFromPurchase {
  const tracked = detail.items.some((it) => it.batch_remaining !== undefined)
  const who = detail.head.partner_name ? `${detail.head.partner_name} 的` : ""
  const items: SaleFromPurchaseLine[] = []
  for (const it of detail.items) {
    const qty = tracked ? qtyText(it.batch_remaining ?? "0") : qtyText(it.qty)
    if (!it.product_id || !(Number(qty) > 0)) continue
    items.push({
      product_id: it.product_id,
      product_name: names[it.product_id],
      unit_id: it.unit_id,
      unit_name: it.unit_name,
      line_no: items.length + 1,
      qty,
      unit_price: "",
      source_item_id: it.id,
    })
  }
  const note = tracked
    ? items.length > 0
      ? `来自${who}进货单 ${detail.head.bill_no}。数量是这一批还没卖掉的，审核后从这一批出库。卖价不会带进货价。`
      : `进货单 ${detail.head.bill_no} 上的货已经卖完了。`
    : `来自${who}进货单 ${detail.head.bill_no}。这张进货是开始记批次之前入库的，审核时从仓库里扣，对不上具体这一批。`
  return { items, warehouseId: detail.head.warehouse_id ?? "", note }
}

export function saleLinePayload(
  items: Pick<BillLineItemInput, "product_id" | "unit_id" | "unit_name" | "qty" | "unit_price" | "source_item_id">[],
  warehouseId: string,
  withWarehouse = false,
): BillLineItemInput[] {
  return items.map((it, idx) => ({
    product_id: it.product_id,
    unit_id: it.unit_id,
    unit_name: it.unit_name,
    line_no: idx + 1,
    qty: it.qty,
    unit_price: it.unit_price,
    source_item_id: it.source_item_id,
    warehouse_id: withWarehouse ? warehouseId || undefined : undefined,
  }))
}
