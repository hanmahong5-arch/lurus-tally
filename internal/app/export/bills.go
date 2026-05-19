// Package export provides read-only use cases that stream tenant data as CSV.
// Each use case writes directly to an io.Writer so memory stays bounded
// regardless of tenant data size.
package export

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"

	"github.com/google/uuid"
)

// billsRowLimit caps the number of rows fetched per export to avoid OOM on very
// large tenants. A trailer row is appended when the actual row count equals the
// cap so the operator knows the file is truncated.
const billsRowLimit = 50_000

// billsHeader is the CSV column header row for the bills export.
// Column order matches the query projection below.
var billsHeader = []string{"单号", "类型", "状态", "日期", "合作方", "仓库", "总额", "已付", "备注"}

// BillsExportUseCase streams all bill_head rows for a tenant as CSV.
type BillsExportUseCase struct {
	db  *sql.DB
	log *slog.Logger
}

// NewBillsExportUseCase creates a BillsExportUseCase.
func NewBillsExportUseCase(db *sql.DB, log *slog.Logger) *BillsExportUseCase {
	if log == nil {
		log = slog.Default()
	}
	return &BillsExportUseCase{db: db, log: log}
}

// Execute fetches bill rows for tenantID and writes CSV to w.
// Returns the number of data rows written (excluding the header).
func (uc *BillsExportUseCase) Execute(ctx context.Context, tenantID uuid.UUID, w io.Writer) (int, error) {
	const q = `
		SELECT
			bill_no,
			bill_type,
			status,
			bill_date,
			COALESCE(partner_id::text, ''),
			COALESCE(warehouse_id::text, ''),
			total_amount,
			paid_amount,
			COALESCE(remark, '')
		FROM tally.bill_head
		WHERE tenant_id = $1 AND deleted_at IS NULL
		ORDER BY bill_date DESC
		LIMIT $2`

	rows, err := uc.db.QueryContext(ctx, q, tenantID, billsRowLimit+1)
	if err != nil {
		return 0, fmt.Errorf("export bills: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	cw := csv.NewWriter(w)
	if err := cw.Write(billsHeader); err != nil {
		return 0, fmt.Errorf("export bills: write header: %w", err)
	}

	count := 0
	for rows.Next() {
		var billNo, billType, remark, partnerID, warehouseID string
		var status int16
		var billDate sql.NullTime
		var totalAmount, paidAmount string

		if err := rows.Scan(&billNo, &billType, &status, &billDate, &partnerID, &warehouseID, &totalAmount, &paidAmount, &remark); err != nil {
			return count, fmt.Errorf("export bills: scan row: %w", err)
		}
		if count == billsRowLimit {
			// Extra row fetched confirms truncation — emit trailer and stop.
			uc.log.Warn("export bills: result truncated at row limit",
				slog.String("tenant_id", tenantID.String()),
				slog.Int("limit", billsRowLimit))
			if err := cw.Write([]string{"[截断]", fmt.Sprintf("数据超过 %d 行限制，请联系管理员导出完整数据", billsRowLimit), "", "", "", "", "", "", ""}); err != nil {
				return count, fmt.Errorf("export bills: write truncation note: %w", err)
			}
			break
		}

		dateStr := ""
		if billDate.Valid {
			dateStr = billDate.Time.Format("2006-01-02")
		}
		if err := cw.Write([]string{
			billNo,
			billType,
			statusLabel(status),
			dateStr,
			partnerID,
			warehouseID,
			totalAmount,
			paidAmount,
			remark,
		}); err != nil {
			return count, fmt.Errorf("export bills: write row: %w", err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("export bills: rows error: %w", err)
	}
	cw.Flush()
	return count, cw.Error()
}

// statusLabel converts a bill status integer to a readable Chinese label.
func statusLabel(s int16) string {
	switch s {
	case 0:
		return "草稿"
	case 2:
		return "已审核"
	case 9:
		return "已取消"
	default:
		return fmt.Sprintf("%d", s)
	}
}
