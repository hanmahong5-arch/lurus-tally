// Package stock implements use cases for inventory management.
// The InventoryCalculator interface is the sole authorised path for modifying stock state (Rule R-4).
package stock

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// StockRepo is the persistence interface that InventoryCalculator implementations depend on.
// It abstracts all stock-related SQL behind a testable interface.
// Methods that accept *sql.Tx execute within the caller's transaction.
type StockRepo interface {
	// GetSnapshot returns the current snapshot or nil if no row exists yet (no lock).
	GetSnapshot(ctx context.Context, tenantID, productID, warehouseID uuid.UUID) (*domain.Snapshot, error)

	// SelectForUpdate returns the current snapshot with a row-level lock (SELECT FOR UPDATE).
	// Must be called within a transaction. Returns nil snapshot when no row exists yet.
	SelectForUpdate(ctx context.Context, tx *sql.Tx, tenantID, productID, warehouseID uuid.UUID) (*domain.Snapshot, error)

	// UpsertSnapshot inserts or updates the snapshot row within the transaction.
	UpsertSnapshot(ctx context.Context, tx *sql.Tx, s *domain.Snapshot) error

	// InsertMovement appends an immutable movement record within the transaction.
	InsertMovement(ctx context.Context, tx *sql.Tx, m *domain.Movement) error

	// ListMovements returns paginated movement history (read-only, no transaction needed).
	ListMovements(ctx context.Context, filter MovementFilter) ([]domain.Movement, error)

	// InsertLot creates a new FIFO lot row within the transaction.
	InsertLot(ctx context.Context, tx *sql.Tx, l *domain.Lot) error

	// ListActiveLots returns lots with qty_remaining > 0, ordered by received_at ASC,
	// with a FOR UPDATE row lock (called during FIFO outbound drain).
	ListActiveLots(ctx context.Context, tx *sql.Tx, tenantID, productID, warehouseID uuid.UUID) ([]domain.Lot, error)

	// UpdateLotQty persists the new qty_remaining for a lot within the transaction.
	UpdateLotQty(ctx context.Context, tx *sql.Tx, lotID uuid.UUID, qtyRemaining decimal.Decimal) error

	// AcquireAdvisoryLock obtains a transaction-scoped advisory lock keyed by the
	// FNV-64a hash of (tenantID || productID || warehouseID), serialising concurrent
	// writes to the same SKU/warehouse combination.
	AcquireAdvisoryLock(ctx context.Context, tx *sql.Tx, tenantID, productID, warehouseID uuid.UUID) error

	// ListSnapshots returns paginated snapshots (read-only, no transaction needed).
	ListSnapshots(ctx context.Context, filter ListSnapshotsFilter) ([]domain.Snapshot, error)

	// WithTx executes fn inside a new database transaction, committing on success
	// and rolling back on error or panic.
	WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error
}

// MovementFilter holds the query parameters for listing stock movements.
type MovementFilter struct {
	TenantID    uuid.UUID
	ProductID   uuid.UUID
	WarehouseID uuid.UUID
	Limit       int
	Offset      int
}

// InventoryCalculator is the strategy interface for cost-accounting methods.
// Each implementation encapsulates one cost-flow method (WAC or FIFO).
// Implementations must only call StockRepo mutators; direct SQL is forbidden.
type InventoryCalculator interface {
	// ValidateMovement checks business rules (e.g. sufficient stock for out/adjust-negative)
	// without persisting anything. Returns *InsufficientStockError when stock would go negative.
	ValidateMovement(ctx context.Context, tx *sql.Tx, m *domain.Movement) error

	// ApplyMovement executes the cost calculation and persists all side-effects
	// (movement row, snapshot update, lot inserts/updates) within tx.
	// The caller is responsible for the advisory lock and the transaction boundary.
	ApplyMovement(ctx context.Context, tx *sql.Tx, m *domain.Movement) (*domain.Snapshot, error)

	// Name returns the strategy identifier string ("wac" or "fifo").
	Name() string
}
