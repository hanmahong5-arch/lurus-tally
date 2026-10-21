package export

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
)

const paymentsRowLimit = 50_000

var paymentsHeader = []string{"单号", "收付款方", "金额", "方式", "时间", "收/付", "是否计入实收", "关联单据类型"}

// payTypeLabels maps pay_type codes to the Chinese names shown to shop staff.
var payTypeLabels = map[string]string{
	"cash":     "现金",
	"wechat":   "微信",
	"alipay":   "支付宝",
	"card":     "银行卡",
	"credit":   "赊账",
	"transfer": "转账",
}

// payTypeLabel returns the Chinese label for a pay_type code; unknown codes are
// returned unchanged so legacy data is never hidden.
func payTypeLabel(code string) string {
	if l, ok := payTypeLabels[code]; ok {
		return l
	}
	return code
}

// directionLabel names the side of the shop a payment sits on; "" for a payment with no bill.
func directionLabel(d string) string {
	switch d {
	case "in":
		return "收"
	case "out":
		return "付"
	}
	return ""
}

// moneyCell shows an amount to the cent. The monthly report rounds each payment the same way
// before summing, so the CSV rows add up to its cards exactly.
func moneyCell(s string) string {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return s
	}
	return d.StringFixed(2)
}

func yesNo(b bool) string {
	if b {
		return "是"
	}
	return "否"
}

// PaymentsExportUseCase streams payment_head rows for a tenant as CSV.
type PaymentsExportUseCase struct {
	db       *sql.DB
	log      *slog.Logger
	rowLimit int
}

// NewPaymentsExportUseCase creates a PaymentsExportUseCase. Pass WithRowLimit to
// override the default cap (paymentsRowLimit).
func NewPaymentsExportUseCase(db *sql.DB, log *slog.Logger, opts ...Option) *PaymentsExportUseCase {
	if log == nil {
		log = slog.Default()
	}
	o := resolve(paymentsRowLimit, opts)
	return &PaymentsExportUseCase{db: db, log: log, rowLimit: o.rowLimit}
}

// Execute fetches payment rows for tenantID and writes CSV to w.
// Returns the number of data rows written (excluding the header).
func (uc *PaymentsExportUseCase) Execute(ctx context.Context, tenantID uuid.UUID, w io.Writer) (int, error) {
	return uc.ExecuteRange(ctx, tenantID, nil, nil, w)
}

// PaymentsFilter narrows the payments CSV. Direction is "in" (sales side: sales and sales
// returns), "out" (purchases side) or "" for both; CountedOnly keeps only the rows the
// monthly report counts as money actually moved.
type PaymentsFilter struct {
	From, To    *time.Time
	Direction   string
	CountedOnly bool
}

// ExecuteRange is Execute narrowed to pay_date in [from, to); a nil bound is open.
func (uc *PaymentsExportUseCase) ExecuteRange(ctx context.Context, tenantID uuid.UUID, from, to *time.Time, w io.Writer) (int, error) {
	return uc.ExecuteFiltered(ctx, tenantID, PaymentsFilter{From: from, To: to}, w)
}

// Both sides of the shop, as bill sub types. A payment hung on a return bill flows the other
// way, so its amount is negated and nets off against the same side; this is what makes the
// "in" rows add up to the monthly receipts and the "out" rows to the monthly payments.
const (
	inSideSubTypes     = `'销售', '销售退货'`
	outSideSubTypes    = `'采购', '采购退货'`
	countedSubTypesSQL = inSideSubTypes + `, ` + outSideSubTypes
)

// ExecuteFiltered writes the payments CSV for tenantID narrowed by f.
func (uc *PaymentsExportUseCase) ExecuteFiltered(ctx context.Context, tenantID uuid.UUID, f PaymentsFilter, w io.Writer) (int, error) {
	args := []any{tenantID}
	where := ""
	if f.From != nil {
		args = append(args, *f.From)
		where += fmt.Sprintf(" AND ph.pay_date >= $%d", len(args))
	}
	if f.To != nil {
		args = append(args, *f.To)
		where += fmt.Sprintf(" AND ph.pay_date < $%d", len(args))
	}
	switch f.Direction {
	case "":
	case "in":
		where += " AND bh.sub_type IN (" + inSideSubTypes + ")"
	case "out":
		where += " AND bh.sub_type IN (" + outSideSubTypes + ")"
	default:
		return 0, fmt.Errorf("export payments: unknown direction %q", f.Direction)
	}
	if f.CountedOnly {
		where += " AND ph.pay_type <> 'credit' AND bh.sub_type IN (" + countedSubTypesSQL + ")"
	}
	args = append(args, uc.rowLimit+1)
	q := `
		SELECT
			COALESCE(bh.bill_no, ph.bill_no, ''),
			COALESCE(p.name, ''),
			ROUND(CASE WHEN bh.sub_type IN ('销售退货', '采购退货') THEN -ph.amount ELSE ph.amount END, 2),
			ph.pay_type,
			ph.pay_date,
			CASE WHEN bh.sub_type IN (` + inSideSubTypes + `) THEN 'in'
			     WHEN bh.sub_type IN (` + outSideSubTypes + `) THEN 'out' ELSE '' END,
			COALESCE(ph.pay_type <> 'credit' AND bh.sub_type IN (` + countedSubTypesSQL + `), false),
			COALESCE(bh.sub_type, '')
		FROM tally.payment_head ph
		LEFT JOIN tally.bill_head bh ON bh.id = ph.related_bill_id AND bh.tenant_id = ph.tenant_id
		LEFT JOIN tally.partner p ON p.id = ph.partner_id AND p.tenant_id = ph.tenant_id
		WHERE ph.tenant_id = $1 AND ph.deleted_at IS NULL` + where + fmt.Sprintf(`
		ORDER BY ph.pay_date DESC
		LIMIT $%d`, len(args))

	rows, err := dbscope.From(ctx, uc.db).QueryContext(ctx, q, args...)
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
		var billNo, partnerName, amount, payType, direction, subType string
		var payDate sql.NullTime
		var counted bool
		if err := rows.Scan(&billNo, &partnerName, &amount, &payType, &payDate, &direction, &counted, &subType); err != nil {
			return count, fmt.Errorf("export payments: scan row: %w", err)
		}
		if count == uc.rowLimit {
			uc.log.Warn("export payments: result truncated at row limit",
				slog.String("tenant_id", tenantID.String()),
				slog.Int("limit", uc.rowLimit))
			if err := cw.Write([]string{"[截断]", fmt.Sprintf("数据超过 %d 行限制", uc.rowLimit), "", "", "", "", "", ""}); err != nil {
				return count, fmt.Errorf("export payments: write truncation note: %w", err)
			}
			break
		}
		if err := cw.Write([]string{billNo, partnerName, moneyCell(amount), payTypeLabel(payType), formatNullDate(inShanghai(payDate), dateTimeLayout), directionLabel(direction), yesNo(counted), subType}); err != nil {
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
