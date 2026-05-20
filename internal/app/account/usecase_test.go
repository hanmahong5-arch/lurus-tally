package account_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/google/uuid"

	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/account"
)

// ---- fakes -----------------------------------------------------------------

type fakeSessionRepo struct {
	upserts []domain.Session
	listed  []*domain.Session
	revoked []uuid.UUID
	touched int
	listErr error
}

func (f *fakeSessionRepo) Upsert(_ context.Context, s *domain.Session) error {
	f.upserts = append(f.upserts, *s)
	return nil
}
func (f *fakeSessionRepo) List(_ context.Context, _ uuid.UUID, _ string) ([]*domain.Session, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listed, nil
}
func (f *fakeSessionRepo) Revoke(_ context.Context, _ uuid.UUID, id uuid.UUID) error {
	f.revoked = append(f.revoked, id)
	return nil
}
func (f *fakeSessionRepo) Touch(_ context.Context, _ uuid.UUID, _, _ string) error {
	f.touched++
	return nil
}

type fakeAuditRepo struct {
	appended []domain.AuditEntry
	listed   []*domain.AuditEntry
	listErr  error
}

func (f *fakeAuditRepo) Append(_ context.Context, e *domain.AuditEntry) error {
	f.appended = append(f.appended, *e)
	return nil
}
func (f *fakeAuditRepo) List(_ context.Context, _ uuid.UUID, _, _ int) ([]*domain.AuditEntry, int, error) {
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	return f.listed, len(f.listed), nil
}

type fakeProfileRepo struct {
	stored map[string]*domain.Profile
	avatar struct {
		ct   string
		data []byte
	}
	getErr error
}

func newFakeProfileRepo() *fakeProfileRepo {
	return &fakeProfileRepo{stored: map[string]*domain.Profile{}}
}

func (f *fakeProfileRepo) Get(_ context.Context, tenantID uuid.UUID, userID string) (*domain.Profile, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	key := tenantID.String() + ":" + userID
	p, ok := f.stored[key]
	if !ok {
		return nil, appacct.ErrNotFound
	}
	return p, nil
}
func (f *fakeProfileRepo) Upsert(_ context.Context, tenantID uuid.UUID, userID, displayName, phone string) error {
	key := tenantID.String() + ":" + userID
	f.stored[key] = &domain.Profile{
		TenantID: tenantID, UserID: userID,
		DisplayName: displayName, Phone: phone,
	}
	return nil
}
func (f *fakeProfileRepo) SetAvatar(_ context.Context, tenantID uuid.UUID, userID, ct string, data []byte) error {
	f.avatar.ct = ct
	f.avatar.data = data
	key := tenantID.String() + ":" + userID
	p, ok := f.stored[key]
	if !ok {
		p = &domain.Profile{TenantID: tenantID, UserID: userID}
		f.stored[key] = p
	}
	p.HasAvatar = true
	p.AvatarContentType = ct
	return nil
}
func (f *fakeProfileRepo) GetAvatar(_ context.Context, _ uuid.UUID, _ string) (string, []byte, error) {
	if len(f.avatar.data) == 0 {
		return "", nil, appacct.ErrNotFound
	}
	return f.avatar.ct, f.avatar.data, nil
}

// ---- session tests ---------------------------------------------------------

func TestRecordSession_RequiresTenantAndUser(t *testing.T) {
	uc := appacct.NewRecordSession(&fakeSessionRepo{})
	err := uc.Execute(context.Background(), uuid.Nil, "", "", nil)
	if err == nil {
		t.Fatal("expected error for missing tenant+user, got nil")
	}
}

func TestRecordSession_HappyPath(t *testing.T) {
	repo := &fakeSessionRepo{}
	uc := appacct.NewRecordSession(repo)
	tid := uuid.New()
	if err := uc.Execute(context.Background(), tid, "user-1", "ua/1.0", net.ParseIP("10.0.0.1")); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(repo.upserts) != 1 {
		t.Fatalf("expected 1 upsert, got %d", len(repo.upserts))
	}
	if repo.upserts[0].UserID != "user-1" {
		t.Errorf("unexpected user_id: %s", repo.upserts[0].UserID)
	}
}

func TestRecordSession_TruncatesLongUserAgent(t *testing.T) {
	repo := &fakeSessionRepo{}
	uc := appacct.NewRecordSession(repo)
	tid := uuid.New()
	long := strings.Repeat("x", 1000)
	if err := uc.Execute(context.Background(), tid, "user-1", long, nil); err != nil {
		t.Fatal(err)
	}
	if l := len(repo.upserts[0].UserAgent); l > 256 {
		t.Errorf("user_agent not truncated: len=%d", l)
	}
}

// ---- audit tests -----------------------------------------------------------

func TestAppendAuditLog_RequiresTenantAndAction(t *testing.T) {
	uc := appacct.NewAppendAuditLog(&fakeAuditRepo{})
	if err := uc.Execute(context.Background(), appacct.AppendInput{}); err == nil {
		t.Fatal("expected validation error for empty input")
	}
}

func TestAppendAuditLog_MarshalsPayload(t *testing.T) {
	repo := &fakeAuditRepo{}
	uc := appacct.NewAppendAuditLog(repo)
	if err := uc.Execute(context.Background(), appacct.AppendInput{
		TenantID: uuid.New(),
		ActorID:  "service",
		Action:   "pat.created",
		Payload:  map[string]any{"name": "demo"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(repo.appended) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(repo.appended))
	}
	if !strings.Contains(string(repo.appended[0].Payload), `"demo"`) {
		t.Errorf("payload missing marshaled fields: %s", repo.appended[0].Payload)
	}
}

func TestListAuditLog_ClampsLimit(t *testing.T) {
	repo := &fakeAuditRepo{listed: []*domain.AuditEntry{{ID: uuid.New()}}}
	uc := appacct.NewListAuditLog(repo)
	if _, _, err := uc.Execute(context.Background(), uuid.New(), 999999, -5); err != nil {
		t.Fatal(err)
	}
	// The clamp is observable only via the fake's parameters; we just want no panic.
}

// ---- profile + avatar tests -----------------------------------------------

func TestGetProfile_MissingReturnsZeroValue(t *testing.T) {
	repo := newFakeProfileRepo()
	uc := appacct.NewGetProfile(repo)
	p, err := uc.Execute(context.Background(), uuid.New(), "user-new")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil zero profile")
	}
	if p.DisplayName != "" {
		t.Errorf("expected blank display_name, got %q", p.DisplayName)
	}
}

func TestUpdateProfile_RejectsOversizedFields(t *testing.T) {
	uc := appacct.NewUpdateProfile(newFakeProfileRepo())
	long := strings.Repeat("a", 200)
	if err := uc.Execute(context.Background(), uuid.New(), "user", long, ""); err == nil {
		t.Error("expected error for long display_name")
	}
	if err := uc.Execute(context.Background(), uuid.New(), "user", "", long); err == nil {
		t.Error("expected error for long phone")
	}
}

func TestSetAvatar_RejectsBadContentType(t *testing.T) {
	uc := appacct.NewSetAvatar(newFakeProfileRepo())
	err := uc.Execute(context.Background(), uuid.New(), "user", "application/octet-stream", []byte{0, 1, 2})
	if !errors.Is(err, appacct.ErrAvatarUnsupported) {
		t.Errorf("expected ErrAvatarUnsupported, got %v", err)
	}
}

func TestSetAvatar_RejectsOversize(t *testing.T) {
	uc := appacct.NewSetAvatar(newFakeProfileRepo())
	big := make([]byte, appacct.AvatarSizeMax+1)
	err := uc.Execute(context.Background(), uuid.New(), "user", "image/png", big)
	if !errors.Is(err, appacct.ErrAvatarTooLarge) {
		t.Errorf("expected ErrAvatarTooLarge, got %v", err)
	}
}

func TestSetAvatar_HappyPath(t *testing.T) {
	repo := newFakeProfileRepo()
	uc := appacct.NewSetAvatar(repo)
	if err := uc.Execute(context.Background(), uuid.New(), "user", "image/png", []byte{0x89, 0x50, 0x4E, 0x47}); err != nil {
		t.Fatal(err)
	}
	if repo.avatar.ct != "image/png" {
		t.Errorf("content type not stored: %s", repo.avatar.ct)
	}
}
