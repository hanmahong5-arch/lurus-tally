package account_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	handleracct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/account"
	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/account"
)

func init() { gin.SetMode(gin.TestMode) }

// errSessionRepo fails every call with a planted error whose text mimics leaked
// infrastructure detail.
type errSessionRepo struct{ err error }

func (e errSessionRepo) Upsert(context.Context, *domain.Session) error { return e.err }
func (e errSessionRepo) List(context.Context, uuid.UUID, string) ([]*domain.Session, error) {
	return nil, e.err
}
func (e errSessionRepo) Revoke(context.Context, uuid.UUID, uuid.UUID) error { return e.err }
func (e errSessionRepo) Touch(context.Context, uuid.UUID, string, string) error {
	return e.err
}

// TestListSessions_InternalError_DoesNotLeak is the sampling proof that the
// httperr wiring holds end-to-end at a real handler: a repo failure surfaces as
// a 500 whose body carries the canonical code but NONE of the underlying error
// text (which would otherwise expose DSNs / driver detail).
func TestListSessions_InternalError_DoesNotLeak(t *testing.T) {
	const secret = `pq: connection refused dbname=tally host=10.0.0.5 sslmode=disable DSN-LEAK`
	h := handleracct.New(
		appacct.NewListSessions(errSessionRepo{err: errors.New(secret)}),
		nil, nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/account/sessions", nil)
	c.Set("tenant_id", uuid.New())
	c.Set("zitadel_sub", "user-123")

	h.ListSessions(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
	body := w.Body.String()
	for _, marker := range []string{"DSN-LEAK", "host=10.0.0.5", "sslmode", "pq:", "connection refused"} {
		if strings.Contains(body, marker) {
			t.Errorf("5xx body leaked %q: %s", marker, body)
		}
	}
	if !strings.Contains(body, "internal_error") {
		t.Errorf("body missing canonical code: %s", body)
	}
}
