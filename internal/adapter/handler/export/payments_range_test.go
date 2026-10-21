package export_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	handlerexport "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/export"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appexport "github.com/hanmahong5-arch/lurus-tally/internal/app/export"
)

// rangeSpy is a payments exporter that also understands a date range.
type rangeSpy struct {
	spyExporter
	from, to *time.Time
}

func (s *rangeSpy) ExecuteRange(_ context.Context, _ uuid.UUID, from, to *time.Time, w io.Writer) (int, error) {
	s.from, s.to = from, to
	_, err := io.WriteString(w, "h\n")
	return 0, err
}

func paymentsGet(t *testing.T, pay *rangeSpy, query string) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, uuid.New())
		c.Next()
	})
	handlerexport.New(&spyExporter{}, &spyExporter{}, pay, nil).RegisterRoutes(api)
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/exports/payments.csv"+query, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// The end day is included: date_to=2026-09-30 means pay_date < 2026-10-01 Beijing time.
func TestPayments_DateRangePassedThrough(t *testing.T) {
	pay := &rangeSpy{}
	w := paymentsGet(t, pay, "?date_from=2026-09-01&date_to=2026-09-30")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}
	if pay.from == nil || pay.to == nil || pay.from.Format("2006-01-02 15:04") != "2026-09-01 00:00" || pay.to.Format("2006-01-02") != "2026-10-01" {
		t.Errorf("range = %v .. %v", pay.from, pay.to)
	}
}

func TestPayments_BadDateIs400(t *testing.T) {
	w := paymentsGet(t, &rangeSpy{}, "?date_from=bad")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", w.Code)
	}
}

// filterSpy is a payments exporter that understands the full filter.
type filterSpy struct {
	spyExporter
	got appexport.PaymentsFilter
}

func (s *filterSpy) ExecuteFiltered(_ context.Context, _ uuid.UUID, f appexport.PaymentsFilter, w io.Writer) (int, error) {
	s.got = f
	_, err := io.WriteString(w, "h\n")
	return 0, err
}

func filteredGet(t *testing.T, pay *filterSpy, query string) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, uuid.New())
		c.Next()
	})
	handlerexport.New(&spyExporter{}, &spyExporter{}, pay, nil).RegisterRoutes(api)
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/exports/payments.csv"+query, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestPayments_DirectionAndCountedOnlyPassedThrough(t *testing.T) {
	pay := &filterSpy{}
	w := filteredGet(t, pay, "?date_from=2026-10-01&direction=out&counted_only=1")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}
	if pay.got.Direction != "out" || !pay.got.CountedOnly || pay.got.From == nil || pay.got.To != nil {
		t.Errorf("filter = %+v", pay.got)
	}
}

func TestPayments_BadDirectionIs400(t *testing.T) {
	w := filteredGet(t, &filterSpy{}, "?direction=sideways")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", w.Code)
	}
}
