package bill_test

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// TestCreateReturnBill_ValidRequest_ReturnsBillID verifies a valid request produces
// a bill with RT-prefixed bill_no, bill_type=入库, sub_type=销售退货, status=draft.
func TestCreateReturnBill_ValidRequest_ReturnsBillID(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreateReturnBillUseCase(repo)

	req := appbill.CreateReturnRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		BillDate:  time.Now(),
		Remark:    "cancel:shopify:order-001:original_bill_id=abc",
		Items: []appbill.ReturnItem{
			{ProductID: uuid.New(), LineNo: 1, Qty: decimal.NewFromFloat(3), UnitPrice: decimal.NewFromFloat(50)},
			{ProductID: uuid.New(), LineNo: 2, Qty: decimal.NewFromFloat(1), UnitPrice: decimal.NewFromFloat(100)},
		},
	}

	out, err := uc.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.BillID == uuid.Nil {
		t.Error("BillID is nil UUID")
	}
	ok, _ := regexp.MatchString(`^RT-\d{8}-\d{4}$`, out.BillNo)
	if !ok {
		t.Errorf("BillNo format invalid: %q", out.BillNo)
	}
	if repo.storedHead == nil {
		t.Fatal("head was not stored")
	}
	if repo.storedHead.BillType != domain.BillTypePurchase {
		t.Errorf("BillType = %q, want %q (入库)", repo.storedHead.BillType, domain.BillTypePurchase)
	}
	if repo.storedHead.SubType != domain.BillSubTypeSaleReturn {
		t.Errorf("SubType = %q, want %q (销售退货)", repo.storedHead.SubType, domain.BillSubTypeSaleReturn)
	}
	if repo.storedHead.Status != domain.StatusDraft {
		t.Errorf("Status = %d, want %d (Draft)", repo.storedHead.Status, domain.StatusDraft)
	}
	// total = 3*50 + 1*100 = 250
	wantTotal := decimal.NewFromFloat(250)
	if !repo.storedHead.TotalAmount.Equal(wantTotal) {
		t.Errorf("TotalAmount = %s, want %s", repo.storedHead.TotalAmount, wantTotal)
	}
}

// TestCreateReturnBill_EmptyItems_ReturnsValidationError verifies zero items returns ErrValidation.
func TestCreateReturnBill_EmptyItems_ReturnsValidationError(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreateReturnBillUseCase(repo)

	_, err := uc.Execute(context.Background(), appbill.CreateReturnRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		BillDate:  time.Now(),
		Items:     nil,
	})
	if err == nil {
		t.Fatal("expected validation error for zero items, got nil")
	}
	if !errors.Is(err, appbill.ErrValidation) {
		t.Errorf("expected ErrValidation, got %T: %v", err, err)
	}
}

// TestCreateReturnBill_MissingTenantID_ReturnsValidationError verifies required tenant guard.
func TestCreateReturnBill_MissingTenantID_ReturnsValidationError(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreateReturnBillUseCase(repo)

	_, err := uc.Execute(context.Background(), appbill.CreateReturnRequest{
		CreatorID: testCreatorID,
		BillDate:  time.Now(),
		Items: []appbill.ReturnItem{
			{ProductID: uuid.New(), LineNo: 1, Qty: decimal.NewFromFloat(1), UnitPrice: decimal.NewFromFloat(10)},
		},
	})
	if err == nil {
		t.Fatal("expected validation error for missing tenant_id, got nil")
	}
	if !errors.Is(err, appbill.ErrValidation) {
		t.Errorf("expected ErrValidation, got %T: %v", err, err)
	}
}
