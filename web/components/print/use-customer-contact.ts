"use client"

import { useEffect, useState } from "react"
import { getCustomer } from "@/lib/api/customers"

export interface CustomerContact {
  contact: string
  phone: string
  address: string
}

/** Contact person, phone and delivery address of the bill's customer; empty until loaded, or if the lookup fails. */
export function useCustomerContact(partnerId?: string, tenantId?: string): { contact: CustomerContact; loaded: boolean } {
  const [state, setState] = useState<{ id?: string; contact: CustomerContact }>({ contact: { contact: "", phone: "", address: "" } })

  useEffect(() => {
    if (!partnerId) return
    let cancelled = false
    Promise.resolve(getCustomer(partnerId, tenantId)).then(
      (c) => {
        if (cancelled) return
        setState({
          id: partnerId,
          contact: { contact: c?.contact_name ?? "", phone: c?.mobile || c?.phone || "", address: c?.address ?? "" },
        })
      },
      () => !cancelled && setState({ id: partnerId, contact: { contact: "", phone: "", address: "" } })
    )
    return () => {
      cancelled = true
    }
  }, [partnerId, tenantId])

  const loaded = !partnerId || state.id === partnerId
  return { contact: loaded && partnerId ? state.contact : { contact: "", phone: "", address: "" }, loaded }
}
