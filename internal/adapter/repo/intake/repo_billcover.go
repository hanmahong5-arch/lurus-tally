package intake

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	domainbill "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/intake"
)

// remainingSQL is what a bill can still take on an invoice. It expects the
// sale-returns CTE joined as rsum r, the same returns the bill list counts.
var remainingSQL = fmt.Sprintf(
	`GREATEST(TRUNC(GREATEST(b.total_amount - CASE WHEN b.sub_type = '%s' THEN COALESCE(r.amt, 0) ELSE 0 END, 0), 2) - %s, 0)`,
	domainbill.BillSubTypeSale, liveInvoicedSQL,
)

func saleReturnsQuery(body string) string {
	return "WITH" + repobill.SaleReturnsCTE("") + body
}

func scanOpenBills(rows *sql.Rows) ([]domain.OpenBill, error) {
	var out []domain.OpenBill
	for rows.Next() {
		var id uuid.UUID
		var no, day, total, remaining string
		var status int16
		if err := rows.Scan(&id, &no, &status, &day, &total, &remaining); err != nil {
			return nil, err
		}
		out = append(out, openBill(id, no, status, day, total, remaining))
	}
	return out, rows.Err()
}

func openBill(id uuid.UUID, no string, status int16, day, total, remaining string) domain.OpenBill {
	b := domain.OpenBill{ID: id, BillNo: no, Status: status, BillDate: day}
	b.Total, _ = decimal.NewFromString(total)
	b.Remaining, _ = decimal.NewFromString(remaining)
	return b
}
