package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	handlerAuth "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/auth"
	appTenant "github.com/hanmahong5-arch/lurus-tally/internal/app/tenant"
)

// GET /me carries offline_queue_available: false by default (no Redis idempotency store),
// true once the store is wired; the original fields stay flat in the same object.
func TestAuthHandler_GetMe_OfflineQueueAvailableFlag(t *testing.T) {
	for _, want := range []bool{false, true} {
		stub := &stubGetMe{result: &appTenant.GetMeOutput{UserSub: "sub-abc", TenantID: "t-1"}}
		h := handlerAuth.New(&stubChooseProfile{}, stub)
		if want {
			h.WithOfflineQueueAvailable(true)
		}
		e := newTestEngine(h, "sub-abc", "a@x.com", "A")

		req, _ := http.NewRequest(http.MethodGet, "/api/v1/me", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if got, ok := body["offline_queue_available"].(bool); !ok || got != want {
			t.Errorf("offline_queue_available = %v, want %v (body %s)", body["offline_queue_available"], want, rec.Body.String())
		}
		if body["user_id"] != "sub-abc" || body["tenant_id"] != "t-1" {
			t.Errorf("flat /me fields lost: %s", rec.Body.String())
		}
	}
}
