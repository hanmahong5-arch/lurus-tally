import { redirect } from "next/navigation"

// /finance currently has only one sub-route. Redirect so the sidebar/dashboard
// link works regardless of whether they target /finance or /finance/exchange-rates,
// and a future direct visitor (typed URL, bookmark) lands on something useful.
export default function FinanceIndex(): never {
  redirect("/finance/exchange-rates")
}
