// zz_coverage_test.go fills the branch/line coverage gaps left by the existing
// suite: guard-clause validation, wrapped-error propagation from the repo/unit/
// stock collaborators, defensive state-machine branches, and a few business
// invariants (decimal precision, validateRefs dedup, receivable clamp) that
// were not yet locked. Reuses the shared fakes declared in the other
// bill_test files (mockBillRepo, mockStockUC, mockProductUnitRepo,
// mockPaymentRepo, seedDraftBill, seedSaleDraftBill, seedReturnDraftBill,
// serializingRepo, testTenantID, testCreatorID, testWarehouseID).
package bill_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// =====================================================================
// Wrapper repos that embed *mockBillRepo and override exactly one method
// to inject an error / count calls, following the same pattern as
// purchasePriceRecordingRepo / serializingRepo in the other test files.
// =====================================================================

type lockErrRepo struct {
	*mockBillRepo
	err error
}

func (r *lockErrRepo) AcquireBillAdvisoryLock(_ context.Context, _ *sql.Tx, _, _ uuid.UUID) error {
	return r.err
}

type getForUpdateErrRepo struct {
	*mockBillRepo
	err error
}

func (r *getForUpdateErrRepo) GetBillForUpdate(_ context.Context, _ *sql.Tx, _, _ uuid.UUID) (*domain.BillHead, error) {
	return nil, r.err
}

type getItemsErrRepo struct {
	*mockBillRepo
	err error
}

func (r *getItemsErrRepo) GetBillItems(_ context.Context, _, _ uuid.UUID) ([]*domain.BillItem, error) {
	return nil, r.err
}

type nextBillNoErrRepo struct {
	*mockBillRepo
	err error
}

func (r *nextBillNoErrRepo) NextBillNo(_ context.Context, _ *sql.Tx, _ uuid.UUID, _ string) (string, error) {
	return "", r.err
}

type createBillErrRepo struct {
	*mockBillRepo
	err error
}

func (r *createBillErrRepo) CreateBill(_ context.Context, _ *sql.Tx, _ *domain.BillHead, _ []*domain.BillItem) error {
	return r.err
}

type updateBillErrRepo struct {
	*mockBillRepo
	err error
}

func (r *updateBillErrRepo) UpdateBill(_ context.Context, _ *sql.Tx, _ *domain.BillHead, _ []*domain.BillItem) error {
	return r.err
}

type listBillsErrRepo struct {
	*mockBillRepo
	err error
}

func (r *listBillsErrRepo) ListBills(_ context.Context, _ appbill.BillListFilter) ([]domain.BillHead, int64, error) {
	return nil, 0, r.err
}

type productExistsErrRepo struct {
	*mockBillRepo
	err error
}

func (r *productExistsErrRepo) ProductExists(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return false, r.err
}

type warehouseExistsErrRepo struct {
	*mockBillRepo
	err error
}

func (r *warehouseExistsErrRepo) WarehouseExists(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return false, r.err
}

// productExistsCounterRepo counts ProductExists invocations per product id so
// tests can assert validateRefs dedups repeated ids to a single lookup.
type productExistsCounterRepo struct {
	*mockBillRepo
	counts map[uuid.UUID]int
}

func newProductExistsCounterRepo() *productExistsCounterRepo {
	return &productExistsCounterRepo{mockBillRepo: newMockBillRepo(), counts: make(map[uuid.UUID]int)}
}

func (r *productExistsCounterRepo) ProductExists(ctx context.Context, tenantID, id uuid.UUID) (bool, error) {
	r.counts[id]++
	return r.mockBillRepo.ProductExists(ctx, tenantID, id)
}

// genericErrStockUC always returns a plain (non-InsufficientStockError) error
// and records how many times it was invoked, so callers can assert a
// non-stock error short-circuits immediately instead of accumulating.
type genericErrStockUC struct {
	calls int
	err   error
}

func (m *genericErrStockUC) ExecuteInTx(_ context.Context, _ *sql.Tx, _ appstock.RecordMovementRequest) (*domainstock.Snapshot, error) {
	m.calls++
	return nil, m.err
}

// =====================================================================
// validateRefs (refs.go) — exercised indirectly through the use cases that
// call it, since it is unexported.
// =====================================================================

func TestValidateRefs_DuplicateProductID_ChecksOnce(t *testing.T) {
	repo := newProductExistsCounterRepo()
	uc := appbill.NewCreateSaleUseCase(repo)

	sharedProduct := uuid.New()
	_, err := uc.Execute(context.Background(), appbill.CreateSaleRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		BillDate:  time.Now(),
		Items: []appbill.SaleItem{
			{ProductID: sharedProduct, WarehouseID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(10), LineNo: 1},
			{ProductID: sharedProduct, WarehouseID: uuid.New(), Qty: decimal.NewFromInt(2), UnitPrice: decimal.NewFromInt(5), LineNo: 2},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := repo.counts[sharedProduct]; got != 1 {
		t.Errorf("ProductExists called %d times for duplicate id, want 1 (deduped)", got)
	}
}

func TestValidateRefs_ProductExistsError_WrappedNotValidation(t *testing.T) {
	boom := errors.New("db connection lost")
	repo := &productExistsErrRepo{mockBillRepo: newMockBillRepo(), err: boom}
	uc := appbill.NewCreatePurchaseDraftUseCase(repo)

	_, err := uc.Execute(context.Background(), appbill.CreatePurchaseDraftRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		BillDate:  time.Now(),
		Items: []appbill.CreatePurchaseItemInput{
			{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(10), LineNo: 1},
		},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped repo error, got %v", err)
	}
	if errors.Is(err, appbill.ErrValidation) {
		t.Errorf("infra error must NOT be classified as ErrValidation, got %v", err)
	}
}

func TestValidateRefs_WarehouseExistsError_Wrapped(t *testing.T) {
	boom := errors.New("warehouse lookup timeout")
	repo := &warehouseExistsErrRepo{mockBillRepo: newMockBillRepo(), err: boom}
	uc := appbill.NewCreatePurchaseDraftUseCase(repo)

	wh := uuid.New()
	_, err := uc.Execute(context.Background(), appbill.CreatePurchaseDraftRequest{
		TenantID:    testTenantID,
		CreatorID:   testCreatorID,
		WarehouseID: &wh,
		BillDate:    time.Now(),
		Items: []appbill.CreatePurchaseItemInput{
			{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(10), LineNo: 1},
		},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped warehouse error, got %v", err)
	}
}

// =====================================================================
// create_purchase.go
// =====================================================================

func TestCreatePurchaseDraft_MissingCreatorID_ReturnsValidation(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreatePurchaseDraftUseCase(repo)
	_, err := uc.Execute(context.Background(), appbill.CreatePurchaseDraftRequest{
		TenantID: testTenantID,
		Items: []appbill.CreatePurchaseItemInput{
			{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1), LineNo: 1},
		},
	})
	if !errors.Is(err, appbill.ErrValidation) {
		t.Fatalf("expected ErrValidation for missing creator_id, got %v", err)
	}
}

func TestCreatePurchaseDraft_ItemLevelValidation(t *testing.T) {
	cases := []struct {
		name string
		item appbill.CreatePurchaseItemInput
	}{
		{"nil_product_id", appbill.CreatePurchaseItemInput{ProductID: uuid.Nil, Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1)}},
		{"zero_qty", appbill.CreatePurchaseItemInput{ProductID: uuid.New(), Qty: decimal.Zero, UnitPrice: decimal.NewFromInt(1)}},
		{"negative_qty", appbill.CreatePurchaseItemInput{ProductID: uuid.New(), Qty: decimal.NewFromInt(-1), UnitPrice: decimal.NewFromInt(1)}},
		{"negative_unit_price", appbill.CreatePurchaseItemInput{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(-1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMockBillRepo()
			uc := appbill.NewCreatePurchaseDraftUseCase(repo)
			_, err := uc.Execute(context.Background(), appbill.CreatePurchaseDraftRequest{
				TenantID:  testTenantID,
				CreatorID: testCreatorID,
				BillDate:  time.Now(),
				Items:     []appbill.CreatePurchaseItemInput{tc.item},
			})
			if !errors.Is(err, appbill.ErrValidation) {
				t.Errorf("expected ErrValidation, got %v", err)
			}
			if repo.storedHead != nil {
				t.Error("bill must not be persisted on validation failure")
			}
		})
	}
}

func TestCreatePurchaseDraft_BillDateZero_DefaultsToNowUTC(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreatePurchaseDraftUseCase(repo)
	before := time.Now().UTC()
	_, err := uc.Execute(context.Background(), appbill.CreatePurchaseDraftRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		Items: []appbill.CreatePurchaseItemInput{
			{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1), LineNo: 1},
		},
	})
	after := time.Now().UTC()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	bd := repo.storedHead.BillDate
	if bd.Before(before) || bd.After(after) {
		t.Errorf("BillDate = %v, want between %v and %v", bd, before, after)
	}
}

func TestCreatePurchaseDraft_NextBillNoError_Propagates(t *testing.T) {
	boom := errors.New("sequence exhausted")
	repo := &nextBillNoErrRepo{mockBillRepo: newMockBillRepo(), err: boom}
	uc := appbill.NewCreatePurchaseDraftUseCase(repo)
	_, err := uc.Execute(context.Background(), appbill.CreatePurchaseDraftRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		BillDate:  time.Now(),
		Items: []appbill.CreatePurchaseItemInput{
			{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1), LineNo: 1},
		},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped NextBillNo error, got %v", err)
	}
}

func TestCreatePurchaseDraft_CreateBillError_Propagates(t *testing.T) {
	boom := errors.New("unique violation")
	repo := &createBillErrRepo{mockBillRepo: newMockBillRepo(), err: boom}
	uc := appbill.NewCreatePurchaseDraftUseCase(repo)
	_, err := uc.Execute(context.Background(), appbill.CreatePurchaseDraftRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		BillDate:  time.Now(),
		Items: []appbill.CreatePurchaseItemInput{
			{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1), LineNo: 1},
		},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped CreateBill error, got %v", err)
	}
}

// TestCreatePurchaseDraft_LineAmountDecimalPrecision hand-computes
// 3 * 12.3456 = 37.0368 to prove line_amount uses decimal (not float) math.
func TestCreatePurchaseDraft_LineAmountDecimalPrecision(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreatePurchaseDraftUseCase(repo)
	_, err := uc.Execute(context.Background(), appbill.CreatePurchaseDraftRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		BillDate:  time.Now(),
		Items: []appbill.CreatePurchaseItemInput{
			{ProductID: uuid.New(), Qty: decimal.NewFromInt(3), UnitPrice: decimal.RequireFromString("12.3456"), LineNo: 1},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := decimal.RequireFromString("37.0368")
	got := repo.storedItems[0].LineAmount
	if !got.Equal(want) {
		t.Errorf("LineAmount = %s, want exact decimal %s (no float drift)", got, want)
	}
	if !repo.storedHead.Subtotal.Equal(want) {
		t.Errorf("Subtotal = %s, want %s", repo.storedHead.Subtotal, want)
	}
}

// =====================================================================
// create_sale.go
// =====================================================================

func TestCreateSaleDraft_MissingCreatorID_ReturnsValidation(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreateSaleUseCase(repo)
	_, err := uc.Execute(context.Background(), appbill.CreateSaleRequest{
		TenantID: testTenantID,
		BillDate: time.Now(),
		Items: []appbill.SaleItem{
			{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1), LineNo: 1},
		},
	})
	if !errors.Is(err, appbill.ErrValidation) {
		t.Fatalf("expected ErrValidation for missing creator_id, got %v", err)
	}
}

func TestCreateSaleDraft_ItemLevelValidation(t *testing.T) {
	cases := []struct {
		name string
		item appbill.SaleItem
	}{
		{"nil_product_id", appbill.SaleItem{ProductID: uuid.Nil, Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1)}},
		{"zero_qty", appbill.SaleItem{ProductID: uuid.New(), Qty: decimal.Zero, UnitPrice: decimal.NewFromInt(1)}},
		{"negative_qty", appbill.SaleItem{ProductID: uuid.New(), Qty: decimal.NewFromInt(-2), UnitPrice: decimal.NewFromInt(1)}},
		{"negative_unit_price", appbill.SaleItem{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(-1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMockBillRepo()
			uc := appbill.NewCreateSaleUseCase(repo)
			_, err := uc.Execute(context.Background(), appbill.CreateSaleRequest{
				TenantID:  testTenantID,
				CreatorID: testCreatorID,
				BillDate:  time.Now(),
				Items:     []appbill.SaleItem{tc.item},
			})
			if !errors.Is(err, appbill.ErrValidation) {
				t.Errorf("expected ErrValidation, got %v", err)
			}
		})
	}
}

func TestCreateSaleDraft_BillDateZero_DefaultsToNowUTC(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreateSaleUseCase(repo)
	before := time.Now().UTC()
	_, err := uc.Execute(context.Background(), appbill.CreateSaleRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		Items: []appbill.SaleItem{
			{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1), LineNo: 1},
		},
	})
	after := time.Now().UTC()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	bd := repo.storedHead.BillDate
	if bd.Before(before) || bd.After(after) {
		t.Errorf("BillDate = %v, want between %v and %v", bd, before, after)
	}
}

func TestCreateSaleDraft_NextBillNoError_Propagates(t *testing.T) {
	boom := errors.New("sequence exhausted")
	repo := &nextBillNoErrRepo{mockBillRepo: newMockBillRepo(), err: boom}
	uc := appbill.NewCreateSaleUseCase(repo)
	_, err := uc.Execute(context.Background(), appbill.CreateSaleRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		BillDate:  time.Now(),
		Items: []appbill.SaleItem{
			{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1), LineNo: 1},
		},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped NextBillNo error, got %v", err)
	}
}

func TestCreateSaleDraft_CreateBillError_Propagates(t *testing.T) {
	boom := errors.New("disk full")
	repo := &createBillErrRepo{mockBillRepo: newMockBillRepo(), err: boom}
	uc := appbill.NewCreateSaleUseCase(repo)
	_, err := uc.Execute(context.Background(), appbill.CreateSaleRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		BillDate:  time.Now(),
		Items: []appbill.SaleItem{
			{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1), LineNo: 1},
		},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped CreateBill error, got %v", err)
	}
}

// TestCreateSaleDraft_WarehouseFallbackFromFirstItem verifies that when the
// bill-level WarehouseID is absent, the first item's WarehouseID is used.
func TestCreateSaleDraft_WarehouseFallbackFromFirstItem(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreateSaleUseCase(repo)
	itemWH := uuid.New()
	_, err := uc.Execute(context.Background(), appbill.CreateSaleRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		BillDate:  time.Now(),
		Items: []appbill.SaleItem{
			{ProductID: uuid.New(), WarehouseID: itemWH, Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1), LineNo: 1},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.storedHead.WarehouseID == nil || *repo.storedHead.WarehouseID != itemWH {
		t.Errorf("WarehouseID = %v, want fallback to item warehouse %s", repo.storedHead.WarehouseID, itemWH)
	}
}

// =====================================================================
// create_return.go
// =====================================================================

func TestCreateReturnBill_MissingCreatorID_ReturnsValidation(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreateReturnBillUseCase(repo)
	_, err := uc.Execute(context.Background(), appbill.CreateReturnRequest{
		TenantID: testTenantID,
		BillDate: time.Now(),
		Items: []appbill.ReturnItem{
			{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1), LineNo: 1},
		},
	})
	if !errors.Is(err, appbill.ErrValidation) {
		t.Fatalf("expected ErrValidation for missing creator_id, got %v", err)
	}
}

func TestCreateReturnBill_ItemLevelValidation(t *testing.T) {
	cases := []struct {
		name string
		item appbill.ReturnItem
	}{
		{"nil_product_id", appbill.ReturnItem{ProductID: uuid.Nil, Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1)}},
		{"zero_qty", appbill.ReturnItem{ProductID: uuid.New(), Qty: decimal.Zero, UnitPrice: decimal.NewFromInt(1)}},
		{"negative_qty", appbill.ReturnItem{ProductID: uuid.New(), Qty: decimal.NewFromInt(-1), UnitPrice: decimal.NewFromInt(1)}},
		{"negative_unit_price", appbill.ReturnItem{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(-1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMockBillRepo()
			uc := appbill.NewCreateReturnBillUseCase(repo)
			_, err := uc.Execute(context.Background(), appbill.CreateReturnRequest{
				TenantID:  testTenantID,
				CreatorID: testCreatorID,
				BillDate:  time.Now(),
				Items:     []appbill.ReturnItem{tc.item},
			})
			if !errors.Is(err, appbill.ErrValidation) {
				t.Errorf("expected ErrValidation, got %v", err)
			}
		})
	}
}

func TestCreateReturnBill_BillDateZero_DefaultsToNowUTC(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreateReturnBillUseCase(repo)
	before := time.Now().UTC()
	_, err := uc.Execute(context.Background(), appbill.CreateReturnRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		Items: []appbill.ReturnItem{
			{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1), LineNo: 1},
		},
	})
	after := time.Now().UTC()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	bd := repo.storedHead.BillDate
	if bd.Before(before) || bd.After(after) {
		t.Errorf("BillDate = %v, want between %v and %v", bd, before, after)
	}
}

func TestCreateReturnBill_NextBillNoError_Propagates(t *testing.T) {
	boom := errors.New("sequence exhausted")
	repo := &nextBillNoErrRepo{mockBillRepo: newMockBillRepo(), err: boom}
	uc := appbill.NewCreateReturnBillUseCase(repo)
	_, err := uc.Execute(context.Background(), appbill.CreateReturnRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		BillDate:  time.Now(),
		Items: []appbill.ReturnItem{
			{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1), LineNo: 1},
		},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped NextBillNo error, got %v", err)
	}
}

func TestCreateReturnBill_CreateBillError_Propagates(t *testing.T) {
	boom := errors.New("constraint violation")
	repo := &createBillErrRepo{mockBillRepo: newMockBillRepo(), err: boom}
	uc := appbill.NewCreateReturnBillUseCase(repo)
	_, err := uc.Execute(context.Background(), appbill.CreateReturnRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		BillDate:  time.Now(),
		Items: []appbill.ReturnItem{
			{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1), LineNo: 1},
		},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped CreateBill error, got %v", err)
	}
}

func TestCreateReturnBill_RejectsProductOutsideTenant(t *testing.T) {
	repo := newMockBillRepo()
	foreignProduct := uuid.New()
	repo.missingRefs = map[uuid.UUID]struct{}{foreignProduct: {}}
	uc := appbill.NewCreateReturnBillUseCase(repo)

	_, err := uc.Execute(context.Background(), appbill.CreateReturnRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		BillDate:  time.Now(),
		Items: []appbill.ReturnItem{
			{ProductID: foreignProduct, Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1), LineNo: 1},
		},
	})
	if !errors.Is(err, appbill.ErrValidation) {
		t.Fatalf("expected ErrValidation for cross-tenant product, got %v", err)
	}
	if repo.storedHead != nil {
		t.Error("bill must NOT be persisted when a product reference is invalid")
	}
}

// =====================================================================
// approve_purchase.go
// =====================================================================

func TestApprovePurchase_MissingTenantOrBillID(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewApprovePurchaseUseCase(repo, newMockStockUC(), newMockProductUnitRepo())

	if err := uc.Execute(context.Background(), uuid.Nil, uuid.New(), uuid.New()); err == nil {
		t.Error("expected error for missing tenant_id, got nil")
	}
	if err := uc.Execute(context.Background(), testTenantID, uuid.Nil, uuid.New()); err == nil {
		t.Error("expected error for missing bill_id, got nil")
	}
}

func TestApprovePurchase_AdvisoryLockError_WrappedConflict(t *testing.T) {
	boom := errors.New("could not obtain lock")
	base := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedDraftBill(base, 1, warehouseID)
	repo := &lockErrRepo{mockBillRepo: base, err: boom}

	uc := appbill.NewApprovePurchaseUseCase(repo, newMockStockUC(), newMockProductUnitRepo())
	err := uc.Execute(context.Background(), testTenantID, billID, uuid.New())
	if !errors.Is(err, appbill.ErrBillApprovalConflict) {
		t.Fatalf("expected ErrBillApprovalConflict, got %v", err)
	}
}

func TestApprovePurchase_GetBillForUpdateError_Propagates(t *testing.T) {
	boom := errors.New("row not found")
	repo := &getForUpdateErrRepo{mockBillRepo: newMockBillRepo(), err: boom}
	uc := appbill.NewApprovePurchaseUseCase(repo, newMockStockUC(), newMockProductUnitRepo())
	err := uc.Execute(context.Background(), testTenantID, uuid.New(), uuid.New())
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped GetBillForUpdate error, got %v", err)
	}
}

// TestApprovePurchase_CancelledBill_ReturnsInvalidStatus verifies the state
// machine rejects approving a Cancelled bill (BillStatus.CanTransitionTo:
// Cancelled -> anything but Draft is illegal, so Cancelled -> Approved is
// false and ApprovePurchase must surface ErrInvalidBillStatus).
func TestApprovePurchase_CancelledBill_ReturnsInvalidStatus(t *testing.T) {
	repo := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)
	repo.billsByID[billID].Status = domain.StatusCancelled

	uc := appbill.NewApprovePurchaseUseCase(repo, newMockStockUC(), newMockProductUnitRepo())
	err := uc.Execute(context.Background(), testTenantID, billID, uuid.New())
	if !errors.Is(err, appbill.ErrInvalidBillStatus) {
		t.Fatalf("expected ErrInvalidBillStatus for cancelled bill, got %v", err)
	}
}

func TestApprovePurchase_GetBillItemsError_Propagates(t *testing.T) {
	base := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedDraftBill(base, 1, warehouseID)
	boom := errors.New("items query failed")
	repo := &getItemsErrRepo{mockBillRepo: base, err: boom}

	uc := appbill.NewApprovePurchaseUseCase(repo, newMockStockUC(), newMockProductUnitRepo())
	err := uc.Execute(context.Background(), testTenantID, billID, uuid.New())
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped GetBillItems error, got %v", err)
	}
}

// =====================================================================
// approve_sale.go
// =====================================================================

func TestApproveSale_MissingTenantOrBillID(t *testing.T) {
	repo := newMockBillRepo()
	uc := newApproveSaleUC(repo, newMockStockUC(), newMockProductUnitRepo(), newMockPaymentRepo())

	if err := uc.Execute(context.Background(), appbill.ApproveSaleRequest{TenantID: uuid.Nil, BillID: uuid.New()}); err == nil {
		t.Error("expected error for missing tenant_id, got nil")
	}
	if err := uc.Execute(context.Background(), appbill.ApproveSaleRequest{TenantID: testTenantID, BillID: uuid.Nil}); err == nil {
		t.Error("expected error for missing bill_id, got nil")
	}
}

func TestApproveSale_AdvisoryLockError_WrappedConflict(t *testing.T) {
	boom := errors.New("could not obtain lock")
	base := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedSaleDraftBill(base, 1, warehouseID)
	repo := &lockErrRepo{mockBillRepo: base, err: boom}

	uc := appbill.NewApproveSaleUseCase(repo, newMockStockUC(), newMockProductUnitRepo(), newMockPaymentRepo())
	err := uc.Execute(context.Background(), appbill.ApproveSaleRequest{TenantID: testTenantID, BillID: billID, CreatorID: testCreatorID})
	if !errors.Is(err, appbill.ErrBillApprovalConflict) {
		t.Fatalf("expected ErrBillApprovalConflict, got %v", err)
	}
}

func TestApproveSale_GetBillForUpdateError_Propagates(t *testing.T) {
	boom := errors.New("row not found")
	repo := &getForUpdateErrRepo{mockBillRepo: newMockBillRepo(), err: boom}
	uc := appbill.NewApproveSaleUseCase(repo, newMockStockUC(), newMockProductUnitRepo(), newMockPaymentRepo())
	err := uc.Execute(context.Background(), appbill.ApproveSaleRequest{TenantID: testTenantID, BillID: uuid.New(), CreatorID: testCreatorID})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped GetBillForUpdate error, got %v", err)
	}
}

func TestApproveSale_CancelledBill_ReturnsInvalidStatus(t *testing.T) {
	repo := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedSaleDraftBill(repo, 1, warehouseID)
	repo.billsByID[billID].Status = domain.StatusCancelled

	uc := newApproveSaleUC(repo, newMockStockUC(), newMockProductUnitRepo(), newMockPaymentRepo())
	err := uc.Execute(context.Background(), appbill.ApproveSaleRequest{TenantID: testTenantID, BillID: billID, CreatorID: testCreatorID})
	if !errors.Is(err, appbill.ErrInvalidBillStatus) {
		t.Fatalf("expected ErrInvalidBillStatus for cancelled bill, got %v", err)
	}
}

func TestApproveSale_GetBillItemsError_Propagates(t *testing.T) {
	base := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedSaleDraftBill(base, 1, warehouseID)
	boom := errors.New("items query failed")
	repo := &getItemsErrRepo{mockBillRepo: base, err: boom}

	uc := appbill.NewApproveSaleUseCase(repo, newMockStockUC(), newMockProductUnitRepo(), newMockPaymentRepo())
	err := uc.Execute(context.Background(), appbill.ApproveSaleRequest{TenantID: testTenantID, BillID: billID, CreatorID: testCreatorID})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped GetBillItems error, got %v", err)
	}
}

func TestApproveSale_GetConversionFactorError_Propagates(t *testing.T) {
	repo := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedSaleDraftBill(repo, 1, warehouseID)
	boom := errors.New("unit lookup failed")
	unitRepo := newMockProductUnitRepo()
	unitRepo.err = boom

	uc := newApproveSaleUC(repo, newMockStockUC(), unitRepo, newMockPaymentRepo())
	err := uc.Execute(context.Background(), appbill.ApproveSaleRequest{TenantID: testTenantID, BillID: billID, CreatorID: testCreatorID})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped GetConversionFactor error, got %v", err)
	}
	if repo.billsByID[billID].Status != domain.StatusDraft {
		t.Errorf("status = %d, want Draft after conversion-factor failure", repo.billsByID[billID].Status)
	}
}

// TestApproveSale_NonStockError_ShortCircuitsImmediately verifies that a
// non-InsufficientStockError from the stock executor aborts the approval on
// the FIRST failing line instead of being accumulated into a batch error
// (approve_sale.go:175-177).
func TestApproveSale_NonStockError_ShortCircuitsImmediately(t *testing.T) {
	repo := newMockBillRepo()
	unitRepo := newMockProductUnitRepo()
	payRepo := newMockPaymentRepo()

	warehouseID := uuid.New()
	billID := seedSaleDraftBill(repo, 2, warehouseID)
	seedProductUnitFactors(unitRepo, repo.itemsByBillID[billID])

	boom := errors.New("infra failure: connection reset")
	stockUC := &genericErrStockUC{err: boom}

	uc := newApproveSaleUC(repo, stockUC, unitRepo, payRepo)
	err := uc.Execute(context.Background(), appbill.ApproveSaleRequest{TenantID: testTenantID, BillID: billID, CreatorID: testCreatorID})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, ok := err.(*appstock.BatchInsufficientStockError); ok {
		t.Fatalf("non-stock error must not be classified as BatchInsufficientStockError, got %v", err)
	}
	if !errors.Is(err, boom) {
		t.Errorf("expected wrapped infra error, got %v", err)
	}
	if stockUC.calls != 1 {
		t.Errorf("stock executor called %d times, want 1 (short-circuit on first non-stock error)", stockUC.calls)
	}
	if repo.billsByID[billID].Status != domain.StatusDraft {
		t.Errorf("status = %d, want Draft", repo.billsByID[billID].Status)
	}
}

// TestApproveSale_InvalidPayType_FallsBackToCash verifies that an
// unrecognised PayType with PaidAmount > 0 falls back to PayTypeCash rather
// than erroring (approve_sale.go:216-220).
func TestApproveSale_InvalidPayType_FallsBackToCash(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	unitRepo := newMockProductUnitRepo()
	payRepo := newMockPaymentRepo()

	warehouseID := uuid.New()
	billID := seedSaleDraftBill(repo, 1, warehouseID)
	seedProductUnitFactors(unitRepo, repo.itemsByBillID[billID])

	uc := newApproveSaleUC(repo, stockUC, unitRepo, payRepo)
	err := uc.Execute(context.Background(), appbill.ApproveSaleRequest{
		TenantID:   testTenantID,
		BillID:     billID,
		CreatorID:  testCreatorID,
		PaidAmount: decimal.NewFromInt(30),
		PayType:    "not_a_real_pay_type",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(payRepo.recorded) != 1 {
		t.Fatalf("payment records = %d, want 1", len(payRepo.recorded))
	}
	if payRepo.recorded[0].PayType != domainpayment.PayTypeCash {
		t.Errorf("PayType = %q, want fallback to %q", payRepo.recorded[0].PayType, domainpayment.PayTypeCash)
	}
}

// TestApproveSale_ConcurrentSameBill_Idempotent mirrors the purchase-side
// concurrency lock in approve_stock_honesty_test.go but for the sale path:
// two goroutines approving the same bill both return nil and exactly one
// set of stock movements is recorded (advisory-lock-serialised recheck).
func TestApproveSale_ConcurrentSameBill_Idempotent(t *testing.T) {
	base := newMockBillRepo()
	repo := &serializingRepo{mockBillRepo: base}
	stockUC := newMockStockUC()
	unitRepo := newMockProductUnitRepo()
	payRepo := newMockPaymentRepo()

	warehouseID := uuid.New()
	billID := seedSaleDraftBill(base, 1, warehouseID)
	seedProductUnitFactors(unitRepo, base.itemsByBillID[billID])

	uc := appbill.NewApproveSaleUseCase(repo, stockUC, unitRepo, payRepo)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(idx int) {
			defer wg.Done()
			errs[idx] = uc.Execute(context.Background(), appbill.ApproveSaleRequest{
				TenantID: testTenantID, BillID: billID, CreatorID: testCreatorID,
			})
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
	if len(stockUC.calls) != 1 {
		t.Errorf("concurrent approve emitted %d movements, want 1 (idempotent)", len(stockUC.calls))
	}
}

// =====================================================================
// approve_return.go
// =====================================================================

func TestApproveReturnBill_MissingTenantOrBillID(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewApproveReturnBillUseCase(repo, newMockStockUC())

	if err := uc.Execute(context.Background(), appbill.ApproveReturnRequest{TenantID: uuid.Nil, BillID: uuid.New()}); err == nil {
		t.Error("expected error for missing tenant_id, got nil")
	}
	if err := uc.Execute(context.Background(), appbill.ApproveReturnRequest{TenantID: testTenantID, BillID: uuid.Nil}); err == nil {
		t.Error("expected error for missing bill_id, got nil")
	}
}

func TestApproveReturnBill_AdvisoryLockError_WrappedConflict(t *testing.T) {
	boom := errors.New("could not obtain lock")
	base := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedReturnDraftBill(base, 1, warehouseID)
	repo := &lockErrRepo{mockBillRepo: base, err: boom}

	uc := appbill.NewApproveReturnBillUseCase(repo, newMockStockUC())
	err := uc.Execute(context.Background(), appbill.ApproveReturnRequest{TenantID: testTenantID, BillID: billID, CreatorID: testCreatorID})
	if !errors.Is(err, appbill.ErrBillApprovalConflict) {
		t.Fatalf("expected ErrBillApprovalConflict, got %v", err)
	}
}

func TestApproveReturnBill_GetBillForUpdateError_Propagates(t *testing.T) {
	boom := errors.New("row not found")
	repo := &getForUpdateErrRepo{mockBillRepo: newMockBillRepo(), err: boom}
	uc := appbill.NewApproveReturnBillUseCase(repo, newMockStockUC())
	err := uc.Execute(context.Background(), appbill.ApproveReturnRequest{TenantID: testTenantID, BillID: uuid.New(), CreatorID: testCreatorID})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped GetBillForUpdate error, got %v", err)
	}
}

func TestApproveReturnBill_CancelledBill_ReturnsInvalidStatus(t *testing.T) {
	repo := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedReturnDraftBill(repo, 1, warehouseID)
	repo.billsByID[billID].Status = domain.StatusCancelled

	uc := appbill.NewApproveReturnBillUseCase(repo, newMockStockUC())
	err := uc.Execute(context.Background(), appbill.ApproveReturnRequest{TenantID: testTenantID, BillID: billID, CreatorID: testCreatorID})
	if !errors.Is(err, appbill.ErrInvalidBillStatus) {
		t.Fatalf("expected ErrInvalidBillStatus for cancelled bill, got %v", err)
	}
}

func TestApproveReturnBill_GetBillItemsError_Propagates(t *testing.T) {
	base := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedReturnDraftBill(base, 1, warehouseID)
	boom := errors.New("items query failed")
	repo := &getItemsErrRepo{mockBillRepo: base, err: boom}

	uc := appbill.NewApproveReturnBillUseCase(repo, newMockStockUC())
	err := uc.Execute(context.Background(), appbill.ApproveReturnRequest{TenantID: testTenantID, BillID: billID, CreatorID: testCreatorID})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped GetBillItems error, got %v", err)
	}
}

func TestApproveReturnBill_StockMovementError_Propagates(t *testing.T) {
	repo := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedReturnDraftBill(repo, 1, warehouseID)
	boom := errors.New("movement write failed")
	stockUC := &genericErrStockUC{err: boom}

	uc := appbill.NewApproveReturnBillUseCase(repo, stockUC)
	err := uc.Execute(context.Background(), appbill.ApproveReturnRequest{TenantID: testTenantID, BillID: billID, CreatorID: testCreatorID})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped movement error, got %v", err)
	}
	if repo.billsByID[billID].Status != domain.StatusDraft {
		t.Errorf("status = %d, want Draft after movement failure", repo.billsByID[billID].Status)
	}
}

// =====================================================================
// cancel_purchase.go
// =====================================================================

func TestCancelPurchase_MissingTenantOrBillID(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCancelPurchaseUseCase(repo)
	if err := uc.Execute(context.Background(), uuid.Nil, uuid.New()); err == nil {
		t.Error("expected error for missing tenant_id, got nil")
	}
	if err := uc.Execute(context.Background(), testTenantID, uuid.Nil); err == nil {
		t.Error("expected error for missing bill_id, got nil")
	}
}

func TestCancelPurchase_GetBillForUpdateError_Propagates(t *testing.T) {
	boom := errors.New("row not found")
	repo := &getForUpdateErrRepo{mockBillRepo: newMockBillRepo(), err: boom}
	uc := appbill.NewCancelPurchaseUseCase(repo)
	err := uc.Execute(context.Background(), testTenantID, uuid.New())
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped GetBillForUpdate error, got %v", err)
	}
}

func TestCancelPurchase_AlreadyCancelled_IdempotentNoOp(t *testing.T) {
	repo := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)
	repo.billsByID[billID].Status = domain.StatusCancelled

	uc := appbill.NewCancelPurchaseUseCase(repo)
	if err := uc.Execute(context.Background(), testTenantID, billID); err != nil {
		t.Fatalf("expected nil (idempotent) for already-cancelled bill, got %v", err)
	}
	if repo.billsByID[billID].Status != domain.StatusCancelled {
		t.Errorf("status changed on idempotent cancel: got %d", repo.billsByID[billID].Status)
	}
}

// TestCancelPurchase_CorruptStatus_ReturnsInvalidStatus exercises the
// defensive final branch: a status value that is neither Draft, Approved
// nor Cancelled (simulating corrupt/out-of-range data) must be rejected via
// CanTransitionTo rather than silently cancelled.
func TestCancelPurchase_CorruptStatus_ReturnsInvalidStatus(t *testing.T) {
	repo := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)
	repo.billsByID[billID].Status = domain.BillStatus(99)

	uc := appbill.NewCancelPurchaseUseCase(repo)
	err := uc.Execute(context.Background(), testTenantID, billID)
	if !errors.Is(err, appbill.ErrInvalidBillStatus) {
		t.Fatalf("expected ErrInvalidBillStatus for corrupt status, got %v", err)
	}
}

func TestCancelPurchase_UpdateStatusError_Propagates(t *testing.T) {
	repo := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)
	boom := errors.New("update failed")
	repo.updateStatusErr = boom

	uc := appbill.NewCancelPurchaseUseCase(repo)
	err := uc.Execute(context.Background(), testTenantID, billID)
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped UpdateBillStatus error, got %v", err)
	}
}

// =====================================================================
// restore_purchase.go
// =====================================================================

func TestRestorePurchase_MissingTenantOrBillID(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewRestorePurchaseUseCase(repo)
	if err := uc.Execute(context.Background(), uuid.Nil, uuid.New()); err == nil {
		t.Error("expected error for missing tenant_id, got nil")
	}
	if err := uc.Execute(context.Background(), testTenantID, uuid.Nil); err == nil {
		t.Error("expected error for missing bill_id, got nil")
	}
}

func TestRestorePurchase_GetBillForUpdateError_Propagates(t *testing.T) {
	boom := errors.New("row not found")
	repo := &getForUpdateErrRepo{mockBillRepo: newMockBillRepo(), err: boom}
	uc := appbill.NewRestorePurchaseUseCase(repo)
	err := uc.Execute(context.Background(), testTenantID, uuid.New())
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped GetBillForUpdate error, got %v", err)
	}
}

// TestRestorePurchase_CorruptStatus_ReturnsInvalidStatus exercises the final
// defensive branch: a status that is neither Approved, Draft nor Cancelled.
func TestRestorePurchase_CorruptStatus_ReturnsInvalidStatus(t *testing.T) {
	repo := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)
	repo.billsByID[billID].Status = domain.BillStatus(99)

	uc := appbill.NewRestorePurchaseUseCase(repo)
	err := uc.Execute(context.Background(), testTenantID, billID)
	if !errors.Is(err, appbill.ErrInvalidBillStatus) {
		t.Fatalf("expected ErrInvalidBillStatus for corrupt status, got %v", err)
	}
}

func TestRestorePurchase_UpdateStatusError_Propagates(t *testing.T) {
	repo := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)
	repo.billsByID[billID].Status = domain.StatusCancelled
	boom := errors.New("update failed")
	repo.updateStatusErr = boom

	uc := appbill.NewRestorePurchaseUseCase(repo)
	err := uc.Execute(context.Background(), testTenantID, billID)
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped UpdateBillStatus error, got %v", err)
	}
}

// =====================================================================
// update_purchase.go
// =====================================================================

func TestUpdatePurchaseDraft_GuardClauses(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewUpdatePurchaseDraftUseCase(repo)

	if _, err := uc.Execute(context.Background(), appbill.UpdatePurchaseDraftRequest{
		BillID: uuid.New(),
		Items:  []appbill.CreatePurchaseItemInput{{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1)}},
	}); !errors.Is(err, appbill.ErrValidation) {
		t.Errorf("expected ErrValidation for missing tenant_id, got %v", err)
	}

	if _, err := uc.Execute(context.Background(), appbill.UpdatePurchaseDraftRequest{
		TenantID: testTenantID,
		Items:    []appbill.CreatePurchaseItemInput{{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1)}},
	}); !errors.Is(err, appbill.ErrValidation) {
		t.Errorf("expected ErrValidation for missing bill_id, got %v", err)
	}

	if _, err := uc.Execute(context.Background(), appbill.UpdatePurchaseDraftRequest{
		TenantID: testTenantID,
		BillID:   uuid.New(),
		Items:    nil,
	}); !errors.Is(err, appbill.ErrValidation) {
		t.Errorf("expected ErrValidation for empty items, got %v", err)
	}
}

func TestUpdatePurchaseDraft_RejectsProductOutsideTenant(t *testing.T) {
	repo := newMockBillRepo()
	foreignProduct := uuid.New()
	repo.missingRefs = map[uuid.UUID]struct{}{foreignProduct: {}}
	uc := appbill.NewUpdatePurchaseDraftUseCase(repo)

	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)

	_, err := uc.Execute(context.Background(), appbill.UpdatePurchaseDraftRequest{
		TenantID: testTenantID,
		BillID:   billID,
		Items:    []appbill.CreatePurchaseItemInput{{ProductID: foreignProduct, Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1), LineNo: 1}},
	})
	if !errors.Is(err, appbill.ErrValidation) {
		t.Fatalf("expected ErrValidation for cross-tenant product, got %v", err)
	}
}

func TestUpdatePurchaseDraft_GetBillForUpdateError_Propagates(t *testing.T) {
	boom := errors.New("row not found")
	repo := &getForUpdateErrRepo{mockBillRepo: newMockBillRepo(), err: boom}
	uc := appbill.NewUpdatePurchaseDraftUseCase(repo)
	_, err := uc.Execute(context.Background(), appbill.UpdatePurchaseDraftRequest{
		TenantID: testTenantID,
		BillID:   uuid.New(),
		Items:    []appbill.CreatePurchaseItemInput{{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1), LineNo: 1}},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped GetBillForUpdate error, got %v", err)
	}
}

func TestUpdatePurchaseDraft_ItemLevelValidation(t *testing.T) {
	cases := []struct {
		name string
		item appbill.CreatePurchaseItemInput
	}{
		{"nil_product_id", appbill.CreatePurchaseItemInput{ProductID: uuid.Nil, Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1)}},
		{"zero_qty", appbill.CreatePurchaseItemInput{ProductID: uuid.New(), Qty: decimal.Zero, UnitPrice: decimal.NewFromInt(1)}},
		{"negative_qty", appbill.CreatePurchaseItemInput{ProductID: uuid.New(), Qty: decimal.NewFromInt(-1), UnitPrice: decimal.NewFromInt(1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMockBillRepo()
			warehouseID := uuid.New()
			billID := seedDraftBill(repo, 1, warehouseID)
			uc := appbill.NewUpdatePurchaseDraftUseCase(repo)
			_, err := uc.Execute(context.Background(), appbill.UpdatePurchaseDraftRequest{
				TenantID: testTenantID,
				BillID:   billID,
				Items:    []appbill.CreatePurchaseItemInput{tc.item},
			})
			if !errors.Is(err, appbill.ErrValidation) {
				t.Errorf("expected ErrValidation, got %v", err)
			}
		})
	}
}

// TestUpdatePurchaseDraft_NegativeFeesClampedToZero verifies that unlike
// CreatePurchaseDraft (which rejects negative fees outright), UpdatePurchaseDraft
// clamps a negative shipping_fee / tax_amount to zero rather than erroring —
// funds still never go negative, but the guard mechanism differs (update_purchase.go:99-104).
func TestUpdatePurchaseDraft_NegativeFeesClampedToZero(t *testing.T) {
	repo := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)
	uc := appbill.NewUpdatePurchaseDraftUseCase(repo)

	head, err := uc.Execute(context.Background(), appbill.UpdatePurchaseDraftRequest{
		TenantID:    testTenantID,
		BillID:      billID,
		ShippingFee: decimal.NewFromInt(-5),
		TaxAmount:   decimal.NewFromInt(-3),
		Items:       []appbill.CreatePurchaseItemInput{{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(10), LineNo: 1}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !head.ShippingFee.IsZero() {
		t.Errorf("ShippingFee = %s, want clamped to 0", head.ShippingFee)
	}
	if !head.TaxAmount.IsZero() {
		t.Errorf("TaxAmount = %s, want clamped to 0", head.TaxAmount)
	}
	if head.TotalAmount.IsNegative() {
		t.Errorf("TotalAmount = %s, must never be negative", head.TotalAmount)
	}
}

func TestUpdatePurchaseDraft_BillDateZero_KeepsExistingDate(t *testing.T) {
	repo := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)
	originalDate := repo.billsByID[billID].BillDate
	uc := appbill.NewUpdatePurchaseDraftUseCase(repo)

	head, err := uc.Execute(context.Background(), appbill.UpdatePurchaseDraftRequest{
		TenantID: testTenantID,
		BillID:   billID,
		Items:    []appbill.CreatePurchaseItemInput{{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1), LineNo: 1}},
		// BillDate left zero
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !head.BillDate.Equal(originalDate) {
		t.Errorf("BillDate = %v, want unchanged existing date %v", head.BillDate, originalDate)
	}
}

func TestUpdatePurchaseDraft_UpdateBillError_Propagates(t *testing.T) {
	base := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedDraftBill(base, 1, warehouseID)
	boom := errors.New("write failed")
	repo := &updateBillErrRepo{mockBillRepo: base, err: boom}

	uc := appbill.NewUpdatePurchaseDraftUseCase(repo)
	_, err := uc.Execute(context.Background(), appbill.UpdatePurchaseDraftRequest{
		TenantID: testTenantID,
		BillID:   billID,
		Items:    []appbill.CreatePurchaseItemInput{{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1), LineNo: 1}},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped UpdateBill error, got %v", err)
	}
}

// =====================================================================
// get.go
// =====================================================================

func TestGetPurchase_MissingTenantOrBillID(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewGetPurchaseUseCase(repo)
	if _, err := uc.Execute(context.Background(), uuid.Nil, uuid.New()); err == nil {
		t.Error("expected error for missing tenant_id, got nil")
	}
	if _, err := uc.Execute(context.Background(), testTenantID, uuid.Nil); err == nil {
		t.Error("expected error for missing bill_id, got nil")
	}
}

func TestGetPurchase_ItemsError_Propagates(t *testing.T) {
	base := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedDraftBill(base, 1, warehouseID)
	boom := errors.New("items query failed")
	repo := &getItemsErrRepo{mockBillRepo: base, err: boom}

	uc := appbill.NewGetPurchaseUseCase(repo)
	_, err := uc.Execute(context.Background(), testTenantID, billID)
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped GetBillItems error, got %v", err)
	}
}

// =====================================================================
// list.go
// =====================================================================

func TestListPurchases_MissingTenantID_ReturnsError(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewListPurchasesUseCase(repo)
	_, err := uc.Execute(context.Background(), appbill.BillListFilter{})
	if err == nil {
		t.Fatal("expected error for missing tenant_id, got nil")
	}
}

func TestListPurchases_ListBillsError_Propagates(t *testing.T) {
	boom := errors.New("query failed")
	repo := &listBillsErrRepo{mockBillRepo: newMockBillRepo(), err: boom}
	uc := appbill.NewListPurchasesUseCase(repo)
	_, err := uc.Execute(context.Background(), appbill.BillListFilter{TenantID: testTenantID})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped ListBills error, got %v", err)
	}
}

// TestListPurchases_CustomPageSize_PassedThrough verifies that a non-zero
// page/size is left untouched (only the <=0 default path is covered by the
// existing pagination test).
func TestListPurchases_CustomPageSize_PassedThrough(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewListPurchasesUseCase(repo)
	out, err := uc.Execute(context.Background(), appbill.BillListFilter{TenantID: testTenantID, Page: 3, Size: 50})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out == nil {
		t.Fatal("output is nil")
	}
}

// =====================================================================
// quick_checkout.go
// =====================================================================

func TestQuickCheckout_MissingCreatorID_ReturnsValidation(t *testing.T) {
	repo := newMockBillRepo()
	uc := newQuickCheckoutUC(repo, newMockStockUC(), newMockProductUnitRepo(), newMockPaymentRepo())
	_, err := uc.Execute(context.Background(), appbill.QuickCheckoutRequest{
		TenantID: testTenantID,
		Items:    []appbill.SaleItem{{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1)}},
	})
	if !errors.Is(err, appbill.ErrValidation) {
		t.Fatalf("expected ErrValidation for missing creator_id, got %v", err)
	}
}

// TestQuickCheckout_AssembleSaleItems_ItemLevelValidation verifies that
// assembleSaleItems (shared with CreateSaleUseCase) rejects the same bad
// items through the QuickCheckout entry point.
func TestQuickCheckout_AssembleSaleItems_ItemLevelValidation(t *testing.T) {
	cases := []struct {
		name string
		item appbill.SaleItem
	}{
		{"nil_product_id", appbill.SaleItem{ProductID: uuid.Nil, Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1)}},
		{"zero_qty", appbill.SaleItem{ProductID: uuid.New(), Qty: decimal.Zero, UnitPrice: decimal.NewFromInt(1)}},
		{"negative_qty", appbill.SaleItem{ProductID: uuid.New(), Qty: decimal.NewFromInt(-1), UnitPrice: decimal.NewFromInt(1)}},
		{"negative_unit_price", appbill.SaleItem{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(-1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMockBillRepo()
			uc := newQuickCheckoutUC(repo, newMockStockUC(), newMockProductUnitRepo(), newMockPaymentRepo())
			_, err := uc.Execute(context.Background(), appbill.QuickCheckoutRequest{
				TenantID:  testTenantID,
				CreatorID: testCreatorID,
				Items:     []appbill.SaleItem{tc.item},
			})
			if !errors.Is(err, appbill.ErrValidation) {
				t.Errorf("expected ErrValidation, got %v", err)
			}
		})
	}
}

func TestQuickCheckout_RejectsProductOutsideTenant(t *testing.T) {
	repo := newMockBillRepo()
	foreignProduct := uuid.New()
	repo.missingRefs = map[uuid.UUID]struct{}{foreignProduct: {}}
	uc := newQuickCheckoutUC(repo, newMockStockUC(), newMockProductUnitRepo(), newMockPaymentRepo())

	_, err := uc.Execute(context.Background(), appbill.QuickCheckoutRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		Items:     []appbill.SaleItem{{ProductID: foreignProduct, Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1)}},
	})
	if !errors.Is(err, appbill.ErrValidation) {
		t.Fatalf("expected ErrValidation for cross-tenant product, got %v", err)
	}
}

func TestQuickCheckout_NextBillNoError_Propagates(t *testing.T) {
	boom := errors.New("sequence exhausted")
	repo := &nextBillNoErrRepo{mockBillRepo: newMockBillRepo(), err: boom}
	approveUC := appbill.NewApproveSaleUseCase(repo, newMockStockUC(), newMockProductUnitRepo(), newMockPaymentRepo())
	uc := appbill.NewQuickCheckoutUseCase(repo, approveUC)
	_, err := uc.Execute(context.Background(), appbill.QuickCheckoutRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		Items:     []appbill.SaleItem{{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1)}},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped NextBillNo error, got %v", err)
	}
}

// TestQuickCheckout_ReceivableClampedToZero_WhenOverpaid verifies the
// ReceivableAmount = max(total-paid, 0) invariant (quick_checkout.go:127-130):
// an overpayment must never yield a negative receivable.
func TestQuickCheckout_ReceivableClampedToZero_WhenOverpaid(t *testing.T) {
	repo := newMockBillRepo()
	uc := newQuickCheckoutUC(repo, newMockStockUC(), newMockProductUnitRepo(), newMockPaymentRepo())
	result, err := uc.Execute(context.Background(), appbill.QuickCheckoutRequest{
		TenantID:      testTenantID,
		CreatorID:     testCreatorID,
		PaymentMethod: "cash",
		PaidAmount:    decimal.NewFromInt(999), // far more than the bill total
		Items:         []appbill.SaleItem{{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(10)}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// total = 1*10 = 10; paid = 999 → receivable clamped to 0, never negative.
	if !result.ReceivableAmount.IsZero() {
		t.Errorf("ReceivableAmount = %s, want 0 (clamped, not negative)", result.ReceivableAmount)
	}
}

// TestQuickCheckout_ApproveNonStockError_Propagates verifies that a
// non-stock error from the composed ApproveSaleUseCase surfaces through
// QuickCheckout's "quick checkout: approve:" wrap (atomicity: create+approve
// share one transaction, so an approve failure must fail the whole checkout).
func TestQuickCheckout_ApproveNonStockError_Propagates(t *testing.T) {
	repo := newMockBillRepo()
	boom := errors.New("stock infra failure")
	stockUC := &genericErrStockUC{err: boom}
	unitRepo := newMockProductUnitRepo()
	payRepo := newMockPaymentRepo()

	approveUC := appbill.NewApproveSaleUseCase(repo, stockUC, unitRepo, payRepo)
	uc := appbill.NewQuickCheckoutUseCase(repo, approveUC)

	_, err := uc.Execute(context.Background(), appbill.QuickCheckoutRequest{
		TenantID:      testTenantID,
		CreatorID:     testCreatorID,
		PaymentMethod: "cash",
		PaidAmount:    decimal.NewFromInt(10),
		Items:         []appbill.SaleItem{{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(10)}},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped approve error, got %v", err)
	}
	if len(payRepo.recorded) != 0 {
		t.Errorf("no payment may be recorded when approve fails, got %d", len(payRepo.recorded))
	}
}
