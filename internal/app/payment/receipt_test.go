package payment_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

var testPartnerID = uuid.New()

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// receiptLedger serves the open bills of one partner, oldest first.
type receiptLedger struct{ open []apppayment.OpenBill }

func (l *receiptLedger) OpenBills(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID) ([]apppayment.OpenBill, error) {
	return l.open, nil
}

func (l *receiptLedger) Statement(_ context.Context, _ apppayment.StatementQuery) (*apppayment.Statement, error) {
	return &apppayment.Statement{}, nil
}

// receiptBills is a bill reader whose paid totals follow the payments the use case writes.
type receiptBills struct {
	heads    map[uuid.UUID]*domain.BillHead
	returned map[uuid.UUID]decimal.Decimal
	failOn   int // fail UpdatePaidAmount on the Nth call (1-based); 0 = never
	updates  int
	locked   []uuid.UUID
}

func (b *receiptBills) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error {
	// Emulate rollback: restore paid amounts when fn fails.
	saved := map[uuid.UUID]decimal.Decimal{}
	for id, h := range b.heads {
		saved[id] = h.PaidAmount
	}
	if err := fn(nil); err != nil { //nolint:staticcheck
		for id, p := range saved {
			b.heads[id].PaidAmount = p
		}
		return err
	}
	return nil
}

func (b *receiptBills) GetBillForUpdate(_ context.Context, _ *sql.Tx, _, id uuid.UUID) (*domain.BillHead, error) {
	return b.heads[id], nil
}

func (b *receiptBills) UpdatePaidAmount(_ context.Context, _ *sql.Tx, _, id uuid.UUID, paid decimal.Decimal) error {
	b.updates++
	if b.failOn != 0 && b.updates == b.failOn {
		return errors.New("boom")
	}
	b.heads[id].PaidAmount = paid
	return nil
}

func (b *receiptBills) AcquireBillAdvisoryLock(_ context.Context, _ *sql.Tx, _, id uuid.UUID) error {
	b.locked = append(b.locked, id)
	return nil
}

func (b *receiptBills) ReturnedAmount(_ context.Context, _, id uuid.UUID) (decimal.Decimal, error) {
	return b.returned[id], nil
}

type receiptPayments struct {
	bills    *receiptBills
	recorded []*domainpayment.Payment
}

func (p *receiptPayments) Record(_ context.Context, _ *sql.Tx, pay *domainpayment.Payment) error {
	p.recorded = append(p.recorded, pay)
	return nil
}
func (p *receiptPayments) ListByBill(context.Context, uuid.UUID, uuid.UUID) ([]*domainpayment.Payment, error) {
	return nil, nil
}
func (p *receiptPayments) SumByBill(_ context.Context, _ *sql.Tx, _, id uuid.UUID) (decimal.Decimal, error) {
	return p.bills.heads[id].PaidAmount, nil
}
func (p *receiptPayments) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	return p.bills.WithTx(ctx, fn)
}

// threeBills seeds a partner owing 100, 200, 300 on three approved bills, oldest first.
func threeBills(billType domain.BillType) (*receiptLedger, *receiptBills, *receiptPayments, []uuid.UUID) {
	partner := testPartnerID
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	totals := []string{"100", "200", "300"}
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	l := &receiptLedger{}
	b := &receiptBills{heads: map[uuid.UUID]*domain.BillHead{}, returned: map[uuid.UUID]decimal.Decimal{}}
	for i, id := range ids {
		b.heads[id] = &domain.BillHead{ID: id, TenantID: testTenantID, BillType: billType, Status: domain.StatusApproved,
			PartnerID: &partner, TotalAmount: dec(totals[i]), PaidAmount: decimal.Zero}
		l.open = append(l.open, apppayment.OpenBill{BillID: id, BillNo: "SO-" + totals[i], BillDate: base.AddDate(0, 0, i),
			Total: dec(totals[i]), Outstanding: dec(totals[i])})
	}
	p := &receiptPayments{bills: b}
	return l, b, p, ids
}

func receipt(dir, amount, payType string) apppayment.ReceiptRequest {
	return apppayment.ReceiptRequest{TenantID: testTenantID, CreatorID: testCreatorID, Direction: dir,
		PartnerID: testPartnerID, Amount: dec(amount), PayType: payType}
}

func TestReceipt_FIFO_PartialSecondBill(t *testing.T) {
	l, b, p, ids := threeBills(domain.BillTypeSale)
	uc := apppayment.NewReceiptUseCase(l, b, p)
	res, err := uc.Execute(context.Background(), receipt("ar", "250", "cash"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Allocations) != 2 {
		t.Fatalf("allocations = %d, want 2", len(res.Allocations))
	}
	if !res.Allocations[0].Applied.Equal(dec("100")) || !res.Allocations[0].Remaining.IsZero() {
		t.Errorf("first = %+v, want settled", res.Allocations[0])
	}
	if !res.Allocations[1].Applied.Equal(dec("150")) || !res.Allocations[1].Remaining.Equal(dec("50")) {
		t.Errorf("second = %+v, want applied 150 remaining 50", res.Allocations[1])
	}
	if len(p.recorded) != 2 || !b.heads[ids[0]].PaidAmount.Equal(dec("100")) || !b.heads[ids[1]].PaidAmount.Equal(dec("150")) || !b.heads[ids[2]].PaidAmount.IsZero() {
		t.Errorf("recorded=%d paid=%v/%v/%v", len(p.recorded), b.heads[ids[0]].PaidAmount, b.heads[ids[1]].PaidAmount, b.heads[ids[2]].PaidAmount)
	}
}

func TestReceipt_ExceedsTotalDebt(t *testing.T) {
	l, b, p, _ := threeBills(domain.BillTypeSale)
	uc := apppayment.NewReceiptUseCase(l, b, p)
	_, err := uc.Execute(context.Background(), receipt("ar", "600.01", "cash"))
	var ex *apppayment.ExceedsPartnerDebtError
	if !errors.As(err, &ex) || !ex.Owed.Equal(dec("600")) {
		t.Fatalf("err = %v, want ExceedsPartnerDebtError owed 600", err)
	}
	if len(p.recorded) != 0 {
		t.Errorf("recorded %d payments on rejection", len(p.recorded))
	}
}

func TestReceipt_NetsReturnsUnderLock(t *testing.T) {
	l, b, p, ids := threeBills(domain.BillTypeSale)
	b.returned[ids[0]] = dec("40") // first sale: 100 - 40 returned = 60 owed
	uc := apppayment.NewReceiptUseCase(l, b, p)
	res, err := uc.Execute(context.Background(), receipt("ar", "100", "cash"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Allocations[0].Applied.Equal(dec("60")) || !res.Allocations[1].Applied.Equal(dec("40")) {
		t.Errorf("allocations = %+v", res.Allocations)
	}
}

func TestReceipt_LocksSalesBeforeReadingReturns(t *testing.T) {
	l, b, p, ids := threeBills(domain.BillTypeSale)
	uc := apppayment.NewReceiptUseCase(l, b, p)
	if _, err := uc.Execute(context.Background(), receipt("ar", "100", "cash")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(b.locked) != len(ids) || b.locked[0] != ids[0] {
		t.Errorf("locked = %v, want every candidate sale in order", b.locked)
	}
}

func TestReceipt_RollsBackOnFailure(t *testing.T) {
	l, b, p, ids := threeBills(domain.BillTypeSale)
	b.failOn = 2
	uc := apppayment.NewReceiptUseCase(l, b, p)
	if _, err := uc.Execute(context.Background(), receipt("ar", "250", "cash")); err == nil {
		t.Fatal("want error")
	}
	if !b.heads[ids[0]].PaidAmount.IsZero() {
		t.Errorf("first bill paid = %v after rollback, want 0", b.heads[ids[0]].PaidAmount)
	}
}

func TestReceipt_Validation(t *testing.T) {
	l, b, p, _ := threeBills(domain.BillTypeSale)
	uc := apppayment.NewReceiptUseCase(l, b, p)
	ctx := context.Background()

	if _, err := uc.Execute(ctx, receipt("x", "10", "cash")); !errors.Is(err, apppayment.ErrInvalidReceiptDirection) {
		t.Errorf("direction err = %v", err)
	}
	if _, err := uc.Execute(ctx, receipt("ar", "10", "bitcoin")); !errors.Is(err, apppayment.ErrInvalidPayType) {
		t.Errorf("pay type err = %v", err)
	}
	if _, err := uc.Execute(ctx, receipt("ar", "0", "cash")); !errors.Is(err, apppayment.ErrReceiptAmount) {
		t.Errorf("amount err = %v", err)
	}
	if _, err := uc.Execute(ctx, receipt("ar", "1.005", "cash")); !errors.Is(err, apppayment.ErrReceiptAmount) {
		t.Errorf("3-decimal amount err = %v", err)
	}
}

func TestReceipt_NoDebt(t *testing.T) {
	l, b, p, _ := threeBills(domain.BillTypeSale)
	l.open = nil
	uc := apppayment.NewReceiptUseCase(l, b, p)
	_, err := uc.Execute(context.Background(), receipt("ar", "1", "cash"))
	var ex *apppayment.ExceedsPartnerDebtError
	if !errors.As(err, &ex) || !ex.Owed.IsZero() {
		t.Fatalf("err = %v, want owed 0", err)
	}
}

func TestReceipt_PreviewWritesNothing(t *testing.T) {
	l, b, p, _ := threeBills(domain.BillTypeSale)
	uc := apppayment.NewReceiptUseCase(l, b, p)
	res, err := uc.Preview(context.Background(), testTenantID, "ar", testPartnerID, dec("250"))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(res.Allocations) != 2 || !res.Owed.Equal(dec("600")) || len(p.recorded) != 0 {
		t.Errorf("res=%+v recorded=%d", res, len(p.recorded))
	}
}

func TestReceipt_Purchase_SkipsReturnNetting(t *testing.T) {
	l, b, p, ids := threeBills(domain.BillTypePurchase)
	b.returned[ids[0]] = dec("40") // a purchase has no sales returns; must be ignored
	uc := apppayment.NewReceiptUseCase(l, b, p)
	res, err := uc.Execute(context.Background(), receipt("ap", "100", "transfer"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Allocations) != 1 || !res.Allocations[0].Applied.Equal(dec("100")) {
		t.Errorf("allocations = %+v", res.Allocations)
	}
}

func TestReceipt_CreditIsNotAPaymentMethod(t *testing.T) {
	l, b, p, _ := threeBills(domain.BillTypeSale)
	uc := apppayment.NewReceiptUseCase(l, b, p)
	_, err := uc.Execute(context.Background(), receipt("ar", "100", "credit"))
	if !errors.Is(err, apppayment.ErrCreditNotPayment) {
		t.Fatalf("err = %v, want ErrCreditNotPayment", err)
	}
	if len(p.recorded) != 0 {
		t.Errorf("recorded %d payments on rejection", len(p.recorded))
	}
}

// A legacy four-decimal bill is settled by the rounded remainder, booked to the cent.
func TestReceipt_LegacyFourDecimalBill_SettlesCompletely(t *testing.T) {
	l, b, p, ids := threeBills(domain.BillTypeSale)
	b.heads[ids[0]].TotalAmount = dec("41.625")
	l.open[0].Outstanding = dec("41.625")
	uc := apppayment.NewReceiptUseCase(l, b, p)
	res, err := uc.Execute(context.Background(), receipt("ar", "41.63", "cash"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Allocations) != 1 || !res.Allocations[0].Remaining.IsZero() {
		t.Fatalf("allocations = %+v, want one settled", res.Allocations)
	}
	if !b.heads[ids[0]].PaidAmount.Equal(dec("41.625")) || !p.recorded[0].Amount.Equal(dec("41.63")) {
		t.Errorf("paid = %s recorded = %s, want 41.625 / 41.63", b.heads[ids[0]].PaidAmount, p.recorded[0].Amount)
	}
	if !res.Booked.Equal(dec("41.63")) || !res.Diff.Equal(dec("0.005")) {
		t.Errorf("booked/diff = %s/%s, want 41.63/0.005", res.Booked, res.Diff)
	}
}

// The payments of one receipt add up to exactly the money handed over, so the till and the
// books agree; the tail is reported, not dropped.
func TestReceipt_TwoLegacyBills_BooksTheWholeAmount(t *testing.T) {
	l, b, p, ids := threeBills(domain.BillTypeSale)
	l.open = l.open[:2]
	for i := 0; i < 2; i++ {
		b.heads[ids[i]].TotalAmount = dec("41.625")
		l.open[i].Outstanding = dec("41.63")
	}
	uc := apppayment.NewReceiptUseCase(l, b, p)
	res, err := uc.Execute(context.Background(), receipt("ar", "83.26", "cash"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Owed.Equal(dec("83.26")) {
		t.Errorf("owed = %s, want 83.26", res.Owed)
	}
	if !res.Amount.Equal(dec("83.26")) || !res.Booked.Equal(dec("83.26")) || !res.Diff.Equal(dec("0.01")) {
		t.Errorf("amount/booked/diff = %s/%s/%s, want 83.26/83.26/0.01", res.Amount, res.Booked, res.Diff)
	}
	sum, applied := decimal.Zero, decimal.Zero
	for _, r := range p.recorded {
		sum = sum.Add(r.Amount)
		if !r.Amount.Equal(r.Amount.Round(2)) {
			t.Errorf("payment %s is not whole cents", r.Amount)
		}
	}
	for _, a := range res.Allocations {
		applied = applied.Add(a.Applied)
		if !a.Remaining.IsZero() {
			t.Errorf("allocation %+v left a remainder", a)
		}
	}
	if !sum.Equal(dec("83.26")) || !applied.Equal(sum) {
		t.Errorf("payments sum = %s applied = %s, want both 83.26", sum, applied)
	}
	for i := 0; i < 2; i++ {
		// paid_amount is capped at the bill total (the table forbids more); the payment rows
		// above carry the whole cent.
		if !b.heads[ids[i]].PaidAmount.Equal(dec("41.625")) {
			t.Errorf("bill %d paid = %s, want 41.625", i, b.heads[ids[i]].PaidAmount)
		}
	}
}

// Three 0.005 bills age as 0.01 each: paying 0.03 books all 0.03, none of it lost.
func TestReceipt_ThreeHalfCentBills_BookEveryCent(t *testing.T) {
	l, b, p, ids := threeBills(domain.BillTypeSale)
	for i := 0; i < 3; i++ {
		b.heads[ids[i]].TotalAmount = dec("0.005")
		l.open[i].Outstanding = dec("0.01")
	}
	uc := apppayment.NewReceiptUseCase(l, b, p)
	res, err := uc.Execute(context.Background(), receipt("ar", "0.03", "cash"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Owed.Equal(dec("0.03")) || !res.Booked.Equal(dec("0.03")) || !res.Diff.Equal(dec("0.015")) {
		t.Errorf("owed/booked/diff = %s/%s/%s, want 0.03/0.03/0.015", res.Owed, res.Booked, res.Diff)
	}
	sum := decimal.Zero
	for _, r := range p.recorded {
		sum = sum.Add(r.Amount)
	}
	if len(p.recorded) != 3 || !sum.Equal(dec("0.03")) {
		t.Errorf("recorded %d payments, sum %s, want 3 / 0.03", len(p.recorded), sum)
	}
	for i := 0; i < 3; i++ {
		if !b.heads[ids[i]].PaidAmount.Equal(dec("0.005")) {
			t.Errorf("bill %d paid = %s, want 0.005 (capped at total)", i, b.heads[ids[i]].PaidAmount)
		}
	}
}

// A raw remainder just under the rounded figure's next cent (41.624 -> 41.62) books the
// rounded money and reports the negative tail.
func TestReceipt_LegacyBillRoundedDown_NegativeDiff(t *testing.T) {
	l, b, p, ids := threeBills(domain.BillTypeSale)
	l.open = l.open[:1]
	b.heads[ids[0]].TotalAmount = dec("41.624")
	uc := apppayment.NewReceiptUseCase(l, b, p)
	res, err := uc.Execute(context.Background(), receipt("ar", "41.62", "cash"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Booked.Equal(dec("41.62")) || !res.Diff.Equal(dec("-0.004")) {
		t.Errorf("booked/diff = %s/%s, want 41.62/-0.004", res.Booked, res.Diff)
	}
	if len(p.recorded) != 1 || !p.recorded[0].Amount.Equal(dec("41.62")) {
		t.Errorf("recorded = %+v", p.recorded)
	}
	// Settled in full: the bill takes the raw remainder, so it leaves the open list.
	if !b.heads[ids[0]].PaidAmount.Equal(dec("41.624")) {
		t.Errorf("paid_amount = %s, want 41.624", b.heads[ids[0]].PaidAmount)
	}
}

// A bill whose remainder rounds to zero takes no payment: it is not an open item.
func TestReceipt_BillRoundingToZero_TakesNoPayment(t *testing.T) {
	l, b, p, ids := threeBills(domain.BillTypeSale)
	l.open = l.open[:2]
	b.heads[ids[1]].TotalAmount = dec("0.004")
	l.open[1].Outstanding = dec("0")
	uc := apppayment.NewReceiptUseCase(l, b, p)
	res, err := uc.Execute(context.Background(), receipt("ar", "100", "cash"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !b.heads[ids[1]].PaidAmount.IsZero() || len(p.recorded) != 1 {
		t.Errorf("tiny bill paid = %s recorded = %d, want 0 / 1", b.heads[ids[1]].PaidAmount, len(p.recorded))
	}
	if !res.Booked.Equal(dec("100")) {
		t.Errorf("booked = %s, want 100", res.Booked)
	}
}
