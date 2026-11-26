package payment_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	domainbill "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

func TestWholesaleFile_BooksBothDirections(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "domain", "payment", "testdata", "industries", "wholesale-alipay.csv"))
	if err != nil {
		t.Fatal(err)
	}
	channel, _, lines, err := domainpayment.ParseMoneyFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]domainpayment.ParsedLine{}
	for _, ln := range lines {
		byID[ln.ExternalID] = ln
	}
	saleID := uuid.New()
	buyID := uuid.New()
	salePay := bookLine(t, channel, byID["W2"], saleID, domainbill.BillTypeSale, domainpayment.MoneyAR)
	buyPay := bookLine(t, channel, byID["W1"], buyID, domainbill.BillTypePurchase, domainpayment.MoneyAP)
	if salePay.PayType != domainpayment.PayTypeAlipay || salePay.Remark != "流水 W2" || !salePay.Amount.Equal(decimal.RequireFromString("2560.00")) {
		t.Fatalf("sale payment = %+v", salePay)
	}
	if buyPay.PayType != domainpayment.PayTypeAlipay || buyPay.Remark != "流水 W1" || !buyPay.Amount.Equal(decimal.RequireFromString("1280.00")) {
		t.Fatalf("purchase payment = %+v", buyPay)
	}
}

func TestBurstConfirm_SameBillBooksOnce(t *testing.T) {
	const callers = 64
	lineID := uuid.New()
	billID := uuid.New()
	led := newBurstLedger(domainpayment.MoneyLine{
		ID: lineID, Channel: domainpayment.ChannelAlipay, ExternalID: "BURST-1",
		Amount: decimal.RequireFromString("1280.00"), Status: domainpayment.MoneyOpen, Counterparty: "A客户",
	}, oneBill(billID, domainbill.BillTypeSale, "1280.00"), oneMoneyBill(billID, "SL-1", "A客户", domainpayment.MoneyAR, "1280.00"))
	led.gate = &openGate{want: callers}
	uc := apppayment.NewMoneyUseCase(led, led, apppayment.NewRecordPaymentUseCase(led, led))
	errs := make([]error, callers)
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = uc.Confirm(context.Background(), testTenantID, testCreatorID, lineID, billID)
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)
	for i, err := range errs {
		if err != nil {
			t.Errorf("call %d: %v", i, err)
		}
	}
	if len(led.pays) != 1 || !led.heads[billID].PaidAmount.Equal(decimal.RequireFromString("1280.00")) {
		t.Fatalf("payments=%d paid=%s", len(led.pays), led.heads[billID].PaidAmount)
	}
	t.Logf("callers=%d elapsed=%s payments=%d", callers, elapsed, len(led.pays))
}

func TestBurstConfirm_TwoBillsLeaveOnePayment(t *testing.T) {
	const callers = 64
	lineID := uuid.New()
	billA := uuid.New()
	billB := uuid.New()
	heads := oneBill(billA, domainbill.BillTypeSale, "1280.00")
	for id, head := range oneBill(billB, domainbill.BillTypeSale, "1280.00") {
		heads[id] = head
	}
	money := oneMoneyBill(billA, "SL-A", "A客户", domainpayment.MoneyAR, "1280.00")
	for id, bill := range oneMoneyBill(billB, "SL-B", "A客户", domainpayment.MoneyAR, "1280.00") {
		money[id] = bill
	}
	led := newBurstLedger(domainpayment.MoneyLine{
		ID: lineID, Channel: domainpayment.ChannelWechat, ExternalID: "BURST-2",
		Amount: decimal.RequireFromString("1280.00"), Status: domainpayment.MoneyOpen, Counterparty: "A客户",
	}, heads, money)
	led.gate = &openGate{want: callers}
	uc := apppayment.NewMoneyUseCase(led, led, apppayment.NewRecordPaymentUseCase(led, led))
	targets := make([]uuid.UUID, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		target := billA
		if i%2 == 1 {
			target = billB
		}
		targets[i] = target
		wg.Add(1)
		go func(i int, target uuid.UUID) {
			defer wg.Done()
			errs[i] = uc.Confirm(context.Background(), testTenantID, testCreatorID, lineID, target)
		}(i, target)
	}
	wg.Wait()
	if led.line.BillID == nil || len(led.pays) != 1 {
		t.Fatalf("payments=%d bill=%v", len(led.pays), led.line.BillID)
	}
	won := *led.line.BillID
	if !led.heads[won].PaidAmount.Equal(decimal.RequireFromString("1280.00")) {
		t.Fatalf("winner paid %s", led.heads[won].PaidAmount)
	}
	other := billA
	if won == billA {
		other = billB
	}
	if !led.heads[other].PaidAmount.IsZero() {
		t.Fatalf("loser paid %s", led.heads[other].PaidAmount)
	}
	settled, taken := 0, 0
	for i, err := range errs {
		if err == nil {
			settled++
			if targets[i] != won {
				t.Errorf("call %d succeeded on the other bill", i)
			}
			continue
		}
		if errors.Is(err, domainpayment.ErrMoneyTaken) {
			taken++
			if targets[i] == won {
				t.Errorf("call %d was refused on the winning bill", i)
			}
			continue
		}
		t.Errorf("call %d: %v", i, err)
	}
	if settled != callers/2 || taken != callers/2 {
		t.Fatalf("settled=%d taken=%d", settled, taken)
	}
}

func bookLine(t *testing.T, channel string, ln domainpayment.ParsedLine, billID uuid.UUID, kind domainbill.BillType, direction string) *domainpayment.Payment {
	t.Helper()
	led := newBurstLedger(domainpayment.MoneyLine{
		ID: uuid.New(), Channel: channel, ExternalID: ln.ExternalID, BookedAt: ln.BookedAt,
		Amount: ln.Amount, Counterparty: ln.Counterparty, Memo: ln.Memo, Status: domainpayment.MoneyOpen,
	}, oneBill(billID, kind, ln.Amount.Abs().String()), oneMoneyBill(billID, "B", ln.Counterparty, direction, ln.Amount.Abs().String()))
	uc := apppayment.NewMoneyUseCase(led, led, apppayment.NewRecordPaymentUseCase(led, led))
	if err := uc.Confirm(context.Background(), testTenantID, testCreatorID, led.line.ID, billID); err != nil {
		t.Fatal(err)
	}
	if len(led.pays) != 1 {
		t.Fatalf("payments = %d", len(led.pays))
	}
	return led.pays[0]
}

func oneBill(id uuid.UUID, kind domainbill.BillType, total string) map[uuid.UUID]*domainbill.BillHead {
	return map[uuid.UUID]*domainbill.BillHead{
		id: {
			ID: id, TenantID: testTenantID, Status: domainbill.StatusApproved,
			BillType: kind, TotalAmount: decimal.RequireFromString(total),
		},
	}
}

func oneMoneyBill(id uuid.UUID, no, partner, direction, outstanding string) map[uuid.UUID]domainpayment.MoneyBill {
	return map[uuid.UUID]domainpayment.MoneyBill{
		id: {
			ID: id, BillNo: no, PartnerName: partner, Direction: direction,
			Outstanding: decimal.RequireFromString(outstanding),
		},
	}
}

type openGate struct {
	mu   sync.Mutex
	n    int
	want int
	ch   chan struct{}
}

func (g *openGate) wait() {
	g.mu.Lock()
	if g.ch == nil {
		g.ch = make(chan struct{})
	}
	g.n++
	ch := g.ch
	release := g.n >= g.want
	g.mu.Unlock()
	if release {
		close(ch)
		return
	}
	<-ch
}

type burstLedger struct {
	mu    sync.Mutex
	gate  *openGate
	line  domainpayment.MoneyLine
	heads map[uuid.UUID]*domainbill.BillHead
	pays  []*domainpayment.Payment
	bills map[uuid.UUID]domainpayment.MoneyBill
}

func newBurstLedger(line domainpayment.MoneyLine, heads map[uuid.UUID]*domainbill.BillHead, bills map[uuid.UUID]domainpayment.MoneyBill) *burstLedger {
	return &burstLedger{line: line, heads: heads, pays: nil, bills: bills}
}

func (b *burstLedger) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	paid := make(map[uuid.UUID]decimal.Decimal, len(b.heads))
	for id, head := range b.heads {
		paid[id] = head.PaidAmount
	}
	kept := len(b.pays)
	status, billID, payID := b.line.Status, b.line.BillID, b.line.PaymentID
	err := fn(nil)
	if err != nil {
		for id, amount := range paid {
			b.heads[id].PaidAmount = amount
		}
		b.pays = b.pays[:kept]
		b.line.Status = status
		b.line.BillID = billID
		b.line.PaymentID = payID
	}
	return err
}

func (b *burstLedger) GetBillForUpdate(_ context.Context, _ *sql.Tx, _, billID uuid.UUID) (*domainbill.BillHead, error) {
	head, ok := b.heads[billID]
	if !ok {
		return nil, domainpayment.ErrMoneyBill
	}
	return head, nil
}

func (b *burstLedger) UpdatePaidAmount(_ context.Context, _ *sql.Tx, _, billID uuid.UUID, paid decimal.Decimal) error {
	b.heads[billID].PaidAmount = paid
	return nil
}

func (b *burstLedger) Record(_ context.Context, _ *sql.Tx, p *domainpayment.Payment) error {
	b.pays = append(b.pays, p)
	return nil
}

func (b *burstLedger) ListByBill(_ context.Context, _, billID uuid.UUID) ([]*domainpayment.Payment, error) {
	var out []*domainpayment.Payment
	for _, p := range b.pays {
		if p.BillID == billID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (b *burstLedger) SumByBill(_ context.Context, _ *sql.Tx, _, billID uuid.UUID) (decimal.Decimal, error) {
	sum := decimal.Zero
	for _, p := range b.pays {
		if p.BillID == billID {
			sum = sum.Add(p.Amount)
		}
	}
	return sum, nil
}

func (b *burstLedger) InsertNew(context.Context, uuid.UUID, string, string, []domainpayment.ParsedLine) (int, int, error) {
	return 0, 0, nil
}

func (b *burstLedger) ListOpen(context.Context, uuid.UUID) ([]domainpayment.MoneyLine, error) {
	return nil, nil
}

func (b *burstLedger) Get(_ context.Context, _, id uuid.UUID) (domainpayment.MoneyLine, error) {
	b.mu.Lock()
	if b.line.ID != id {
		b.mu.Unlock()
		return domainpayment.MoneyLine{}, domainpayment.ErrMoneyLine
	}
	line := b.line
	b.mu.Unlock()
	if b.gate != nil && line.Status == domainpayment.MoneyOpen {
		b.gate.wait()
	}
	return line, nil
}

func (b *burstLedger) MarkMatched(_ context.Context, _ *sql.Tx, _, _, bill, payment uuid.UUID) error {
	if b.line.Status != domainpayment.MoneyOpen {
		return domainpayment.ErrMoneyTaken
	}
	b.line.Status = domainpayment.MoneyMatched
	b.line.BillID = &bill
	b.line.PaymentID = &payment
	return nil
}

func (b *burstLedger) Ignore(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (b *burstLedger) ListOpenMoneyBills(context.Context, uuid.UUID) ([]domainpayment.MoneyBill, error) {
	return nil, nil
}

func (b *burstLedger) OpenMoneyBill(_ context.Context, _, id uuid.UUID) (domainpayment.MoneyBill, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	bill, ok := b.bills[id]
	if !ok {
		return domainpayment.MoneyBill{}, domainpayment.ErrMoneyBill
	}
	return bill, nil
}

var (
	_ apppayment.MoneyStore      = (*burstLedger)(nil)
	_ apppayment.MoneyBillReader = (*burstLedger)(nil)
	_ apppayment.BillReader      = (*burstLedger)(nil)
	_ apppayment.PaymentRepo     = (*burstLedger)(nil)
)
