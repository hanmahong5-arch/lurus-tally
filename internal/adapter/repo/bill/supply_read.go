package bill

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

func (r *Repo) PurchaseLine(ctx context.Context, tx *sql.Tx, tenantID, itemID uuid.UUID) (domain.PurchaseLine, error) {
	const q = `
		SELECT i.head_id, i.product_id, h.status, h.sub_type
		FROM tally.bill_item i
		JOIN tally.bill_head h ON h.id = i.head_id AND h.tenant_id = i.tenant_id
		WHERE i.id = $1 AND i.tenant_id = $2 AND h.deleted_at IS NULL`
	var line domain.PurchaseLine
	var status int16
	var sub string
	err := tx.QueryRowContext(ctx, q, itemID, tenantID).Scan(&line.HeadID, &line.ProductID, &status, &sub)
	if err != nil {
		return domain.PurchaseLine{}, err
	}
	line.Status = domain.BillStatus(status)
	line.SubType = domain.BillSubType(sub)
	return line, nil
}

func (r *Repo) ClearLinkBill(ctx context.Context, tx *sql.Tx, tenantID, billID uuid.UUID) error {
	_, err := tx.ExecContext(ctx, `UPDATE tally.bill_head SET link_bill_id = NULL WHERE id = $1 AND tenant_id = $2`, billID, tenantID)
	if err != nil {
		return fmt.Errorf("bill repo: clear link bill: %w", err)
	}
	return nil
}

func (r *Repo) OpenedSales(ctx context.Context, tenantID, purchaseID uuid.UUID) ([]domain.SupplyBill, error) {
	const q = `
		SELECT h.id, h.bill_no, h.status, COALESCE(p.name, '')
		FROM tally.bill_head h
		LEFT JOIN tally.partner p ON p.id = h.partner_id AND p.tenant_id = h.tenant_id
		WHERE h.tenant_id = $1 AND h.link_bill_id = $2 AND h.sub_type = '销售' AND h.deleted_at IS NULL
		ORDER BY h.bill_date DESC, h.created_at DESC
		LIMIT 20`
	return r.supplyBills(ctx, q, tenantID, purchaseID)
}

func (r *Repo) SourcePurchase(ctx context.Context, tenantID, saleID uuid.UUID) (*domain.SupplyBill, error) {
	const q = `
		SELECT p.id, p.bill_no, p.status, COALESCE(n.name, '')
		FROM tally.bill_head s
		JOIN tally.bill_head p ON p.id = s.link_bill_id AND p.tenant_id = s.tenant_id
		LEFT JOIN tally.partner n ON n.id = p.partner_id AND n.tenant_id = p.tenant_id
		WHERE s.id = $2 AND s.tenant_id = $1 AND p.sub_type = '采购' AND p.deleted_at IS NULL`
	rows, err := r.supplyBills(ctx, q, tenantID, saleID)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

func (r *Repo) supplyBills(ctx context.Context, q string, args ...any) ([]domain.SupplyBill, error) {
	rows, err := dbscope.From(ctx, r.db).QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("bill repo: supply bills: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.SupplyBill
	for rows.Next() {
		var b domain.SupplyBill
		if err := rows.Scan(&b.ID, &b.BillNo, &b.Status, &b.PartnerName); err != nil {
			return nil, fmt.Errorf("bill repo: supply bills: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bill repo: supply bills: %w", err)
	}
	return out, nil
}

func (r *Repo) BatchRemaining(ctx context.Context, tenantID uuid.UUID, itemIDs []uuid.UUID) (map[uuid.UUID]decimal.Decimal, error) {
	out := map[uuid.UUID]decimal.Decimal{}
	if len(itemIDs) == 0 {
		return out, nil
	}
	const q = `
		SELECT source_item_id, qty_remaining::text, conv_factor::text
		FROM tally.receipt_batch
		WHERE tenant_id = $1 AND source_item_id = ANY($2::uuid[])`
	ids := make([]string, len(itemIDs))
	for i, id := range itemIDs {
		ids[i] = id.String()
	}
	rows, err := dbscope.From(ctx, r.db).QueryContext(ctx, q, tenantID, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("bill repo: batch remaining: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id uuid.UUID
		var qty, factor string
		if err := rows.Scan(&id, &qty, &factor); err != nil {
			return nil, fmt.Errorf("bill repo: batch remaining: %w", err)
		}
		left, err := decimalutil.Parse(qty, "qty_remaining")
		if err != nil {
			return nil, err
		}
		fac, err := decimalutil.Parse(factor, "conv_factor")
		if err != nil {
			return nil, err
		}
		if !fac.IsPositive() {
			fac = decimal.NewFromInt(1)
		}
		out[id] = left.Div(fac)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bill repo: batch remaining: %w", err)
	}
	return out, nil
}
