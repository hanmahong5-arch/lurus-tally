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
	"strings"
	"time"

	"github.com/google/uuid"
	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	billdomain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// billsRowLimit caps the number of rows fetched per export to avoid OOM on very
// large tenants. A trailer row is appended when the actual row count equals the
// cap so the operator knows the file is truncated.
const billsRowLimit = 50_000

// BusinessTZ is the business time zone. Bill filters cut day boundaries here, so
// exported dates must be rendered in the same zone or monthly reconciliation
// drifts by a day for bills created between 00:00 and 08:00 local time.
var BusinessTZ = time.FixedZone("Asia/Shanghai", 8*3600)

// inShanghai returns t converted to the business time zone (NULL stays NULL).
func inShanghai(t sql.NullTime) sql.NullTime {
	if t.Valid {
		t.Time = t.Time.In(BusinessTZ)
	}
	return t
}

// billsHeader is the CSV column header row for the bills export.
// Column order matches the query projection below. The type column is the bill's sub type
// (采购 / 销售 / 销售退货 / 采购退货), so returns are told apart from the bills they undo.
// A return has no unpaid amount of its own, so those two columns say 退货单 instead.
var billsHeader = []string{"单号", "类型", "状态", "日期", "合作方", "仓库", "总额", "已付", "备注", "未收付金额", "收付款状态"}

// BillsExportUseCase streams all bill_head rows for a tenant as CSV.
type BillsExportUseCase struct {
	db       *sql.DB
	log      *slog.Logger
	rowLimit int
}

// NewBillsExportUseCase creates a BillsExportUseCase. Pass WithRowLimit to
// override the default cap (billsRowLimit).
func NewBillsExportUseCase(db *sql.DB, log *slog.Logger, opts ...Option) *BillsExportUseCase {
	if log == nil {
		log = slog.Default()
	}
	o := resolve(billsRowLimit, opts)
	return &BillsExportUseCase{db: db, log: log, rowLimit: o.rowLimit}
}

// BillsFilter narrows a bills export. The zero value exports every bill.
// DateFrom is inclusive and DateTo exclusive (callers pass the start of the day
// after the last wanted day); both are compared against bill_date.
type BillsFilter struct {
	Type          string // "purchase" | "sale" | ""
	Status        *int16
	PartnerID     *uuid.UUID
	DateFrom      *time.Time
	DateTo        *time.Time
	PaymentStatus string // "open" (unpaid + partial) | "paid" | "partial" | "unpaid" | ""
	InvoiceStatus string // "none" | "partial" | "full" | "" (same rule as the bill list)
	Q             string // bill_no or partner name substring
	CreatorID     *uuid.UUID
}

// Execute fetches every bill row for tenantID and writes CSV to w.
// Returns the number of data rows written (excluding the header).
func (uc *BillsExportUseCase) Execute(ctx context.Context, tenantID uuid.UUID, w io.Writer) (int, error) {
	return uc.ExecuteFiltered(ctx, tenantID, BillsFilter{}, w)
}

// escapeLike escapes LIKE wildcards so user input matches literally.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// billsInvoicedExpr is the amount of a bill covered by live invoices (issued,
// non-deleted; void and red-struck invoices do not count). Mirrors the bill list
// repo so the export honours the same invoice_status filter.
const billsInvoicedExpr = `COALESCE((SELECT SUM(ib.amount) FROM tally.invoice_bill ib
		JOIN tally.invoice iv ON iv.id = ib.invoice_id AND iv.tenant_id = ib.tenant_id
		WHERE ib.bill_id = bh.id AND ib.tenant_id = bh.tenant_id AND iv.status = 'issued' AND iv.deleted_at IS NULL), 0)`

// billsNetTotalExpr is the bill total after approved sales returns (floored at zero),
// the balance the bill list and the receivables ageing compare payments against. It
// relies on the `rsum r` join of ExecuteFiltered; purchases never match it, so they keep
// their plain total.
const billsNetTotalExpr = "GREATEST(bh.total_amount - COALESCE(r.amt, 0), 0)"

// buildBillsWhere returns the extra WHERE conditions and their args, numbering
// placeholders from $2 ($1 is the tenant).
func buildBillsWhere(f BillsFilter) (string, []any) {
	var conds []string
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, strings.Replace(cond, "?", fmt.Sprintf("$%d", len(args)+1), 1))
	}
	switch f.Type {
	case "purchase":
		add("bh.bill_type = ?", string(billdomain.BillTypePurchase))
		add("bh.sub_type = ?", string(billdomain.BillSubTypePurchase))
	case "sale":
		add("bh.bill_type = ?", string(billdomain.BillTypeSale))
		add("bh.sub_type = ?", string(billdomain.BillSubTypeSale))
	}
	if f.Status != nil {
		add("bh.status = ?", *f.Status)
	}
	if f.PartnerID != nil {
		add("bh.partner_id = ?", *f.PartnerID)
	}
	if f.DateFrom != nil {
		add("bh.bill_date >= ?", *f.DateFrom)
	}
	if f.DateTo != nil {
		add("bh.bill_date < ?", *f.DateTo)
	}
	if f.CreatorID != nil {
		add("bh.creator_id = ?", *f.CreatorID)
	}
	if f.Q != "" {
		args = append(args, "%"+escapeLike(f.Q)+"%")
		n := len(args) + 1
		conds = append(conds, fmt.Sprintf("(bh.bill_no ILIKE $%d OR p.name ILIKE $%d)", n, n))
	}
	switch f.PaymentStatus {
	case "paid":
		conds = append(conds, "bh.status = 2 AND bh.paid_amount > 0 AND bh.paid_amount >= "+billsNetTotalExpr)
	case "unpaid":
		conds = append(conds, "bh.status = 2 AND bh.paid_amount <= 0")
	case "partial":
		conds = append(conds, "bh.status = 2 AND bh.paid_amount > 0 AND bh.paid_amount < "+billsNetTotalExpr)
	case "open":
		conds = append(conds, "bh.status = 2 AND "+billsNetTotalExpr+" > bh.paid_amount")
	}
	switch f.InvoiceStatus {
	case billdomain.InvoiceNone:
		conds = append(conds, "bh.status = 2 AND "+billsInvoicedExpr+" <= 0")
	case billdomain.InvoicePartial:
		conds = append(conds, "bh.status = 2 AND "+billsInvoicedExpr+" > 0 AND "+billsInvoicedExpr+" < TRUNC(bh.total_amount, 2)")
	case billdomain.InvoiceFull:
		conds = append(conds, "bh.status = 2 AND "+billsInvoicedExpr+" > 0 AND "+billsInvoicedExpr+" >= TRUNC(bh.total_amount, 2)")
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " AND " + strings.Join(conds, " AND "), args
}

// ExecuteFiltered writes the bills matching f as CSV to w, newest first.
// Returns the number of data rows written (excluding the header).
func (uc *BillsExportUseCase) ExecuteFiltered(ctx context.Context, tenantID uuid.UUID, f BillsFilter, w io.Writer) (int, error) {
	where, fargs := buildBillsWhere(f)
	q := "WITH" + repobill.SaleReturnsCTE("") + `
		SELECT
			bh.bill_no,
			COALESCE(NULLIF(bh.sub_type, ''), bh.bill_type),
			bh.status,
			bh.bill_date,
			COALESCE(p.name, ''),
			COALESCE(wh.name, ''),
			bh.total_amount,
			bh.paid_amount,
			COALESCE(bh.remark, ''),
			CASE WHEN bh.status = 2 AND bh.sub_type NOT IN ('销售退货', '采购退货') THEN GREATEST(` + billsNetTotalExpr + ` - bh.paid_amount, 0)::text ELSE '' END,
			CASE
				WHEN bh.status <> 2 THEN ''
				WHEN bh.sub_type IN ('销售退货', '采购退货') THEN '退货单'
				WHEN bh.paid_amount <= 0 THEN '未付'
				WHEN bh.paid_amount < ` + billsNetTotalExpr + ` THEN '部分'
				ELSE '已付清'
			END
		FROM tally.bill_head bh
		LEFT JOIN tally.partner p ON p.id = bh.partner_id AND p.tenant_id = bh.tenant_id
		LEFT JOIN tally.warehouse wh ON wh.id = bh.warehouse_id AND wh.tenant_id = bh.tenant_id
		LEFT JOIN rsum r ON r.sale_id = bh.id
		WHERE bh.tenant_id = $1 AND bh.deleted_at IS NULL` + where + fmt.Sprintf(`
		ORDER BY bh.bill_date DESC, bh.created_at DESC
		LIMIT $%d`, len(fargs)+2)
	args := append([]any{tenantID}, fargs...)
	args = append(args, uc.rowLimit+1)

	rows, err := dbscope.From(ctx, uc.db).QueryContext(ctx, q, args...)
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
		var billNo, billType, remark, partnerName, warehouseName, unpaid, payState string
		var status int16
		var billDate sql.NullTime
		var totalAmount, paidAmount string

		if err := rows.Scan(&billNo, &billType, &status, &billDate, &partnerName, &warehouseName, &totalAmount, &paidAmount, &remark, &unpaid, &payState); err != nil {
			return count, fmt.Errorf("export bills: scan row: %w", err)
		}
		if count == uc.rowLimit {
			// Extra row fetched confirms truncation — emit trailer and stop.
			uc.log.Warn("export bills: result truncated at row limit",
				slog.String("tenant_id", tenantID.String()),
				slog.Int("limit", uc.rowLimit))
			if err := cw.Write([]string{"[截断]", fmt.Sprintf("数据超过 %d 行限制，请联系管理员导出完整数据", uc.rowLimit), "", "", "", "", "", "", "", "", ""}); err != nil {
				return count, fmt.Errorf("export bills: write truncation note: %w", err)
			}
			break
		}

		if err := cw.Write([]string{
			billNo,
			billType,
			statusLabel(status),
			formatNullDate(inShanghai(billDate), dateLayout),
			partnerName,
			warehouseName,
			totalAmount,
			paidAmount,
			remark,
			unpaid,
			payState,
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
