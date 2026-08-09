package httperr_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// ---- Error() / Unwrap() ----------------------------------------------------

// TestError_MessageOnly_WhenNoInternalCause covers the Error() branch where
// Internal is nil: the one-liner falls back to the safe Message, not "%v" on
// a nil cause.
func TestError_MessageOnly_WhenNoInternalCause(t *testing.T) {
	e := httperr.New(http.StatusNotFound, "bill_not_found", "bill not found", "")
	got := e.Error()
	want := fmt.Sprintf("httperr: %d %s: %s", http.StatusNotFound, "bill_not_found", "bill not found")
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if e.Unwrap() != nil {
		t.Errorf("Unwrap() should be nil when Internal is nil, got %v", e.Unwrap())
	}
}

// TestError_IncludesCause_WhenInternalSet covers the Internal != nil branch of
// Error(), and proves Unwrap()/errors.Is/errors.As can see through to a
// sentinel wrapped by Wrap.
func TestError_IncludesCause_WhenInternalSet(t *testing.T) {
	sentinel := errors.New("sentinel: connection refused")
	e := httperr.Wrap(http.StatusServiceUnavailable, "service_unavailable", "a downstream service is temporarily unavailable", "retry shortly", sentinel)

	got := e.Error()
	if !strings.Contains(got, sentinel.Error()) {
		t.Errorf("Error() = %q, want it to contain cause %q (log-only path)", got, sentinel.Error())
	}
	if e.Unwrap() != sentinel {
		t.Errorf("Unwrap() = %v, want %v", e.Unwrap(), sentinel)
	}
	if !errors.Is(e, sentinel) {
		t.Error("errors.Is(e, sentinel) = false, want true (Unwrap must let it through)")
	}

	var target *httperr.Error
	if !errors.As(e, &target) {
		t.Fatal("errors.As(e, &target) = false, want true")
	}
	if target != e {
		t.Errorf("errors.As target = %+v, want the same *Error instance", target)
	}
}

// ---- Forbidden / Conflict constructors (0% and partial before this file) ---

func TestConstructors_TableDriven(t *testing.T) {
	cases := []struct {
		name        string
		err         *httperr.Error
		wantStatus  int
		wantCode    string
		wantMessage string
		wantAction  string
	}{
		{
			name:        "Forbidden",
			err:         httperr.Forbidden("forbidden", "you may not perform this action"),
			wantStatus:  http.StatusForbidden,
			wantCode:    "forbidden",
			wantMessage: "you may not perform this action",
			wantAction:  "",
		},
		{
			name:        "Conflict",
			err:         httperr.Conflict("stale_version", "the record was modified concurrently"),
			wantStatus:  http.StatusConflict,
			wantCode:    "stale_version",
			wantMessage: "the record was modified concurrently",
			wantAction:  "",
		},
		{
			name:        "NotFound",
			err:         httperr.NotFound("bill_not_found", "bill not found"),
			wantStatus:  http.StatusNotFound,
			wantCode:    "bill_not_found",
			wantMessage: "bill not found",
			wantAction:  "",
		},
		{
			name:        "Unauthorized",
			err:         httperr.Unauthorized("unauthorized", "missing token"),
			wantStatus:  http.StatusUnauthorized,
			wantCode:    "unauthorized",
			wantMessage: "missing token",
			wantAction:  "sign in and retry",
		},
		{
			name:        "BadRequest",
			err:         httperr.BadRequest("invalid_qty", "qty must be positive", "send a positive qty"),
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_qty",
			wantMessage: "qty must be positive",
			wantAction:  "send a positive qty",
		},
		{
			name:        "New_generic",
			err:         httperr.New(http.StatusTeapot, "im_a_teapot", "short and stout", "tip me over"),
			wantStatus:  http.StatusTeapot,
			wantCode:    "im_a_teapot",
			wantMessage: "short and stout",
			wantAction:  "tip me over",
		},
		{
			name:        "Internal",
			err:         httperr.Internal(errors.New("db down")),
			wantStatus:  http.StatusInternalServerError,
			wantCode:    "internal_error",
			wantMessage: "an internal error occurred",
			wantAction:  "retry shortly; contact support if it keeps happening",
		},
		{
			name:        "Unavailable",
			err:         httperr.Unavailable(errors.New("dep down")),
			wantStatus:  http.StatusServiceUnavailable,
			wantCode:    "service_unavailable",
			wantMessage: "a downstream service is temporarily unavailable",
			wantAction:  "retry shortly",
		},
		{
			name:        "BadGateway",
			err:         httperr.BadGateway(errors.New("upstream garbage")),
			wantStatus:  http.StatusBadGateway,
			wantCode:    "bad_gateway",
			wantMessage: "a downstream service returned an invalid response",
			wantAction:  "retry shortly",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Status != tc.wantStatus {
				t.Errorf("Status = %d, want %d", tc.err.Status, tc.wantStatus)
			}
			if tc.err.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", tc.err.Code, tc.wantCode)
			}
			if tc.err.Message != tc.wantMessage {
				t.Errorf("Message = %q, want %q", tc.err.Message, tc.wantMessage)
			}
			if tc.err.Action != tc.wantAction {
				t.Errorf("Action = %q, want %q", tc.err.Action, tc.wantAction)
			}
		})
	}
}

// ---- Write: request_id propagation + cause-fallback branch ----------------

// TestWrite_LogsRequestIDAndMethodPath drives the 5xx branch of Write with a
// request_id set on the gin context (as middleware would), and with a method
// + registered route so c.FullPath()/c.Request.Method are non-empty. We can't
// intercept slog output without touching source, so this asserts the
// observable contract instead: status/body are correct and unaffected by the
// logging side effect, exercising the slog.Error call path for coverage.
func TestWrite_LogsRequestIDAndMethodPath(t *testing.T) {
	w := httptest.NewRecorder()
	c, engine := gin.CreateTestContext(w)
	engine.GET("/api/v1/bills/:id", func(c *gin.Context) {
		httperr.Write(c, httperr.Internal(errors.New("secret sql failure dsn=postgres://user:pass@host/db")))
	})
	c.Set("request_id", "req-123")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/bills/42", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "secret sql failure") {
		t.Errorf("5xx body leaked cause: %s", w.Body.String())
	}
}

// TestWrite_CauseFallback_WhenInternalNil covers the `cause==nil -> cause=err`
// branch inside Write: a *Error with Status>=500 but Internal==nil (built via
// New, not Internal/Wrap) must still log something (the error itself) instead
// of a nil, and the body must remain the safe static message.
func TestWrite_CauseFallback_WhenInternalNil(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/x", nil)

	bare := httperr.New(http.StatusInternalServerError, "internal_error", "an internal error occurred", "")
	if bare.Internal != nil {
		t.Fatal("test setup: expected Internal to be nil for this branch")
	}
	httperr.Write(c, bare)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
	if !strings.Contains(w.Body.String(), "an internal error occurred") {
		t.Errorf("body = %s, want the safe static message", w.Body.String())
	}
}

// ---- WriteUnavailable (0% before this file) --------------------------------

func TestWriteUnavailable(t *testing.T) {
	secret := errors.New("dial tcp 10.0.0.9:5432: connection refused")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
	httperr.WriteUnavailable(c, secret)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "service_unavailable") {
		t.Errorf("body missing service_unavailable code: %s", body)
	}
	if strings.Contains(body, "10.0.0.9") || strings.Contains(body, "connection refused") {
		t.Errorf("503 body leaked cause: %s", body)
	}
}

// ---- WriteInternal: 23503/23505 -> 409 vs 23502/23514/unknown -> 500 ------
// (classifyDBError itself was already fully hit; this exercises WriteInternal
// end-to-end plus a non-pg error and a bare (unwrapped) PgError, matching the
// business invariant that only FK/unique are ever reclassified.)

func TestWriteInternal_NonDBError_FallsBackToInternal500(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/x", nil)

	httperr.WriteInternal(c, errors.New("plain go error, not a pg constraint"))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "plain go error") {
		t.Errorf("500 body leaked cause: %s", w.Body.String())
	}
}

func TestWriteInternal_BarePgError_NotWrapped(t *testing.T) {
	// classifyDBError uses errors.As, which also matches an un-wrapped
	// *pgconn.PgError passed directly (not just fmt.Errorf %w-wrapped).
	pgErr := &pgconn.PgError{Code: "23505", ConstraintName: "sku_unique", Detail: "Key (sku)=(ABC) already exists."}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/x", nil)
	httperr.WriteInternal(c, pgErr)

	if w.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409", w.Code)
	}
	if strings.Contains(w.Body.String(), "sku_unique") || strings.Contains(w.Body.String(), "Key (sku)") {
		t.Errorf("409 body leaked driver constraint detail: %s", w.Body.String())
	}
}

// ---- AsError: classifyDBError branch reached directly (not via *Error) ----

func TestAsError_ClassifiesRawPgError(t *testing.T) {
	cases := []struct {
		name       string
		code       string
		wantStatus int
		wantCode   string
	}{
		{"fk_violation", "23503", http.StatusConflict, "invalid_reference"},
		{"unique_violation", "23505", http.StatusConflict, "duplicate"},
		{"not_null_stays_500", "23502", http.StatusInternalServerError, "internal_error"},
		{"check_stays_500", "23514", http.StatusInternalServerError, "internal_error"},
		{"unknown_pg_code_stays_500", "99999", http.StatusInternalServerError, "internal_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pgErr := &pgconn.PgError{Code: tc.code}
			wrapped := fmt.Errorf("repo: %w", pgErr)

			got := httperr.AsError(wrapped)
			if got.Status != tc.wantStatus {
				t.Errorf("Status = %d, want %d", got.Status, tc.wantStatus)
			}
			if got.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", got.Code, tc.wantCode)
			}
		})
	}
}

// TestAsError_NonPgNonTypedError_Is500 covers the final fallback: an error
// that is neither *Error nor a classifiable pg error becomes a generic,
// safe 500.
func TestAsError_NonPgNonTypedError_Is500(t *testing.T) {
	got := httperr.AsError(errors.New("some random failure with secret token=abc123"))
	if got.Status != http.StatusInternalServerError {
		t.Errorf("Status = %d, want 500", got.Status)
	}
	if got.Code != "internal_error" {
		t.Errorf("Code = %q, want internal_error", got.Code)
	}
	if strings.Contains(got.Message, "secret token") {
		t.Errorf("Message must be the safe static string, got %q", got.Message)
	}
}

// TestAsError_WrappedTypedError proves a *Error hidden a level deep behind a
// plain wrap (fmt.Errorf %w) still round-trips via errors.As, unchanged.
func TestAsError_WrappedTypedError(t *testing.T) {
	orig := httperr.Conflict("duplicate", "a record with these values already exists")
	wrapped := fmt.Errorf("service: %w", orig)

	got := httperr.AsError(wrapped)
	if got != orig {
		t.Errorf("AsError did not unwrap to the original *Error instance: got %+v", got)
	}
}

// ---- concurrency: constructors + Write must be race-safe under parallel use

func TestWrite_ConcurrentSafe(t *testing.T) {
	var wg sync.WaitGroup
	errs := []error{
		httperr.Internal(errors.New("a")),
		httperr.NotFound("x", "y"),
		fmt.Errorf("w: %w", &pgconn.PgError{Code: "23503"}),
		errors.New("plain"),
	}
	for i := 0; i < 50; i++ {
		wg.Add(1)
		e := errs[i%len(errs)]
		go func(err error) {
			defer wg.Done()
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
			httperr.Write(c, err)
		}(e)
	}
	wg.Wait()
}
