package payment_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	handlerpayment "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/payment"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

func init() {
	gin.SetMode(gin.TestMode)
}

var (
	phTenantID  = uuid.New()
	phBillID    = uuid.New()
	phCreatorID = uuid.New()
)

// ----- mock BillReader -----

type phMockBillReader struct {
	bills map[uuid.UUID]*domain.BillHead
}

func newPHMockBillReader() *phMockBillReader {
	return &phMockBillReader{bills: make(map[uuid.UUID]*domain.BillHead)}
}

func (m *phMockBillReader) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil) //nolint:staticcheck
}

func (m *phMockBillReader) GetBillForUpdate(_ context.Context, _ *sql.Tx, _, billID uuid.UUID) (*domain.BillHead, error) {
	h, ok := m.bills[billID]
	if !ok {
		return nil, appbill.ErrBillNotFound
	}
	return h, nil
}

func (m *phMockBillReader) UpdatePaidAmount(_ context.Context, _ *sql.Tx, _, billID uuid.UUID, paidAmount decimal.Decimal) error {
	if h, ok := m.bills[billID]; ok {
		h.PaidAmount = paidAmount
	}
	return nil
}

var _ apppayment.BillReader = (*phMockBillReader)(nil)

// ----- mock PaymentRepo -----

type phMockPaymentRepo struct {
	recorded []*domainpayment.Payment
}

func (m *phMockPaymentRepo) Record(_ context.Context, _ *sql.Tx, p *domainpayment.Payment) error {
	m.recorded = append(m.recorded, p)
	return nil
}

func (m *phMockPaymentRepo) ListByBill(_ context.Context, _, _ uuid.UUID) ([]*domainpayment.Payment, error) {
	if m.recorded == nil {
		return []*domainpayment.Payment{}, nil
	}
	return m.recorded, nil
}

func (m *phMockPaymentRepo) SumByBill(_ context.Context, _ *sql.Tx, _, _ uuid.UUID) (decimal.Decimal, error) {
	return decimal.Zero, nil
}

func (m *phMockPaymentRepo) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil) //nolint:staticcheck
}

var _ apppayment.PaymentRepo = (*phMockPaymentRepo)(nil)

// ----- builder -----

func buildPaymentHandler(billReader *phMockBillReader, payRepo *phMockPaymentRepo) *handlerpayment.Handler {
	recordUC := apppayment.NewRecordPaymentUseCase(billReader, payRepo)
	listUC := apppayment.NewListPaymentsUseCase(payRepo)
	return handlerpayment.New(recordUC, listUC)
}

func newPaymentRouter(h *handlerpayment.Handler) *gin.Engine {
	r := gin.New()
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)
	return r
}

// ----- tests -----

// TestPaymentHandler_Record_Returns201 verifies a valid payment record returns 201.
func TestPaymentHandler_Record_Returns201(t *testing.T) {
	billReader := newPHMockBillReader()
	payRepo := &phMockPaymentRepo{}
	billReader.bills[phBillID] = &domain.BillHead{
		ID:          phBillID,
		TenantID:    phTenantID,
		Status:      domain.StatusApproved,
		TotalAmount: decimal.NewFromFloat(500),
		CreatedAt:   time.Now(),
	}

	h := buildPaymentHandler(billReader, payRepo)
	r := newPaymentRouter(h)

	body := map[string]any{
		"bill_id":        phBillID.String(),
		"amount":         "100",
		"payment_method": "cash",
	}
	b, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/payments", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", phTenantID.String())
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	if len(payRepo.recorded) != 1 {
		t.Errorf("payment records = %d, want 1", len(payRepo.recorded))
	}
}

// TestPaymentHandler_List_Returns200 verifies the list endpoint returns 200.
func TestPaymentHandler_List_Returns200(t *testing.T) {
	billReader := newPHMockBillReader()
	payRepo := &phMockPaymentRepo{
		recorded: []*domainpayment.Payment{
			{ID: uuid.New(), BillID: phBillID, Amount: decimal.NewFromFloat(100), PayType: domainpayment.PayTypeCash, PayDate: time.Now()},
		},
	}

	h := buildPaymentHandler(billReader, payRepo)
	r := newPaymentRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/payments?bill_id=%s", phBillID), nil)
	req.Header.Set("X-Tenant-ID", phTenantID.String())
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}
