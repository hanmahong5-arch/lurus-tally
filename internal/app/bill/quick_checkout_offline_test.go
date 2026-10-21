package bill_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// offlineRepo adds the optional capabilities the repository adapter provides:
// client_ref lookup / unique-index conflict and the negative-stock operation-log note.
type offlineRepo struct {
	*mockBillRepo
	existingID uuid.UUID
	existingNo string
	existingRf string // client_ref already booked ("" = none)
	raceOnly   bool   // the pre-check misses it, only the unique index catches it
	conflicted bool   // CreateBill already hit the unique index once
	notes      []negNote
}

type negNote struct {
	billID, productID uuid.UUID
	onHand            decimal.Decimal
}

func (r *offlineRepo) FindBillByClientRef(_ context.Context, _ uuid.UUID, ref string) (uuid.UUID, string, bool, error) {
	if r.existingRf != "" && ref == r.existingRf && (!r.raceOnly || r.conflicted) {
		return r.existingID, r.existingNo, true, nil
	}
	return uuid.Nil, "", false, nil
}

func (r *offlineRepo) CreateBill(ctx context.Context, tx *sql.Tx, head *domain.BillHead, items []*domain.BillItem) error {
	if r.existingRf != "" && head.ClientRef == r.existingRf {
		r.conflicted = true
		return appbill.ErrDuplicateClientRef
	}
	return r.mockBillRepo.CreateBill(ctx, tx, head, items)
}

func (r *offlineRepo) RecordNegativeStock(_ context.Context, _ *sql.Tx, _, billID, _, productID uuid.UUID, onHand decimal.Decimal) error {
	r.notes = append(r.notes, negNote{billID, productID, onHand})
	return nil
}

// shortStockUC models a warehouse holding onHand units: an outbound beyond it fails
// unless the request allows negative stock, exactly like the real calculators.
type shortStockUC struct {
	onHand decimal.Decimal
	calls  []appstock.RecordMovementRequest
}

func (s *shortStockUC) ExecuteInTx(_ context.Context, _ *sql.Tx, req appstock.RecordMovementRequest) (*domainstock.Snapshot, error) {
	if req.Qty.GreaterThan(s.onHand) && !req.AllowNegative {
		return nil, &appstock.InsufficientStockError{ProductID: req.ProductID, Available: s.onHand, Requested: req.Qty}
	}
	s.calls = append(s.calls, req)
	return &domainstock.Snapshot{ProductID: req.ProductID, OnHandQty: s.onHand.Sub(req.Qty)}, nil
}

func newOfflineUC(repo *offlineRepo, stockUC *shortStockUC) *appbill.QuickCheckoutUseCase {
	approveUC := appbill.NewApproveSaleUseCase(repo, stockUC, newMockProductUnitRepo(), newMockPaymentRepo())
	return appbill.NewQuickCheckoutUseCase(repo, approveUC)
}

func offlineReq(source, ref string, occurred time.Time) appbill.QuickCheckoutRequest {
	// pos_offline 必须带 client_ref 与 occurred_at；辅助函数按需补齐，专测校验的用例走 rawOfflineReq。
	if source == "pos_offline" {
		if ref == "" {
			ref = "auto-ref"
		}
		if occurred.IsZero() {
			occurred = time.Now().Add(-time.Hour)
		}
	}
	return rawOfflineReq(source, ref, occurred)
}

func rawOfflineReq(source, ref string, occurred time.Time) appbill.QuickCheckoutRequest {
	return appbill.QuickCheckoutRequest{
		TenantID: testTenantID, CreatorID: testCreatorID,
		PaymentMethod: "cash", PaidAmount: decimal.NewFromInt(50),
		Items:      quickCheckoutItems(10, 5),
		Source:     source,
		ClientRef:  ref,
		OccurredAt: occurred,
	}
}

func TestQuickCheckout_DuplicateClientRef_ReturnsExistingBill(t *testing.T) {
	existing := uuid.New()
	for _, race := range []bool{false, true} {
		repo := &offlineRepo{mockBillRepo: newMockBillRepo(), existingID: existing, existingNo: "SL-20261015-0007", existingRf: "ref-1", raceOnly: race}
		uc := newOfflineUC(repo, &shortStockUC{onHand: decimal.NewFromInt(100)})
		_, err := uc.Execute(context.Background(), offlineReq("pos_offline", "ref-1", time.Time{}))
		var dup *appbill.DuplicateCheckoutError
		if !errors.As(err, &dup) || !errors.Is(err, appbill.ErrCheckoutAlreadyExists) {
			t.Fatalf("race=%v: want DuplicateCheckoutError, got %v", race, err)
		}
		if dup.BillID != existing || dup.BillNo != "SL-20261015-0007" {
			t.Fatalf("race=%v: wrong existing bill: %+v", race, dup)
		}
		if race && !repo.conflicted {
			t.Fatal("race case must go through the unique-index conflict path")
		}
	}
}

func TestQuickCheckout_NegativeStock_OnlyPosOfflineAndNoted(t *testing.T) {
	// 普通 POS：库存不足照旧拒绝。
	repo := &offlineRepo{mockBillRepo: newMockBillRepo()}
	stockUC := &shortStockUC{onHand: decimal.NewFromInt(2)}
	_, err := newOfflineUC(repo, stockUC).Execute(context.Background(), offlineReq("pos", "", time.Time{}))
	var bise *appstock.BatchInsufficientStockError
	if !errors.As(err, &bise) {
		t.Fatalf("pos 来源库存不足应被拒绝, got %v", err)
	}
	if len(repo.notes) != 0 {
		t.Fatalf("被拒的单不应留负库存记录")
	}

	// 离线回放：放行，库存 2 - 5 = -3，留一条盘点提醒。
	repo = &offlineRepo{mockBillRepo: newMockBillRepo()}
	stockUC = &shortStockUC{onHand: decimal.NewFromInt(2)}
	res, err := newOfflineUC(repo, stockUC).Execute(context.Background(), offlineReq("pos_offline", "off-1", time.Time{}))
	if err != nil {
		t.Fatalf("pos_offline 应放行: %v", err)
	}
	if len(repo.notes) != 1 || repo.notes[0].billID != res.BillID || !repo.notes[0].onHand.Equal(decimal.NewFromInt(-3)) {
		t.Fatalf("负库存记录不对: %+v", repo.notes)
	}
	if !stockUC.calls[0].AllowNegative {
		t.Fatal("pos_offline 应向库存层传 AllowNegative")
	}
	if h := repo.storedHead; h.Source != "pos_offline" || h.ClientRef != "off-1" {
		t.Fatalf("来源/client_ref 未落单: %+v", h)
	}

	// 库存够时不留提醒。
	repo = &offlineRepo{mockBillRepo: newMockBillRepo()}
	if _, err := newOfflineUC(repo, &shortStockUC{onHand: decimal.NewFromInt(50)}).Execute(context.Background(), offlineReq("pos_offline", "off-2", time.Time{})); err != nil {
		t.Fatal(err)
	}
	if len(repo.notes) != 0 {
		t.Fatalf("库存充足不应留提醒: %+v", repo.notes)
	}
}

func TestQuickCheckout_OccurredAtBounds(t *testing.T) {
	cases := []struct {
		name string
		off  time.Duration // relative to now
		ok   bool
	}{
		{"4m ahead ok", 4 * time.Minute, true},
		{"6m ahead rejected", 6 * time.Minute, false},
		{"29d ago ok", -29 * 24 * time.Hour, true},
		{"31d ago rejected", -31 * 24 * time.Hour, false},
		{"yesterday ok", -24 * time.Hour, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &offlineRepo{mockBillRepo: newMockBillRepo()}
			at := time.Now().Add(tc.off)
			_, err := newOfflineUC(repo, &shortStockUC{onHand: decimal.NewFromInt(100)}).Execute(context.Background(), offlineReq("pos_offline", "", at))
			if !tc.ok {
				if !errors.Is(err, appbill.ErrValidation) {
					t.Fatalf("want ErrValidation, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			h := repo.storedHead
			if h.OccurredAt == nil || h.OccurredAt.Sub(at).Abs() > time.Second || time.Since(h.BillDate) > time.Minute {
				t.Fatalf("occurred_at 应保留、bill_date 应按重放时刻: %+v", h)
			}
		})
	}
}

func TestQuickCheckout_OccurredAtOmitted_UsesNow(t *testing.T) {
	repo := &offlineRepo{mockBillRepo: newMockBillRepo()}
	if _, err := newOfflineUC(repo, &shortStockUC{onHand: decimal.NewFromInt(100)}).Execute(context.Background(), offlineReq("", "", time.Time{})); err != nil {
		t.Fatal(err)
	}
	h := repo.storedHead
	if h.OccurredAt != nil || time.Since(h.BillDate) > time.Minute || h.Source != "pos" {
		t.Fatalf("缺省应为 now / source=pos / occurred_at 为空: %+v", h)
	}
}

func TestQuickCheckout_RejectsBadClientRefAndSource(t *testing.T) {
	for name, req := range map[string]appbill.QuickCheckoutRequest{
		"client_ref 65": offlineReq("pos", strings.Repeat("a", 65), time.Time{}),
		"client_ref 64": offlineReq("pos", strings.Repeat("a", 64), time.Time{}),
		"bad source":    offlineReq("shopify", "", time.Time{}),
	} {
		repo := &offlineRepo{mockBillRepo: newMockBillRepo()}
		_, err := newOfflineUC(repo, &shortStockUC{onHand: decimal.NewFromInt(100)}).Execute(context.Background(), req)
		wantOK := name == "client_ref 64"
		if wantOK && err != nil {
			t.Fatalf("%s: want ok, got %v", name, err)
		}
		if !wantOK && !errors.Is(err, appbill.ErrValidation) {
			t.Fatalf("%s: want ErrValidation, got %v", name, err)
		}
	}
}

func TestQuickCheckout_PosOfflineRequiresClientRefAndOccurredAt(t *testing.T) {
	for name, req := range map[string]appbill.QuickCheckoutRequest{
		"缺 client_ref":  rawOfflineReq("pos_offline", "", time.Now().Add(-time.Hour)),
		"缺 occurred_at": rawOfflineReq("pos_offline", "r-1", time.Time{}),
	} {
		repo := &offlineRepo{mockBillRepo: newMockBillRepo()}
		stockUC := &shortStockUC{onHand: decimal.Zero}
		_, err := newOfflineUC(repo, stockUC).Execute(context.Background(), req)
		if !errors.Is(err, appbill.ErrValidation) {
			t.Fatalf("%s: want ErrValidation, got %v", name, err)
		}
		if len(stockUC.calls) != 0 {
			t.Fatalf("%s: 不应出库", name)
		}
	}
}
