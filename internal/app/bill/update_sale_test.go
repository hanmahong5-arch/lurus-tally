package bill_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

func updateSaleReq(billID uuid.UUID, qty, price string) appbill.UpdateSaleDraftRequest {
	return appbill.UpdateSaleDraftRequest{
		TenantID:    testTenantID,
		BillID:      billID,
		ShippingFee: decimal.RequireFromString("3"),
		Items: []appbill.SaleItem{
			{ProductID: uuid.New(), LineNo: 1, Qty: decimal.RequireFromString(qty), UnitPrice: decimal.RequireFromString(price)},
		},
	}
}

// 草稿改数量、改单价后，行和合计按新值重算；审核读到的就是新数量。
func TestUpdateSaleDraft_ReplacesLinesAndRecalculatesTotals(t *testing.T) {
	repo := newMockBillRepo()
	billID := seedSaleDraftBill(repo, 2, uuid.New())

	head, err := appbill.NewUpdateSaleDraftUseCase(repo).Execute(context.Background(), updateSaleReq(billID, "4", "12.5"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !head.Subtotal.Equal(decimal.NewFromInt(50)) || !head.TotalAmount.Equal(decimal.NewFromInt(53)) {
		t.Errorf("subtotal=%s total=%s, want 50 / 53", head.Subtotal, head.TotalAmount)
	}
	items := repo.itemsByBillID[billID]
	if len(items) != 1 || !items[0].Qty.Equal(decimal.NewFromInt(4)) || !items[0].LineAmount.Equal(decimal.NewFromInt(50)) {
		t.Errorf("items = %+v, want one line of 4 at 50", items)
	}
	if head.Status != domain.StatusDraft || head.BillNo == "" {
		t.Errorf("head = %+v, want the same draft kept", head)
	}
}

// 已审核的销售单改了就是改库存和账，只能走退货。
func TestUpdateSaleDraft_OnlyDrafts(t *testing.T) {
	repo := newMockBillRepo()
	billID := seedSaleDraftBill(repo, 1, uuid.New())
	repo.billsByID[billID].Status = domain.StatusApproved
	_, err := appbill.NewUpdateSaleDraftUseCase(repo).Execute(context.Background(), updateSaleReq(billID, "1", "1"))
	if !errors.Is(err, appbill.ErrInvalidBillStatus) {
		t.Fatalf("err = %v, want ErrInvalidBillStatus", err)
	}
}

// 防的事故：拿采购草稿的 id 调销售改单接口，把入库单改成了销售数据。
func TestUpdateSaleDraft_RejectsPurchaseBill(t *testing.T) {
	repo := newMockBillRepo()
	billID := seedDraftBill(repo, 1, uuid.New())
	_, err := appbill.NewUpdateSaleDraftUseCase(repo).Execute(context.Background(), updateSaleReq(billID, "1", "1"))
	if !errors.Is(err, appbill.ErrBillNotFound) {
		t.Fatalf("err = %v, want ErrBillNotFound", err)
	}
}

func TestUpdateSaleDraft_ValidatesInput(t *testing.T) {
	repo := newMockBillRepo()
	billID := seedSaleDraftBill(repo, 1, uuid.New())
	uc := appbill.NewUpdateSaleDraftUseCase(repo)

	noItems := updateSaleReq(billID, "1", "1")
	noItems.Items = nil
	if _, err := uc.Execute(context.Background(), noItems); !errors.Is(err, appbill.ErrValidation) {
		t.Errorf("no items: err = %v, want ErrValidation", err)
	}
	if _, err := uc.Execute(context.Background(), updateSaleReq(billID, "0", "1")); !errors.Is(err, appbill.ErrValidation) {
		t.Errorf("zero qty: err = %v, want ErrValidation", err)
	}
	if _, err := uc.Execute(context.Background(), updateSaleReq(billID, "1", "-1")); !errors.Is(err, appbill.ErrValidation) {
		t.Errorf("negative price: err = %v, want ErrValidation", err)
	}
	neg := updateSaleReq(billID, "1", "1")
	neg.TaxAmount = decimal.NewFromInt(-1)
	if _, err := uc.Execute(context.Background(), neg); !errors.Is(err, appbill.ErrNegativeFee) {
		t.Errorf("negative tax: err = %v, want ErrNegativeFee", err)
	}
}
