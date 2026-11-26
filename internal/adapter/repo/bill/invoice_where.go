package bill

import domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"

func appendInvoiceWhere(where []string, status, invoiceable string) []string {
	switch status {
	case domain.InvoiceNone:
		where = append(where, invoicedExpr+" <= 0 AND "+invoiceable+" > 0")
	case domain.InvoicePartial:
		where = append(where, invoicedExpr+" > 0 AND "+invoicedExpr+" < "+invoiceable)
	case domain.InvoiceFull:
		where = append(where, invoicedExpr+" >= "+invoiceable)
	case "open":
		where = append(where, invoicedExpr+" < "+invoiceable)
	}
	return where
}
