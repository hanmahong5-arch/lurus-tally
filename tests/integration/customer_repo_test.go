//go:build integration

// Customer repository real-SQL tests: customers are tally.partner rows with
// partner_type='customer'. Covers create + read-back, List search across
// name/code/mobile/phone with the customer-kind filter (a supplier partner row
// must not surface), and the duplicate-code path end to end through the HTTP
// handler (unique violation 23505 -> 409). Connects as the testcontainer
// superuser, so isolation here is the repo's explicit WHERE tenant_id clause
// (RLS-level isolation is covered by rls_e2e_test.go).
//
//	go test -v -tags integration -timeout 180s ./tests/integration/ -run TestCustomerRepo
package integration

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	handlercust "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/customer"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	customerrepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/customer"
	appcust "github.com/hanmahong5-arch/lurus-tally/internal/app/customer"
	domaincust "github.com/hanmahong5-arch/lurus-tally/internal/domain/customer"
)

func newCustomer(tenantID uuid.UUID, code, name, mobile, phone string) *domaincust.Customer {
	now := time.Now().UTC()
	return &domaincust.Customer{
		ID: uuid.New(), TenantID: tenantID, Code: code, Name: name,
		Mobile: mobile, Phone: phone, CreatedAt: now, UpdatedAt: now,
	}
}

func TestCustomerRepo_CreateSearchAndDuplicateCode(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()
	ctx := context.Background()
	repo := customerrepo.New(db)

	tenantID := insertTenant(t, db, ctx)
	otherTenant := insertTenant(t, db, ctx)

	a := newCustomer(tenantID, "CUS-A", "Alpha Trading", "13800001111", "")
	b := newCustomer(tenantID, "", "Beta Store", "", "0571-88886666")
	if err := repo.Create(ctx, a); err != nil {
		t.Fatalf("Create a: %v", err)
	}
	if err := repo.Create(ctx, b); err != nil {
		t.Fatalf("Create b: %v", err)
	}
	if err := repo.Create(ctx, newCustomer(otherTenant, "CUS-X", "Alpha Other Tenant", "13800001111", "")); err != nil {
		t.Fatalf("Create other tenant: %v", err)
	}
	// A supplier-kind partner row must never surface as a customer.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO tally.partner (id, tenant_id, partner_type, name, code)
		VALUES ($1, $2, 'supplier', 'Alpha Supplier', 'SUP-A')`, uuid.New(), tenantID); err != nil {
		t.Fatalf("insert supplier partner: %v", err)
	}

	t.Run("create_persists_partner_type_customer", func(t *testing.T) {
		var pt string
		if err := db.QueryRowContext(ctx, `SELECT partner_type FROM tally.partner WHERE id = $1`, a.ID).Scan(&pt); err != nil {
			t.Fatalf("read partner_type: %v", err)
		}
		if pt != "customer" {
			t.Errorf("partner_type = %q, want customer", pt)
		}
		got, err := repo.GetByID(ctx, tenantID, a.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.Code != "CUS-A" || got.Mobile != "13800001111" || got.Phone != "" {
			t.Errorf("GetByID = %+v", got)
		}
	})

	t.Run("get_other_tenant_is_not_found", func(t *testing.T) {
		if _, err := repo.GetByID(ctx, otherTenant, a.ID); !errors.Is(err, appcust.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("search_matches_name_code_mobile_phone_customer_only", func(t *testing.T) {
		cases := []struct {
			q    string
			want string
		}{
			{"alpha", "Alpha Trading"},   // name, case-insensitive; supplier + other tenant excluded
			{"cus-a", "Alpha Trading"},   // code
			{"0000111", "Alpha Trading"}, // mobile
			{"8888", "Beta Store"},       // phone
		}
		for _, tc := range cases {
			got, total, err := repo.List(ctx, domaincust.ListFilter{TenantID: tenantID, Query: tc.q, Limit: 10})
			if err != nil {
				t.Fatalf("List(%q): %v", tc.q, err)
			}
			if total != 1 || len(got) != 1 || got[0].Name != tc.want {
				t.Errorf("List(%q): total=%d rows=%d, want 1 %s", tc.q, total, len(got), tc.want)
			}
		}

		all, total, err := repo.List(ctx, domaincust.ListFilter{TenantID: tenantID, Limit: 10})
		if err != nil {
			t.Fatalf("List all: %v", err)
		}
		if total != 2 || len(all) != 2 {
			t.Fatalf("List all: total=%d rows=%d, want 2 (supplier row excluded)", total, len(all))
		}
		if all[0].ID != b.ID {
			t.Errorf("order: first = %s, want most recently updated (Beta Store)", all[0].Name)
		}
	})

	t.Run("duplicate_code_is_unique_violation", func(t *testing.T) {
		err := repo.Create(ctx, newCustomer(tenantID, "CUS-A", "Alpha Again", "", ""))
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "23505" {
			t.Fatalf("err = %v, want unique violation 23505", err)
		}
		// Same code in another tenant is fine.
		if err := repo.Create(ctx, newCustomer(otherTenant, "CUS-A", "Alpha Other", "", "")); err != nil {
			t.Errorf("same code in another tenant: %v", err)
		}
	})

	t.Run("duplicate_code_http_409", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		h := handlercust.New(appcust.NewCreateUseCase(repo), appcust.NewGetByIDUseCase(repo), appcust.NewListUseCase(repo))
		r := gin.New()
		r.Use(func(c *gin.Context) { c.Set(middleware.CtxKeyTenantID, tenantID); c.Next() })
		h.RegisterRoutes(r.Group("/api/v1"))

		post := func(body string) int {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", bytes.NewReader([]byte(body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			return w.Code
		}
		if code := post(`{"name":"Gamma","code":"CUS-G"}`); code != http.StatusCreated {
			t.Fatalf("first create: status %d, want 201", code)
		}
		if code := post(`{"name":"Gamma 2","code":"CUS-G"}`); code != http.StatusConflict {
			t.Fatalf("duplicate create: status %d, want 409", code)
		}
	})
}
