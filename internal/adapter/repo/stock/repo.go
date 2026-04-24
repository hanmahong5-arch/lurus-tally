// Package stock implements the StockRepo interface using PostgreSQL via pgx/v5 stdlib.
// All mutations are wrapped in database/sql transactions and respect the RLS policy
// (app.tenant_id session variable must be set by the connection pool before any query).
package stock

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// ErrNotFound is returned when a required row does not exist.
var ErrNotFound = errors.New("stock: record not found")

// Repo implements appstock.StockRepo backed by *sql.DB.
type Repo struct {
	db *sql.DB
}

// New constructs a Repo.
func New(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// Ensure Repo satisfies the interface at compile time.
var _ appstock.StockRepo = (*Repo)(nil)

// ----- Transaction boundary -----

// WithTx opens a transaction, passes it to fn, commits on success, and rolls back on error or panic.
func (r *Repo) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("stock repo: begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("stock repo: rollback after error (%v): %w", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("stock repo: commit tx: %w", err)
	}
	return nil
}

// ----- Advisory lock -----

// AcquireAdvisoryLock acquires a PG transaction-scoped advisory lock.
// The key is FNV-64a(tenantID || productID || warehouseID), ensuring only one writer
// per SKU+warehouse combination can proceed at a time.
func (r *Repo) AcquireAdvisoryLock(ctx context.Context, tx *sql.Tx, tenantID, productID, warehouseID uuid.UUID) error {
	key := advisoryKey(tenantID, productID, warehouseID)
	_, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", key)
	if err != nil {
		return fmt.Errorf("stock repo: advisory lock: %w", err)
	}
	return nil
}

// ----- Snapshot -----

const snapshotCols = `id, tenant_id, product_id, warehouse_id, on_hand_qty, available_qty, unit_cost, cost_strategy, updated_at`

// GetSnapshot returns the snapshot for the given SKU/warehouse without a row lock.
// Returns nil (no error) when no row exists yet.
func (r *Repo) GetSnapshot(ctx context.Context, tenantID, productID, warehouseID uuid.UUID) (*domain.Snapshot, error) {
	const q = `SELECT ` + snapshotCols + `
		FROM tally.stock_snapshot
		WHERE tenant_id = $1 AND product_id = $2 AND warehouse_id = $3`

	return scanSnapshot(r.db.QueryRowContext(ctx, q, tenantID, productID, warehouseID))
}

// SelectForUpdate returns the snapshot with a row-level FOR UPDATE lock (must be inside a tx).
func (r *Repo) SelectForUpdate(ctx context.Context, tx *sql.Tx, tenantID, productID, warehouseID uuid.UUID) (*domain.Snapshot, error) {
	const q = `SELECT ` + snapshotCols + `
		FROM tally.stock_snapshot
		WHERE tenant_id = $1 AND product_id = $2 AND warehouse_id = $3
		FOR UPDATE`

	return scanSnapshot(tx.QueryRowContext(ctx, q, tenantID, productID, warehouseID))
}

func scanSnapshot(row *sql.Row) (*domain.Snapshot, error) {
	var s domain.Snapshot
	var onHand, available, unitCost string
	err := row.Scan(
		&s.ID, &s.TenantID, &s.ProductID, &s.WarehouseID,
		&onHand, &available, &unitCost, &s.CostStrategy, &s.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // no snapshot yet
	}
	if err != nil {
		return nil, fmt.Errorf("stock repo: scan snapshot: %w", err)
	}
	s.OnHandQty, _ = decimal.NewFromString(onHand)
	s.AvailableQty, _ = decimal.NewFromString(available)
	s.UnitCost, _ = decimal.NewFromString(unitCost)
	return &s, nil
}

// UpsertSnapshot inserts or updates the stock_snapshot row.
// Uses ON CONFLICT DO UPDATE to handle the first-insert vs update case.
func (r *Repo) UpsertSnapshot(ctx context.Context, tx *sql.Tx, s *domain.Snapshot) error {
	const q = `
		INSERT INTO tally.stock_snapshot
			(id, tenant_id, product_id, warehouse_id, on_hand_qty, available_qty,
			 avg_cost_price, unit_cost, cost_strategy, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (tenant_id, product_id, warehouse_id) DO UPDATE SET
			on_hand_qty    = EXCLUDED.on_hand_qty,
			available_qty  = EXCLUDED.available_qty,
			avg_cost_price = EXCLUDED.avg_cost_price,
			unit_cost      = EXCLUDED.unit_cost,
			cost_strategy  = EXCLUDED.cost_strategy,
			updated_at     = EXCLUDED.updated_at`

	now := time.Now().UTC()
	_, err := tx.ExecContext(ctx, q,
		s.ID, s.TenantID, s.ProductID, s.WarehouseID,
		s.OnHandQty.String(), s.AvailableQty.String(),
		s.UnitCost.String(), // avg_cost_price (kept in sync)
		s.UnitCost.String(),
		s.CostStrategy,
		now,
	)
	if err != nil {
		return fmt.Errorf("stock repo: upsert snapshot: %w", err)
	}
	return nil
}

// ListSnapshots returns paginated snapshots filtered by the provided criteria.
func (r *Repo) ListSnapshots(ctx context.Context, f appstock.ListSnapshotsFilter) ([]domain.Snapshot, error) {
	q := `SELECT ` + snapshotCols + ` FROM tally.stock_snapshot WHERE tenant_id = $1`
	args := []any{f.TenantID}
	idx := 2

	if f.ProductID != uuid.Nil {
		q += fmt.Sprintf(" AND product_id = $%d", idx)
		args = append(args, f.ProductID)
		idx++
	}
	if f.WarehouseID != uuid.Nil {
		q += fmt.Sprintf(" AND warehouse_id = $%d", idx)
		args = append(args, f.WarehouseID)
		idx++
	}

	lim := f.Limit
	if lim <= 0 {
		lim = 20
	}
	q += fmt.Sprintf(" ORDER BY updated_at DESC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, lim, f.Offset)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("stock repo: list snapshots: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var snaps []domain.Snapshot
	for rows.Next() {
		var s domain.Snapshot
		var onHand, available, unitCost string
		if err := rows.Scan(
			&s.ID, &s.TenantID, &s.ProductID, &s.WarehouseID,
			&onHand, &available, &unitCost, &s.CostStrategy, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("stock repo: list snapshots scan: %w", err)
		}
		s.OnHandQty, _ = decimal.NewFromString(onHand)
		s.AvailableQty, _ = decimal.NewFromString(available)
		s.UnitCost, _ = decimal.NewFromString(unitCost)
		snaps = append(snaps, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stock repo: list snapshots rows: %w", err)
	}
	return snaps, nil
}

// ----- Movement -----

// InsertMovement appends an immutable movement record.
func (r *Repo) InsertMovement(ctx context.Context, tx *sql.Tx, m *domain.Movement) error {
	const q = `
		INSERT INTO tally.stock_movement
			(id, tenant_id, product_id, warehouse_id, direction, qty_base,
			 unit_cost, total_cost, reference_type, reference_id,
			 occurred_at, created_by, note, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`

	_, err := tx.ExecContext(ctx, q,
		m.ID, m.TenantID, m.ProductID, m.WarehouseID,
		string(m.Direction), m.QtyBase.String(),
		m.UnitCost.String(), m.TotalCost.String(),
		string(m.ReferenceType), m.ReferenceID,
		m.OccurredAt, m.CreatedBy, m.Note, m.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("stock repo: insert movement: %w", err)
	}
	return nil
}

// ListMovements returns paginated movements for a given product/warehouse.
func (r *Repo) ListMovements(ctx context.Context, f appstock.MovementFilter) ([]domain.Movement, error) {
	q := `SELECT id, tenant_id, product_id, warehouse_id, direction, qty_base,
			unit_cost, total_cost, reference_type, reference_id, occurred_at, created_by, note, created_at
		FROM tally.stock_movement WHERE tenant_id = $1`
	args := []any{f.TenantID}
	idx := 2

	if f.ProductID != uuid.Nil {
		q += fmt.Sprintf(" AND product_id = $%d", idx)
		args = append(args, f.ProductID)
		idx++
	}
	if f.WarehouseID != uuid.Nil {
		q += fmt.Sprintf(" AND warehouse_id = $%d", idx)
		args = append(args, f.WarehouseID)
		idx++
	}

	lim := f.Limit
	if lim <= 0 {
		lim = 50
	}
	q += fmt.Sprintf(" ORDER BY occurred_at DESC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, lim, f.Offset)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("stock repo: list movements: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var mvs []domain.Movement
	for rows.Next() {
		var m domain.Movement
		var qtyBase, unitCost, totalCost string
		var dir, refType string
		if err := rows.Scan(
			&m.ID, &m.TenantID, &m.ProductID, &m.WarehouseID,
			&dir, &qtyBase, &unitCost, &totalCost,
			&refType, &m.ReferenceID, &m.OccurredAt, &m.CreatedBy, &m.Note, &m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("stock repo: list movements scan: %w", err)
		}
		m.Direction = domain.Direction(dir)
		m.ReferenceType = domain.ReferenceType(refType)
		m.QtyBase, _ = decimal.NewFromString(qtyBase)
		m.UnitCost, _ = decimal.NewFromString(unitCost)
		m.TotalCost, _ = decimal.NewFromString(totalCost)
		mvs = append(mvs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stock repo: list movements rows: %w", err)
	}
	return mvs, nil
}

// ----- Lots (FIFO) -----

// InsertLot creates a new FIFO lot within the transaction.
func (r *Repo) InsertLot(ctx context.Context, tx *sql.Tx, l *domain.Lot) error {
	const q = `
		INSERT INTO tally.stock_lot
			(id, tenant_id, product_id, warehouse_id, lot_no, qty, qty_remaining,
			 unit_cost, received_at, source_movement_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`

	_, err := tx.ExecContext(ctx, q,
		l.ID, l.TenantID, l.ProductID, l.WarehouseID, l.LotNo,
		l.Qty.String(), l.QtyRemaining.String(), l.UnitCost.String(),
		l.ReceivedAt, l.SourceMovementID, l.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("stock repo: insert lot: %w", err)
	}
	return nil
}

// ListActiveLots returns lots with qty_remaining > 0, ordered oldest-first, with a FOR UPDATE lock.
func (r *Repo) ListActiveLots(ctx context.Context, tx *sql.Tx, tenantID, productID, warehouseID uuid.UUID) ([]domain.Lot, error) {
	const q = `
		SELECT id, tenant_id, product_id, warehouse_id, lot_no, qty, qty_remaining,
			   unit_cost, received_at, source_movement_id, created_at
		FROM tally.stock_lot
		WHERE tenant_id = $1 AND product_id = $2 AND warehouse_id = $3
		  AND qty_remaining > 0
		ORDER BY received_at ASC
		FOR UPDATE`

	rows, err := tx.QueryContext(ctx, q, tenantID, productID, warehouseID)
	if err != nil {
		return nil, fmt.Errorf("stock repo: list active lots: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var lots []domain.Lot
	for rows.Next() {
		var l domain.Lot
		var qty, qtyRem, unitCost string
		if err := rows.Scan(
			&l.ID, &l.TenantID, &l.ProductID, &l.WarehouseID, &l.LotNo,
			&qty, &qtyRem, &unitCost, &l.ReceivedAt, &l.SourceMovementID, &l.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("stock repo: list active lots scan: %w", err)
		}
		l.Qty, _ = decimal.NewFromString(qty)
		l.QtyRemaining, _ = decimal.NewFromString(qtyRem)
		l.UnitCost, _ = decimal.NewFromString(unitCost)
		lots = append(lots, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stock repo: list active lots rows: %w", err)
	}
	return lots, nil
}

// UpdateLotQty persists the new qty_remaining for a lot.
func (r *Repo) UpdateLotQty(ctx context.Context, tx *sql.Tx, lotID uuid.UUID, qtyRemaining decimal.Decimal) error {
	const q = `UPDATE tally.stock_lot SET qty_remaining = $1 WHERE id = $2`
	_, err := tx.ExecContext(ctx, q, qtyRemaining.String(), lotID)
	if err != nil {
		return fmt.Errorf("stock repo: update lot qty: %w", err)
	}
	return nil
}
