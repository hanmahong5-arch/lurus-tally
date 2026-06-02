//go:build integration

// rls_e2e_webhook_test.go drives the PUBLIC shopify webhook -> import path
// end to end against the real app (non-superuser owner, FORCE), proving the
// imported order's sale/stock rows land on the resolved tenant via the pinned
// connection (BuildShopifyHandler.WithPinner + dbscope.WithPinnedConn). This is
// the safety net + gate for flipping the money/stock tables strict: the webhook
// resolves its tenant outside the auth middleware, so without the pin its writes
// would be unpinned and a strict policy would reject them.
package integration

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// rlsWebhookSecret is the SHOPIFY_WEBHOOK_SECRET bootRLSApp configures; the test
// signs payloads with it so HMAC verification passes.
const rlsWebhookSecret = "e2e-shopify-secret"

func signShopify(body []byte) string {
	mac := hmac.New(sha256.New, []byte(rlsWebhookSecret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// TestRLS_E2E_WebhookImportIsolation: an orders/create webhook for tenant A's
// shop imports a sale that decrements A's stock, on a connection pinned to A.
func TestRLS_E2E_WebhookImportIsolation(t *testing.T) {
	h, db, cleanup := bootRLSApp(t)
	defer cleanup()
	ctx := context.Background()

	tenantA := insertTenant(t, db, ctx)
	patA := seedPAT(t, db, ctx, tenantA)

	// Product + warehouse + stock (via the real pinned API money path).
	prodA := createProduct(t, h, patA, "WH-SKU-A", "Webhook Product A")
	whA := jsonField(t, postExpectCreated(t, h, patA, "/api/v1/warehouses", `{"name":"WH-Webhook-A"}`), "id")
	billA := jsonField(t, postExpectCreated(t, h, patA, "/api/v1/purchase-bills",
		`{"warehouse_id":"`+whA+`","items":[{"product_id":"`+prodA+`","qty":"50.00","unit_price":"3.00"}]}`), "bill_id")
	if st, body := doReq(t, h, http.MethodPost, "/api/v1/purchase-bills/"+billA+"/approve", patA, "{}"); st != http.StatusOK {
		t.Fatalf("seed stock: approve purchase want 200, got %d body=%s", st, body)
	}

	// Map the shop domain -> tenant A (+ warehouse/creator), and the platform SKU
	// -> product A, both via the superuser connection (RLS bypassed for seeding).
	const shopDomain = "a-shop.myshopify.com"
	const platformSKU = "SKU-WH-A"
	creatorA := uuid.New()
	mustExec(t, db, `INSERT INTO tally.shopify_shop_map (id, tenant_id, shop_domain, warehouse_id, creator_id)
		VALUES ($1, $2, $3, $4, $5)`, uuid.New(), tenantA, shopDomain, whA, creatorA)
	mustExec(t, db, `INSERT INTO tally.import_sku_map (id, tenant_id, platform, platform_sku, product_id)
		VALUES ($1, $2, 'shopify', $3, $4)`, uuid.New(), tenantA, platformSKU, prodA)

	// Deliver a signed orders/create webhook selling 2 units of the mapped SKU.
	body := `{"id":1001,"name":"#WH1001","currency":"USD","created_at":"2026-06-01T00:00:00Z",` +
		`"line_items":[{"sku":"` + platformSKU + `","quantity":2,"price":"20.00"}]}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/shopify/orders", strings.NewReader(body))
	req.Header.Set("X-Shopify-Topic", "orders/create")
	req.Header.Set("X-Shopify-Shop-Domain", shopDomain)
	req.Header.Set("X-Shopify-Hmac-Sha256", signShopify([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("webhook orders/create: want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("webhook response not JSON: %v (%s)", err, w.Body.String())
	}
	if resp["status"] != "imported" {
		t.Fatalf("webhook import did not succeed: %s", w.Body.String())
	}
	importedBillID, _ := resp["bill_id"].(string)
	if importedBillID == "" {
		t.Fatalf("webhook response missing bill_id: %s", w.Body.String())
	}

	// The imported sale bill belongs to tenant A (the pin scoped the write).
	var billTenant string
	if err := db.QueryRowContext(ctx,
		"SELECT tenant_id::text FROM tally.bill_head WHERE id = $1", importedBillID).Scan(&billTenant); err != nil {
		t.Fatalf("read imported bill tenant: %v", err)
	}
	if billTenant != tenantA.String() {
		t.Errorf("FAIL: imported bill tenant = %s, want A %s", billTenant, tenantA)
	}

	// And a stock-out movement was recorded for A's product.
	var outMovements int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM tally.stock_movement
		 WHERE tenant_id = $1 AND product_id = $2 AND direction = 'out'`,
		tenantA, prodA).Scan(&outMovements); err != nil {
		t.Fatalf("count stock-out movements: %v", err)
	}
	if outMovements == 0 {
		t.Errorf("FAIL: webhook import recorded no stock-out movement for A's product")
	}
	if !t.Failed() {
		t.Logf("PASS: pinned webhook import created tenant-A sale + stock-out (bill %s, %d out-movement)", importedBillID, outMovements)
	}
}
