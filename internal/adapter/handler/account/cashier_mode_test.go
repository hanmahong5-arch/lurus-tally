package account_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	handleracct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/account"
	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
)

type memCashierRepo struct {
	rec     appacct.CashierRecord
	profile string
}

func (m *memCashierRepo) ProfileType(context.Context, uuid.UUID) (string, error) {
	if m.profile == "" {
		return "retail", nil
	}
	return m.profile, nil
}
func (m *memCashierRepo) PutCashierFirst(_ context.Context, _ uuid.UUID, r appacct.CashierRecord) error {
	if m.rec.PinHash != "" {
		return appacct.ErrPinAlreadySet
	}
	m.rec = r
	return nil
}

func (m *memCashierRepo) GetCashier(context.Context, uuid.UUID) (appacct.CashierRecord, error) {
	return m.rec, nil
}
func (m *memCashierRepo) PutCashier(_ context.Context, _ uuid.UUID, r appacct.CashierRecord) error {
	m.rec = r
	return nil
}

func cashierRouter(repo *memCashierRepo, tenant uuid.UUID) *gin.Engine {
	h := handleracct.New(nil, nil, nil, nil, nil, nil, nil).
		WithCashierMode(appacct.NewGetCashierMode(repo), appacct.NewSetCashierMode(repo))
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("tenant_id", tenant); c.Next() })
	h.RegisterRoutes(r.Group("/api/v1"))
	return r
}

func cashierReq(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func cashierPut(r *gin.Engine, body string) *httptest.ResponseRecorder {
	return cashierReq(r, http.MethodPut, "/api/v1/tenant/cashier-mode", body)
}

func cashierVerify(r *gin.Engine, body string) *httptest.ResponseRecorder {
	return cashierReq(r, http.MethodPost, "/api/v1/tenant/cashier-mode/verify", body)
}

func TestCashierMode_SetPinThenGetOnlyHasPin(t *testing.T) {
	tid := uuid.New()
	r := cashierRouter(&memCashierRepo{}, tid)
	if w := cashierPut(r, `{"enabled":true,"pin":"1234"}`); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"has_pin":true`) {
		t.Fatalf("put: %d %s", w.Code, w.Body.String())
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/tenant/cashier-mode", nil))
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, `"has_pin":true`) || !strings.Contains(body, tid.String()) {
		t.Fatalf("get: %d %s", w.Code, body)
	}
	if strings.Contains(body, "enabled") || strings.Contains(body, "pin_hash") || strings.Contains(body, "salt") {
		t.Fatalf("must only expose has_pin and tenant_id: %s", body)
	}
}

func TestCashierMode_LegacyEnabledFlagIsIgnored(t *testing.T) {
	repo := &memCashierRepo{rec: appacct.CashierRecord{Enabled: true, PinHash: "x", Salt: "00"}}
	r := cashierRouter(repo, uuid.New())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/tenant/cashier-mode", nil))
	if strings.Contains(w.Body.String(), "enabled") {
		t.Fatalf("legacy flag leaked: %s", w.Body.String())
	}
}

func TestCashierMode_VerifyRightPinIs200(t *testing.T) {
	r := cashierRouter(&memCashierRepo{}, uuid.New())
	cashierPut(r, `{"pin":"1234"}`)
	if w := cashierVerify(r, `{"pin":"1234"}`); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}

func TestCashierMode_VerifyWrongPinIs403Chinese(t *testing.T) {
	r := cashierRouter(&memCashierRepo{}, uuid.New())
	cashierPut(r, `{"pin":"1234"}`)
	w := cashierVerify(r, `{"pin":"0000"}`)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "密码不对") {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}

func TestCashierMode_VerifyLockedIs429(t *testing.T) {
	r := cashierRouter(&memCashierRepo{}, uuid.New())
	cashierPut(r, `{"pin":"1234"}`)
	for i := 0; i < 5; i++ {
		cashierVerify(r, `{"pin":"0000"}`)
	}
	if w := cashierVerify(r, `{"pin":"1234"}`); w.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}

func TestCashierMode_VerifyWithoutPinSetIs409(t *testing.T) {
	r := cashierRouter(&memCashierRepo{}, uuid.New())
	w := cashierVerify(r, `{"pin":"1234"}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "pin_not_set") {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}

func TestCashierMode_BadPinFormatIs400(t *testing.T) {
	r := cashierRouter(&memCashierRepo{}, uuid.New())
	w := cashierPut(r, `{"pin":"ab"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "4 到 6 位数字") {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	if w := cashierVerify(r, `{"pin":"ab"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("verify format: %d", w.Code)
	}
	if w := cashierPut(r, `not json`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad json: %d", w.Code)
	}
	if w := cashierVerify(r, `not json`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad json: %d", w.Code)
	}
}

func TestCashierMode_NoTenantIs401(t *testing.T) {
	r := cashierRouter(&memCashierRepo{}, uuid.Nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/tenant/cashier-mode", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
	if w := cashierVerify(r, `{"pin":"1234"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("verify: want 401, got %d", w.Code)
	}
}

func TestCashierMode_NonRetailSetPinIs409Chinese(t *testing.T) {
	r := cashierRouter(&memCashierRepo{profile: "horticulture"}, uuid.New())
	w := cashierPut(r, `{"pin":"1234"}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "零售") {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}

func TestCashierMode_PinAlreadySetIs409Chinese(t *testing.T) {
	repo := &memCashierRepo{}
	h := handleracct.New(nil, nil, nil, nil, nil, nil, nil).
		WithCashierMode(appacct.NewGetCashierMode(repo), appacct.NewSetCashierMode(racyRepo{repo}))
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("tenant_id", uuid.New()); c.Next() })
	h.RegisterRoutes(r.Group("/api/v1"))
	w := cashierPut(r, `{"pin":"1111"}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "已被设置") {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}

// racyRepo reads an empty record, then reports another device won the first write.
type racyRepo struct{ *memCashierRepo }

func (racyRepo) PutCashierFirst(context.Context, uuid.UUID, appacct.CashierRecord) error {
	return appacct.ErrPinAlreadySet
}

// Sixty guesses fired at once must not get more than the five allowed tries.
func TestCashierMode_ConcurrentWrongPinsAreCappedAtFive(t *testing.T) {
	r := cashierRouter(&memCashierRepo{}, uuid.New())
	cashierPut(r, `{"pin":"1234"}`)
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := cashierVerify(r, `{"pin":"0000"}`)
			mu.Lock()
			codes[w.Code]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	if codes[http.StatusForbidden] != 5 || codes[http.StatusTooManyRequests] != 55 {
		t.Fatalf("status counts = %v, want 5x403 and 55x429", codes)
	}
}

// A personal access token carries no subject: failures are counted per token,
// so one token's typos cannot lock another and a token cannot dodge the limit.
func TestCashierMode_PatVerifyCountsPerToken(t *testing.T) {
	r := cashierRouter(&memCashierRepo{}, uuid.New())
	cashierPut(r, `{"pin":"1234"}`)
	tokA := "tally_pat_" + "AAAAAAAA" + strings.Repeat("a", 32)
	tokB := "tally_pat_" + "BBBBBBBB" + strings.Repeat("b", 32)
	verify := func(tok, pin string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenant/cashier-mode/verify", strings.NewReader(`{"pin":"`+pin+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)
		r.ServeHTTP(w, req)
		return w.Code
	}
	for i := 0; i < 5; i++ {
		if got := verify(tokA, "0000"); got != http.StatusForbidden {
			t.Fatalf("token A try %d = %d, want 403", i, got)
		}
	}
	if got := verify(tokA, "1234"); got != http.StatusTooManyRequests {
		t.Fatalf("token A locked = %d, want 429", got)
	}
	if got := verify(tokB, "0000"); got != http.StatusForbidden {
		t.Fatalf("token B must be unaffected = %d, want 403", got)
	}
}
