package account

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/account"
)

func init() { gin.SetMode(gin.TestMode) }

// ---- fakes ------------------------------------------------------------------

type fakeSessionRepo struct {
	listFn   func(ctx context.Context, tenantID uuid.UUID, userID string) ([]*domain.Session, error)
	revokeFn func(ctx context.Context, tenantID, id uuid.UUID) error
}

func (f fakeSessionRepo) Upsert(context.Context, *domain.Session) error { return nil }
func (f fakeSessionRepo) List(ctx context.Context, tenantID uuid.UUID, userID string) ([]*domain.Session, error) {
	if f.listFn != nil {
		return f.listFn(ctx, tenantID, userID)
	}
	return nil, nil
}
func (f fakeSessionRepo) Revoke(ctx context.Context, tenantID, id uuid.UUID) error {
	if f.revokeFn != nil {
		return f.revokeFn(ctx, tenantID, id)
	}
	return nil
}
func (f fakeSessionRepo) Touch(context.Context, uuid.UUID, string, string) error { return nil }

type fakeAuditRepo struct {
	listFn func(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*domain.AuditEntry, int, error)
}

func (f fakeAuditRepo) Append(context.Context, *domain.AuditEntry) error { return nil }
func (f fakeAuditRepo) List(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*domain.AuditEntry, int, error) {
	if f.listFn != nil {
		return f.listFn(ctx, tenantID, limit, offset)
	}
	return nil, 0, nil
}

type fakeProfileRepo struct {
	getFn       func(ctx context.Context, tenantID uuid.UUID, userID string) (*domain.Profile, error)
	upsertFn    func(ctx context.Context, tenantID uuid.UUID, userID, displayName, phone string) error
	setAvatarFn func(ctx context.Context, tenantID uuid.UUID, userID, contentType string, data []byte) error
	getAvatarFn func(ctx context.Context, tenantID uuid.UUID, userID string) (string, []byte, error)
}

func (f fakeProfileRepo) Get(ctx context.Context, tenantID uuid.UUID, userID string) (*domain.Profile, error) {
	if f.getFn != nil {
		return f.getFn(ctx, tenantID, userID)
	}
	return &domain.Profile{}, nil
}
func (f fakeProfileRepo) Upsert(ctx context.Context, tenantID uuid.UUID, userID, displayName, phone string) error {
	if f.upsertFn != nil {
		return f.upsertFn(ctx, tenantID, userID, displayName, phone)
	}
	return nil
}
func (f fakeProfileRepo) SetAvatar(ctx context.Context, tenantID uuid.UUID, userID, contentType string, data []byte) error {
	if f.setAvatarFn != nil {
		return f.setAvatarFn(ctx, tenantID, userID, contentType, data)
	}
	return nil
}
func (f fakeProfileRepo) GetAvatar(ctx context.Context, tenantID uuid.UUID, userID string) (string, []byte, error) {
	if f.getAvatarFn != nil {
		return f.getAvatarFn(ctx, tenantID, userID)
	}
	return "", nil, nil
}

// newTestCtx builds a gin context with the given method/target/body, wired to
// the given recorder.
func newTestCtx(method, target string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var reqBody *bytes.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	c.Request = httptest.NewRequest(method, target, reqBody)
	return c, w
}

func setTenantSubject(c *gin.Context, tenant uuid.UUID, subject string) {
	c.Set(middleware.CtxKeyTenantID, tenant)
	c.Set(middleware.CtxKeyIDPSubject, subject)
}

// buildMultipart writes one multipart/form-data body. When fieldName is empty,
// no file part is written at all (simulates a missing "file" field).
func buildMultipart(t *testing.T, fieldName, filename, contentType string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	buf := &bytes.Buffer{}
	w := multipart.NewWriter(buf)
	if fieldName != "" {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", `form-data; name="`+fieldName+`"; filename="`+filename+`"`)
		if contentType != "" {
			h.Set("Content-Type", contentType)
		}
		part, err := w.CreatePart(h)
		if err != nil {
			t.Fatalf("CreatePart: %v", err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatalf("part.Write: %v", err)
		}
	} else {
		if err := w.WriteField("note", "no file here"); err != nil {
			t.Fatalf("WriteField: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf, w.FormDataContentType()
}

// ---- ListSessions -------------------------------------------------------

func TestListSessions_Unauthorized(t *testing.T) {
	cases := []struct {
		name    string
		tenant  uuid.UUID
		subject string
	}{
		{"both missing", uuid.Nil, ""},
		{"tenant only", uuid.New(), ""},
		{"subject only", uuid.Nil, "sub-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := New(appacct.NewListSessions(fakeSessionRepo{}), nil, nil, nil, nil, nil, nil)
			c, w := newTestCtx(http.MethodGet, "/account/sessions", nil)
			c.Set(middleware.CtxKeyTenantID, tc.tenant)
			c.Set(middleware.CtxKeyIDPSubject, tc.subject)

			h.ListSessions(c)

			if c.Writer.Status() != http.StatusUnauthorized {
				t.Fatalf("status: got %d, want 401, body=%s", c.Writer.Status(), w.Body.String())
			}
		})
	}
}

// TestListSessions_CurrentFlagMatchesRequestUserAgent asserts the business
// invariant: Current=true only for the session whose UserAgent equals the
// request's User-Agent header.
func TestListSessions_CurrentFlagMatchesRequestUserAgent(t *testing.T) {
	sessionA := &domain.Session{ID: uuid.New(), UserAgent: "ua-match", IPAddr: net.ParseIP("203.0.113.7"), CreatedAt: time.Now(), LastActive: time.Now()}
	sessionB := &domain.Session{ID: uuid.New(), UserAgent: "ua-other", CreatedAt: time.Now(), LastActive: time.Now()}
	repo := fakeSessionRepo{listFn: func(context.Context, uuid.UUID, string) ([]*domain.Session, error) {
		return []*domain.Session{sessionA, sessionB}, nil
	}}
	h := New(appacct.NewListSessions(repo), nil, nil, nil, nil, nil, nil)
	c, w := newTestCtx(http.MethodGet, "/account/sessions", nil)
	c.Request.Header.Set("User-Agent", "ua-match")
	setTenantSubject(c, uuid.New(), "sub-1")

	h.ListSessions(c)

	if c.Writer.Status() != http.StatusOK {
		t.Fatalf("status: got %d, want 200, body=%s", c.Writer.Status(), w.Body.String())
	}
	var out struct {
		Items []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
			IPAddr  string `json:"ip_addr"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Items) != 2 {
		t.Fatalf("items: got %d, want 2", len(out.Items))
	}
	for _, item := range out.Items {
		wantCurrent := item.ID == sessionA.ID.String()
		if item.Current != wantCurrent {
			t.Errorf("session %s: current=%v, want %v", item.ID, item.Current, wantCurrent)
		}
		if item.ID == sessionA.ID.String() && item.IPAddr != "203.0.113.7" {
			t.Errorf("session %s: ip_addr=%q, want 203.0.113.7", item.ID, item.IPAddr)
		}
	}
}

func TestListSessions_RepoError_Writes500(t *testing.T) {
	repo := fakeSessionRepo{listFn: func(context.Context, uuid.UUID, string) ([]*domain.Session, error) {
		return nil, errors.New("boom")
	}}
	h := New(appacct.NewListSessions(repo), nil, nil, nil, nil, nil, nil)
	c, w := newTestCtx(http.MethodGet, "/account/sessions", nil)
	setTenantSubject(c, uuid.New(), "sub-1")

	h.ListSessions(c)

	if c.Writer.Status() != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500, body=%s", c.Writer.Status(), w.Body.String())
	}
}

// ---- RevokeSession -------------------------------------------------------

func TestRevokeSession_Unauthorized(t *testing.T) {
	h := New(nil, appacct.NewRevokeSession(fakeSessionRepo{}), nil, nil, nil, nil, nil)
	c, w := newTestCtx(http.MethodDelete, "/account/sessions/x", nil)
	c.Set(middleware.CtxKeyTenantID, uuid.Nil)

	h.RevokeSession(c)

	if c.Writer.Status() != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401, body=%s", c.Writer.Status(), w.Body.String())
	}
}

func TestRevokeSession_InvalidID(t *testing.T) {
	h := New(nil, appacct.NewRevokeSession(fakeSessionRepo{}), nil, nil, nil, nil, nil)
	c, w := newTestCtx(http.MethodDelete, "/account/sessions/not-a-uuid", nil)
	c.Set(middleware.CtxKeyTenantID, uuid.New())
	c.Params = gin.Params{{Key: "id", Value: "not-a-uuid"}}

	h.RevokeSession(c)

	if c.Writer.Status() != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400, body=%s", c.Writer.Status(), w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "validation_error") {
		t.Errorf("body missing validation_error: %s", w.Body.String())
	}
}

func TestRevokeSession_Success(t *testing.T) {
	sessID := uuid.New()
	var gotTenant, gotID uuid.UUID
	repo := fakeSessionRepo{revokeFn: func(_ context.Context, tenantID, id uuid.UUID) error {
		gotTenant, gotID = tenantID, id
		return nil
	}}
	h := New(nil, appacct.NewRevokeSession(repo), nil, nil, nil, nil, nil)
	c, w := newTestCtx(http.MethodDelete, "/account/sessions/"+sessID.String(), nil)
	tenant := uuid.New()
	c.Set(middleware.CtxKeyTenantID, tenant)
	c.Params = gin.Params{{Key: "id", Value: sessID.String()}}

	h.RevokeSession(c)

	if c.Writer.Status() != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204, body=%s", c.Writer.Status(), w.Body.String())
	}
	if gotTenant != tenant || gotID != sessID {
		t.Errorf("repo called with (%s,%s), want (%s,%s)", gotTenant, gotID, tenant, sessID)
	}
}

func TestRevokeSession_RepoError_Writes500(t *testing.T) {
	repo := fakeSessionRepo{revokeFn: func(context.Context, uuid.UUID, uuid.UUID) error {
		return errors.New("boom")
	}}
	h := New(nil, appacct.NewRevokeSession(repo), nil, nil, nil, nil, nil)
	c, w := newTestCtx(http.MethodDelete, "/account/sessions/"+uuid.New().String(), nil)
	c.Set(middleware.CtxKeyTenantID, uuid.New())
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}

	h.RevokeSession(c)

	if c.Writer.Status() != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500, body=%s", c.Writer.Status(), w.Body.String())
	}
}

// ---- ListAuditLog ---------------------------------------------------------

func TestListAuditLog_Unauthorized(t *testing.T) {
	h := New(nil, nil, appacct.NewListAuditLog(fakeAuditRepo{}), nil, nil, nil, nil)
	c, w := newTestCtx(http.MethodGet, "/account/audit-log", nil)
	c.Set(middleware.CtxKeyTenantID, uuid.Nil)

	h.ListAuditLog(c)

	if c.Writer.Status() != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401, body=%s", c.Writer.Status(), w.Body.String())
	}
}

// TestListAuditLog_AtoiDefaultEdgeCases asserts atoiDefault semantics
// (empty -> default, negative -> default, non-numeric -> default, valid
// numeric string -> parsed value verbatim, even beyond AuditLogMax since the
// handler echoes the pre-clamp value) via the JSON response the handler
// renders.
func TestListAuditLog_AtoiDefaultEdgeCases(t *testing.T) {
	cases := []struct {
		name       string
		limitQ     string
		offsetQ    string
		wantLimit  int
		wantOffset int
	}{
		{"empty both", "", "", 50, 0},
		{"negative limit", "-5", "", 50, 0},
		{"negative offset", "", "-1", 50, 0},
		{"non-numeric limit", "abc", "", 50, 0},
		{"non-numeric offset", "", "xyz", 50, 0},
		{"valid values", "20", "10", 20, 10},
		{"valid beyond cap echoed verbatim", "300", "0", 300, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := fakeAuditRepo{listFn: func(context.Context, uuid.UUID, int, int) ([]*domain.AuditEntry, int, error) {
				return nil, 0, nil
			}}
			h := New(nil, nil, appacct.NewListAuditLog(repo), nil, nil, nil, nil)
			target := "/account/audit-log?limit=" + tc.limitQ + "&offset=" + tc.offsetQ
			c, w := newTestCtx(http.MethodGet, target, nil)
			c.Set(middleware.CtxKeyTenantID, uuid.New())

			h.ListAuditLog(c)

			if c.Writer.Status() != http.StatusOK {
				t.Fatalf("status: got %d, want 200, body=%s", c.Writer.Status(), w.Body.String())
			}
			var out struct {
				Limit  int `json:"limit"`
				Offset int `json:"offset"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if out.Limit != tc.wantLimit || out.Offset != tc.wantOffset {
				t.Errorf("got limit=%d offset=%d, want limit=%d offset=%d", out.Limit, out.Offset, tc.wantLimit, tc.wantOffset)
			}
		})
	}
}

func TestListAuditLog_SuccessMapsEntries(t *testing.T) {
	entry := &domain.AuditEntry{
		ID: uuid.New(), ActorID: "actor-1", Action: "pat.created",
		TargetKind: "pat", TargetID: "pat-1", Payload: []byte(`{"k":1}`),
		CreatedAt: time.Now(),
	}
	repo := fakeAuditRepo{listFn: func(context.Context, uuid.UUID, int, int) ([]*domain.AuditEntry, int, error) {
		return []*domain.AuditEntry{entry}, 1, nil
	}}
	h := New(nil, nil, appacct.NewListAuditLog(repo), nil, nil, nil, nil)
	c, w := newTestCtx(http.MethodGet, "/account/audit-log", nil)
	c.Set(middleware.CtxKeyTenantID, uuid.New())

	h.ListAuditLog(c)

	if c.Writer.Status() != http.StatusOK {
		t.Fatalf("status: got %d, want 200, body=%s", c.Writer.Status(), w.Body.String())
	}
	var out struct {
		Items []struct {
			ActorID string `json:"actor_id"`
			Payload string `json:"payload"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Total != 1 || len(out.Items) != 1 {
		t.Fatalf("got total=%d items=%d, want 1/1", out.Total, len(out.Items))
	}
	if out.Items[0].ActorID != "actor-1" || out.Items[0].Payload != `{"k":1}` {
		t.Errorf("item mismatch: %+v", out.Items[0])
	}
}

func TestListAuditLog_RepoError_Writes500(t *testing.T) {
	repo := fakeAuditRepo{listFn: func(context.Context, uuid.UUID, int, int) ([]*domain.AuditEntry, int, error) {
		return nil, 0, errors.New("boom")
	}}
	h := New(nil, nil, appacct.NewListAuditLog(repo), nil, nil, nil, nil)
	c, w := newTestCtx(http.MethodGet, "/account/audit-log", nil)
	c.Set(middleware.CtxKeyTenantID, uuid.New())

	h.ListAuditLog(c)

	if c.Writer.Status() != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500, body=%s", c.Writer.Status(), w.Body.String())
	}
}

// ---- GetProfile -----------------------------------------------------------

func TestGetProfile_Unauthorized(t *testing.T) {
	cases := []struct {
		name    string
		tenant  uuid.UUID
		subject string
	}{
		{"both missing", uuid.Nil, ""},
		{"tenant only", uuid.New(), ""},
		{"subject only", uuid.Nil, "sub-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := New(nil, nil, nil, appacct.NewGetProfile(fakeProfileRepo{}), nil, nil, nil)
			c, w := newTestCtx(http.MethodGet, "/account/profile", nil)
			c.Set(middleware.CtxKeyTenantID, tc.tenant)
			c.Set(middleware.CtxKeyIDPSubject, tc.subject)

			h.GetProfile(c)

			if c.Writer.Status() != http.StatusUnauthorized {
				t.Fatalf("status: got %d, want 401, body=%s", c.Writer.Status(), w.Body.String())
			}
		})
	}
}

func TestGetProfile_Success_AvatarURLReflectsHasAvatar(t *testing.T) {
	cases := []struct {
		name      string
		hasAvatar bool
		wantURL   string
	}{
		{"no avatar", false, ""},
		{"has avatar", true, "/api/v1/account/avatar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := fakeProfileRepo{getFn: func(_ context.Context, tenantID uuid.UUID, userID string) (*domain.Profile, error) {
				return &domain.Profile{
					TenantID: tenantID, UserID: userID,
					DisplayName: "Alice", Phone: "123",
					HasAvatar: tc.hasAvatar, UpdatedAt: time.Now(),
				}, nil
			}}
			h := New(nil, nil, nil, appacct.NewGetProfile(repo), nil, nil, nil)
			c, w := newTestCtx(http.MethodGet, "/account/profile", nil)
			setTenantSubject(c, uuid.New(), "sub-1")

			h.GetProfile(c)

			if c.Writer.Status() != http.StatusOK {
				t.Fatalf("status: got %d, want 200, body=%s", c.Writer.Status(), w.Body.String())
			}
			var out struct {
				AvatarURL string `json:"avatar_url"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if out.AvatarURL != tc.wantURL {
				t.Errorf("avatar_url: got %q, want %q", out.AvatarURL, tc.wantURL)
			}
		})
	}
}

func TestGetProfile_UseCaseError_Writes500(t *testing.T) {
	repo := fakeProfileRepo{getFn: func(context.Context, uuid.UUID, string) (*domain.Profile, error) {
		return nil, errors.New("boom")
	}}
	h := New(nil, nil, nil, appacct.NewGetProfile(repo), nil, nil, nil)
	c, w := newTestCtx(http.MethodGet, "/account/profile", nil)
	setTenantSubject(c, uuid.New(), "sub-1")

	h.GetProfile(c)

	if c.Writer.Status() != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500, body=%s", c.Writer.Status(), w.Body.String())
	}
}

// ---- UpdateProfile ---------------------------------------------------------

func TestUpdateProfile_Unauthorized(t *testing.T) {
	h := New(nil, nil, nil, nil, appacct.NewUpdateProfile(fakeProfileRepo{}), nil, nil)
	c, w := newTestCtx(http.MethodPut, "/account/profile", []byte(`{}`))
	c.Set(middleware.CtxKeyTenantID, uuid.Nil)

	h.UpdateProfile(c)

	if c.Writer.Status() != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401, body=%s", c.Writer.Status(), w.Body.String())
	}
}

func TestUpdateProfile_BindJSONFailure(t *testing.T) {
	h := New(nil, nil, nil, nil, appacct.NewUpdateProfile(fakeProfileRepo{}), nil, nil)
	c, w := newTestCtx(http.MethodPut, "/account/profile", []byte(`{"display_name":`)) // truncated JSON
	c.Request.Header.Set("Content-Type", "application/json")
	setTenantSubject(c, uuid.New(), "sub-1")

	h.UpdateProfile(c)

	if c.Writer.Status() != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400, body=%s", c.Writer.Status(), w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "validation_error") {
		t.Errorf("body missing validation_error: %s", w.Body.String())
	}
}

func TestUpdateProfile_UseCaseError_Is400NotValidationError(t *testing.T) {
	// display_name > 64 runes triggers appacct.UpdateProfile.Execute's own
	// validation error (not a repo error), which the handler must still map
	// to 400 validation_error, not 500.
	longName := strings.Repeat("x", 65)
	h := New(nil, nil, nil, nil, appacct.NewUpdateProfile(fakeProfileRepo{}), nil, nil)
	body, _ := json.Marshal(map[string]string{"display_name": longName, "phone": "1"})
	c, w := newTestCtx(http.MethodPut, "/account/profile", body)
	c.Request.Header.Set("Content-Type", "application/json")
	setTenantSubject(c, uuid.New(), "sub-1")

	h.UpdateProfile(c)

	if c.Writer.Status() != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400, body=%s", c.Writer.Status(), w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "validation_error") {
		t.Errorf("body missing validation_error: %s", w.Body.String())
	}
}

func TestUpdateProfile_Success(t *testing.T) {
	var gotDisplay, gotPhone string
	repo := fakeProfileRepo{upsertFn: func(_ context.Context, _ uuid.UUID, _ string, displayName, phone string) error {
		gotDisplay, gotPhone = displayName, phone
		return nil
	}}
	h := New(nil, nil, nil, nil, appacct.NewUpdateProfile(repo), nil, nil)
	body, _ := json.Marshal(map[string]string{"display_name": "Alice", "phone": "555"})
	c, w := newTestCtx(http.MethodPut, "/account/profile", body)
	c.Request.Header.Set("Content-Type", "application/json")
	setTenantSubject(c, uuid.New(), "sub-1")

	h.UpdateProfile(c)

	if c.Writer.Status() != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204, body=%s", c.Writer.Status(), w.Body.String())
	}
	if gotDisplay != "Alice" || gotPhone != "555" {
		t.Errorf("repo called with (%q,%q), want (Alice,555)", gotDisplay, gotPhone)
	}
}

// ---- UploadAvatar -----------------------------------------------------------

func TestUploadAvatar_Unauthorized(t *testing.T) {
	h := New(nil, nil, nil, nil, nil, appacct.NewSetAvatar(fakeProfileRepo{}), nil)
	c, w := newTestCtx(http.MethodPost, "/account/avatar", nil)
	c.Set(middleware.CtxKeyTenantID, uuid.Nil)

	h.UploadAvatar(c)

	if c.Writer.Status() != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401, body=%s", c.Writer.Status(), w.Body.String())
	}
}

func TestUploadAvatar_MissingFileField(t *testing.T) {
	buf, ct := buildMultipart(t, "", "", "", nil)
	h := New(nil, nil, nil, nil, nil, appacct.NewSetAvatar(fakeProfileRepo{}), nil)
	c, w := newTestCtx(http.MethodPost, "/account/avatar", buf.Bytes())
	c.Request.Header.Set("Content-Type", ct)
	setTenantSubject(c, uuid.New(), "sub-1")

	h.UploadAvatar(c)

	if c.Writer.Status() != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400, body=%s", c.Writer.Status(), w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "missing form field") {
		t.Errorf("body missing detail: %s", w.Body.String())
	}
}

func TestUploadAvatar_MultipartParseFailure(t *testing.T) {
	h := New(nil, nil, nil, nil, nil, appacct.NewSetAvatar(fakeProfileRepo{}), nil)
	c, w := newTestCtx(http.MethodPost, "/account/avatar", []byte("this is not multipart"))
	c.Request.Header.Set("Content-Type", "multipart/form-data; boundary=zzz")
	setTenantSubject(c, uuid.New(), "sub-1")

	h.UploadAvatar(c)

	if c.Writer.Status() != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400, body=%s", c.Writer.Status(), w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "multipart parse") {
		t.Errorf("body missing detail: %s", w.Body.String())
	}
}

func TestUploadAvatar_FileHeaderSizeExceedsCap(t *testing.T) {
	// AvatarSizeMax=200*1024; pick a size comfortably above the cap but well
	// under avatarUploadMax (AvatarSizeMax+16KB) so ParseMultipartForm
	// succeeds and the handler's explicit fh.Size check fires (413).
	data := bytes.Repeat([]byte("a"), appacct.AvatarSizeMax+2000)
	buf, ct := buildMultipart(t, "file", "big.png", "image/png", data)
	h := New(nil, nil, nil, nil, nil, appacct.NewSetAvatar(fakeProfileRepo{}), nil)
	c, w := newTestCtx(http.MethodPost, "/account/avatar", buf.Bytes())
	c.Request.Header.Set("Content-Type", ct)
	setTenantSubject(c, uuid.New(), "sub-1")

	h.UploadAvatar(c)

	if c.Writer.Status() != http.StatusRequestEntityTooLarge {
		t.Fatalf("status: got %d, want 413, body=%s", c.Writer.Status(), w.Body.String())
	}
}

func TestUploadAvatar_BodyExceedsMultipartCap(t *testing.T) {
	// A file large enough that the multipart body itself exceeds
	// avatarUploadMax; MaxBytesReader/ParseMultipartForm fails before the
	// handler ever inspects fh.Size, surfacing as 400 rather than 413.
	data := bytes.Repeat([]byte("a"), appacct.AvatarSizeMax+64*1024)
	buf, ct := buildMultipart(t, "file", "huge.png", "image/png", data)
	h := New(nil, nil, nil, nil, nil, appacct.NewSetAvatar(fakeProfileRepo{}), nil)
	c, w := newTestCtx(http.MethodPost, "/account/avatar", buf.Bytes())
	c.Request.Header.Set("Content-Type", ct)
	setTenantSubject(c, uuid.New(), "sub-1")

	h.UploadAvatar(c)

	if c.Writer.Status() != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400, body=%s", c.Writer.Status(), w.Body.String())
	}
}

func TestUploadAvatar_SetAvatarErrorMapping(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode int
	}{
		{"too large", appacct.ErrAvatarTooLarge, http.StatusRequestEntityTooLarge},
		{"unsupported", appacct.ErrAvatarUnsupported, http.StatusUnsupportedMediaType},
		{"other", errors.New("db down"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := fakeProfileRepo{setAvatarFn: func(context.Context, uuid.UUID, string, string, []byte) error {
				return tc.err
			}}
			data := []byte("tiny-file-contents")
			buf, ct := buildMultipart(t, "file", "a.png", "image/png", data)
			h := New(nil, nil, nil, nil, nil, appacct.NewSetAvatar(repo), nil)
			c, w := newTestCtx(http.MethodPost, "/account/avatar", buf.Bytes())
			c.Request.Header.Set("Content-Type", ct)
			setTenantSubject(c, uuid.New(), "sub-1")

			h.UploadAvatar(c)

			if c.Writer.Status() != tc.wantCode {
				t.Fatalf("status: got %d, want %d, body=%s", c.Writer.Status(), tc.wantCode, w.Body.String())
			}
		})
	}
}

func TestUploadAvatar_Success(t *testing.T) {
	var gotCT string
	var gotData []byte
	repo := fakeProfileRepo{setAvatarFn: func(_ context.Context, _ uuid.UUID, _ string, contentType string, data []byte) error {
		gotCT, gotData = contentType, data
		return nil
	}}
	data := []byte("tiny-file-contents")
	buf, ct := buildMultipart(t, "file", "a.png", "image/png", data)
	h := New(nil, nil, nil, nil, nil, appacct.NewSetAvatar(repo), nil)
	c, w := newTestCtx(http.MethodPost, "/account/avatar", buf.Bytes())
	c.Request.Header.Set("Content-Type", ct)
	setTenantSubject(c, uuid.New(), "sub-1")

	h.UploadAvatar(c)

	if c.Writer.Status() != http.StatusOK {
		t.Fatalf("status: got %d, want 200, body=%s", c.Writer.Status(), w.Body.String())
	}
	if gotCT != "image/png" || !bytes.Equal(gotData, data) {
		t.Errorf("setAvatar called with ct=%q data=%q, want ct=image/png data=%q", gotCT, gotData, data)
	}
	var out struct {
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.AvatarURL != "/api/v1/account/avatar" {
		t.Errorf("avatar_url: got %q", out.AvatarURL)
	}
}

// TestUploadAvatar_ContentTypeFallsBackToDetection asserts that when the
// multipart part carries no explicit Content-Type header, the handler
// derives it via http.DetectContentType(data) rather than leaving it empty.
func TestUploadAvatar_ContentTypeFallsBackToDetection(t *testing.T) {
	// A real PNG signature so http.DetectContentType sniffs "image/png",
	// which is on appacct.AvatarAllowedTypes and so exercises the success
	// path end-to-end.
	pngSig := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00}
	var gotCT string
	repo := fakeProfileRepo{setAvatarFn: func(_ context.Context, _ uuid.UUID, _ string, contentType string, _ []byte) error {
		gotCT = contentType
		return nil
	}}
	// contentType="" -> buildMultipart omits the part's Content-Type header.
	buf, formCT := buildMultipart(t, "file", "a.png", "", pngSig)
	h := New(nil, nil, nil, nil, nil, appacct.NewSetAvatar(repo), nil)
	c, w := newTestCtx(http.MethodPost, "/account/avatar", buf.Bytes())
	c.Request.Header.Set("Content-Type", formCT)
	setTenantSubject(c, uuid.New(), "sub-1")

	h.UploadAvatar(c)

	if c.Writer.Status() != http.StatusOK {
		t.Fatalf("status: got %d, want 200, body=%s", c.Writer.Status(), w.Body.String())
	}
	if gotCT != "image/png" {
		t.Errorf("setAvatar content-type: got %q, want image/png (detected)", gotCT)
	}
}

// ---- DownloadAvatar ---------------------------------------------------------

func TestDownloadAvatar_Unauthorized(t *testing.T) {
	h := New(nil, nil, nil, nil, nil, nil, appacct.NewGetAvatar(fakeProfileRepo{}))
	c, w := newTestCtx(http.MethodGet, "/account/avatar", nil)
	c.Set(middleware.CtxKeyTenantID, uuid.Nil)

	h.DownloadAvatar(c)

	if c.Writer.Status() != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401, body=%s", c.Writer.Status(), w.Body.String())
	}
}

func TestDownloadAvatar_NotFound_BareStatusNoBody(t *testing.T) {
	repo := fakeProfileRepo{getAvatarFn: func(context.Context, uuid.UUID, string) (string, []byte, error) {
		return "", nil, appacct.ErrNotFound
	}}
	h := New(nil, nil, nil, nil, nil, nil, appacct.NewGetAvatar(repo))
	c, w := newTestCtx(http.MethodGet, "/account/avatar", nil)
	setTenantSubject(c, uuid.New(), "sub-1")

	h.DownloadAvatar(c)

	if c.Writer.Status() != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404, body=%s", c.Writer.Status(), w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Errorf("body: got %q, want empty (bare status)", w.Body.String())
	}
}

func TestDownloadAvatar_OtherError_Writes500(t *testing.T) {
	repo := fakeProfileRepo{getAvatarFn: func(context.Context, uuid.UUID, string) (string, []byte, error) {
		return "", nil, errors.New("boom")
	}}
	h := New(nil, nil, nil, nil, nil, nil, appacct.NewGetAvatar(repo))
	c, w := newTestCtx(http.MethodGet, "/account/avatar", nil)
	setTenantSubject(c, uuid.New(), "sub-1")

	h.DownloadAvatar(c)

	if c.Writer.Status() != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500, body=%s", c.Writer.Status(), w.Body.String())
	}
}

func TestDownloadAvatar_Success(t *testing.T) {
	wantData := []byte("png-bytes")
	repo := fakeProfileRepo{getAvatarFn: func(context.Context, uuid.UUID, string) (string, []byte, error) {
		return "image/png", wantData, nil
	}}
	h := New(nil, nil, nil, nil, nil, nil, appacct.NewGetAvatar(repo))
	c, w := newTestCtx(http.MethodGet, "/account/avatar", nil)
	setTenantSubject(c, uuid.New(), "sub-1")

	h.DownloadAvatar(c)

	if c.Writer.Status() != http.StatusOK {
		t.Fatalf("status: got %d, want 200, body=%s", c.Writer.Status(), w.Body.String())
	}
	if w.Header().Get("Content-Type") != "image/png" {
		t.Errorf("content-type: got %q", w.Header().Get("Content-Type"))
	}
	if w.Header().Get("Cache-Control") != "private, max-age=60" {
		t.Errorf("cache-control: got %q", w.Header().Get("Cache-Control"))
	}
	if !bytes.Equal(w.Body.Bytes(), wantData) {
		t.Errorf("body: got %q, want %q", w.Body.Bytes(), wantData)
	}
}

// ---- unit-level helpers -----------------------------------------------------

func TestAvatarURL(t *testing.T) {
	cases := []struct {
		hasAvatar bool
		want      string
	}{
		{false, ""},
		{true, "/api/v1/account/avatar"},
	}
	for _, tc := range cases {
		t.Run(strconv.FormatBool(tc.hasAvatar), func(t *testing.T) {
			got := avatarURL(uuid.New(), tc.hasAvatar)
			if got != tc.want {
				t.Errorf("avatarURL(_, %v) = %q, want %q", tc.hasAvatar, got, tc.want)
			}
		})
	}
}

func TestAtoiDefault(t *testing.T) {
	cases := []struct {
		name string
		in   string
		def  int
		want int
	}{
		{"empty", "", 50, 50},
		{"valid", "7", 50, 7},
		{"zero", "0", 50, 0},
		{"negative", "-3", 50, 50},
		{"non-numeric", "nope", 9, 9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := atoiDefault(tc.in, tc.def)
			if got != tc.want {
				t.Errorf("atoiDefault(%q,%d) = %d, want %d", tc.in, tc.def, got, tc.want)
			}
		})
	}
}

// TestRegisterRoutes ensures the route table wiring itself is exercised.
func TestRegisterRoutes(t *testing.T) {
	h := New(
		appacct.NewListSessions(fakeSessionRepo{}),
		appacct.NewRevokeSession(fakeSessionRepo{}),
		appacct.NewListAuditLog(fakeAuditRepo{}),
		appacct.NewGetProfile(fakeProfileRepo{}),
		appacct.NewUpdateProfile(fakeProfileRepo{}),
		appacct.NewSetAvatar(fakeProfileRepo{}),
		appacct.NewGetAvatar(fakeProfileRepo{}),
	)
	engine := gin.New()
	rg := engine.Group("/api/v1")
	h.RegisterRoutes(rg)

	routes := engine.Routes()
	if len(routes) != 7 {
		t.Fatalf("routes registered: got %d, want 7", len(routes))
	}
}
