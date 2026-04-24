package stock

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// GetSnapshotUseCase retrieves a single stock snapshot.
type GetSnapshotUseCase struct {
	repo StockRepo
}

// NewGetSnapshotUseCase constructs the use case.
func NewGetSnapshotUseCase(repo StockRepo) *GetSnapshotUseCase {
	return &GetSnapshotUseCase{repo: repo}
}

// Execute returns the snapshot for the given SKU/warehouse, or nil when none exists.
func (uc *GetSnapshotUseCase) Execute(ctx context.Context, tenantID, productID, warehouseID uuid.UUID) (*domain.Snapshot, error) {
	s, err := uc.repo.GetSnapshot(ctx, tenantID, productID, warehouseID)
	if err != nil {
		return nil, fmt.Errorf("get snapshot: %w", err)
	}
	return s, nil
}

// ListSnapshotsFilter holds filter criteria for ListSnapshotsUseCase.
type ListSnapshotsFilter struct {
	TenantID    uuid.UUID
	ProductID   uuid.UUID // zero → all products
	WarehouseID uuid.UUID // zero → all warehouses
	Limit       int
	Offset      int
}

// SnapshotLister is the minimal read interface for snapshot queries.
// Implemented by the repo; separated to keep it testable without a full StockRepo mock.
type SnapshotLister interface {
	ListSnapshots(ctx context.Context, filter ListSnapshotsFilter) ([]domain.Snapshot, error)
}

// ListSnapshotsUseCase paginates over stock snapshots.
type ListSnapshotsUseCase struct {
	lister SnapshotLister
}

// NewListSnapshotsUseCase constructs the use case.
func NewListSnapshotsUseCase(lister SnapshotLister) *ListSnapshotsUseCase {
	return &ListSnapshotsUseCase{lister: lister}
}

// Execute returns paginated snapshots matching the filter.
func (uc *ListSnapshotsUseCase) Execute(ctx context.Context, f ListSnapshotsFilter) ([]domain.Snapshot, error) {
	if f.Limit <= 0 {
		f.Limit = 20
	}
	snaps, err := uc.lister.ListSnapshots(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	return snaps, nil
}

// ListMovementsUseCase paginates over stock movement history.
type ListMovementsUseCase struct {
	repo StockRepo
}

// NewListMovementsUseCase constructs the use case.
func NewListMovementsUseCase(repo StockRepo) *ListMovementsUseCase {
	return &ListMovementsUseCase{repo: repo}
}

// Execute returns paginated movements matching the filter.
func (uc *ListMovementsUseCase) Execute(ctx context.Context, f MovementFilter) ([]domain.Movement, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	mvs, err := uc.repo.ListMovements(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("list movements: %w", err)
	}
	return mvs, nil
}
