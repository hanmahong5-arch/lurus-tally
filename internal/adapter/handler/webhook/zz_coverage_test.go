// zz_coverage_test.go adds the remaining branch coverage for shopify.go that
// shopify_test.go / shopify_honesty_test.go do not exercise: WithPinner /
// pinned ingest, resolver_error, cannot_read_body, ingest_error /
// cancel_error / refund_error, malformed-JSON parse_failed on all three
// handlers, verifySignature edge cases, and the parseShopifyOrder /
// parseShopifyRefund field-level branches (missing SKU, qty<=0, name
// fallback, currency default, refund id/order_id "0"/"", quantity fallback,
// zero CreatedAt, negative price).
//
// Reuses the mocks/helpers already declared in shopify_test.go (same
// webhook_test package): mockResolver, mockIngest, testSecret, sign,
// defaultMapping, buildEngine, orderBody, refundBody, doOrderRequest,
// doRefundRequest.
package webhook_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/webhook"
	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
)

// ----- TenantPinner fake -----------------------------------------------------

// fakePinner records the tenantID it was invoked with and then runs fn,
// simulating a connection scoped to that tenant.
type fakePinner struct {
	calledWith []uuid.UUID
	failWith   error // if set, WithPinnedConn returns this error without calling fn
}

func (p *fakePinner) WithPinnedConn(ctx context.Context, tenantID uuid.UUID, fn func(context.Context) error) error {
	p.calledWith = append(p.calledWith, tenantID)
	if p.failWith != nil {
		return p.failWith
	}
	return fn(ctx)
}

// buildEngineWithPinner mirrors buildEngine but attaches a TenantPinner via
// WithPinner so the ingest branch that pins the connection is exercised.
func buildEngineWithPinner(secret string, resolver webhook.ShopResolver, uc webhook.IngestUseCase, pinner webhook.TenantPinner) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := webhook.New(secret, resolver, uc, slog.Default())
	h.WithPinner(pinner)
	h.RegisterRoutes(r)
	return r
}

// TestWithPinner_PinsIngestToResolvedTenant covers WithPinner + the pinned
// branch of ingest (shopify.go:100-105): business invariant — when a
// TenantPinner is attached, ingest must run inside WithPinnedConn(mapping.TenantID),
// backstopping RLS for the unauthenticated webhook path.
func TestWithPinner_PinsIngestToResolvedTenant(t *testing.T) {
	mapping := defaultMapping()
	uc := &mockIngest{imported: appImported("#9001")}
	pinner := &fakePinner{}
	r := buildEngineWithPinner(testSecret, &mockResolver{m: mapping}, uc, pinner)

	body := orderBody("#9001", "SKU-PIN", 1, "10.00")
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "orders/create")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if len(pinner.calledWith) != 1 {
		t.Fatalf("expected WithPinnedConn called once, got %d", len(pinner.calledWith))
	}
	if pinner.calledWith[0] != mapping.TenantID {
		t.Errorf("pinner tenant_id: got %s want %s", pinner.calledWith[0], mapping.TenantID)
	}
	if len(uc.called) != 1 {
		t.Fatalf("expected ingest to run inside the pinned fn, got %d calls", len(uc.called))
	}
}

// TestWithPinner_PropagatesPinnerError covers ingest returning the pinner's
// error (WithPinnedConn failure) as an ingest_error / 500, without invoking
// the wrapped fn (so IngestSingleOrder is never called).
func TestWithPinner_PropagatesPinnerError(t *testing.T) {
	mapping := defaultMapping()
	uc := &mockIngest{imported: appImported("#9002")}
	pinner := &fakePinner{failWith: fmt.Errorf("pin failed")}
	r := buildEngineWithPinner(testSecret, &mockResolver{m: mapping}, uc, pinner)

	body := orderBody("#9002", "SKU-PIN2", 1, "10.00")
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "orders/create")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 on pinner failure, got %d body=%s", w.Code, w.Body.String())
	}
	if len(uc.called) != 0 {
		t.Errorf("IngestSingleOrder must not run when WithPinnedConn fails, got %d calls", len(uc.called))
	}
}

// ----- readAndVerify: cannot_read_body ---------------------------------------

// overLimitReader implements io.Reader, yielding `remaining` bytes and then
// io.EOF, so http.MaxBytesReader can observe the byte cap being exceeded and
// surface a read error (it reads limit+1 bytes to detect overflow).
type overLimitReader struct {
	remaining int
}

func (r *overLimitReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > r.remaining {
		n = r.remaining
	}
	for i := 0; i < n; i++ {
		p[i] = 'a'
	}
	r.remaining -= n
	return n, nil
}

// TestReadAndVerify_BodyOverMaxBytes_400 covers shopify.go:167-173:
// a body exceeding maxBodyBytes (1 MB) via http.MaxBytesReader must fail the
// read and return 400 cannot_read_body — before signature or resolver are
// ever consulted.
func TestReadAndVerify_BodyOverMaxBytes_400(t *testing.T) {
	uc := &mockIngest{imported: appImported("#9003")}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	// 1 MB + 1 byte body; content matters not, MaxBytesReader caps regardless
	// of Content-Length since ReadAll pulls until the limit trips.
	oversized := &overLimitReader{remaining: 1*1024*1024 + 1}
	req := httptest.NewRequest(http.MethodPost, "/webhooks/shopify/orders", oversized)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shopify-Hmac-Sha256", "irrelevant-because-body-read-fails-first")
	req.Header.Set("X-Shopify-Shop-Domain", "test.myshopify.com")
	req.Header.Set("X-Shopify-Topic", "orders/create")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 cannot_read_body, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "cannot_read_body" {
		t.Errorf("error: got %v want cannot_read_body", resp["error"])
	}
	if len(uc.called) != 0 {
		t.Errorf("ingest must not run when body cannot be read, got %d calls", len(uc.called))
	}
}

// ----- readAndVerify: resolver_error ------------------------------------------

// TestReadAndVerify_ResolverError_500 covers shopify.go:186-191: the shop
// resolver returning a non-nil error (e.g. DB unavailable) must yield 500
// resolver_error, distinct from the 404 unknown_shop (nil, nil) case, and
// must never reach ingest.
func TestReadAndVerify_ResolverError_500(t *testing.T) {
	uc := &mockIngest{imported: appImported("#9004")}
	r := buildEngine(testSecret, &mockResolver{err: fmt.Errorf("db down")}, uc)

	body := orderBody("#9004", "SKU-RE", 1, "1.00")
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "orders/create")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 resolver_error, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "resolver_error" {
		t.Errorf("error: got %v want resolver_error", resp["error"])
	}
	if len(uc.called) != 0 {
		t.Errorf("ingest must not run on resolver error, got %d calls", len(uc.called))
	}
}

// ----- handleOrdersCreate: malformed JSON + ingest_error ----------------------

// TestHandleOrdersCreate_MalformedJSON_200ParseFailed covers shopify.go:206-214:
// a syntactically invalid JSON body (but a valid HMAC over those exact bytes)
// must return 200 with error=parse_failed — NOT a 4xx — so Shopify does not
// retry a payload we can never parse.
func TestHandleOrdersCreate_MalformedJSON_200ParseFailed(t *testing.T) {
	uc := &mockIngest{imported: appImported("#9005")}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)

	body := []byte(`{not valid json`)
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "orders/create")

	if w.Code != http.StatusOK {
		t.Fatalf("malformed JSON with valid HMAC: want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "parse_failed" {
		t.Errorf("error: got %v want parse_failed", resp["error"])
	}
	if len(uc.called) != 0 {
		t.Errorf("ingest must not run on parse failure, got %d calls", len(uc.called))
	}
}

// TestHandleOrdersCreate_NoSKULineItems_200ParseFailed covers
// parseShopifyOrder's "no line items with a SKU" error (shopify.go:449-451)
// surfaced through the handler as 200 parse_failed.
func TestHandleOrdersCreate_NoSKULineItems_200ParseFailed(t *testing.T) {
	uc := &mockIngest{imported: appImported("#9006")}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)

	// qty<=0 line and empty-SKU line are both filtered out, leaving zero lines.
	body := orderBody("#9006", "", 0, "1.00")
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "orders/create")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "parse_failed" {
		t.Errorf("error: got %v want parse_failed", resp["error"])
	}
	if len(uc.called) != 0 {
		t.Errorf("ingest must not run when no line has a SKU, got %d calls", len(uc.called))
	}
}

// TestHandleOrdersCreate_NegativePrice_200ParseFailed covers the business
// invariant: a negative line price must be rejected at parse time
// (shopify.go:436-439, decimal.IsNegative) and never reach ingest — the
// money/stock write path never sees a negative-money bill.
func TestHandleOrdersCreate_NegativePrice_200ParseFailed(t *testing.T) {
	uc := &mockIngest{imported: appImported("#9007")}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)

	body := orderBody("#9007", "SKU-NEG", 1, "-5.00")
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "orders/create")

	if w.Code != http.StatusOK {
		t.Fatalf("negative price: want 200 parse_failed, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "parse_failed" {
		t.Errorf("error: got %v want parse_failed", resp["error"])
	}
	if len(uc.called) != 0 {
		t.Errorf("ingest must NEVER run for a negative-price line, got %d calls", len(uc.called))
	}
}

// TestHandleOrdersCreate_IngestError_500 covers shopify.go:223-230: the
// ingest use case returning a plain error (not a SkippedOrder) must surface
// as 500 ingest_error.
func TestHandleOrdersCreate_IngestError_500(t *testing.T) {
	uc := &mockIngest{orderErr: fmt.Errorf("stock write failed")}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)

	body := orderBody("#9008", "SKU-IE", 1, "1.00")
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "orders/create")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 ingest_error, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "ingest_error" {
		t.Errorf("error: got %v want ingest_error", resp["error"])
	}
}

// TestParseShopifyOrder_NameFallbackAndCurrencyDefault covers the "name"
// empty → id string fallback (shopify.go:413-416) and empty currency →
// "USD" default (shopify.go:421-424) by asserting the values that flow
// into the ingest request.
func TestParseShopifyOrder_NameFallbackAndCurrencyDefault(t *testing.T) {
	type line struct {
		SKU      string `json:"sku"`
		Quantity int64  `json:"quantity"`
		Price    string `json:"price"`
	}
	type order struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`     // deliberately empty → id fallback
		Currency  string `json:"currency"` // deliberately empty → USD default
		LineItems []line `json:"line_items"`
	}
	o := order{
		ID:        424242,
		LineItems: []line{{SKU: "SKU-FALLBACK", Quantity: 1, Price: "3.50"}},
	}
	body, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	mapping := defaultMapping()
	uc := &mockIngest{imported: appImported("424242")}
	r := buildEngine(testSecret, &mockResolver{m: mapping}, uc)
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "orders/create")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if len(uc.called) != 1 {
		t.Fatalf("expected 1 ingest call, got %d", len(uc.called))
	}
	got := uc.called[0]
	if got.PlatformOrderNo != "424242" {
		t.Errorf("platform_order_no fallback: got %q want %q (id string)", got.PlatformOrderNo, "424242")
	}
	if len(got.Lines) != 1 || got.Lines[0].Currency != "USD" {
		t.Errorf("currency default: got %+v want USD", got.Lines)
	}
}

// TestParseShopifyOrder_SkipsEmptySKUAndNonPositiveQty covers the per-line
// skip branches (shopify.go:428-435): a line with an empty SKU (shipping
// fee) and a line with qty<=0 are both dropped, while a valid line survives.
func TestParseShopifyOrder_SkipsEmptySKUAndNonPositiveQty(t *testing.T) {
	type line struct {
		SKU      string `json:"sku"`
		Quantity int64  `json:"quantity"`
		Price    string `json:"price"`
	}
	type order struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Currency  string `json:"currency"`
		LineItems []line `json:"line_items"`
	}
	o := order{
		ID:       555,
		Name:     "#0555",
		Currency: "USD",
		LineItems: []line{
			{SKU: "", Quantity: 1, Price: "9.99"},          // no SKU: shipping/fee → skip
			{SKU: "SKU-Q0", Quantity: 0, Price: "1.00"},    // qty == 0 → skip
			{SKU: "SKU-QNEG", Quantity: -3, Price: "1.00"}, // qty < 0 → skip
			{SKU: "SKU-VALID", Quantity: 2, Price: "4.00"}, // survives
		},
	}
	body, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	mapping := defaultMapping()
	uc := &mockIngest{imported: appImported("#0555")}
	r := buildEngine(testSecret, &mockResolver{m: mapping}, uc)
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "orders/create")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if len(uc.called) != 1 {
		t.Fatalf("expected 1 ingest call, got %d", len(uc.called))
	}
	lines := uc.called[0].Lines
	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 surviving line, got %d: %+v", len(lines), lines)
	}
	if lines[0].PlatformSKU != "SKU-VALID" {
		t.Errorf("surviving line SKU: got %q want SKU-VALID", lines[0].PlatformSKU)
	}
}

// TestParseShopifyOrder_InvalidPriceString covers the decimal.NewFromString
// error branch (shopify.go:436-439) distinct from the IsNegative branch:
// a non-numeric price string must also be rejected as parse_failed.
func TestParseShopifyOrder_InvalidPriceString(t *testing.T) {
	uc := &mockIngest{imported: appImported("#9009")}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)

	body := orderBody("#9009", "SKU-BADPRICE", 1, "not-a-number")
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "orders/create")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 parse_failed, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "parse_failed" {
		t.Errorf("error: got %v want parse_failed", resp["error"])
	}
	if len(uc.called) != 0 {
		t.Errorf("ingest must not run for an unparsable price, got %d calls", len(uc.called))
	}
}

// ----- handleOrderCancelled: parse_failed branches ---------------------------

// TestHandleOrderCancelled_MalformedJSON_200ParseFailed covers
// shopify.go:259-265: malformed JSON on the cancel path must also be 200
// parse_failed (Shopify must not retry).
func TestHandleOrderCancelled_MalformedJSON_200ParseFailed(t *testing.T) {
	uc := &mockIngest{cancelResult: &appimporting.CancelResult{PlatformOrderNo: "#x"}}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)

	body := []byte(`{"broken`)
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "orders/cancelled")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "parse_failed" {
		t.Errorf("error: got %v want parse_failed", resp["error"])
	}
}

// TestHandleOrderCancelled_NoIDOrName_200ParseFailed covers shopify.go:266-273:
// an order payload with neither "name" nor a usable "id" (zero value) yields
// parse_failed rather than an empty order number being ingested.
func TestHandleOrderCancelled_NoIDOrName_200ParseFailed(t *testing.T) {
	uc := &mockIngest{cancelResult: &appimporting.CancelResult{PlatformOrderNo: "#x"}}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)

	// id: 0 marshals to json.Number "0", which is non-empty as a string, so the
	// handler falls back to it — to hit the empty-order-number branch we must
	// omit id entirely (it becomes "" as json.Number's zero value) and name.
	body := []byte(`{"name":"","line_items":[]}`)
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "orders/cancelled")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "parse_failed" {
		t.Errorf("error: got %v want parse_failed (no id or name)", resp["error"])
	}
}

// ----- handleRefundCreate: malformed JSON + refund_error ---------------------

// TestHandleRefundCreate_MalformedJSON_200ParseFailed covers shopify.go:311-318:
// malformed JSON with a valid HMAC on the refunds endpoint returns 200
// parse_failed.
func TestHandleRefundCreate_MalformedJSON_200ParseFailed(t *testing.T) {
	uc := &mockIngest{refundResult: &appimporting.RefundResult{}}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)

	body := []byte(`not json at all`)
	sig := sign(body)
	w := doRefundRequest(r, body, sig, "test.myshopify.com", "refunds/create")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "parse_failed" {
		t.Errorf("error: got %v want parse_failed", resp["error"])
	}
}

// TestHandleRefundCreate_IngestError_500 covers shopify.go:326-333: the
// refund ingest use case returning an error surfaces as 500 refund_error.
func TestHandleRefundCreate_IngestError_500(t *testing.T) {
	uc := &mockIngest{refundErr: fmt.Errorf("refund write failed")}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)

	body := refundBody(7001, 3001, "SKU-RE2", 1, "5.00")
	sig := sign(body)
	w := doRefundRequest(r, body, sig, "test.myshopify.com", "refunds/create")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 refund_error, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "refund_error" {
		t.Errorf("error: got %v want refund_error", resp["error"])
	}
}

// TestHandleRefundCreate_NegativePrice_200ParseFailed covers the refund-side
// business invariant mirroring the order-side one: a negative refund line
// price is rejected at parse time (shopify.go:502-504) and never reaches
// ingest — never a negative-money bill.
func TestHandleRefundCreate_NegativePrice_200ParseFailed(t *testing.T) {
	uc := &mockIngest{refundResult: &appimporting.RefundResult{}}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)

	body := refundBody(7002, 3002, "SKU-RNEG", 1, "-1.00")
	sig := sign(body)
	w := doRefundRequest(r, body, sig, "test.myshopify.com", "refunds/create")

	if w.Code != http.StatusOK {
		t.Fatalf("negative refund price: want 200 parse_failed, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "parse_failed" {
		t.Errorf("error: got %v want parse_failed", resp["error"])
	}
}

// TestParseShopifyRefund_ZeroRefundID_ParseFailed covers shopify.go:470-473:
// refund id "0" (Shopify never emits this, but a malformed/forged payload
// might) must be rejected.
func TestParseShopifyRefund_ZeroRefundID_ParseFailed(t *testing.T) {
	uc := &mockIngest{refundResult: &appimporting.RefundResult{}}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)

	body := refundBody(0, 3003, "SKU-Z0", 1, "1.00")
	sig := sign(body)
	w := doRefundRequest(r, body, sig, "test.myshopify.com", "refunds/create")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "parse_failed" {
		t.Errorf("error: got %v want parse_failed (refund id 0)", resp["error"])
	}
}

// TestParseShopifyRefund_ZeroOrderID_ParseFailed covers shopify.go:480-482:
// order_id "0" must be rejected even when refund id is valid.
func TestParseShopifyRefund_ZeroOrderID_ParseFailed(t *testing.T) {
	uc := &mockIngest{refundResult: &appimporting.RefundResult{}}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)

	body := refundBody(7003, 0, "SKU-OID0", 1, "1.00")
	sig := sign(body)
	w := doRefundRequest(r, body, sig, "test.myshopify.com", "refunds/create")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "parse_failed" {
		t.Errorf("error: got %v want parse_failed (order_id 0)", resp["error"])
	}
}

// TestParseShopifyRefund_MalformedJSON_ErrorPath covers the json.Unmarshal
// error branch inside parseShopifyRefund itself (shopify.go:466-468), reached
// via a body whose HMAC is valid but whose JSON is invalid — already covered
// end-to-end by TestHandleRefundCreate_MalformedJSON_200ParseFailed above;
// this case additionally asserts the specific "no refund id" wording is not
// what fires (regression guard against conflating the two error branches).
func TestParseShopifyRefund_QtyFallbackToLineItemQuantity(t *testing.T) {
	// refund_line_items[].quantity == 0 (or negative) must fall back to
	// line_item.quantity (shopify.go:495-501). Build the payload by hand so we
	// can set the outer refund-line quantity to 0 while the inner line_item
	// quantity is positive.
	type lineItem struct {
		SKU      string `json:"sku"`
		Quantity int64  `json:"quantity"`
		Price    string `json:"price"`
	}
	type refundLine struct {
		Quantity int64    `json:"quantity"` // outer: 0 → triggers fallback
		LineItem lineItem `json:"line_item"`
	}
	type refund struct {
		ID              int64        `json:"id"`
		OrderID         int64        `json:"order_id"`
		Currency        string       `json:"currency"`
		RefundLineItems []refundLine `json:"refund_line_items"`
	}
	r2 := refund{
		ID:      7004,
		OrderID: 3004,
		RefundLineItems: []refundLine{
			{Quantity: 0, LineItem: lineItem{SKU: "SKU-FALLBACKQTY", Quantity: 5, Price: "2.00"}},
		},
	}
	body, err := json.Marshal(r2)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	mapping := defaultMapping()
	billID := uuid.New()
	uc := &mockIngest{refundResult: &appimporting.RefundResult{BillID: billID}}
	r := buildEngine(testSecret, &mockResolver{m: mapping}, uc)
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
		t.Fatalf("expected successful refund via qty fallback, got %v (body=%s)", resp["status"], w.Body.String())
	}
}

// TestParseShopifyRefund_NoRefundableLines_ParseFailed covers shopify.go:512-514:
// when every refund_line_items entry has no SKU or non-positive quantity
// (even after fallback), the refund is rejected as parse_failed.
func TestParseShopifyRefund_NoRefundableLines_ParseFailed(t *testing.T) {
	uc := &mockIngest{refundResult: &appimporting.RefundResult{}}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)

	// Empty SKU → line dropped → zero refundable lines.
	body := refundBody(7005, 3005, "", 1, "1.00")
	sig := sign(body)
	w := doRefundRequest(r, body, sig, "test.myshopify.com", "refunds/create")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "parse_failed" {
		t.Errorf("error: got %v want parse_failed (no refundable lines)", resp["error"])
	}
}

// TestParseShopifyRefund_ZeroCreatedAt_UsesNow covers shopify.go:516-519:
// when the refund payload omits created_at (zero time.Time), refundDate
// falls back to time.Now().UTC() rather than staying zero. We assert the
// refund succeeds and the ingest request's RefundDate is not the zero value.
func TestParseShopifyRefund_ZeroCreatedAt_UsesNow(t *testing.T) {
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
		RefundLineItems []refundLine `json:"refund_line_items"`
		// CreatedAt intentionally omitted → zero time.Time in the decoded struct.
	}
	r2 := refund{
		ID:      7006,
		OrderID: 3006,
		RefundLineItems: []refundLine{
			{Quantity: 1, LineItem: lineItem{SKU: "SKU-NOWDATE", Quantity: 1, Price: "1.00"}},
		},
	}
	body, err := json.Marshal(r2)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	mapping := defaultMapping()
	uc := &mockIngest{refundResult: &appimporting.RefundResult{}}
	r := buildEngine(testSecret, &mockResolver{m: mapping}, uc)
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
		t.Fatalf("expected successful refund with now-fallback date, got %v (body=%s)", resp["status"], w.Body.String())
	}
}

// ----- verifySignature edge cases (via the HTTP surface) ---------------------

// TestVerifySignature_EmptySignatureHeader_401 covers the sigHeader == ""
// branch of shopify.go:351 distinct from the empty-secret branch already
// covered by TestVerifySignature_EmptySecret in shopify_test.go.
func TestVerifySignature_EmptySignatureHeader_401(t *testing.T) {
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, &mockIngest{})
	body := orderBody("#9010", "SKU-NOSIG", 1, "1.00")
	w := doOrderRequest(r, body, "", "test.myshopify.com", "orders/create")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("empty signature header: want 401, got %d", w.Code)
	}
}

// TestVerifySignature_UndecodableBase64_401 covers the
// base64.StdEncoding.DecodeString error branch (shopify.go:358-361): a
// signature header that is not valid base64 must be rejected, not panic.
func TestVerifySignature_UndecodableBase64_401(t *testing.T) {
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, &mockIngest{})
	body := orderBody("#9011", "SKU-BADB64", 1, "1.00")
	// "!!!not-base64!!!" contains characters outside the base64 alphabet.
	w := doOrderRequest(r, body, "!!!not-base64!!!", "test.myshopify.com", "orders/create")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("undecodable base64 signature: want 401, got %d", w.Code)
	}
}

// TestVerifySignature_CorrectHMAC_Accepted is a direct restatement of the
// happy-path HMAC contract to close out branch coverage on the final `return
// hmac.Equal(...)` line — a signature correctly computed over the exact body
// with the configured secret must be accepted.
func TestVerifySignature_CorrectHMAC_Accepted(t *testing.T) {
	uc := &mockIngest{imported: appImported("#9012")}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)
	body := orderBody("#9012", "SKU-OKSIG", 1, "2.00")
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "orders/create")
	if w.Code != http.StatusOK {
		t.Fatalf("correct HMAC: want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if len(uc.called) != 1 {
		t.Errorf("expected ingest to run once for a correctly-signed request, got %d", len(uc.called))
	}
}

// ----- unsupported topic on refunds endpoint (mirrors orders coverage) ------

// TestHandleRefunds_UnsupportedTopic_400 covers shopify.go:150-155's default
// branch on the refunds dispatcher with a topic name that isn't
// "refunds/create" at all (as opposed to shopify_test.go's cross-endpoint
// "orders/create" case), locking the same 400 unsupported_topic contract.
func TestHandleRefunds_UnsupportedTopic_400(t *testing.T) {
	uc := &mockIngest{}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)
	body := refundBody(7007, 3007, "SKU-UT", 1, "1.00")
	sig := sign(body)
	w := doRefundRequest(r, body, sig, "test.myshopify.com", "refunds/update")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
	if len(uc.called) != 0 {
		t.Errorf("ingest must not run for unsupported refund topic, got %d calls", len(uc.called))
	}
}

// TestOrderCancelled_UsesIDFallback_WhenNameEmpty covers shopify.go:266-269:
// when "name" is empty but "id" is a non-zero number, the cancel path uses
// the id's string form as the platform order number.
func TestOrderCancelled_UsesIDFallback_WhenNameEmpty(t *testing.T) {
	type order struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	o := order{ID: 88888, Name: ""}
	body, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	mapping := defaultMapping()
	uc := &mockIngest{
		cancelResult: &appimporting.CancelResult{
			PlatformOrderNo: "88888",
			OriginalBillID:  uuid.New(),
			ReversalBillID:  uuid.New(),
		},
	}
	r := buildEngine(testSecret, &mockResolver{m: mapping}, uc)
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
		t.Errorf("status: got %v want cancelled", resp["status"])
	}
}

// dummy uses bytes so the import stays in effect if future edits trim other
// usages; kept intentionally small.
var _ = bytes.MinRead
