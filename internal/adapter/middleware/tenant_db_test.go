package middleware

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
)

func init() { gin.SetMode(gin.TestMode) }

// TestTenantDB_NilDB_IsNoOp: with no pool, the middleware must pass the request
// through untouched (dev / no-DB wiring).
func TestTenantDB_NilDB_IsNoOp(t *testing.T) {
	reached := false
	r := gin.New()
	r.Use(TenantDB(nil))
	r.GET("/x", func(c *gin.Context) {
		reached = true
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !reached {
		t.Fatal("downstream handler must run when db is nil")
	}
}

// TestTenantDB_NoTenant_DoesNotPin: when the auth layer left no tenant_id (uuid.Nil,
// e.g. a first-time user pre-onboarding), the middleware must NOT pin a connection
// and must NOT touch the database — repos fall back to the shared pool.
func TestTenantDB_NoTenant_DoesNotPin(t *testing.T) {
	// Unreachable DSN: sql.Open does not dial, and the no-tenant path must never
	// use the handle, so any attempt to connect would surface as a test failure
	// (handler would block / error instead of returning the assertions below).
	db, err := sql.Open("pgx", "postgres://u:p@127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close() //nolint:errcheck

	var pinned bool
	r := gin.New()
	r.Use(TenantDB(db)) // no tenant_id set in context → GetTenantID == uuid.Nil
	r.GET("/x", func(c *gin.Context) {
		// With nothing pinned, From returns the fallback pool, not a *sql.Conn.
		if got := dbscope.From(c.Request.Context(), db); got != dbscope.Querier(db) {
			pinned = true
		}
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (no-tenant request must pass through)", w.Code)
	}
	if pinned {
		t.Fatal("middleware must not pin a connection when tenant_id is uuid.Nil")
	}
}

// TestTenantDB_NilTenantConstant guards the contract that GetTenantID's absence
// sentinel really is uuid.Nil (the value TenantDB branches on).
func TestTenantDB_NilTenantConstant(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
	if GetTenantID(c) != uuid.Nil {
		t.Fatal("GetTenantID on a context with no tenant must be uuid.Nil")
	}
}
