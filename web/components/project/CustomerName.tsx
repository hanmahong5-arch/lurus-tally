"use client"

import { useEffect, useState } from "react"
import { getCustomer } from "@/lib/api/customers"

// 同一页的多张卡片常是同一个客户：按 id 共享一次请求；失败不缓存，下次渲染可重试。
const lookups = new Map<string, Promise<string>>()

function lookupName(customerId: string): Promise<string> {
  let p = lookups.get(customerId)
  if (!p) {
    p = getCustomer(customerId).then((c) => c.name)
    p.catch(() => lookups.delete(customerId))
    lookups.set(customerId, p)
  }
  return p
}

// 项目只存客户 id，这里按 id 取客户名显示；取不到时显示「—」，不露 id，也不下结论说客户不存在。
export default function CustomerName({ customerId }: { customerId: string }) {
  const [name, setName] = useState("")
  useEffect(() => {
    let cancelled = false
    setName("")
    lookupName(customerId)
      .then((n) => {
        if (!cancelled) setName(n)
      })
      .catch(() => {
        if (!cancelled) setName("—")
      })
    return () => {
      cancelled = true
    }
  }, [customerId])
  return <>{name || "…"}</>
}
