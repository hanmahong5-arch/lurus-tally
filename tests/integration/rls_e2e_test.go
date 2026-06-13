//go:build integration

// rls_e2e_test.go is the end-to-end gate for the RLS backstop: it boots the REAL
// application (lifecycle.NewApp — full router, middleware, handlers, repos)
// against a Postgres where the app connects as a NON-SUPERUSER role that OWNS the
// tally tables (so FORCE ROW LEVEL SECURITY is operative, mirroring production),
// authenticates as a tenant via a Personal Access Token (the real auth path, no
// Zitadel needed), and drives real HTTP endpoints through httptest.
//
// Unlike the table-level RLS tests, this exercises the full
// auth -> TenantDB-pin -> repo(dbscope) -> RLS chain. It is the only harness that
// can validate that every request handler pins correctly under a non-superuser
// connection — and therefore the prerequisite gate for ever flipping the policy
// CASE arm from "THEN true" to "THEN false". (The other integration tests connect
// as the testcontainer SUPERUSER, which bypasses RLS entirely and cannot prove
// this.)
//
// Run with:
//
//	go test -v -tags integration -timeout 300s ./tests/integration/ -run TestRLS_E2E
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	domainauth "github.com/hanmahong5-arch/lurus-tally/internal/domain/auth"
	"github.com/hanmahong5-arch/lurus-tally/internal/lifecycle"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/config"
)

// bootRLSApp starts Postgres, migrates as superuser, creates a NOSUPERUSER role
// that OWNS every tally table (FORCE operative), boots the real app pointed at
// that role, and returns the HTTP handler + the superuser *sql.DB (for seeding).
func bootRLSApp(t *testing.T) (http.Handler, *sql.DB, func()) {
	t.Helper()
	dsn, stopPG := startPostgres(t)
	ctx := context.Background()

	if err := lifecycle.RunMigrations(ctx, dsn, nil); err != nil {
		stopPG()
		t.Fatalf("RunMigrations: %v", err)
	}
	superDB, err := sql.Open("pgx", dsn)
	if err != nil {
		stopPG()
		t.Fatalf("open superuser db: %v", err)
	}

	// Non-superuser role that OWNS all tally tables so FORCE binds it (prod shape).
	mustExec(t, superDB, `CREATE ROLE `+rlsAppRole+` LOGIN NOSUPERUSER PASSWORD '`+rlsAppPassword+`'`)
	mustExec(t, superDB, `GRANT USAGE ON SCHEMA tally TO `+rlsAppRole)
	mustExec(t, superDB, `GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA tally TO `+rlsAppRole)
	mustExec(t, superDB, `GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA tally TO `+rlsAppRole)
	mustExec(t, superDB, `DO $$
		DECLARE r record;
		BEGIN
			FOR r IN SELECT tablename FROM pg_tables WHERE schemaname='tally' LOOP
				EXECUTE format('ALTER TABLE tally.%I OWNER TO `+rlsAppRole+`', r.tablename);
			END LOOP;
		END $$;`)

	cfg := &config.Config{
		DatabaseDSN:          appDSNFrom(t, dsn), // connect as the non-superuser owner
		GinMode:              "release",
		LogLevel:             "warn", // surface auth-resolver warnings if this ever regresses
		ServiceVersion:       "e2e",
		Port:                 "0",
		ZitadelDomain:        "auth.dummy.local", // wires authMW + patResolver; JWKS fetched lazily (never, for PAT)
		ZitadelAudience:      "tally",
		ShopifyWebhookSecret: rlsWebhookSecret, // enables the public webhook for the import-isolation test
		// Redis / NATS / NewAPI / Platform deliberately empty: idempotency no-op,
		// NATS noop, AI + billing disabled. The converted CRUD/read endpoints
		// under test need only the DB.
	}
	app, err := lifecycle.NewApp(cfg)
	if err != nil {
		superDB.Close() //nolint:errcheck
		stopPG()
		t.Fatalf("NewApp: %v", err)
	}

	cleanup := func() {
		sctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = app.Stop(sctx)
		superDB.Close() //nolint:errcheck
		stopPG()
	}
	return app.Handler(), superDB, cleanup
}

// seedPAT mints a real PAT for tenantID and inserts it via the superuser
// connection. Returns the plaintext bearer token. PATs carry no scopes (no
// route ever enforced them; column dropped in 000052), so a single token is
// sufficient for both reads and writes.
func seedPAT(t *testing.T, db *sql.DB, ctx context.Context, tenantID uuid.UUID) string {
	t.Helper()
	plaintext, prefix, hash, err := domainauth.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO tally.personal_access_token (id, tenant_id, name, prefix, hash, created_at)
		VALUES ($1, $2, 'e2e', $3, $4, now())
	`, uuid.New(), tenantID, prefix, hash)
	if err != nil {
		t.Fatalf("seedPAT insert: %v", err)
	}
	return plaintext
}

// doReq drives the app handler in-process with a PAT bearer and returns the
// status + body.
func doReq(t *testing.T, h http.Handler, method, path, bearer, body string) (int, string) {
	t.Helper()
	var br io.Reader
	if body != "" {
		br = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, br)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// createProduct POSTs a product via the real API and returns the new id. Using
// the real create path (rather than raw SQL) seeds correctly-shaped rows AND
// exercises the write side of the auth -> pin -> repo -> RLS chain.
func createProduct(t *testing.T, h http.Handler, bearer, code, name string) string {
	t.Helper()
	status, body := doReq(t, h, http.MethodPost, "/api/v1/products", bearer,
		`{"name":"`+name+`","code":"`+code+`"}`)
	if status != http.StatusCreated {
		t.Fatalf("POST /products (%s): want 201, got %d body=%s", code, status, body)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("POST /products (%s): response not JSON: %v (%s)", code, err, body)
	}
	id, _ := m["id"].(string)
	if id == "" {
		t.Fatalf("POST /products (%s): no id in response: %s", code, body)
	}
	return id
}

// postExpectCreated POSTs body as bearer and fails unless 201.
func postExpectCreated(t *testing.T, h http.Handler, bearer, path, body string) string {
	t.Helper()
	status, resp := doReq(t, h, http.MethodPost, path, bearer, body)
	if status != http.StatusCreated {
		t.Fatalf("POST %s: want 201, got %d body=%s", path, status, resp)
	}
	return resp
}

// TestRLS_E2E_EntityCRUDIsolation drives the simple converted CRUD repos end to
// end and asserts each tenant sees only its own rows. These are exactly the
// tables the 000045 flip targets, so this is the gate that proves they stay
// reachable when the policy turns strict.
func TestRLS_E2E_EntityCRUDIsolation(t *testing.T) {
	h, db, cleanup := bootRLSApp(t)
	defer cleanup()
	ctx := context.Background()

	tenantA := insertTenant(t, db, ctx)
	tenantB := insertTenant(t, db, ctx)
	patA := seedPAT(t, db, ctx, tenantA)
	patB := seedPAT(t, db, ctx, tenantB)

	// Each entity: a create path, a list path, and per-tenant bodies carrying a
	// unique marker so the list assertion is unambiguous.
	cases := []struct {
		name             string
		createPath       string
		listPath         string
		bodyA, bodyB     string
		markerA, markerB string
	}{
		{"suppliers", "/api/v1/suppliers", "/api/v1/suppliers",
			`{"name":"Sup-AAA-zz1"}`, `{"name":"Sup-BBB-zz1"}`, "Sup-AAA-zz1", "Sup-BBB-zz1"},
		{"warehouses", "/api/v1/warehouses", "/api/v1/warehouses",
			`{"name":"WH-AAA-zz2"}`, `{"name":"WH-BBB-zz2"}`, "WH-AAA-zz2", "WH-BBB-zz2"},
		{"projects", "/api/v1/projects", "/api/v1/projects",
			`{"code":"PA-zz3","name":"Proj-AAA-zz3","status":"active","address":"a","manager":"m","remark":""}`,
			`{"code":"PB-zz3","name":"Proj-BBB-zz3","status":"active","address":"a","manager":"m","remark":""}`,
			"Proj-AAA-zz3", "Proj-BBB-zz3"},
		{"units", "/api/v1/units", "/api/v1/units",
			`{"code":"uA-zz4","name":"Unit-AAA-zz4","unit_type":"length"}`,
			`{"code":"uB-zz4","name":"Unit-BBB-zz4","unit_type":"length"}`,
			"Unit-AAA-zz4", "Unit-BBB-zz4"},
	}
	for _, c := range cases {
		postExpectCreated(t, h, patA, c.createPath, c.bodyA)
		postExpectCreated(t, h, patB, c.createPath, c.bodyB)

		_, listA := doReq(t, h, http.MethodGet, c.listPath, patA, "")
		if !strings.Contains(listA, c.markerA) {
			t.Errorf("FAIL [%s]: A's list missing its own row %q: %s", c.name, c.markerA, listA)
		}
		if strings.Contains(listA, c.markerB) {
			t.Errorf("FAIL [%s]: A's list LEAKED B's row %q: %s", c.name, c.markerB, listA)
		}
		_, listB := doReq(t, h, http.MethodGet, c.listPath, patB, "")
		if strings.Contains(listB, c.markerA) {
			t.Errorf("FAIL [%s]: B's list LEAKED A's row %q: %s", c.name, c.markerA, listB)
		}
		if t.Failed() {
			continue
		}
		t.Logf("PASS [%s]: per-tenant CRUD isolation through full stack", c.name)
	}

	// exchange_rate uses GetRate(from,to) rather than a plain list: both tenants
	// book USD->CNY at different rates and must each read back their own.
	postExpectCreated(t, h, patA, "/api/v1/exchange-rates",
		`{"from_currency":"USD","to_currency":"CNY","rate":"6.50","effective_at":"2026-06-01"}`)
	postExpectCreated(t, h, patB, "/api/v1/exchange-rates",
		`{"from_currency":"USD","to_currency":"CNY","rate":"7.77","effective_at":"2026-06-01"}`)
	_, rateA := doReq(t, h, http.MethodGet, "/api/v1/exchange-rates?from=USD&to=CNY", patA, "")
	_, rateB := doReq(t, h, http.MethodGet, "/api/v1/exchange-rates?from=USD&to=CNY", patB, "")
	if !strings.Contains(rateA, "6.5") || strings.Contains(rateA, "7.77") {
		t.Errorf("FAIL [exchange_rate]: A read the wrong tenant's rate: %s", rateA)
	}
	if !strings.Contains(rateB, "7.77") {
		t.Errorf("FAIL [exchange_rate]: B did not read its own rate: %s", rateB)
	}
	if !t.Failed() {
		t.Logf("PASS [exchange_rate]: per-tenant rate isolation")
	}
}

// TestRLS_E2E_MoneyFlowIsolation drives the purchase->approve->stock and
// quick-checkout->payment money path end to end and asserts the resulting
// stock/bill/payment rows are tenant-isolated. (These tables are NOT flipped by
// 000045 — the public shopify webhook writes them unpinned — but proving they
// isolate when pinned locks Phase-2 correctness on the money path and is the
// groundwork for a future webhook-pin + money-table flip.)
func TestRLS_E2E_MoneyFlowIsolation(t *testing.T) {
	h, db, cleanup := bootRLSApp(t)
	defer cleanup()
	ctx := context.Background()

	tenantA := insertTenant(t, db, ctx)
	tenantB := insertTenant(t, db, ctx)
	patA := seedPAT(t, db, ctx, tenantA)
	patB := seedPAT(t, db, ctx, tenantB)

	// Tenant A: product + warehouse, then a purchase booked & approved (stock-in).
	prodA := createProduct(t, h, patA, "MF-A-1", "MoneyFlow A")
	whResp := postExpectCreated(t, h, patA, "/api/v1/warehouses", `{"name":"MF-WH-A"}`)
	whA := jsonField(t, whResp, "id")

	billResp := postExpectCreated(t, h, patA, "/api/v1/purchase-bills",
		`{"warehouse_id":"`+whA+`","items":[{"product_id":"`+prodA+`","qty":"10.00","unit_price":"5.00"}]}`)
	billA := jsonField(t, billResp, "bill_id")
	if st, body := doReq(t, h, http.MethodPost, "/api/v1/purchase-bills/"+billA+"/approve", patA, "{}"); st != http.StatusOK {
		t.Fatalf("approve purchase as A: want 200, got %d body=%s", st, body)
	}

	// Stock snapshot now exists for A; B must not see it.
	_, snapA := doReq(t, h, http.MethodGet, "/api/v1/stock/snapshots", patA, "")
	if !strings.Contains(snapA, prodA) {
		t.Errorf("FAIL: A's stock snapshots missing its product %s: %s", prodA, snapA)
	}
	_, snapB := doReq(t, h, http.MethodGet, "/api/v1/stock/snapshots", patB, "")
	if strings.Contains(snapB, prodA) {
		t.Errorf("FAIL: B's stock snapshots LEAKED A's product %s: %s", prodA, snapB)
	}

	// Purchase bill list isolation.
	_, billsA := doReq(t, h, http.MethodGet, "/api/v1/purchase-bills", patA, "")
	_, billsB := doReq(t, h, http.MethodGet, "/api/v1/purchase-bills", patB, "")
	if !strings.Contains(billsA, billA) {
		t.Errorf("FAIL: A's purchase-bills missing its bill %s", billA)
	}
	if strings.Contains(billsB, billA) {
		t.Errorf("FAIL: B's purchase-bills LEAKED A's bill %s", billA)
	}

	// Quick-checkout sale (stock-out + payment) for A, then payment isolation.
	// paid_amount must be <= total (3 * 20 = 60), per the bill_head_paid_le_total check.
	saleResp := postExpectCreated(t, h, patA, "/api/v1/sale-bills/quick-checkout",
		`{"payment_method":"cash","paid_amount":"60.00","items":[{"product_id":"`+prodA+`","warehouse_id":"`+whA+`","qty":"3.00","unit_price":"20.00"}]}`)
	saleA := jsonField(t, saleResp, "bill_id")
	// A sees its own payment for the sale bill; B querying the same bill id sees
	// none (RLS hides A's payment_head rows under B's pin).
	_, payA := doReq(t, h, http.MethodGet, "/api/v1/payments?bill_id="+saleA, patA, "")
	if !strings.Contains(payA, "amount") {
		t.Errorf("FAIL: A's payments for its sale bill are empty: %s", payA)
	}
	_, payB := doReq(t, h, http.MethodGet, "/api/v1/payments?bill_id="+saleA, patB, "")
	if strings.Contains(payB, "amount") {
		t.Errorf("FAIL: B read A's payment for bill %s: %s", saleA, payB)
	}
	if !t.Failed() {
		t.Logf("PASS: money-flow (purchase->approve->stock, quick-checkout->payment) tenant-isolated")
	}
}

// TestRLS_E2E_ReadPathsIsolation drives the join-heavy analytics/read endpoints
// (reports, replenish, low-stock, weekly-summary) that read across product +
// bill + stock. It fills the flip's coverage blind spot: under the strict policy
// (000046) an unpinned read returns 0, so "tenant A still sees its own product
// marker here, tenant B never does" both proves isolation AND that these read
// paths are pinned. (Under the current empty->true policy it confirms isolation;
// the pin guarantee is what it adds once run against the flipped schema.)
func TestRLS_E2E_ReadPathsIsolation(t *testing.T) {
	h, db, cleanup := bootRLSApp(t)
	defer cleanup()
	ctx := context.Background()

	tenantA := insertTenant(t, db, ctx)
	tenantB := insertTenant(t, db, ctx)
	patA := seedPAT(t, db, ctx, tenantA)
	patB := seedPAT(t, db, ctx, tenantB)

	// Marker appears in BOTH the product code and name so assertions are robust to
	// whether a report surfaces code or name.
	const markerA = "RPZAMARK"
	const markerB = "RPZBMARK"

	// Tenant A: product + warehouse + stock (purchase approve) + a sale
	// (quick-checkout) so the analytics have data, plus a low safety stock so the
	// replenish/low-stock joins produce a row.
	prodA := createProduct(t, h, patA, markerA+"-1", markerA+" product")
	whA := jsonField(t, postExpectCreated(t, h, patA, "/api/v1/warehouses", `{"name":"`+markerA+`-wh"}`), "id")
	billA := jsonField(t, postExpectCreated(t, h, patA, "/api/v1/purchase-bills",
		`{"warehouse_id":"`+whA+`","items":[{"product_id":"`+prodA+`","qty":"100.00","unit_price":"3.00"}]}`), "bill_id")
	if st, b := doReq(t, h, http.MethodPost, "/api/v1/purchase-bills/"+billA+"/approve", patA, "{}"); st != http.StatusOK {
		t.Fatalf("approve purchase: %d %s", st, b)
	}
	if _, b := doReq(t, h, http.MethodPost, "/api/v1/sale-bills/quick-checkout", patA,
		`{"payment_method":"cash","paid_amount":"40.00","items":[{"product_id":"`+prodA+`","warehouse_id":"`+whA+`","qty":"2.00","unit_price":"20.00"}]}`); b == "" {
		t.Fatal("quick-checkout returned empty body")
	}
	// Safety stock so the replenish/low-stock joins flag this product.
	parsedWhA, err := uuid.Parse(whA)
	if err != nil {
		t.Fatalf("parse whA: %v", err)
	}
	parsedProdA, err := uuid.Parse(prodA)
	if err != nil {
		t.Fatalf("parse prodA: %v", err)
	}
	insertStockInitial(t, db, ctx, tenantA, parsedProdA, parsedWhA, 5, 999)

	// Tenant B: a product only (no sales/stock), so its analytics are empty and
	// must never surface A's marker.
	_ = createProduct(t, h, patB, markerB+"-1", markerB+" product")

	// endpoints where A definitely has data and the response names the product.
	wantAMarker := []string{
		"/api/v1/reports/sales-top?metric=revenue&days=30",
		"/api/v1/stock/alerts/low-stock",
	}
	for _, ep := range wantAMarker {
		stA, bodyA := doReq(t, h, http.MethodGet, ep, patA, "")
		if stA != http.StatusOK {
			t.Errorf("FAIL [%s] A: status %d body=%s", ep, stA, bodyA)
		} else if !strings.Contains(bodyA, markerA) {
			t.Errorf("FAIL [%s] A: missing own product marker (read path not pinned / empty?): %s", ep, bodyA)
		} else {
			t.Logf("PASS [%s] A: sees own data", ep)
		}
	}

	// All read endpoints must be 200 for both tenants and never leak A's marker to B.
	allReads := []string{
		"/api/v1/reports/sales-top?metric=revenue&days=30",
		"/api/v1/reports/gross-margin?days=30",
		"/api/v1/reports/abc",
		"/api/v1/reports/dead-stock?days=90",
		"/api/v1/replenish/suggestions",
		"/api/v1/stock/alerts/low-stock",
		"/api/v1/weekly-summary",
	}
	for _, ep := range allReads {
		stB, bodyB := doReq(t, h, http.MethodGet, ep, patB, "")
		if stB != http.StatusOK {
			t.Errorf("FAIL [%s] B: status %d body=%s", ep, stB, bodyB)
		} else if strings.Contains(bodyB, markerA) {
			t.Errorf("FAIL [%s] B: LEAKED tenant A's product marker: %s", ep, bodyB)
		} else {
			t.Logf("PASS [%s] B: 200, no leak of A's data", ep)
		}
	}
}

// jsonField extracts a top-level string field from a JSON object body.
func jsonField(t *testing.T, body, field string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("response not JSON object: %v (%s)", err, body)
	}
	v, _ := m[field].(string)
	if v == "" {
		t.Fatalf("response missing %q: %s", field, body)
	}
	return v
}

// TestRLS_E2E_CrossTenantIsolation proves the full stack isolates tenants when
// the app runs as a non-superuser owner with FORCE RLS, authenticated via PAT,
// driving the real create + read endpoints end to end.
func TestRLS_E2E_CrossTenantIsolation(t *testing.T) {
	h, db, cleanup := bootRLSApp(t)
	defer cleanup()
	ctx := context.Background()

	tenantA := insertTenant(t, db, ctx)
	tenantB := insertTenant(t, db, ctx)
	patA := seedPAT(t, db, ctx, tenantA)
	patB := seedPAT(t, db, ctx, tenantB)

	// Seed through the real write path (each create is itself a pinned, RLS-bound op).
	pA1 := createProduct(t, h, patA, "AE-1", "Alpha A")
	_ = createProduct(t, h, patA, "AE-2", "Beta A")
	pB1 := createProduct(t, h, patB, "BE-1", "Alpha B")

	// (1) GET /products as A: sees A's products, never B's.
	code, body := doReq(t, h, http.MethodGet, "/api/v1/products", patA, "")
	if code != http.StatusOK {
		t.Fatalf("GET /products as A: status %d, body=%s", code, body)
	}
	if !strings.Contains(body, "AE-1") || !strings.Contains(body, "AE-2") {
		t.Errorf("FAIL: A's product list missing A's codes: %s", body)
	}
	if strings.Contains(body, "BE-1") {
		t.Errorf("FAIL: A's product list LEAKED tenant B's product (BE-1): %s", body)
	}
	t.Logf("PASS: GET /products as A returned only A's products")

	// (2) GET /products as B: sees B's, never A's.
	code, body = doReq(t, h, http.MethodGet, "/api/v1/products", patB, "")
	if code != http.StatusOK {
		t.Fatalf("GET /products as B: status %d, body=%s", code, body)
	}
	if !strings.Contains(body, "BE-1") {
		t.Errorf("FAIL: B's product list missing BE-1: %s", body)
	}
	if strings.Contains(body, "AE-1") || strings.Contains(body, "AE-2") {
		t.Errorf("FAIL: B's product list LEAKED tenant A's products: %s", body)
	}
	t.Logf("PASS: GET /products as B returned only B's products")

	// (3) cross-tenant GET by id: A asking for B's product must NOT be 200.
	code, _ = doReq(t, h, http.MethodGet, "/api/v1/products/"+pB1, patA, "")
	if code == http.StatusOK {
		t.Errorf("FAIL: A could fetch B's product by id (status 200) — RLS did not hide it")
	} else {
		t.Logf("PASS: A fetching B's product by id -> %d (hidden)", code)
	}

	// (4) own GET by id works.
	code, _ = doReq(t, h, http.MethodGet, "/api/v1/products/"+pA1, patA, "")
	if code != http.StatusOK {
		t.Errorf("FAIL: A could not fetch its own product by id: %d", code)
	}

	// (5) no/invalid auth -> 401 (request never reaches a tenant-scoped query).
	code, _ = doReq(t, h, http.MethodGet, "/api/v1/products", "", "")
	if code != http.StatusUnauthorized {
		t.Errorf("FAIL: unauthenticated GET /products: want 401, got %d", code)
	} else {
		t.Logf("PASS: unauthenticated GET /products -> 401")
	}
}
