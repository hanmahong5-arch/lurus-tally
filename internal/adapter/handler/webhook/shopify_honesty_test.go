// Honesty lock test (ARCH-14) — converts the verified claim
//
//	"POST /webhooks/shopify/orders HMAC-SHA256 via X-Shopify-Hmac-Sha256,
//	 added 2026-05-28; tenant via tally.shopify_shop_map"
//
// into behavioural contracts. It reuses the mocks/helpers already declared in
// shopify_test.go (same webhook_test package): mockResolver, mockIngest,
// testSecret, sign, defaultMapping, buildEngine, orderBody, doOrderRequest.
//
// These assertions target the security/routing contract a malicious or
// misconfigured caller observes — NOT the handler's internal statements
// (§4.1③ no tautology). Verified named artifact: tally.shopify_shop_map
// exists (migrations/000039_shopify_shop_map.up.sql); HMAC header
// X-Shopify-Hmac-Sha256 is read in shopify.go.
package webhook_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/webhook"
	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
)

// signWith computes the base64(HMAC-SHA256(body)) under an arbitrary secret —
// used to forge a "valid-looking but wrong-key" signature.
func signWith(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// TestShopifyOrders_HMACVerifyAndTenantResolve is the ARCH-14 anchor:
// a correctly-signed orders/create whose shop_domain maps to a tenant must be
// accepted (200) AND the resolved (tenant_id, warehouse_id, creator_id) from
// shopify_shop_map must flow into the ingest request unchanged.
func TestShopifyOrders_HMACVerifyAndTenantResolve(t *testing.T) {
	mapping := &webhook.ShopMapping{
		ShopDomain:  "studio.myshopify.com",
		TenantID:    uuid.New(),
		WarehouseID: uuid.New(),
		CreatorID:   uuid.New(),
	}
	uc := &mockIngest{
		imported: appImported("#5001"),
	}
	r := buildEngine(testSecret, &mockResolver{m: mapping}, uc)

	body := orderBody("#5001", "SKU-T", 4, "12.50")
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "studio.myshopify.com", "orders/create")

	if w.Code != http.StatusOK {
		t.Fatalf("valid signed order: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	if len(uc.called) != 1 {
		t.Fatalf("expected exactly 1 ingest call, got %d", len(uc.called))
	}
	got := uc.called[0]
	if got.TenantID != mapping.TenantID {
		t.Errorf("tenant_id not propagated from shop map: got %s want %s", got.TenantID, mapping.TenantID)
	}
	if got.WarehouseID != mapping.WarehouseID {
		t.Errorf("warehouse_id not propagated from shop map: got %s want %s", got.WarehouseID, mapping.WarehouseID)
	}
	if got.CreatorID != mapping.CreatorID {
		t.Errorf("creator_id not propagated from shop map: got %s want %s", got.CreatorID, mapping.CreatorID)
	}
}

// appImported builds a non-zero ImportedOrder for the "happy" mock path.
func appImported(orderNo string) appimporting.ImportedOrder {
	return appimporting.ImportedOrder{
		PlatformOrderNo: orderNo,
		BillID:          uuid.New(),
		BillNo:          "SL-honesty-0001",
	}
}

// skippedDuplicate builds a SkippedOrder marking the order as a duplicate.
func skippedDuplicate(orderNo string) *appimporting.SkippedOrder {
	return &appimporting.SkippedOrder{
		PlatformOrderNo: orderNo,
		Reason:          "duplicate",
	}
}

// TestShopifyOrders_BadSignature_401_NoIngest locks "签名不符→401且不写库":
// a wrong-key signature must be rejected with 401 and the ingest use case must
// never be called (no DB write).
func TestShopifyOrders_BadSignature_401_NoIngest(t *testing.T) {
	uc := &mockIngest{imported: appImported("#5002")}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)

	body := orderBody("#5002", "SKU-U", 1, "9.00")
	// Sign with a DIFFERENT secret → base64-decodable but HMAC mismatch.
	wrongSig := signWith("not-the-real-secret", body)
	w := doOrderRequest(r, body, wrongSig, "test.myshopify.com", "orders/create")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-key signature: want 401, got %d", w.Code)
	}
	if len(uc.called) != 0 {
		t.Errorf("ingest must NOT be called on signature failure, got %d calls", len(uc.called))
	}
}

// TestShopifyOrders_RawBodyNotReserialized locks "raw body 须原文非重序列化":
// the HMAC is computed over the exact wire bytes. We sign the original body but
// deliver a semantically-identical but byte-different re-serialization; it must
// be rejected (proving the handler hashes raw bytes, not a re-encoded struct).
func TestShopifyOrders_RawBodyNotReserialized(t *testing.T) {
	uc := &mockIngest{imported: appImported("#5003")}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)

	original := orderBody("#5003", "SKU-V", 2, "5.00")
	sig := sign(original) // signature over ORIGINAL bytes

	// Re-serialize through a generic map → key order / whitespace differs from
	// the original marshalled bytes, so the byte stream changes while the JSON
	// is semantically equal.
	var generic map[string]any
	if err := json.Unmarshal(original, &generic); err != nil {
		t.Fatalf("setup unmarshal: %v", err)
	}
	reserialized, _ := json.Marshal(generic)
	if bytes.Equal(original, reserialized) {
		t.Skip("re-serialization produced identical bytes; cannot exercise raw-body contract on this payload")
	}

	w := doOrderRequest(r, reserialized, sig, "test.myshopify.com", "orders/create")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("signature over original bytes vs re-serialized body: want 401, got %d", w.Code)
	}
	if len(uc.called) != 0 {
		t.Errorf("ingest must NOT run when raw body differs from signed bytes, got %d", len(uc.called))
	}
}

// TestShopifyOrders_UnknownShop_NotMapped_Rejected locks "shop_domain 未映射→拒":
// when the resolver returns no mapping (nil), the request is rejected with 404
// and ingest is never called.
func TestShopifyOrders_UnknownShop_NotMapped_Rejected(t *testing.T) {
	uc := &mockIngest{imported: appImported("#5004")}
	r := buildEngine(testSecret, &mockResolver{m: nil}, uc) // no mapping

	body := orderBody("#5004", "SKU-W", 1, "3.00")
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "ghost.myshopify.com", "orders/create")

	if w.Code != http.StatusNotFound {
		t.Fatalf("unmapped shop: want 404, got %d", w.Code)
	}
	if len(uc.called) != 0 {
		t.Errorf("ingest must NOT run for an unmapped shop, got %d calls", len(uc.called))
	}
}

// TestShopifyOrders_NonOrdersTopicRejected locks "Topic 非 orders/create→拒"
// at the orders endpoint: a verified request carrying an unsupported topic
// (e.g. checkouts/create) is rejected with 400 and ingest is not called.
func TestShopifyOrders_NonOrdersTopicRejected(t *testing.T) {
	uc := &mockIngest{imported: appImported("#5005")}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)

	body := orderBody("#5005", "SKU-X", 1, "7.00")
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "checkouts/create")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("unsupported topic on /orders: want 400, got %d", w.Code)
	}
	if len(uc.called) != 0 {
		t.Errorf("ingest must NOT run for an unsupported topic, got %d", len(uc.called))
	}
}

// TestShopifyOrders_EmptySecret_RejectsEverything locks "空 secret → 拒":
// a handler built with an empty SHOPIFY_WEBHOOK_SECRET must reject every signed
// request (the verify path returns false when secret == "") and never ingest.
// This is the runtime guard equivalent of a misconfigured deployment.
func TestShopifyOrders_EmptySecret_RejectsEverything(t *testing.T) {
	uc := &mockIngest{imported: appImported("#5006")}
	r := buildEngine("", &mockResolver{m: defaultMapping()}, uc)

	body := orderBody("#5006", "SKU-Y", 1, "1.00")
	sig := sign(body) // a perfectly valid signature under testSecret
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "orders/create")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("empty secret must reject: want 401, got %d", w.Code)
	}
	if len(uc.called) != 0 {
		t.Errorf("ingest must NOT run with empty secret, got %d", len(uc.called))
	}
}

// TestShopifyOrders_DuplicateOrder_Idempotent locks "重复 order 幂等":
// when the ingest use case reports the order as skipped (duplicate), the
// endpoint returns 200 with status="skipped" — the webhook does not error, so
// Shopify will not retry. We assert the response shape, not the dedup internals.
func TestShopifyOrders_DuplicateOrder_Idempotent(t *testing.T) {
	uc := &mockIngest{skipped: skippedDuplicate("#5007")}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)

	body := orderBody("#5007", "SKU-Z", 2, "8.00")
	sig := sign(body)
	w := doOrderRequest(r, body, sig, "test.myshopify.com", "orders/create")

	if w.Code != http.StatusOK {
		t.Fatalf("duplicate order: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if resp["status"] != "skipped" {
		t.Errorf("status: want skipped, got %v", resp["status"])
	}
}
