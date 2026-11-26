package payment_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

type memLines struct {
	line    domainpayment.MoneyLine
	marked  uuid.UUID
	bill    uuid.UUID
	markErr error
}

func (m *memLines) InsertNew(context.Context, uuid.UUID, string, string, []domainpayment.ParsedLine) (int, int, error) {
	return 0, 0, nil
}
func (m *memLines) ListOpen(context.Context, uuid.UUID) ([]domainpayment.MoneyLine, error) {
	return []domainpayment.MoneyLine{m.line}, nil
}
func (m *memLines) Get(context.Context, uuid.UUID, uuid.UUID) (domainpayment.MoneyLine, error) {
	return m.line, nil
}
func (m *memLines) MarkMatched(_ context.Context, _ *sql.Tx, _, _, bill, payment uuid.UUID) error {
	if m.markErr != nil {
		return m.markErr
	}
	m.marked = payment
	m.bill = bill
	m.line.Status = domainpayment.MoneyMatched
	m.line.BillID = &bill
	m.line.PaymentID = &payment
	return nil
}
func (m *memLines) Ignore(context.Context, uuid.UUID, uuid.UUID) error { return nil }

type memBills struct {
	bills []domainpayment.MoneyBill
}

func (m *memBills) ListOpenMoneyBills(context.Context, uuid.UUID) ([]domainpayment.MoneyBill, error) {
	return m.bills, nil
}
func (m *memBills) OpenMoneyBill(_ context.Context, _, id uuid.UUID) (domainpayment.MoneyBill, error) {
	for _, b := range m.bills {
		if b.ID == id {
			return b, nil
		}
	}
	return domainpayment.MoneyBill{}, domainpayment.ErrMoneyBill
}

func TestMoneyConfirm_BooksPaymentAndMarksLine(t *testing.T) {
	billRepo := newMockBillReader()
	payRepo := newMockPaymentRepo()
	billRepo.seedApprovedBill(testBillID, decimal.NewFromInt(1280), decimal.Zero)
	lines := &memLines{line: domainpayment.MoneyLine{
		ID: uuid.New(), Channel: domainpayment.ChannelAlipay, ExternalID: "A1",
		Amount: decimal.NewFromInt(1280), Status: domainpayment.MoneyOpen, Counterparty: "龙口老李",
	}}
	bills := &memBills{bills: []domainpayment.MoneyBill{{
		ID: testBillID, BillNo: "SL-1", PartnerName: "龙口老李", Direction: domainpayment.MoneyAR,
		Outstanding: decimal.NewFromInt(1280),
	}}}
	uc := apppayment.NewMoneyUseCase(lines, bills, apppayment.NewRecordPaymentUseCase(billRepo, payRepo))
	if err := uc.Confirm(context.Background(), testTenantID, testCreatorID, lines.line.ID, testBillID); err != nil {
		t.Fatal(err)
	}
	if len(payRepo.recorded) != 1 || payRepo.recorded[0].PayType != domainpayment.PayTypeAlipay {
		t.Fatalf("payments = %+v", payRepo.recorded)
	}
	if lines.marked != payRepo.recorded[0].ID || lines.bill != testBillID {
		t.Fatalf("marked payment %s bill %s", lines.marked, lines.bill)
	}
	if payRepo.recorded[0].Remark != "流水 A1" {
		t.Fatalf("remark %q", payRepo.recorded[0].Remark)
	}
}

func TestMoneyConfirm_RefusesTheWrongDirection(t *testing.T) {
	billRepo := newMockBillReader()
	payRepo := newMockPaymentRepo()
	billRepo.seedApprovedBill(testBillID, decimal.NewFromInt(1280), decimal.Zero)
	lines := &memLines{line: domainpayment.MoneyLine{
		ID: uuid.New(), Channel: domainpayment.ChannelAlipay, Amount: decimal.NewFromInt(1280), Status: domainpayment.MoneyOpen,
	}}
	bills := &memBills{bills: []domainpayment.MoneyBill{{ID: testBillID, Direction: domainpayment.MoneyAP, Outstanding: decimal.NewFromInt(1280)}}}
	uc := apppayment.NewMoneyUseCase(lines, bills, apppayment.NewRecordPaymentUseCase(billRepo, payRepo))
	err := uc.Confirm(context.Background(), testTenantID, testCreatorID, lines.line.ID, testBillID)
	if err != domainpayment.ErrMoneyDirection {
		t.Fatalf("err = %v", err)
	}
	if len(payRepo.recorded) != 0 {
		t.Fatalf("recorded %d", len(payRepo.recorded))
	}
}

func TestMoneyConfirm_MatchedLineIsIdempotent(t *testing.T) {
	billID := testBillID
	lines := &memLines{line: domainpayment.MoneyLine{
		ID: uuid.New(), Status: domainpayment.MoneyMatched, BillID: &billID, Amount: decimal.NewFromInt(10),
	}}
	payRepo := newMockPaymentRepo()
	uc := apppayment.NewMoneyUseCase(lines, &memBills{}, apppayment.NewRecordPaymentUseCase(newMockBillReader(), payRepo))
	if err := uc.Confirm(context.Background(), testTenantID, testCreatorID, lines.line.ID, billID); err != nil {
		t.Fatal(err)
	}
	if len(payRepo.recorded) != 0 {
		t.Fatalf("recorded %d", len(payRepo.recorded))
	}
}

func TestMoneyList_SuggestsTheNamedBill(t *testing.T) {
	sale := domainpayment.MoneyBill{ID: uuid.New(), BillNo: "SL-1", PartnerName: "龙口老李", Direction: domainpayment.MoneyAR, Outstanding: decimal.NewFromInt(1280)}
	other := domainpayment.MoneyBill{ID: uuid.New(), BillNo: "SL-2", PartnerName: "别人", Direction: domainpayment.MoneyAR, Outstanding: decimal.NewFromInt(1280)}
	lines := &memLines{line: domainpayment.MoneyLine{
		ID: uuid.New(), Channel: domainpayment.ChannelWechat, Amount: decimal.NewFromInt(1280), Counterparty: "龙口老李", Status: domainpayment.MoneyOpen,
	}}
	uc := apppayment.NewMoneyUseCase(lines, &memBills{bills: []domainpayment.MoneyBill{other, sale}}, nil)
	views, err := uc.List(context.Background(), testTenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || !views[0].Suggested || len(views[0].Candidates) != 1 || views[0].Candidates[0].BillID != sale.ID {
		t.Fatalf("%+v", views)
	}
}
