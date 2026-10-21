package payment_test

import (
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Shop owners must never see an English sentence in a payment error.
func TestPaymentHandler_ErrorMessagesAreChinese(t *testing.T) {
	r := newPaymentRouter(buildPaymentHandler(newPHMockBillReader(), &phMockPaymentRepo{}))
	id := uuid.New().String()
	cases := []struct {
		name, method, path, body string
		noTenant                 bool
	}{
		{"record no tenant", "POST", "/api/v1/payments", `{}`, true},
		{"record bad json", "POST", "/api/v1/payments", `{`, false},
		{"record bad bill id", "POST", "/api/v1/payments", `{"bill_id":"x","amount":"1","payment_method":"cash"}`, false},
		{"record bad amount", "POST", "/api/v1/payments", `{"bill_id":"` + id + `","amount":"abc","payment_method":"cash"}`, false},
		{"record too big", "POST", "/api/v1/payments", `{"bill_id":"` + id + `","amount":"99999999999","payment_method":"cash"}`, false},
		{"record bad method", "POST", "/api/v1/payments", `{"bill_id":"` + id + `","amount":"1","payment_method":"wire"}`, false},
		{"record credit", "POST", "/api/v1/payments", `{"bill_id":"` + id + `","amount":"1","payment_method":"credit"}`, false},
		{"record not found", "POST", "/api/v1/payments", `{"bill_id":"` + id + `","amount":"1","payment_method":"cash"}`, false},
		{"list no tenant", "GET", "/api/v1/payments?bill_id=" + id, ``, true},
		{"list no bill", "GET", "/api/v1/payments", ``, false},
		{"list bad bill", "GET", "/api/v1/payments?bill_id=x", ``, false},
	}
	word := regexp.MustCompile(`[A-Za-z]{2,}`)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if !tc.noTenant {
				req.Header.Set("X-Tenant-ID", phTenantID.String())
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code < 400 || w.Code >= 500 {
				t.Fatalf("status = %d, want 4xx; body: %s", w.Code, w.Body.String())
			}
			var resp struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.Message == "" {
				t.Fatalf("no message: %s", w.Body.String())
			}
			if m := word.FindString(resp.Message); m != "" {
				t.Errorf("message %q contains English word %q", resp.Message, m)
			}
		})
	}
}
