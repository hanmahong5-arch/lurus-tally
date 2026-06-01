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
		DatabaseDSN:     appDSNFrom(t, dsn), // connect as the non-superuser owner
		GinMode:         "release",
		LogLevel:        "warn", // surface auth-resolver warnings if this ever regresses
		ServiceVersion:  "e2e",
		Port:            "0",
		ZitadelDomain:   "auth.dummy.local", // wires authMW + patResolver; JWKS fetched lazily (never, for PAT)
		ZitadelAudience: "tally",
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
// connection. Returns the plaintext bearer token. scopes default to ['read'];
// no route enforces scopes, so that is sufficient for both reads and writes.
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
