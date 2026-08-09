// Package auth_test — supplemental coverage for branches not exercised by
// handler_test.go / pat_handler_test.go: malformed JSON, the
// ErrInconsistentTenantState / default-500 mapping in ChooseProfile, GetMe's
// 500 path, and the PAT handler's remaining validation + 500 branches
// (ListByTenant error, Create error, name-too-long, expires_at in the past,
// empty List result, Revoke's non-NotFound error path).
package auth_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	handlerAuth "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/auth"
	appauth "github.com/hanmahong5-arch/lurus-tally/internal/app/auth"
	appTenant "github.com/hanmahong5-arch/lurus-tally/internal/app/tenant"
	domainauth "github.com/hanmahong5-arch/lurus-tally/internal/domain/auth"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
)

// ============================================================================
// ChooseProfile / GetMe — additional branches
// ============================================================================

// TestAuthHandler_ChooseProfile_MalformedJSON_Returns400 verifies that a body
// that fails ShouldBindJSON (not merely a bad profile_type value) is rejected
// before the use case is ever invoked.
func TestAuthHandler_ChooseProfile_MalformedJSON_Returns400(t *testing.T) {
	stub := &stubChooseProfile{}
	h := handlerAuth.New(stub, &stubGetMe{result: &appTenant.GetMeOutput{}})
	e := newTestEngine(h, "sub-malformed", "", "")

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/tenant/profile", strings.NewReader("{not-json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if stub.called {
		t.Error("use case must not be invoked on malformed JSON")
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["error"] != "bad request" {
		t.Errorf("error field = %v, want %q", body["error"], "bad request")
	}
}

// TestAuthHandler_ChooseProfile_InconsistentState_Returns500 verifies the
// dedicated mapping for appTenant.ErrInconsistentTenantState — distinct from
// the generic default 500 branch, with a fixed, non-echoed detail string.
func TestAuthHandler_ChooseProfile_InconsistentState_Returns500(t *testing.T) {
	stub := &stubChooseProfile{err: appTenant.ErrInconsistentTenantState}
	h := handlerAuth.New(stub, &stubGetMe{result: &appTenant.GetMeOutput{}})
	e := newTestEngine(h, "sub-inconsistent", "", "")

	body := map[string]string{"profile_type": "retail"}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/tenant/profile", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "inconsistent state" {
		t.Errorf("error field = %v, want %q", resp["error"], "inconsistent state")
	}
}

// TestAuthHandler_ChooseProfile_UnmappedError_Returns500WithEchoedDetail
// verifies the `default` branch of the switch — any error that is not one of
// the three known sentinels falls through to a generic 500 whose detail is
// exactly err.Error() (unlike the sentinel branches, which use fixed text).
func TestAuthHandler_ChooseProfile_UnmappedError_Returns500WithEchoedDetail(t *testing.T) {
	underlying := errors.New("boom: db connection reset")
	stub := &stubChooseProfile{err: underlying}
	h := handlerAuth.New(stub, &stubGetMe{result: &appTenant.GetMeOutput{}})
	e := newTestEngine(h, "sub-unmapped", "", "")

	body := map[string]string{"profile_type": "retail"}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/tenant/profile", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "internal server error" {
		t.Errorf("error field = %v, want %q", resp["error"], "internal server error")
	}
	if resp["detail"] != underlying.Error() {
		t.Errorf("detail = %v, want echoed %q", resp["detail"], underlying.Error())
	}
}

// TestAuthHandler_GetMe_UseCaseError_Returns500 verifies GetMe's only
// remaining branch: a failing getMe.Execute maps to a generic 500 whose
// detail echoes err.Error() (there is no sentinel switch here, unlike
// ChooseProfile).
func TestAuthHandler_GetMe_UseCaseError_Returns500(t *testing.T) {
	underlying := errors.New("get me: lookup mapping: connection refused")
	stub := &stubGetMe{err: underlying}
	h := handlerAuth.New(&stubChooseProfile{}, stub)
	e := newTestEngine(h, "sub-getme-fail", "", "")

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["detail"] != underlying.Error() {
		t.Errorf("detail = %v, want echoed %q", resp["detail"], underlying.Error())
	}
}

// ============================================================================
// stub coverage sanity: exercise stubChooseProfile happy path through a
// second distinct profile type to touch domain.NewTenantProfile's alternate
// branch indirectly is unnecessary — domain package has its own tests. No
// extra helper types are declared here; all stubs are reused from
// handler_test.go / pat_handler_test.go (same package, auth_test).
// ============================================================================

// ============================================================================
// PAT Create — remaining validation + 500 branches
// ============================================================================

// TestPATHandler_Create_MalformedJSON_Returns400 verifies invalid JSON body
// (not just a semantically-empty name) is rejected by ShouldBindJSON before
// any field validation runs.
func TestPATHandler_Create_MalformedJSON_Returns400(t *testing.T) {
	r := newPATEngine(&stubRepo{})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/pats", strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "validation_error" {
		t.Errorf("error field = %v, want validation_error", resp["error"])
	}
}

// TestPATHandler_Create_NameTooLong_Returns400 verifies the maxPATNameLen (64)
// boundary: 65 chars must be rejected. A name of exactly 64 chars is valid
// (see TestPATHandler_Create_NameExactly64_Succeeds below) — this pins the
// off-by-one boundary in both directions.
func TestPATHandler_Create_NameTooLong_Returns400(t *testing.T) {
	repo := &stubRepo{}
	r := newPATEngine(repo)

	name := strings.Repeat("a", 65)
	body, _ := json.Marshal(map[string]any{"name": name})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/pats", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if repo.created != nil {
		t.Error("repo.Create must not be called when name is too long")
	}
}

// TestPATHandler_Create_NameExactly64_Succeeds verifies the boundary is
// inclusive: len(name) == maxPATNameLen (64) must NOT be rejected.
func TestPATHandler_Create_NameExactly64_Succeeds(t *testing.T) {
	repo := &stubRepo{}
	r := newPATEngine(repo)

	name := strings.Repeat("b", 64)
	body, _ := json.Marshal(map[string]any{"name": name})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/pats", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if repo.created == nil || repo.created.Name != name {
		t.Errorf("repo.created = %+v, want Name len 64", repo.created)
	}
}

// TestPATHandler_Create_ExpiresAtInPast_Returns400 verifies expires_at must
// be strictly after time.Now() — a timestamp 1h in the past is rejected.
func TestPATHandler_Create_ExpiresAtInPast_Returns400(t *testing.T) {
	repo := &stubRepo{}
	r := newPATEngine(repo)

	past := time.Now().Add(-1 * time.Hour)
	body, _ := json.Marshal(map[string]any{"name": "expiring-token", "expires_at": past})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/pats", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if repo.created != nil {
		t.Error("repo.Create must not be called when expires_at is in the past")
	}
}

// TestPATHandler_Create_ExpiresAtNow_Returns400 verifies the boundary is
// exclusive: expires_at == now (not strictly After(now)) must be rejected.
func TestPATHandler_Create_ExpiresAtNow_Returns400(t *testing.T) {
	repo := &stubRepo{}
	r := newPATEngine(repo)

	now := time.Now()
	body, _ := json.Marshal(map[string]any{"name": "now-token", "expires_at": now})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/pats", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if repo.created != nil {
		t.Error("repo.Create must not be called when expires_at equals now")
	}
}

// TestPATHandler_Create_ExpiresAtFuture_Succeeds verifies a future expires_at
// (1h ahead) is accepted and propagated to the repo/response.
func TestPATHandler_Create_ExpiresAtFuture_Succeeds(t *testing.T) {
	repo := &stubRepo{}
	r := newPATEngine(repo)

	future := time.Now().Add(1 * time.Hour)
	body, _ := json.Marshal(map[string]any{"name": "future-token", "expires_at": future})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/pats", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if repo.created == nil || repo.created.ExpiresAt == nil {
		t.Fatalf("repo.created.ExpiresAt = nil, want propagated future timestamp")
	}
}

// TestPATHandler_Create_ListByTenantError_Returns500 verifies the soft-cap
// lookup's error path is mapped through httperr.WriteInternal (generic 500,
// cause not leaked to the client) rather than skipped.
func TestPATHandler_Create_ListByTenantError_Returns500(t *testing.T) {
	repo := &stubRepo{listErr: errors.New("pg: connection reset by peer")}
	r := newPATEngine(repo)

	body, _ := json.Marshal(map[string]any{"name": "n"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/pats", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "connection reset by peer") {
		t.Errorf("internal cause leaked into response body: %s", rec.Body.String())
	}
	if repo.created != nil {
		t.Error("repo.Create must not be called when ListByTenant fails")
	}
}

// TestPATHandler_Create_RepoCreateError_Returns500 verifies a failing
// repo.Create is mapped to a generic 500 without leaking driver text, and
// that the plaintext token generated before the failed write is discarded
// (never returned to the client — the response body only carries the error
// envelope on this path).
func TestPATHandler_Create_RepoCreateError_Returns500(t *testing.T) {
	repo := &stubRepo{createErr: errors.New("pg: unique_violation on pat_prefix_idx")}
	r := newPATEngine(repo)

	body, _ := json.Marshal(map[string]any{"name": "will-fail"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/pats", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "unique_violation") {
		t.Errorf("internal cause leaked into response body: %s", rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, hasToken := resp["token"]; hasToken {
		t.Errorf("plaintext token must not appear in a failed-create response: %s", rec.Body.String())
	}
}

// TestPATHandler_Create_LimitExactly19_Succeeds verifies the boundary is
// exclusive on the low side: 19 existing tokens (one below the cap of 20)
// must still allow creation of the 20th.
func TestPATHandler_Create_LimitExactly19_Succeeds(t *testing.T) {
	nineteen := make([]*domainauth.PAT, 19)
	for i := range nineteen {
		nineteen[i] = &domainauth.PAT{ID: uuid.New()}
	}
	repo := &stubRepo{listResult: nineteen}
	r := newPATEngine(repo)

	body, _ := json.Marshal(map[string]any{"name": "20th-token"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/pats", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (19 existing must not trip the cap): %s", rec.Code, rec.Body.String())
	}
	if repo.created == nil {
		t.Error("repo.Create must be called when under the cap")
	}
}

// ============================================================================
// PAT List — remaining branches
// ============================================================================

// TestPATHandler_List_RepoError_Returns500 verifies a failing ListByTenant
// maps to a generic 500 via httperr.WriteInternal.
func TestPATHandler_List_RepoError_Returns500(t *testing.T) {
	repo := &stubRepo{listErr: errors.New("pg: read timeout")}
	r := newPATEngine(repo)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/auth/pats", nil)
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "read timeout") {
		t.Errorf("internal cause leaked into response body: %s", rec.Body.String())
	}
}

// TestPATHandler_List_NoTenant_Returns401 verifies the tenant-isolation guard
// on List (the brief's "all Create/List/Revoke calls with tenantID==Nil→401"
// invariant) — no X-Tenant-ID header set.
func TestPATHandler_List_NoTenant_Returns401(t *testing.T) {
	r := newPATEngine(&stubRepo{})
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/auth/pats", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// TestPATHandler_List_Empty_ReturnsEmptyItemsArray verifies the zero-PATs
// case renders `{"items":[]}` (a JSON array, never null) — the handler
// pre-allocates with make([]patSummary, 0, ...) specifically to guarantee this.
func TestPATHandler_List_Empty_ReturnsEmptyItemsArray(t *testing.T) {
	repo := &stubRepo{listResult: nil}
	r := newPATEngine(repo)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/auth/pats", nil)
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Items == nil {
		t.Error("items must be an empty array, not null")
	}
	if len(resp.Items) != 0 {
		t.Errorf("items = %v, want empty", resp.Items)
	}
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("body = %s, want literal empty array `[]`", rec.Body.String())
	}
}

// TestPATHandler_List_MultipleItems_NoHashOrPlaintextField verifies patSummary
// serialisation across N items never carries a "hash" key regardless of how
// many rows are present, and that last_used_at is omitted (omitempty) when nil.
func TestPATHandler_List_MultipleItems_NoHashOrPlaintextField(t *testing.T) {
	now := time.Now().UTC()
	repo := &stubRepo{listResult: []*domainauth.PAT{
		{ID: uuid.New(), Name: "one", Prefix: "aaaaaaaa", Hash: strings.Repeat("1", 64), CreatedAt: now},
		{ID: uuid.New(), Name: "two", Prefix: "bbbbbbbb", Hash: strings.Repeat("2", 64), CreatedAt: now, LastUsedAt: &now},
	}}
	r := newPATEngine(repo)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/auth/pats", nil)
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items len = %d, want 2", len(resp.Items))
	}
	for i, item := range resp.Items {
		if _, hasHash := item["hash"]; hasHash {
			t.Errorf("item[%d] leaked hash field: %+v", i, item)
		}
		if _, hasToken := item["token"]; hasToken {
			t.Errorf("item[%d] leaked token field: %+v", i, item)
		}
	}
	if _, hasLastUsed := resp.Items[0]["last_used_at"]; hasLastUsed {
		t.Errorf("item[0] (nil LastUsedAt) must omit last_used_at, got %+v", resp.Items[0])
	}
	if _, hasLastUsed := resp.Items[1]["last_used_at"]; !hasLastUsed {
		t.Errorf("item[1] (non-nil LastUsedAt) must include last_used_at, got %+v", resp.Items[1])
	}
}

// ============================================================================
// PAT Revoke — remaining branches
// ============================================================================

// TestPATHandler_Revoke_NoTenant_Returns401 verifies the tenant-isolation
// guard on Revoke — no X-Tenant-ID header set.
func TestPATHandler_Revoke_NoTenant_Returns401(t *testing.T) {
	r := newPATEngine(&stubRepo{})
	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/auth/pats/"+uuid.NewString(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// TestPATHandler_Revoke_RepoErrorOtherThanNotFound_Returns500 verifies that
// ONLY appauth.ErrNotFound is downgraded to 204 — any other repo error must
// still surface as a 500 via httperr.WriteInternal, not be swallowed as
// idempotent success.
func TestPATHandler_Revoke_RepoErrorOtherThanNotFound_Returns500(t *testing.T) {
	repo := &stubRepo{revokeErr: errors.New("pg: deadlock detected")}
	r := newPATEngine(repo)

	id := uuid.New()
	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/auth/pats/"+id.String(), nil)
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (non-ErrNotFound must not be treated as idempotent success): %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "deadlock detected") {
		t.Errorf("internal cause leaked into response body: %s", rec.Body.String())
	}
}

// TestPATHandler_Revoke_TenantScopePropagated verifies the (tenantID, id)
// pair reaching repo.Revoke matches exactly what was resolved from context —
// guards against accidentally revoking cross-tenant by only matching on id.
func TestPATHandler_Revoke_TenantScopePropagated(t *testing.T) {
	repo := &stubRepo{}
	r := newPATEngine(repo)

	id := uuid.New()
	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/auth/pats/"+id.String(), nil)
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	wantTenant, _ := uuid.Parse(testTenantHeader)
	if repo.revokedTen != wantTenant {
		t.Errorf("revoked tenant = %s, want %s", repo.revokedTen, wantTenant)
	}
	if repo.revokedID != id {
		t.Errorf("revoked id = %s, want %s", repo.revokedID, id)
	}
}

// ============================================================================
// GetIDPSubject / context wiring sanity (guards handler.go:64-71 sub-extraction
// against silent regressions if middleware key names ever drift).
// ============================================================================

// TestAuthHandler_ChooseProfile_TenantIsolation_EmailAndNamePropagate is a
// belt-and-suspenders check that Email/DisplayName flow through unchanged
// when both happen to be empty strings (distinguishing "field present but
// empty" from "field absent" in the ChooseProfileInput built at handler.go:82).
func TestAuthHandler_ChooseProfile_TenantIsolation_EmailAndNamePropagate(t *testing.T) {
	stub := &stubChooseProfile{}
	h := handlerAuth.New(stub, &stubGetMe{result: &appTenant.GetMeOutput{}})
	e := newTestEngine(h, "sub-noemail", "", "")

	body := map[string]string{"profile_type": "horticulture"}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/tenant/profile", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if stub.in.Email != "" || stub.in.DisplayName != "" {
		t.Errorf("expected empty email/name propagated as-is, got email=%q name=%q", stub.in.Email, stub.in.DisplayName)
	}
	if stub.in.ProfileType != "horticulture" {
		t.Errorf("profile_type = %q, want horticulture", stub.in.ProfileType)
	}
}

// compile-time interface sanity: ensure the stub/fake types referenced here
// (declared in sibling _test.go files of the same auth_test package) satisfy
// the handler's narrow executor interfaces. This costs nothing at runtime but
// fails fast at compile time if a sibling test file's stub signature drifts.
var (
	_ handlerAuth.ChooseProfileExecutor = (*stubChooseProfile)(nil)
	_ handlerAuth.GetMeExecutor         = (*stubGetMe)(nil)
	_ appauth.Repository                = (*stubRepo)(nil)
)

// sanity: domain sentinel errors used above really are distinct values, so
// the errors.Is branches in handler.go are meaningfully exercised rather than
// coincidentally matching a shared underlying error.
func TestSentinelErrors_AreDistinct(t *testing.T) {
	if errors.Is(domain.ErrInvalidProfileType, domain.ErrProfileAlreadySet) {
		t.Fatal("ErrInvalidProfileType must not equal ErrProfileAlreadySet")
	}
	if errors.Is(domain.ErrProfileAlreadySet, appTenant.ErrInconsistentTenantState) {
		t.Fatal("ErrProfileAlreadySet must not equal ErrInconsistentTenantState")
	}
}
