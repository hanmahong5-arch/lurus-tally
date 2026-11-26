package bill

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

func (r *Repo) Open(ctx context.Context, tx *sql.Tx, tenant, warehouse uuid.UUID, lines []domain.BatchOpen, at time.Time) error {
	const q = `
		INSERT INTO tally.receipt_batch
			(id, tenant_id, warehouse_id, product_id, source_item_id, qty_base, qty_remaining, conv_factor, received_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`
	for _, ln := range lines {
		if ln.SourceItemID == uuid.Nil || !ln.QtyBase.IsPositive() {
			continue
		}
		factor := ln.Factor
		if !factor.IsPositive() {
			factor = decimal.NewFromInt(1)
		}
		left := domain.PlanOpenRemaining(ln.QtyBase, ln.OnHandAfter)
		_, err := tx.ExecContext(ctx, q,
			uuid.New(), tenant, warehouse, ln.ProductID, ln.SourceItemID,
			ln.QtyBase.String(), left.String(), factor.String(), at,
		)
		if err != nil {
			return fmt.Errorf("bill repo: open receipt batch: %w", err)
		}
	}
	return nil
}

func (r *Repo) Draw(ctx context.Context, tx *sql.Tx, tenant, warehouse uuid.UUID, lines []domain.BatchDrawLine, allowNegative bool) error {
	type group struct {
		after decimal.Decimal
		base  decimal.Decimal
		lines []domain.DrawRequest
		pins  []uuid.UUID
	}
	groups := map[uuid.UUID]*group{}
	var order []uuid.UUID
	for _, ln := range lines {
		if !ln.QtyBase.IsPositive() {
			continue
		}
		gr := groups[ln.ProductID]
		if gr == nil {
			gr = &group{}
			groups[ln.ProductID] = gr
			order = append(order, ln.ProductID)
		}
		gr.after = ln.OnHandAfter
		gr.base = gr.base.Add(ln.QtyBase)
		gr.lines = append(gr.lines, domain.DrawRequest{
			SaleItemID: ln.SaleItemID, QtyBase: ln.QtyBase, Factor: ln.Factor, SourceItemID: ln.SourceItemID,
		})
		if ln.SourceItemID != uuid.Nil {
			gr.pins = append(gr.pins, ln.SourceItemID)
		}
	}
	for _, productID := range order {
		gr := groups[productID]
		lots, err := r.lockLots(ctx, tx, tenant, warehouse, productID, gr.pins)
		if err != nil {
			return err
		}
		draws, rem, err := domain.PlanDraws(warehouse, gr.after.Add(gr.base), lots, gr.lines, allowNegative)
		if err != nil {
			return err
		}
		for id, left := range rem {
			if err := setBatchRemaining(ctx, tx, tenant, id, left); err != nil {
				return err
			}
		}
		for _, d := range draws {
			if err := insertDraw(ctx, tx, tenant, d); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Repo) lockLots(ctx context.Context, tx *sql.Tx, tenant, warehouse, product uuid.UUID, pins []uuid.UUID) ([]domain.BatchLot, error) {
	ids := make([]string, len(pins))
	for i, id := range pins {
		ids[i] = id.String()
	}
	const q = `
		SELECT id, source_item_id, warehouse_id, qty_remaining::text, received_at
		FROM tally.receipt_batch
		WHERE tenant_id = $1 AND (
			(warehouse_id = $2 AND product_id = $3) OR source_item_id = ANY($4::uuid[])
		)
		ORDER BY id
		FOR UPDATE`
	rows, err := tx.QueryContext(ctx, q, tenant, warehouse, product, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("bill repo: lock receipt batches: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var lots []domain.BatchLot
	for rows.Next() {
		var lot domain.BatchLot
		var qty string
		if err := rows.Scan(&lot.ID, &lot.SourceItemID, &lot.WarehouseID, &qty, &lot.ReceivedAt); err != nil {
			return nil, fmt.Errorf("bill repo: lock receipt batches: %w", err)
		}
		if lot.Remaining, err = decimalutil.Parse(qty, "qty_remaining"); err != nil {
			return nil, err
		}
		lots = append(lots, lot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bill repo: lock receipt batches: %w", err)
	}
	return lots, nil
}

func setBatchRemaining(ctx context.Context, tx *sql.Tx, tenant, id uuid.UUID, qty decimal.Decimal) error {
	const q = `UPDATE tally.receipt_batch SET qty_remaining = $3 WHERE id = $2 AND tenant_id = $1`
	if _, err := tx.ExecContext(ctx, q, tenant, id, qty.String()); err != nil {
		return fmt.Errorf("bill repo: set batch remaining: %w", err)
	}
	return nil
}

func insertDraw(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, d domain.PlannedDraw) error {
	const q = `
		INSERT INTO tally.batch_draw (id, tenant_id, sale_item_id, batch_id, qty_base)
		VALUES ($1,$2,$3,$4,$5)`
	var batch any
	if d.BatchID != uuid.Nil {
		batch = d.BatchID
	}
	_, err := tx.ExecContext(ctx, q, uuid.New(), tenant, d.SaleItemID, batch, d.QtyBase.String())
	if err != nil {
		return fmt.Errorf("bill repo: insert batch draw: %w", err)
	}
	return nil
}

func (r *Repo) GiveBack(ctx context.Context, tx *sql.Tx, tenant, purchaseItem uuid.UUID, qtyBase decimal.Decimal) error {
	const q = `
		SELECT id, qty_remaining::text, conv_factor::text
		FROM tally.receipt_batch
		WHERE tenant_id = $1 AND source_item_id = $2
		FOR UPDATE`
	var id uuid.UUID
	var remaining, factor string
	err := tx.QueryRowContext(ctx, q, tenant, purchaseItem).Scan(&id, &remaining, &factor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("bill repo: give back batch: %w", err)
	}
	left, err := decimalutil.Parse(remaining, "qty_remaining")
	if err != nil {
		return err
	}
	fac, err := decimalutil.Parse(factor, "conv_factor")
	if err != nil {
		return err
	}
	if !fac.IsPositive() {
		fac = decimal.NewFromInt(1)
	}
	if left.LessThan(qtyBase) {
		return &domain.BatchShortError{Remaining: left.Div(fac), Requested: qtyBase.Div(fac)}
	}
	return setBatchRemaining(ctx, tx, tenant, id, left.Sub(qtyBase))
}

func (r *Repo) Restore(ctx context.Context, tx *sql.Tx, tenant, saleItem uuid.UUID, qtyBase decimal.Decimal) error {
	const q = `
		SELECT id, batch_id, qty_base::text
		FROM tally.batch_draw
		WHERE tenant_id = $1 AND sale_item_id = $2
		ORDER BY created_at DESC, id DESC
		FOR UPDATE`
	rows, err := tx.QueryContext(ctx, q, tenant, saleItem)
	if err != nil {
		return fmt.Errorf("bill repo: restore draws: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var rowsOut []domain.DrawRow
	for rows.Next() {
		var row domain.DrawRow
		var batch *uuid.UUID
		var qty string
		if err := rows.Scan(&row.ID, &batch, &qty); err != nil {
			return fmt.Errorf("bill repo: restore draws: %w", err)
		}
		if batch != nil {
			row.BatchID = *batch
		}
		if row.QtyBase, err = decimalutil.Parse(qty, "qty_base"); err != nil {
			return err
		}
		rowsOut = append(rowsOut, row)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("bill repo: restore draws: %w", err)
	}
	add, left := domain.PlanRestore(rowsOut, qtyBase)
	for id, qty := range add {
		const up = `
			UPDATE tally.receipt_batch
			SET qty_remaining = LEAST(qty_base, qty_remaining + $3)
			WHERE id = $2 AND tenant_id = $1`
		if _, err := tx.ExecContext(ctx, up, tenant, id, qty.String()); err != nil {
			return fmt.Errorf("bill repo: restore batch: %w", err)
		}
	}
	for id, qty := range left {
		if qty.IsZero() {
			if _, err := tx.ExecContext(ctx, `DELETE FROM tally.batch_draw WHERE id = $1 AND tenant_id = $2`, id, tenant); err != nil {
				return fmt.Errorf("bill repo: drop draw: %w", err)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tally.batch_draw SET qty_base = $3 WHERE id = $1 AND tenant_id = $2`, id, tenant, qty.String()); err != nil {
			return fmt.Errorf("bill repo: shrink draw: %w", err)
		}
	}
	return nil
}
