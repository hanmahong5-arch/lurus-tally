package stock_test

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// mockRepo implements appstock.StockRepo in memory for unit tests.
// Transactions are no-ops (nil tx is accepted by all methods).
type mockRepo struct {
	snapshot  *domain.Snapshot
	movements []domain.Movement
	lots      []domain.Lot

	// selectForUpdateErr triggers an error on SelectForUpdate when set.
	selectForUpdateErr error
}

func newMockRepo(snap *domain.Snapshot) *mockRepo {
	return &mockRepo{snapshot: snap}
}

func (m *mockRepo) GetSnapshot(_ context.Context, _, _, _ uuid.UUID) (*domain.Snapshot, error) {
	return m.snapshot, nil
}

func (m *mockRepo) SelectForUpdate(_ context.Context, _ *sql.Tx, _, _, _ uuid.UUID) (*domain.Snapshot, error) {
	if m.selectForUpdateErr != nil {
		return nil, m.selectForUpdateErr
	}
	return m.snapshot, nil
}

func (m *mockRepo) UpsertSnapshot(_ context.Context, _ *sql.Tx, s *domain.Snapshot) error {
	m.snapshot = s
	return nil
}

func (m *mockRepo) InsertMovement(_ context.Context, _ *sql.Tx, mv *domain.Movement) error {
	m.movements = append(m.movements, *mv)
	return nil
}

func (m *mockRepo) ListMovements(_ context.Context, _ appstock.MovementFilter) ([]domain.Movement, error) {
	return m.movements, nil
}

func (m *mockRepo) ListSnapshots(_ context.Context, _ appstock.ListSnapshotsFilter) ([]domain.Snapshot, error) {
	if m.snapshot != nil {
		return []domain.Snapshot{*m.snapshot}, nil
	}
	return nil, nil
}

func (m *mockRepo) InsertLot(_ context.Context, _ *sql.Tx, l *domain.Lot) error {
	m.lots = append(m.lots, *l)
	return nil
}

func (m *mockRepo) ListActiveLots(_ context.Context, _ *sql.Tx, _, _, _ uuid.UUID) ([]domain.Lot, error) {
	var active []domain.Lot
	for _, l := range m.lots {
		if l.QtyRemaining.IsPositive() {
			active = append(active, l)
		}
	}
	return active, nil
}

func (m *mockRepo) UpdateLotQty(_ context.Context, _ *sql.Tx, lotID uuid.UUID, qtyRemaining decimal.Decimal) error {
	for i := range m.lots {
		if m.lots[i].ID == lotID {
			m.lots[i].QtyRemaining = qtyRemaining
			return nil
		}
	}
	return fmt.Errorf("mock: lot %s not found", lotID)
}

func (m *mockRepo) AcquireAdvisoryLock(_ context.Context, _ *sql.Tx, _, _, _ uuid.UUID) error {
	return nil
}

func (m *mockRepo) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil)
}

// newMovement builds a Movement with an ID already set.
func newMovement(tenantID, productID, warehouseID uuid.UUID, dir domain.Direction, qty, cost decimal.Decimal, refType domain.ReferenceType) *domain.Movement {
	return &domain.Movement{
		ID:            uuid.New(),
		TenantID:      tenantID,
		ProductID:     productID,
		WarehouseID:   warehouseID,
		Direction:     dir,
		QtyBase:       qty,
		UnitCost:      cost,
		ReferenceType: refType,
	}
}
