package reports_test

// This file adds coverage for the reports handler's error/edge branches not
// exercised by handler_test.go: the 401 tenant gate on all four routes, the
// 500 repo-error branch on all four routes, ParseLimitQuery clamping
// (out-of-range inputs are clamped, not rejected), the default metric/day/
// limit values when query params are absent, and empty-repo 200 responses.
//
// It deliberately does not touch handler_test.go (may carry concurrent
// session WIP) and does not duplicate its helper names.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	handlerreports "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/reports"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appreports "github.com/hanmahong5-arch/lurus-tally/internal/app/reports"
)

// ---- fakes ------------------------------------------------------------

// zzErrRepo returns a fixed error from both Repo methods, driving the
// httperr.WriteInternal (500) branch on every route.
type zzErrRepo struct {
	err error
}

func (r *zzErrRepo) ListRecentSaleLines(_ context.Context, _ uuid.UUID, _ int) ([]appreports.SaleRow, error) {
	return nil, r.err
}

func (r *zzErrRepo) ListStockSnapshots(_ context.Context, _ uuid.UUID) ([]appreports.StockRow, error) {
	return nil, r.err
}

// zzTenantRepo records the tenantID it was invoked with (business invariant:
// the handler must pass the request-scoped tenant through to the UC) and can
// synthesize N distinct sale rows / return zero rows, for clamp and
// empty-data assertions.
type zzTenantRepo struct {
	saleTenant  uuid.UUID
	stockTenant uuid.UUID
	saleCalls   int
	stockCalls  int
	nSaleRows   int
	stocks      []appreports.StockRow
}

func (r *zzTenantRepo) ListRecentSaleLines(_ context.Context, tenantID uuid.UUID, _ int) ([]appreports.SaleRow, error) {
	r.saleTenant = tenantID
	r.saleCalls++
	rows := make([]appreports.SaleRow, 0, r.nSaleRows)
	for i := 0; i < r.nSaleRows; i++ {
		rows = append(rows, appreports.SaleRow{
			ProductID:   uuid.New(),
			ProductName: fmt.Sprintf("P%d", i),
		})
	}
	return rows, nil
}

func (r *zzTenantRepo) ListStockSnapshots(_ context.Context, tenantID uuid.UUID) ([]appreports.StockRow, error) {
	r.stockTenant = tenantID
	r.stockCalls++
	return r.stocks, nil
}

// ---- router builders ----------------------------------------------------

// zzRouterWithTenant injects tenant directly into the gin context key that
// middleware.GetTenantID reads (no header round-trip needed).
func zzRouterWithTenant(repo appreports.Repo, tenant uuid.UUID) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, tenant)
		c.Next()
	})
	uc := appreports.New(repo)
	h := handlerreports.New(uc)
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)
	return r
}

// zzRouterNoTenant builds a router with no tenant-injecting middleware at
// all, so middleware.GetTenantID falls back to uuid.Nil and every route must
// 401.
func zzRouterNoTenant(repo appreports.Repo) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	uc := appreports.New(repo)
	h := handlerreports.New(uc)
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)
	return r
}

var zzAllRoutes = []string{
	"/api/v1/reports/gross-margin",
	"/api/v1/reports/abc",
	"/api/v1/reports/dead-stock",
	"/api/v1/reports/sales-top",
}

// ---- 401 tenant gate: business invariant, all four routes ---------------

func TestZZ_AllRoutes_NilTenant_Returns401(t *testing.T) {
	for _, path := range zzAllRoutes {
		t.Run(path, func(t *testing.T) {
			r := zzRouterNoTenant(&zzTenantRepo{})
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401; body=%s", w.Code, w.Body.String())
			}
		})
	}
}

// ---- 500 on repo error: all four routes ----------------------------------

func TestZZ_AllRoutes_RepoError_Returns500(t *testing.T) {
	wantErr := errors.New("boom: db unreachable")
	for _, path := range zzAllRoutes {
		t.Run(path, func(t *testing.T) {
			r := zzRouterWithTenant(&zzErrRepo{err: wantErr}, uuid.New())
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500; body=%s", w.Code, w.Body.String())
			}
			// httperr never leaks the underlying cause text into the body.
			if got := w.Body.String(); got == "" {
				t.Errorf("expected a non-empty safe error body")
			}
		})
	}
}

// ---- SalesTop metric validation -------------------------------------------

func TestZZ_SalesTop_MetricValidation(t *testing.T) {
	cases := []struct {
		name       string
		query      string
		wantStatus int
		wantMetric string // only checked on 200
	}{
		{"revenue explicit", "?metric=revenue", http.StatusOK, "revenue"},
		{"margin explicit", "?metric=margin", http.StatusOK, "margin"},
		{"qty explicit", "?metric=qty", http.StatusOK, "qty"},
		{"absent defaults to revenue", "", http.StatusOK, "revenue"},
		{"invalid metric rejected", "?metric=bogus", http.StatusBadRequest, ""},
		{"empty metric value rejected", "?metric=", http.StatusBadRequest, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &zzTenantRepo{nSaleRows: 2}
			r := zzRouterWithTenant(repo, uuid.New())
			req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/sales-top"+tc.query, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantStatus != http.StatusOK {
				return
			}
			var resp appreports.SalesTopResult
			mustUnmarshalZZ(t, w.Body.Bytes(), &resp)
			if resp.Metric != tc.wantMetric {
				t.Errorf("Metric = %q, want %q", resp.Metric, tc.wantMetric)
			}
		})
	}
}

// ---- ParseLimitQuery clamping: days on gross-margin/dead-stock/sales-top --

func TestZZ_DaysClamping(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		query    string
		wantDays int // hand-computed per middleware.ParseLimitQuery semantics
	}{
		// gross-margin: def=30, max=365
		{"gross-margin absent -> default 30", "/api/v1/reports/gross-margin", "", 30},
		{"gross-margin over max clamps to 365", "/api/v1/reports/gross-margin", "?days=999999", 365},
		{"gross-margin zero -> default 30", "/api/v1/reports/gross-margin", "?days=0", 30},
		{"gross-margin negative -> default 30", "/api/v1/reports/gross-margin", "?days=-5", 30},
		{"gross-margin non-numeric -> default 30", "/api/v1/reports/gross-margin", "?days=abc", 30},
		{"gross-margin within range unchanged", "/api/v1/reports/gross-margin", "?days=45", 45},

		// dead-stock: def=90, max=365
		{"dead-stock absent -> default 90", "/api/v1/reports/dead-stock", "", 90},
		{"dead-stock over max clamps to 365", "/api/v1/reports/dead-stock", "?days=99999", 365},
		{"dead-stock within range unchanged", "/api/v1/reports/dead-stock", "?days=120", 120},

		// sales-top: def=7, max=365
		{"sales-top absent -> default 7", "/api/v1/reports/sales-top", "", 7},
		{"sales-top over max clamps to 365", "/api/v1/reports/sales-top", "?days=1000", 365},
		{"sales-top within range unchanged", "/api/v1/reports/sales-top", "?days=14", 14},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &zzTenantRepo{nSaleRows: 1}
			r := zzRouterWithTenant(repo, uuid.New())
			req := httptest.NewRequest(http.MethodGet, tc.path+tc.query, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
			}

			var gotDays int
			switch tc.path {
			case "/api/v1/reports/gross-margin":
				var resp appreports.GrossMarginResult
				mustUnmarshalZZ(t, w.Body.Bytes(), &resp)
				gotDays = resp.Days
			case "/api/v1/reports/dead-stock":
				var resp appreports.DeadStockResult
				mustUnmarshalZZ(t, w.Body.Bytes(), &resp)
				gotDays = resp.ThresholdDays
			case "/api/v1/reports/sales-top":
				var resp appreports.SalesTopResult
				mustUnmarshalZZ(t, w.Body.Bytes(), &resp)
				gotDays = resp.Days
			}
			if gotDays != tc.wantDays {
				t.Errorf("days = %d, want %d (clamped, not rejected)", gotDays, tc.wantDays)
			}
		})
	}
}

// ---- ParseLimitQuery clamping: limit on sales-top --------------------------

func TestZZ_SalesTop_LimitClamping(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		nSaleRows int
		wantLen   int // hand-computed: min(nSaleRows distinct products, clamped limit)
	}{
		// def=10, max=100
		{"absent -> default 10, capped by default", "", 15, 10},
		{"over max clamps to 100 not rejected", "?limit=999999", 150, 100},
		{"zero -> default 10", "?limit=0", 15, 10},
		{"negative -> default 10", "?limit=-3", 15, 10},
		{"within range respected", "?limit=5", 15, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &zzTenantRepo{nSaleRows: tc.nSaleRows}
			r := zzRouterWithTenant(repo, uuid.New())
			req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/sales-top"+tc.query, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
			}
			var resp appreports.SalesTopResult
			mustUnmarshalZZ(t, w.Body.Bytes(), &resp)
			if len(resp.TopProducts) != tc.wantLen {
				t.Errorf("len(TopProducts) = %d, want %d (clamp semantics)", len(resp.TopProducts), tc.wantLen)
			}
		})
	}
}

// ---- Tenant-scoping business invariant ------------------------------------

func TestZZ_TenantIsPropagatedToRepo(t *testing.T) {
	tenant := uuid.New()

	t.Run("gross-margin", func(t *testing.T) {
		repo := &zzTenantRepo{}
		r := zzRouterWithTenant(repo, tenant)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/gross-margin", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
		}
		if repo.saleTenant != tenant {
			t.Errorf("saleTenant = %s, want %s (tenant must be scoped)", repo.saleTenant, tenant)
		}
	})

	t.Run("abc", func(t *testing.T) {
		repo := &zzTenantRepo{}
		r := zzRouterWithTenant(repo, tenant)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/abc", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
		}
		if repo.saleTenant != tenant {
			t.Errorf("saleTenant = %s, want %s", repo.saleTenant, tenant)
		}
	})

	t.Run("dead-stock", func(t *testing.T) {
		repo := &zzTenantRepo{}
		r := zzRouterWithTenant(repo, tenant)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/dead-stock", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
		}
		if repo.stockTenant != tenant {
			t.Errorf("stockTenant = %s, want %s (tenant must be scoped)", repo.stockTenant, tenant)
		}
	})

	t.Run("sales-top", func(t *testing.T) {
		repo := &zzTenantRepo{}
		r := zzRouterWithTenant(repo, tenant)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/sales-top", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
		}
		if repo.saleTenant != tenant {
			t.Errorf("saleTenant = %s, want %s", repo.saleTenant, tenant)
		}
	})
}

// ---- empty-repo 200s: no nil-panic, well-formed shape ---------------------

func TestZZ_EmptyRepo_StillWellFormed200(t *testing.T) {
	tenant := uuid.New()

	t.Run("gross-margin", func(t *testing.T) {
		repo := &zzTenantRepo{nSaleRows: 0}
		r := zzRouterWithTenant(repo, tenant)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/gross-margin", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
		}
		var resp appreports.GrossMarginResult
		mustUnmarshalZZ(t, w.Body.Bytes(), &resp)
		if resp.OverallMargin != "0.0%" {
			t.Errorf("OverallMargin = %q, want %q for zero rows", resp.OverallMargin, "0.0%")
		}
		if len(resp.Top10) != 0 || len(resp.Bottom10) != 0 {
			t.Errorf("expected empty top10/bottom10, got %d/%d", len(resp.Top10), len(resp.Bottom10))
		}
	})

	t.Run("abc", func(t *testing.T) {
		repo := &zzTenantRepo{nSaleRows: 0}
		r := zzRouterWithTenant(repo, tenant)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/abc", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
		}
		var resp appreports.ABCResult
		mustUnmarshalZZ(t, w.Body.Bytes(), &resp)
		if resp.TotalSKUs != 0 {
			t.Errorf("TotalSKUs = %d, want 0", resp.TotalSKUs)
		}
	})

	t.Run("dead-stock", func(t *testing.T) {
		repo := &zzTenantRepo{stocks: nil}
		r := zzRouterWithTenant(repo, tenant)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/dead-stock", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
		}
		var resp appreports.DeadStockResult
		mustUnmarshalZZ(t, w.Body.Bytes(), &resp)
		if resp.Count != 0 {
			t.Errorf("Count = %d, want 0", resp.Count)
		}
		if resp.Items == nil {
			t.Errorf("Items should be an empty slice, not nil (well-formed JSON array)")
		}
	})

	t.Run("sales-top", func(t *testing.T) {
		repo := &zzTenantRepo{nSaleRows: 0}
		r := zzRouterWithTenant(repo, tenant)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/sales-top", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
		}
		var resp appreports.SalesTopResult
		mustUnmarshalZZ(t, w.Body.Bytes(), &resp)
		if len(resp.TopProducts) != 0 {
			t.Errorf("TopProducts len = %d, want 0", len(resp.TopProducts))
		}
	})
}

// ---- json helper -----------------------------------------------------

func mustUnmarshalZZ(t *testing.T, data []byte, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, string(data))
	}
}
