"use client"

import { useEffect, useState } from "react"
import { getSupplier } from "@/lib/api/suppliers"
import type { CustomerContact } from "./use-customer-contact"

const EMPTY: CustomerContact = { contact: "", phone: "", address: "" }

/** Contact person, phone and address of the bill's supplier; empty until loaded, or if the lookup fails. */
export function useSupplierContact(partnerId?: string, tenantId?: string): { contact: CustomerContact; loaded: boolean } {
  const [state, setState] = useState<{ id?: string; contact: CustomerContact }>({ contact: EMPTY })

  useEffect(() => {
    if (!partnerId) return
    let cancelled = false
    Promise.resolve(getSupplier(partnerId, tenantId)).then(
      (s) => !cancelled && setState({ id: partnerId, contact: { contact: s?.contact ?? "", phone: s?.phone ?? "", address: s?.address ?? "" } }),
      () => !cancelled && setState({ id: partnerId, contact: EMPTY })
    )
    return () => {
      cancelled = true
    }
  }, [partnerId, tenantId])

  const loaded = !partnerId || state.id === partnerId
  return { contact: loaded && partnerId ? state.contact : EMPTY, loaded }
}
