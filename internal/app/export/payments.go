package export

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
)

const paymentsRowLimit = 50_000

var paymentsHeader = []string{"单号", "收付款方", "金额", "方式", "时间"}

// PaymentsExportUseCase streams payment_head rows for a tenant as CSV.
type PaymentsExportUseCase struct {
	db  *sql.DB
	log *slog.Logger
}

// NewPaymentsExportUseCase creates a PaymentsExportUseCase.
func NewPaymentsExportUseCase(db *sql.DB, log *slog.Logger) *PaymentsExportUseCase {
	if log == nil {
		log = slog.Default()
	}
	return &PaymentsExportUseCase{db: db, log: log}
}

// Execute fetches payment rows for tenantID and writes CSV to w.
// Returns the number of data rows written (excluding the header).
func (uc *PaymentsExportUseCase) Execute(ctx context.Context, tenantID uuid.UUID, w io.Writer) (int, error) {
	const q = `
		SELECT
			COALESCE(bill_no, ''),
			COALESCE(partner_id::text, ''),
			amount,
			pay_type,
			pay_date
		FROM tally.payment_head
		WHERE tenant_id = $1 AND deleted_at IS NULL
		ORDER BY pay_date DESC
		LIMIT $2`

	rows, err := dbscope.From(ctx, uc.db).QueryContext(ctx, q, tenantID, paymentsRowLimit+1)
	if err != nil {
		return 0, fmt.Errorf("export payments: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	cw := csv.NewWriter(w)
	if err := cw.Write(paymentsHeader); err != nil {
		return 0, fmt.Errorf("export payments: write header: %w", err)
	}

	count := 0
	for rows.Next() {
		var billNo, partnerID, amount, payType string
		var payDate sql.NullTime
		if err := rows.Scan(&billNo, &partnerID, &amount, &payType, &payDate); err != nil {
			return count, fmt.Errorf("export payments: scan row: %w", err)
		}
		if count == paymentsRowLimit {
			uc.log.Warn("export payments: result truncated at row limit",
				slog.String("tenant_id", tenantID.String()),
				slog.Int("limit", paymentsRowLimit))
			if err := cw.Write([]string{"[截断]", fmt.Sprintf("数据超过 %d 行限制", paymentsRowLimit), "", "", ""}); err != nil {
				return count, fmt.Errorf("export payments: write truncation note: %w", err)
			}
			break
		}
		dateStr := ""
		if payDate.Valid {
			dateStr = payDate.Time.Format("2006-01-02 15:04:05")
		}
		if err := cw.Write([]string{billNo, partnerID, amount, payType, dateStr}); err != nil {
			return count, fmt.Errorf("export payments: write row: %w", err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("export payments: rows error: %w", err)
	}
	cw.Flush()
	return count, cw.Error()
}
