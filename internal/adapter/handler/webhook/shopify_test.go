package webhook_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/webhook"
	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
)

// ----- mocks ----------------------------------------------------------------

type mockResolver struct {
	m   *webhook.ShopMapping
	err error
}

func (r *mockResolver) GetByDomain(_ context.Context, _ string) (*webhook.ShopMapping, error) {
	return r.m, r.err
}

type mockIngest struct {
	// orders/create
	imported appimporting.ImportedOrder
	skipped  *appimporting.SkippedOrder
	orderErr error
	called   []appimporting.SingleOrderRequest

	// orders/cancelled
	cancelResult *appimporting.CancelResult
	cancelErr    error

	// refunds/create
	refundResult *appimporting.RefundResult
	refundErr    error
}

func (m *mockIngest) IngestSingleOrder(_ context.Context, req appimporting.SingleOrderRequest) (appimporting.ImportedOrder, *appimporting.SkippedOrder, error) {
	m.called = append(m.called, req)
	return m.imported, m.skipped, m.orderErr
}

func (m *mockIngest) IngestCancelOrder(_ context.Context, _ appimporting.CancelRequest) (*appimporting.CancelResult, error) {
	return m.cancelResult, m.cancelErr
}

func (m *mockIngest) IngestRefund(_ context.Context, _ appimporting.RefundRequest) (*appimporting.RefundResult, error) {
	return m.refundResult, m.refundErr
}

// ----- helpers --------------------------------------------------------------

const testSecret = "whsec_test_secret"

func sign(body []byte) string {
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func defaultMapping() *webhook.ShopMapping {
	return &webhook.ShopMapping{
		ShopDomain:  "test.myshopify.com",
		TenantID:    uuid.New(),
		WarehouseID: uuid.New(),
		CreatorID:   uuid.New(),
	}
}

func buildEngine(secret string, resolver webhook.ShopResolver, uc webhook.IngestUseCase) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := webhook.New(secret, resolver, uc, slog.Default())
	h.RegisterRoutes(r)
	return r
}

func orderBody(orderName, sku string, qty int64, price string) []byte {
	type line struct {
		SKU      string `json:"sku"`
		Quantity int64  `json:"quantity"`
		Price    string `json:"price"`
	}
	type order struct {
		ID        int64     `json:"id"`
		Name      string    `json:"name"`
		Currency  string    `json:"currency"`
		CreatedAt time.Time `json:"created_at"`
		LineItems []line    `json:"line_items"`
	}
	o := order{
		ID:        1001,
		Name:      orderName,
		Currency:  "USD",
		CreatedAt: time.Now().UTC(),
		LineItems: []line{{SKU: sku, Quantity: qty, Price: price}},
	}
	b, _ := json.Marshal(o)
	return b
}

// refundBody builds a minimal refunds/create JSON payload.
func refundBody(refundID int64, orderID int64, sku string, qty int64, price string) []byte {
	type lineItem struct {
		SKU      string `json:"sku"`
		Quantity int64  `json:"quantity"`
		Price    string `json:"price"`
	}
	type refundLine struct {
		Quantity int64    `json:"quantity"`
		LineItem lineItem `json:"line_item"`
	}
	type refund struct {
		ID              int64        `json:"id"`
		OrderID         int64        `json:"order_id"`
		Currency        string       `json:"currency"`
		CreatedAt       time.Time    `json:"created_at"`
		RefundLineItems []refundLine `json:"refund_line_items"`
	}
	r := refund{
		ID:        refundID,
		OrderID:   orderID,
		Currency:  "USD",
		CreatedAt: time.Now().UTC(),
		RefundLineItems: []refundLine{
			{Quantity: qty, LineItem: lineItem{SKU: sku, Quantity: qty, Price: price}},
		},
	}
	b, _ := json.Marshal(r)
	return b
}

func doOrderRequest(r *gin.Engine, body []byte, sig, shop, topic string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/shopify/orders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shopify-Hmac-Sha256", sig)
	req.Header.Set("X-Shopify-Shop-Domain", shop)
	req.Header.Set("X-Shopify-Topic", topic)
	r.ServeHTTP(w, req)
	return w
}

func doRefundRequest(r *gin.Engine, body []byte, sig, shop, topic string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/shopify/refunds", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shopify-Hmac-Sha256", sig)
	req.Header.Set("X-Shopify-Shop-Domain", shop)
	req.Header.Set("X-Shopify-Topic", topic)
	r.ServeHTTP(w, req)
	return w
}

// ----- orders/create tests --------------------------------------------------

func TestShopifyWebhook_HappyPath(t *testing.T) {
	mapping := defaultMapping()
	billID := uuid.New()
	uc := &mockIngest{
		imported: appimporting.ImportedOrder{
			PlatformOrderNo: "#1001",
			BillID:          billID,
			BillNo:          "SL-20260528-0001",
		},
	}
	r := buildEngine(testSecret, &mockResolver{m: mapping}, uc)

	body := orderBody("#1001", "SKU-A", 2, "49.99")
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "orders/create")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "imported" {
		t.Errorf("status: got %v, want imported", resp["status"])
	}
	if len(uc.called) != 1 {
		t.Fatalf("expected 1 IngestSingleOrder call, got %d", len(uc.called))
	}
	req := uc.called[0]
	if req.PlatformOrderNo != "#1001" {
		t.Errorf("platform_order_no: got %q", req.PlatformOrderNo)
	}
	if req.TenantID != mapping.TenantID {
		t.Errorf("tenant_id mismatch")
	}
	if len(req.Lines) != 1 || req.Lines[0].PlatformSKU != "SKU-A" {
		t.Errorf("lines: %+v", req.Lines)
	}
}

func TestShopifyWebhook_InvalidSignature(t *testing.T) {
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, &mockIngest{})
	body := orderBody("#1002", "SKU-B", 1, "10.00")
	w := doOrderRequest(r, body, "badsig==", "test.myshopify.com", "orders/create")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestShopifyWebhook_UnsupportedTopicOnOrdersEndpoint(t *testing.T) {
	body := orderBody("#1003", "SKU-C", 1, "5.00")
	sig := sign(body)
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, &mockIngest{})
	// "refunds/create" is not a valid topic for /webhooks/shopify/orders
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "refunds/create")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestShopifyWebhook_UnknownShop_Returns404(t *testing.T) {
	r := buildEngine(testSecret, &mockResolver{m: nil}, &mockIngest{})
	body := orderBody("#1004", "SKU-D", 1, "20.00")
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "unknown.myshopify.com", "orders/create")
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestShopifyWebhook_DuplicateOrder_Returns200Skipped(t *testing.T) {
	mapping := defaultMapping()
	uc := &mockIngest{
		skipped: &appimporting.SkippedOrder{
			PlatformOrderNo: "#1005",
			Reason:          fmt.Sprintf("duplicate:bill_id=%s", uuid.New()),
		},
	}
	r := buildEngine(testSecret, &mockResolver{m: mapping}, uc)
	body := orderBody("#1005", "SKU-E", 3, "15.00")
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "orders/create")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "skipped" {
		t.Errorf("status: got %v, want skipped", resp["status"])
	}
}

func TestVerifySignature_EmptySecret(t *testing.T) {
	// Empty secret must reject everything regardless of header content.
	r := buildEngine("", &mockResolver{m: defaultMapping()}, &mockIngest{})
	body := orderBody("#1006", "SKU-F", 1, "1.00")
	sig := sign(body) // signed with testSecret, but engine uses ""
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "orders/create")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 with empty secret, got %d", w.Code)
	}
}

// ----- orders/cancelled tests -----------------------------------------------

func TestShopifyWebhook_OrderCancelled_HappyPath(t *testing.T) {
	mapping := defaultMapping()
	origBillID := uuid.New()
	revBillID := uuid.New()
	uc := &mockIngest{
		cancelResult: &appimporting.CancelResult{
			PlatformOrderNo: "#2001",
			OriginalBillID:  origBillID,
			ReversalBillID:  revBillID,
			ReversalBillNo:  "RT-20260528-0001",
		},
	}
	r := buildEngine(testSecret, &mockResolver{m: mapping}, uc)

	body := orderBody("#2001", "SKU-G", 1, "99.00")
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "orders/cancelled")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "cancelled" {
		t.Errorf("status: got %v, want cancelled", resp["status"])
	}
	if resp["reversal_bill_id"] != revBillID.String() {
		t.Errorf("reversal_bill_id: got %v", resp["reversal_bill_id"])
	}
}

func TestShopifyWebhook_OrderCancelled_IngestError_Returns500(t *testing.T) {
	uc := &mockIngest{
		cancelErr: fmt.Errorf("db unavailable"),
	}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)
	body := orderBody("#2002", "SKU-H", 1, "10.00")
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "orders/cancelled")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

// ----- refunds/create tests -------------------------------------------------

func TestShopifyWebhook_RefundCreate_HappyPath(t *testing.T) {
	mapping := defaultMapping()
	billID := uuid.New()
	uc := &mockIngest{
		refundResult: &appimporting.RefundResult{
			PlatformOrderNo:  "3001",
			PlatformRefundID: "9001",
			BillID:           billID,
			BillNo:           "RT-20260528-0002",
		},
	}
	r := buildEngine(testSecret, &mockResolver{m: mapping}, uc)

	body := refundBody(9001, 3001, "SKU-I", 1, "49.99")
	sig := sign(body)
	w := doRefundRequest(r, body, sig, "test.myshopify.com", "refunds/create")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "refunded" {
		t.Errorf("status: got %v, want refunded", resp["status"])
	}
	if resp["bill_id"] != billID.String() {
		t.Errorf("bill_id: got %v", resp["bill_id"])
	}
}

func TestShopifyWebhook_RefundCreate_UnsupportedTopicOnRefundsEndpoint(t *testing.T) {
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, &mockIngest{})
	body := refundBody(9002, 3002, "SKU-J", 1, "10.00")
	sig := sign(body)
	// "orders/create" is not valid on /webhooks/shopify/refunds
	w := doRefundRequest(r, body, sig, "test.myshopify.com", "orders/create")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestShopifyWebhook_RefundCreate_InvalidSignature(t *testing.T) {
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, &mockIngest{})
	body := refundBody(9003, 3003, "SKU-K", 1, "20.00")
	w := doRefundRequest(r, body, "badsig==", "test.myshopify.com", "refunds/create")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}
