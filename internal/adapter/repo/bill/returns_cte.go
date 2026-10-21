package bill

// SaleReturnsCTE exposes the per-sale returns CTE (rt / rsum) to read models outside
// this package, such as the bills CSV export, so they net returns off the sale total by
// the same rule as the bill list and the receivables ageing. The caller prefixes WITH,
// binds $1 to the tenant and joins `rsum r ON r.sale_id = <sale id>`.
func SaleReturnsCTE(extra string) string { return saleReturnsCTE(extra) }
