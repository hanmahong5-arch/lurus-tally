"use client"

import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import { listShops, bindShop, unbindShop, type ShopItem } from "@/lib/api/shopify"
import { listWarehouses, type WarehouseItem } from "@/lib/api/warehouses"
import { useConfirm } from "@/hooks/useConfirm"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { ApiError } from "@/lib/api/errors"

/**
 * ShopifyPage lets tenant admins self-service bind / unbind Shopify stores.
 *
 * Webhook URL to configure in Shopify Partner Dashboard:
 *   https://tally.lurus.cn/webhooks/shopify/orders
 * The signing secret must match SHOPIFY_WEBHOOK_SECRET in the backend env.
 */
export default function ShopifyPage() {
  const [shops, setShops] = useState<ShopItem[]>([])
  const [warehouses, setWarehouses] = useState<WarehouseItem[]>([])
  const [loading, setLoading] = useState(true)

  const [domain, setDomain] = useState("")
  const [warehouseId, setWarehouseId] = useState("")
  const [binding, setBinding] = useState(false)

  const confirm = useConfirm()

  const reload = useCallback((signal?: AbortSignal) => {
    setLoading(true)
    Promise.all([
      listShops(signal),
      listWarehouses({ limit: 200, signal }),
    ])
      .then(([shopRes, whRes]) => {
        setShops(shopRes.items ?? [])
        setWarehouses(whRes.items ?? [])
      })
      .catch((e) => {
        if (signal?.aborted) return
        toast.error("加载失败：" + String(e))
      })
      .finally(() => {
        setLoading(false)
      })
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    reload(controller.signal)
    return () => controller.abort()
  }, [reload])

  async function handleBind() {
    if (!domain.trim()) {
      toast.error("请输入店铺域名")
      return
    }
    if (!warehouseId) {
      toast.error("请选择仓库")
      return
    }
    setBinding(true)
    try {
      await bindShop({ shop_domain: domain.trim(), warehouse_id: warehouseId })
      toast.success("店铺绑定成功")
      setDomain("")
      setWarehouseId("")
      reload()
    } catch (e) {
      if (e instanceof ApiError) {
        if (e.status === 409) {
          toast.error("该店铺已被其他账户绑定，请联系 Tally 客服")
          return
        }
        if (e.status === 422) {
          toast.error("绑定失败：" + e.message)
          return
        }
      }
      toast.error("绑定失败：" + String(e))
    } finally {
      setBinding(false)
    }
  }

  async function handleUnbind(shop: ShopItem) {
    const ok = await confirm({
      title: "解绑 Shopify 店铺",
      body: `确认解绑「${shop.shop_domain}」？解绑后该店铺的 webhook 将不再入库。`,
      confirmText: "解绑",
      danger: true,
    })
    if (!ok) return
    try {
      await unbindShop(shop.id)
      toast.success("已解绑")
      reload()
    } catch (e) {
      toast.error("解绑失败：" + String(e))
    }
  }

  const warehouseMap = Object.fromEntries(warehouses.map((w) => [w.id, w.name]))

  return (
    <PageContainer width="wide">
      <PageHeader
        title="Shopify 店铺"
        subtitle="绑定 Shopify 店铺后，订单 webhook 将自动入库为销售单"
      />

      {/* Webhook 配置提示 */}
      <div className="mb-6 rounded-lg border border-border bg-muted/40 p-4 text-sm text-muted-foreground">
        <p className="mb-1 font-medium text-foreground">Shopify Webhook 配置说明</p>
        <ol className="list-decimal pl-5 space-y-1">
          <li>
            在 Shopify Partner Dashboard → 你的 App → Webhooks，将以下 URL 注册为{" "}
            <code className="rounded bg-muted px-1 py-0.5 font-mono text-xs">orders/create</code>{" "}
            事件：
          </li>
          <li>
            <code className="rounded bg-muted px-1 py-0.5 font-mono text-xs select-all">
              https://tally.lurus.cn/webhooks/shopify/orders
            </code>
          </li>
          <li>
            将 Shopify 生成的 Signing Secret 配置到后端环境变量{" "}
            <code className="rounded bg-muted px-1 py-0.5 font-mono text-xs">
              SHOPIFY_WEBHOOK_SECRET
            </code>
          </li>
        </ol>
      </div>

      {/* 绑定表单 */}
      <div className="mb-6 rounded-lg border border-border p-4">
        <h2 className="mb-4 text-sm font-semibold">绑定新店铺</h2>
        <div className="flex flex-col gap-4 sm:flex-row sm:items-end">
          <div className="flex flex-col gap-1.5 flex-1">
            <Label htmlFor="shop-domain">店铺域名</Label>
            <Input
              id="shop-domain"
              value={domain}
              onChange={(e) => setDomain(e.target.value)}
              placeholder="your-shop.myshopify.com"
              disabled={binding}
            />
          </div>
          <div className="flex flex-col gap-1.5 w-56">
            <Label htmlFor="warehouse-select">关联仓库</Label>
            <Select value={warehouseId} onValueChange={(v) => setWarehouseId(v ?? "")} disabled={binding || loading}>
              <SelectTrigger id="warehouse-select">
                <SelectValue placeholder="选择仓库" />
              </SelectTrigger>
              <SelectContent>
                {warehouses.map((w) => (
                  <SelectItem key={w.id} value={w.id}>
                    {w.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <Button
            onClick={() => void handleBind()}
            disabled={binding || loading}
            className="sm:self-end"
          >
            {binding ? "绑定中..." : "绑定"}
          </Button>
        </div>
      </div>

      {/* 已绑店铺列表 */}
      <div className="rounded-lg border border-border overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-muted/50">
            <tr>
              <th className="px-4 py-3 text-left font-medium text-muted-foreground">店铺域名</th>
              <th className="px-4 py-3 text-left font-medium text-muted-foreground">关联仓库</th>
              <th className="px-4 py-3 text-right font-medium text-muted-foreground">操作</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {loading && (
              <tr>
                <td colSpan={3} className="px-4 py-6 text-center text-muted-foreground">
                  加载中...
                </td>
              </tr>
            )}
            {!loading && shops.length === 0 && (
              <tr>
                <td colSpan={3} className="px-4 py-6 text-center text-muted-foreground">
                  暂无绑定店铺
                </td>
              </tr>
            )}
            {!loading &&
              shops.map((shop) => (
                <tr key={shop.id} className="hover:bg-muted/30 transition-colors">
                  <td className="px-4 py-3 font-mono text-xs">{shop.shop_domain}</td>
                  <td className="px-4 py-3 text-muted-foreground">
                    {warehouseMap[shop.warehouse_id] ?? shop.warehouse_id}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <button
                      type="button"
                      onClick={() => void handleUnbind(shop)}
                      className="text-xs text-destructive hover:underline"
                    >
                      解绑
                    </button>
                  </td>
                </tr>
              ))}
          </tbody>
        </table>
      </div>
    </PageContainer>
  )
}
