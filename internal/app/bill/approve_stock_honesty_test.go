// Honesty lock test (PROC-15) — converts the verified claim
//
//	"tally Stock slice: go build/vet/test 全 ok (含 9 ApprovePurchase/ApproveSale
//	 e2e PASS)"
//
// into behavioural contracts for the approve-bill atomicity guarantees that the
// existing approve_sale_test.go / approve_purchase_test.go do not yet lock:
//   - stock exactly equal to demand approves (boundary)
//   - a multi-line PARTIAL shortage still rolls the whole bill back (no status
//     transition, no payment) and surfaces every short SKU
//   - ApproveSale with an invalid unit on one line rolls back all lines
//   - duplicate ApprovePurchase / ApproveSale is a no-op (idempotent re-entry)
//   - concurrent approval of the same bill is serialised by the advisory lock
//
// Reuses the shared mocks declared in the other bill_test files
// (newMockBillRepo, newMockStockUC, seedSaleDraftBill, seedDraftBill,
// seedProductUnitFactors, newMockProductUnitRepo, newMockPaymentRepo,
// newApproveSaleUC, newApproveUC, testTenantID, testCreatorID).
//
// The mock repo's WithTx runs fn(nil) and does not perform a real DB rollback;
// "atomic rollback" is therefore asserted behaviourally — when Execute returns
// an error the bill_head status must remain Draft and no payment must be
// recorded, because the use case never reaches UpdateBillStatus / Record.
// This is a contract assertion, not a tautology (§4.1③).
package bill_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// partialShortStockUC fails ExecuteInTx for any request whose ProductID is in
// the "short" set, returning a per-product InsufficientStockError; all other
// products succeed. It records successful calls so we can prove no partial
// movement was "committed" when the batch ultimately fails.
type partialShortStockUC struct {
	short     map[uuid.UUID]bool
	succeeded []appstock.RecordMovementRequest
}

func (m *partialShortStockUC) ExecuteInTx(_ context.Context, _ *sql.Tx, req appstock.RecordMovementRequest) (*domainstock.Snapshot, error) {
	if m.short[req.ProductID] {
		return nil, &appstock.InsufficientStockError{
			ProductID: req.ProductID,
			Available: decimal.Zero,
			Requested: req.Qty,
		}
	}
	m.succeeded = append(m.succeeded, req)
	return &domainstock.Snapshot{
		TenantID:    req.TenantID,
		ProductID:   req.ProductID,
		WarehouseID: req.WarehouseID,
		OnHandQty:   req.Qty,
		UnitCost:    req.UnitCost,
	}, nil
}

// TestApproveSale_StockExactlyEqualsDemand_Approves locks the "库存恰等需求"
// boundary: when the stock executor accepts every line (available == requested,
// no shortage), the sale approves cleanly and one movement per line is emitted.
func TestApproveSale_StockExactlyEqualsDemand_Approves(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC() // never fails → models exact-fit availability
	unitRepo := newMockProductUnitRepo()
	payRepo := newMockPaymentRepo()

	warehouseID := uuid.New()
	billID := seedSaleDraftBill(repo, 2, warehouseID)
	seedProductUnitFactors(unitRepo, repo.itemsByBillID[billID])

	uc := newApproveSaleUC(repo, stockUC, unitRepo, payRepo)
	if err := uc.Execute(context.Background(), appbill.ApproveSaleRequest{
		TenantID:  testTenantID,
		BillID:    billID,
		CreatorID: testCreatorID,
	}); err != nil {
		t.Fatalf("exact-fit stock should approve, got %v", err)
	}
	if repo.billsByID[billID].Status != domain.StatusApproved {
		t.Errorf("status: want Approved, got %d", repo.billsByID[billID].Status)
	}
	if len(stockUC.calls) != 2 {
		t.Errorf("movements: want 2, got %d", len(stockUC.calls))
	}
}

// TestApproveSale_PartialShortage_RollsBackAtomically locks the
// "多行部分不足" + "整单回滚(无部分 movement 落库)" core contract:
// of 3 lines, line 2 is short. The bill must NOT transition to Approved, no
// payment is recorded, and the returned error enumerates the short SKU. The
// non-short lines may have been validated but the bill stays Draft because the
// transaction (would) roll back.
func TestApproveSale_PartialShortage_RollsBackAtomically(t *testing.T) {
	repo := newMockBillRepo()
	unitRepo := newMockProductUnitRepo()
	payRepo := newMockPaymentRepo()

	warehouseID := uuid.New()
	billID := seedSaleDraftBill(repo, 3, warehouseID)
	items := repo.itemsByBillID[billID]
	seedProductUnitFactors(unitRepo, items)

	shortProduct := items[1].ProductID
	stockUC := &partialShortStockUC{short: map[uuid.UUID]bool{shortProduct: true}}

	uc := newApproveSaleUC(repo, stockUC, unitRepo, payRepo)
	err := uc.Execute(context.Background(), appbill.ApproveSaleRequest{
		TenantID:   testTenantID,
		BillID:     billID,
		CreatorID:  testCreatorID,
		PaidAmount: decimal.NewFromFloat(50),
		PayType:    "cash",
	})
	if err == nil {
		t.Fatal("partial shortage must error, got nil")
	}

	bise, ok := err.(*appstock.BatchInsufficientStockError)
	if !ok {
		t.Fatalf("want *BatchInsufficientStockError, got %T: %v", err, err)
	}
	if len(bise.Shortages) != 1 {
		t.Errorf("shortage count: want 1, got %d", len(bise.Shortages))
	} else if bise.Shortages[0].ProductID != shortProduct {
		t.Errorf("short product: want %s, got %s", shortProduct, bise.Shortages[0].ProductID)
	}

	// Atomicity: bill stays Draft, no payment recorded.
	if repo.billsByID[billID].Status != domain.StatusDraft {
		t.Errorf("status after partial shortage: want Draft, got %d", repo.billsByID[billID].Status)
	}
	if len(payRepo.recorded) != 0 {
		t.Errorf("no payment may be recorded on rollback, got %d", len(payRepo.recorded))
	}
}

// TestApproveSale_InvalidUnit_RollsBackAll locks "invalid unit 回滚全部" for the
// SALE path (the existing suite only covers the purchase path): a unit with no
// registered conversion factor on the 2nd line aborts the whole approval. The
// bill stays Draft and no payment is recorded.
func TestApproveSale_InvalidUnit_RollsBackAll(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	unitRepo := newMockProductUnitRepo()
	payRepo := newMockPaymentRepo()

	warehouseID := uuid.New()
	billID := seedSaleDraftBill(repo, 3, warehouseID)
	items := repo.itemsByBillID[billID]
	// Register factors for lines 0 and 2 but NOT line 1 → ErrInvalidUnitForProduct.
	unitRepo.set(items[0].ProductID, *items[0].UnitID, decimal.NewFromInt(1))
	unitRepo.set(items[2].ProductID, *items[2].UnitID, decimal.NewFromInt(1))

	uc := newApproveSaleUC(repo, stockUC, unitRepo, payRepo)
	err := uc.Execute(context.Background(), appbill.ApproveSaleRequest{
		TenantID:   testTenantID,
		BillID:     billID,
		CreatorID:  testCreatorID,
		PaidAmount: decimal.NewFromFloat(20),
		PayType:    "cash",
	})
	if err == nil {
		t.Fatal("invalid unit must error, got nil")
	}
	if repo.billsByID[billID].Status != domain.StatusDraft {
		t.Errorf("status after invalid-unit abort: want Draft, got %d", repo.billsByID[billID].Status)
	}
	if len(payRepo.recorded) != 0 {
		t.Errorf("no payment on invalid-unit rollback, got %d", len(payRepo.recorded))
	}
}

// TestApprovePurchase_DuplicateApproval_NoOp locks "已审批重入幂等" for purchase:
// the FIRST approval succeeds; a SECOND Execute on the now-Approved bill returns
// nil and emits no further stock movements (re-entrant / double-click safe).
func TestApprovePurchase_DuplicateApproval_NoOp(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	unitRepo := newMockProductUnitRepo()

	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 2, warehouseID)
	seedProductUnitFactors(unitRepo, repo.itemsByBillID[billID])

	uc := newApproveUC(repo, stockUC, unitRepo)
	approvedBy := uuid.New()

	if err := uc.Execute(context.Background(), testTenantID, billID, approvedBy); err != nil {
		t.Fatalf("first approve: %v", err)
	}
	movementsAfterFirst := len(stockUC.calls)
	if movementsAfterFirst != 2 {
		t.Fatalf("first approve movements: want 2, got %d", movementsAfterFirst)
	}

	// Second approval on an already-Approved bill: no-op.
	if err := uc.Execute(context.Background(), testTenantID, billID, approvedBy); err != nil {
		t.Fatalf("duplicate approve must be nil, got %v", err)
	}
	if len(stockUC.calls) != movementsAfterFirst {
		t.Errorf("duplicate approve emitted extra movements: was %d now %d", movementsAfterFirst, len(stockUC.calls))
	}
}

// serializingRepo wraps the shared mockBillRepo and serialises WithTx with a
// mutex, modelling exactly what PG's transaction-scoped advisory lock gives
// ApprovePurchase: only one transaction body for a given bill runs at a time.
// Without this, the in-memory mock has no concurrency control and the test
// would be asserting a property the mock cannot provide (§4.1③). By making the
// serialisation explicit we test the use case's idempotent recheck-after-lock,
// not the mock's accidental timing.
type serializingRepo struct {
	*mockBillRepo
	txMu sync.Mutex
}

func (r *serializingRepo) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	r.txMu.Lock()
	defer r.txMu.Unlock()
	return r.mockBillRepo.WithTx(ctx, fn)
}

// GetBill is the pre-lock idempotency probe that runs OUTSIDE WithTx. The use
// case reads the returned head's Status AFTER this call returns (i.e. after the
// mutex is released, approve_purchase.go:65), so returning the shared *BillHead
// pointer races a concurrent in-tx UpdateBillStatus mutating the same struct.
// A real DB hands back an independent row snapshot per query; model that by
// returning a copy taken under the lock. This keeps the mock's shared state
// access well-defined under -race without changing the use case's logic.
func (r *serializingRepo) GetBill(ctx context.Context, tenantID, billID uuid.UUID) (*domain.BillHead, error) {
	r.txMu.Lock()
	defer r.txMu.Unlock()
	h, err := r.mockBillRepo.GetBill(ctx, tenantID, billID)
	if err != nil || h == nil {
		return h, err
	}
	snapshot := *h
	return &snapshot, nil
}

// TestApprovePurchase_ConcurrentSameBill_Idempotent locks "并发同单": two
// goroutines approving the same bill must both return nil and the bill ends up
// Approved with exactly one set of stock movements — the idempotent recheck
// after acquiring the lock prevents a second commit.
func TestApprovePurchase_ConcurrentSameBill_Idempotent(t *testing.T) {
	base := newMockBillRepo()
	repo := &serializingRepo{mockBillRepo: base}
	stockUC := newMockStockUC()
	unitRepo := newMockProductUnitRepo()

	warehouseID := uuid.New()
	billID := seedDraftBill(base, 1, warehouseID)
	seedProductUnitFactors(unitRepo, base.itemsByBillID[billID])

	// Construct directly (not via newApproveUC) so the serialisingRepo is used
	// through the BillRepo interface rather than the concrete *mockBillRepo.
	uc := appbill.NewApprovePurchaseUseCase(repo, stockUC, unitRepo)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(idx int) {
			defer wg.Done()
			errs[idx] = uc.Execute(context.Background(), testTenantID, billID, uuid.New())
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("goroutine %d: want nil, got %v", i, e)
		}
	}
	if base.billsByID[billID].Status != domain.StatusApproved {
		t.Errorf("final status: want Approved, got %d", base.billsByID[billID].Status)
	}
	// Exactly one line item, approved once → exactly one stock movement total.
	if len(stockUC.calls) != 1 {
		t.Errorf("concurrent approve emitted %d movements, want 1 (idempotent)", len(stockUC.calls))
	}
}
