package bill_test

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// TestCreateSaleDraft_ValidRequest_ReturnsBillID verifies a valid request produces a bill ID and
// SL-prefixed bill number.
func TestCreateSaleDraft_ValidRequest_ReturnsBillID(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreateSaleUseCase(repo)

	req := appbill.CreateSaleRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		BillDate:  time.Now(),
		Items: []appbill.SaleItem{
			{ProductID: uuid.New(), WarehouseID: uuid.New(), Qty: decimal.NewFromFloat(5), UnitPrice: decimal.NewFromFloat(20), LineNo: 1},
			{ProductID: uuid.New(), WarehouseID: uuid.New(), Qty: decimal.NewFromFloat(3), UnitPrice: decimal.NewFromFloat(10), LineNo: 2},
		},
	}

	out, err := uc.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.BillID == uuid.Nil {
		t.Error("BillID is nil UUID")
	}
	ok, _ := regexp.MatchString(`^SL-\d{8}-\d{4}$`, out.BillNo)
	if !ok {
		t.Errorf("BillNo format invalid: %q", out.BillNo)
	}
	if repo.storedHead == nil {
		t.Fatal("head was not stored")
	}
	if repo.storedHead.BillType != domain.BillTypeSale {
		t.Errorf("BillType = %q, want %q", repo.storedHead.BillType, domain.BillTypeSale)
	}
	if repo.storedHead.SubType != domain.BillSubTypeSale {
		t.Errorf("SubType = %q, want %q", repo.storedHead.SubType, domain.BillSubTypeSale)
	}
	if repo.storedHead.Status != domain.StatusDraft {
		t.Errorf("Status = %d, want %d (Draft)", repo.storedHead.Status, domain.StatusDraft)
	}
	// total = 5*20 + 3*10 = 100+30 = 130
	wantTotal := decimal.NewFromFloat(130)
	if !repo.storedHead.TotalAmount.Equal(wantTotal) {
		t.Errorf("TotalAmount = %s, want %s", repo.storedHead.TotalAmount, wantTotal)
	}
}

// TestCreateSaleDraft_EmptyItems_ReturnsError verifies empty items returns validation error.
func TestCreateSaleDraft_EmptyItems_ReturnsError(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreateSaleUseCase(repo)

	_, err := uc.Execute(context.Background(), appbill.CreateSaleRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		BillDate:  time.Now(),
		Items:     nil,
	})
	if err == nil {
		t.Fatal("expected validation error for zero items, got nil")
	}
}

// TestCreateSaleDraft_MissingTenantID_ReturnsError validates required tenant guard.
func TestCreateSaleDraft_MissingTenantID_ReturnsError(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreateSaleUseCase(repo)

	_, err := uc.Execute(context.Background(), appbill.CreateSaleRequest{
		CreatorID: testCreatorID,
		BillDate:  time.Now(),
		Items: []appbill.SaleItem{
			{ProductID: uuid.New(), Qty: decimal.NewFromFloat(1), UnitPrice: decimal.NewFromFloat(10), LineNo: 1},
		},
	})
	if err == nil {
		t.Fatal("expected error for missing TenantID, got nil")
	}
}
