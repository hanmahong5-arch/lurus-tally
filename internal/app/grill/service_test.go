package grill_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	grillapp "github.com/hanmahong5-arch/lurus-tally/internal/app/grill"
	domaingrill "github.com/hanmahong5-arch/lurus-tally/internal/domain/grill"
)

var errItemNotFound = errors.New("fake: item not found")

// fakeInv records Deduct/Reverse calls per ref so tests can prove idempotency
// (INV-3) and that settlement never touches inventory (INV-4).
type fakeInv struct {
	deducts  map[uuid.UUID]int
	reverses map[uuid.UUID]int
}

func newFakeInv() *fakeInv {
	return &fakeInv{deducts: map[uuid.UUID]int{}, reverses: map[uuid.UUID]int{}}
}

func (f *fakeInv) Deduct(_ context.Context, _ uuid.UUID, req grillapp.DeductRequest) (uuid.UUID, error) {
	f.deducts[req.Ref]++
	return uuid.New(), nil
}

func (f *fakeInv) Reverse(_ context.Context, _ uuid.UUID, req grillapp.ReverseRequest) error {
	f.reverses[req.Ref]++
	return nil
}

func (f *fakeInv) totalDeducts() int {
	n := 0
	for _, c := range f.deducts {
		n += c
	}
	return n
}

// fakeStore is an in-memory implementation of grillapp.Store.
type fakeStore struct {
	items   map[uuid.UUID]domaingrill.OrderItem
	charges map[uuid.UUID][]domaingrill.SharedCharge
	people  map[uuid.UUID]int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		items:   map[uuid.UUID]domaingrill.OrderItem{},
		charges: map[uuid.UUID][]domaingrill.SharedCharge{},
		people:  map[uuid.UUID]int{},
	}
}

func (s *fakeStore) GetItem(_ context.Context, _, itemID uuid.UUID) (domaingrill.OrderItem, error) {
	it, ok := s.items[itemID]
	if !ok {
		return domaingrill.OrderItem{}, errItemNotFound
	}
	return it, nil
}

func (s *fakeStore) SaveItem(_ context.Context, _ uuid.UUID, item domaingrill.OrderItem) error {
	s.items[item.ID] = item
	return nil
}

func (s *fakeStore) ListItems(_ context.Context, _, sessionID uuid.UUID) ([]domaingrill.OrderItem, error) {
	var out []domaingrill.OrderItem
	for _, it := range s.items {
		if it.SessionID == sessionID {
			out = append(out, it)
		}
	}
	return out, nil
}

func (s *fakeStore) ListCharges(_ context.Context, _, sessionID uuid.UUID) ([]domaingrill.SharedCharge, error) {
	return s.charges[sessionID], nil
}

func (s *fakeStore) CountPeople(_ context.Context, _, sessionID uuid.UUID) (int, error) {
	return s.people[sessionID], nil
}

func calibratedItem(store *fakeStore, sessionID uuid.UUID, price string, qty int) uuid.UUID {
	id := uuid.New()
	cq := qty
	store.items[id] = domaingrill.OrderItem{
		ID:                id,
		SessionID:         sessionID,
		SKUID:             uuid.New(),
		Qty:               qty,
		CalibratedQty:     &cq,
		UnitPriceSnapshot: decimal.RequireFromString(price),
		Status:            domaingrill.ItemPending,
	}
	return id
}

// TestConfirmItem_DeductsOnceIdempotent: 下单即扣 writes exactly one movement,
// and a retry does not write a second (INV-3).
func TestConfirmItem_DeductsOnceIdempotent(t *testing.T) {
	ctx := context.Background()
	store, inv := newFakeStore(), newFakeInv()
	svc := grillapp.NewService(store, inv)
	tenant, session := uuid.New(), uuid.New()
	id := calibratedItem(store, session, "2.50", 14)

	if err := svc.ConfirmItem(ctx, tenant, id); err != nil {
		t.Fatalf("ConfirmItem: %v", err)
	}
	if inv.deducts[id] != 1 {
		t.Fatalf("deducts[item]=%d, want 1", inv.deducts[id])
	}
	got, _ := store.GetItem(ctx, tenant, id)
	if got.MovementID == nil {
		t.Fatal("MovementID should be set after confirm")
	}

	// Retry — must not double-deduct.
	if err := svc.ConfirmItem(ctx, tenant, id); err != nil {
		t.Fatalf("ConfirmItem retry: %v", err)
	}
	if inv.deducts[id] != 1 {
		t.Errorf("after retry deducts[item]=%d, want still 1 (INV-3)", inv.deducts[id])
	}
}

// TestConfirmItem_RequiresCalibration: 下单 before calibration is rejected and
// nothing is deducted.
func TestConfirmItem_RequiresCalibration(t *testing.T) {
	ctx := context.Background()
	store, inv := newFakeStore(), newFakeInv()
	svc := grillapp.NewService(store, inv)
	tenant, session := uuid.New(), uuid.New()

	id := uuid.New()
	store.items[id] = domaingrill.OrderItem{
		ID: id, SessionID: session, SKUID: uuid.New(), Qty: 5,
		UnitPriceSnapshot: decimal.RequireFromString("3.00"), Status: domaingrill.ItemPending,
	}

	if err := svc.ConfirmItem(ctx, tenant, id); !errors.Is(err, grillapp.ErrNotCalibrated) {
		t.Fatalf("ConfirmItem uncalibrated: got %v, want ErrNotCalibrated", err)
	}
	if inv.totalDeducts() != 0 {
		t.Errorf("deducts=%d, want 0 when uncalibrated", inv.totalDeducts())
	}
}

// TestCancelItem_ReversesAndReducesTotal: 退单 writes one compensating reverse
// movement and the settlement total drops accordingly.
func TestCancelItem_ReversesAndReducesTotal(t *testing.T) {
	ctx := context.Background()
	store, inv := newFakeStore(), newFakeInv()
	svc := grillapp.NewService(store, inv)
	tenant, session := uuid.New(), uuid.New()
	keep := calibratedItem(store, session, "5.00", 2)   // 10.00
	drop := calibratedItem(store, session, "2.50", 14)  // 35.00
	store.people[session] = 1

	for _, id := range []uuid.UUID{keep, drop} {
		if err := svc.ConfirmItem(ctx, tenant, id); err != nil {
			t.Fatalf("ConfirmItem: %v", err)
		}
	}
	before, _ := svc.SettleSession(ctx, tenant, session)
	if !before.Equal(decimal.RequireFromString("45.00")) {
		t.Fatalf("total before cancel=%s, want 45.00", before)
	}

	if err := svc.CancelItem(ctx, tenant, drop); err != nil {
		t.Fatalf("CancelItem: %v", err)
	}
	if inv.reverses[drop] != 1 {
		t.Errorf("reverses[drop]=%d, want 1", inv.reverses[drop])
	}
	after, _ := svc.SettleSession(ctx, tenant, session)
	if !after.Equal(decimal.RequireFromString("10.00")) {
		t.Errorf("total after cancel=%s, want 10.00", after)
	}

	// Cancel is idempotent — no second reverse.
	if err := svc.CancelItem(ctx, tenant, drop); err != nil {
		t.Fatalf("CancelItem repeat: %v", err)
	}
	if inv.reverses[drop] != 1 {
		t.Errorf("reverses[drop]=%d after repeat, want still 1", inv.reverses[drop])
	}
}

// TestCancelItem_NoReverseIfNeverDeducted: cancelling an item that was never
// confirmed writes no reverse movement.
func TestCancelItem_NoReverseIfNeverDeducted(t *testing.T) {
	ctx := context.Background()
	store, inv := newFakeStore(), newFakeInv()
	svc := grillapp.NewService(store, inv)
	tenant, session := uuid.New(), uuid.New()
	id := calibratedItem(store, session, "2.50", 4)

	if err := svc.CancelItem(ctx, tenant, id); err != nil {
		t.Fatalf("CancelItem: %v", err)
	}
	if inv.reverses[id] != 0 {
		t.Errorf("reverses=%d, want 0 (never deducted)", inv.reverses[id])
	}
	got, _ := store.GetItem(ctx, tenant, id)
	if got.Status != domaingrill.ItemCancelled {
		t.Errorf("status=%q, want cancelled", got.Status)
	}
}

// TestSettleSession_NoReDeduct: settlement computes INV-1 and never calls
// inventory (INV-4).
func TestSettleSession_NoReDeduct(t *testing.T) {
	ctx := context.Background()
	store, inv := newFakeStore(), newFakeInv()
	svc := grillapp.NewService(store, inv)
	tenant, session := uuid.New(), uuid.New()
	a := calibratedItem(store, session, "2.50", 14) // 35.00
	_ = calibratedItem(store, session, "5.00", 2)   // 10.00
	store.charges[session] = []domaingrill.SharedCharge{
		{Amount: decimal.RequireFromString("5.00"), SplitMode: domaingrill.SplitFixedPerPerson}, // ×2 = 10.00
	}
	store.people[session] = 2

	if err := svc.ConfirmItem(ctx, tenant, a); err != nil {
		t.Fatalf("ConfirmItem: %v", err)
	}
	deductsBefore := inv.totalDeducts()

	total, err := svc.SettleSession(ctx, tenant, session)
	if err != nil {
		t.Fatalf("SettleSession: %v", err)
	}
	if !total.Equal(decimal.RequireFromString("55.00")) {
		t.Errorf("settle total=%s, want 55.00 (35+10+10)", total)
	}
	if inv.totalDeducts() != deductsBefore {
		t.Errorf("settlement must not deduct: deducts changed %d→%d (INV-4)", deductsBefore, inv.totalDeducts())
	}
}
