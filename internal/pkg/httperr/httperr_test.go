package httperr_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

func init() { gin.SetMode(gin.TestMode) }

// render runs Write(err) inside a throwaway gin context and returns the
// recorder so tests can assert on status + body.
func render(err error) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
	httperr.Write(c, err)
	return w
}

func TestWrite_EnvelopeMapping(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantAction bool
	}{
		{"internal", httperr.Internal(errors.New("boom")), http.StatusInternalServerError, "internal_error", true},
		{"unavailable", httperr.Unavailable(errors.New("boom")), http.StatusServiceUnavailable, "service_unavailable", true},
		{"bad_gateway", httperr.BadGateway(errors.New("boom")), http.StatusBadGateway, "bad_gateway", true},
		{"bad_request", httperr.BadRequest("invalid_qty", "qty must be positive", "send a positive qty"), http.StatusBadRequest, "invalid_qty", true},
		{"not_found", httperr.NotFound("bill_not_found", "bill not found"), http.StatusNotFound, "bill_not_found", false},
		{"unauthorized", httperr.Unauthorized("unauthorized", "missing token"), http.StatusUnauthorized, "unauthorized", true},
		{"unclassified->500", errors.New("random error"), http.StatusInternalServerError, "internal_error", true},
		{"wrapped *Error", errors.New("ctx: " + httperr.NotFound("x", "y").Error()), http.StatusInternalServerError, "internal_error", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := render(tc.err)
			if w.Code != tc.wantStatus {
				t.Fatalf("status: got %d, want %d", w.Code, tc.wantStatus)
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("body not JSON: %v (%s)", err, w.Body.String())
			}
			if body["error"] != tc.wantCode {
				t.Errorf("error code: got %v, want %q", body["error"], tc.wantCode)
			}
			if _, ok := body["message"]; !ok {
				t.Error("message field missing")
			}
			if _, ok := body["action"]; ok != tc.wantAction {
				t.Errorf("action present=%v, want %v", ok, tc.wantAction)
			}
		})
	}
}

// TestWrite_5xx_DoesNotLeakInternalError is the security contract: the real
// cause carries markers that look like leaked infrastructure detail, and the
// response body must contain NONE of them.
func TestWrite_5xx_DoesNotLeakInternalError(t *testing.T) {
	secret := `pq: password authentication failed for user "tally" host=10.0.0.5 dbname=tally sslmode=disable DSN-LEAK`
	cause := errors.New(secret)

	for _, err := range []error{
		httperr.Internal(cause),
		httperr.Unavailable(cause),
		httperr.BadGateway(cause),
		httperr.Wrap(http.StatusServiceUnavailable, "billing_unavailable", "billing is unavailable", "retry", cause),
		cause, // unclassified -> 500
	} {
		w := render(err)
		body := w.Body.String()
		for _, marker := range []string{"DSN-LEAK", "password authentication", "host=10.0.0.5", "sslmode", "pq:"} {
			if strings.Contains(body, marker) {
				t.Errorf("5xx body leaked %q from the internal error: %s", marker, body)
			}
		}
		if w.Code < 500 {
			t.Errorf("expected 5xx, got %d", w.Code)
		}
	}
}

// TestWrite_4xx_EchoesValidationDetail is the dual contract: a 4xx SHOULD echo
// the client-correctable validation message (over-sanitising hurts usability).
func TestWrite_4xx_EchoesValidationDetail(t *testing.T) {
	w := render(httperr.BadRequest("invalid_sku", "sku 'ABC-∅' contains an illegal character", "use alphanumeric SKUs"))
	body := w.Body.String()
	if !strings.Contains(body, "illegal character") {
		t.Errorf("4xx body should echo validation detail, got: %s", body)
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
}

// TestWriteInternal_ClassifiesConstraintViolations proves a PG constraint
// violation passed to WriteInternal is mapped to a 4xx (not a bare 500), while
// the driver's table/column/constraint detail is NEVER echoed to the client.
func TestWriteInternal_ClassifiesConstraintViolations(t *testing.T) {
	leak := []string{"bill_head_partner_id_fkey", "tally.partner", "Key (partner_id)", "SQLSTATE"}

	cases := []struct {
		name       string
		pgCode     string
		wantStatus int
		wantCode   string
	}{
		{"foreign_key→409", "23503", http.StatusConflict, "invalid_reference"},
		{"unique→409", "23505", http.StatusConflict, "duplicate"},
		{"not_null→500", "23502", http.StatusInternalServerError, "internal_error"},
		{"check→500", "23514", http.StatusInternalServerError, "internal_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Wrap a realistic PgError the way a repo would (fmt.Errorf %w).
			pgErr := &pgconn.PgError{
				Code:           tc.pgCode,
				Message:        `insert or update on table "bill_head" violates foreign key constraint "bill_head_partner_id_fkey"`,
				Detail:         `Key (partner_id)=(00000000-0000-0000-0000-000000000000) is not present in table "tally.partner".`,
				ConstraintName: "bill_head_partner_id_fkey",
			}
			wrapped := fmt.Errorf("bill repo: create bill: %w", pgErr)

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/purchase-bills", nil)
			httperr.WriteInternal(c, wrapped)

			if w.Code != tc.wantStatus {
				t.Fatalf("status: got %d, want %d (body=%s)", w.Code, tc.wantStatus, w.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("body not JSON: %v", err)
			}
			if body["error"] != tc.wantCode {
				t.Errorf("code: got %v, want %q", body["error"], tc.wantCode)
			}
			// Security: never leak driver internals regardless of status.
			for _, m := range leak {
				if strings.Contains(w.Body.String(), m) {
					t.Errorf("body leaked driver detail %q: %s", m, w.Body.String())
				}
			}
		})
	}
}

// TestAsError_PreservesTypedError proves a typed *Error round-trips unchanged.
func TestAsError_PreservesTypedError(t *testing.T) {
	orig := httperr.NotFound("bill_not_found", "bill not found")
	got := httperr.AsError(orig)
	if got != orig {
		t.Fatalf("AsError mutated a typed error: got %+v", got)
	}
	if got := httperr.AsError(errors.New("x")); got.Status != http.StatusInternalServerError {
		t.Errorf("unclassified should be 500, got %d", got.Status)
	}
}
