package account_test

// This file fills coverage gaps left by usecase_test.go / usecase_eventid_test.go:
// ListSessions, RevokeSession, GetAvatar (all 0% before this file), the
// json.Marshal failure branch of AppendAuditLog, the non-ErrNotFound
// propagation branch of GetProfile, the tenant/user guard + CJK rune-count
// branches of UpdateProfile, and the tenant/user guard + empty-body branch of
// SetAvatar. It reuses the fakeSessionRepo / fakeAuditRepo / fakeProfileRepo
// fakes already declared in usecase_test.go (same test package) rather than
// redeclaring them.

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/account"
)

// ---- ListSessions -----------------------------------------------------------

func TestListSessions_ReturnsRepoResult(t *testing.T) {
	want := []*domain.Session{{ID: uuid.New(), UserID: "u1"}, {ID: uuid.New(), UserID: "u2"}}
	repo := &fakeSessionRepo{listed: want}
	uc := appacct.NewListSessions(repo)

	got, err := uc.Execute(context.Background(), uuid.New(), "u1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 2 || got[0].UserID != "u1" || got[1].UserID != "u2" {
		t.Errorf("expected passthrough of repo.List result, got %+v", got)
	}
}

func TestListSessions_PropagatesRepoError(t *testing.T) {
	sentinel := errors.New("boom: session store unavailable")
	repo := &fakeSessionRepo{listErr: sentinel}
	uc := appacct.NewListSessions(repo)

	_, err := uc.Execute(context.Background(), uuid.New(), "u1")
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error passthrough, got %v", err)
	}
}

// ---- RevokeSession (idempotent per doc comment) -----------------------------

func TestRevokeSession_Idempotent(t *testing.T) {
	// The fake's Revoke always records the id and returns nil, mirroring the
	// documented idempotent contract: revoking a missing or already-revoked
	// session returns nil, not an error.
	repo := &fakeSessionRepo{}
	uc := appacct.NewRevokeSession(repo)
	tid := uuid.New()
	sid := uuid.New()

	if err := uc.Execute(context.Background(), tid, sid); err != nil {
		t.Fatalf("RevokeSession must be idempotent (nil), got %v", err)
	}
	if len(repo.revoked) != 1 || repo.revoked[0] != sid {
		t.Errorf("expected session id %s forwarded to repo.Revoke, got %v", sid, repo.revoked)
	}

	// Revoking again (simulating an already-revoked / missing session) must
	// still return nil.
	if err := uc.Execute(context.Background(), tid, sid); err != nil {
		t.Fatalf("second revoke must also be idempotent (nil), got %v", err)
	}
}

// ---- AppendAuditLog: json.Marshal failure -----------------------------------

func TestAppendAuditLog_MarshalFailure(t *testing.T) {
	repo := &fakeAuditRepo{}
	uc := appacct.NewAppendAuditLog(repo)

	// A bare channel value cannot be marshalled by encoding/json.
	err := uc.Execute(context.Background(), appacct.AppendInput{
		TenantID: uuid.New(),
		Action:   "pat.created",
		Payload:  make(chan int),
	})
	if err == nil {
		t.Fatal("expected marshal error, got nil")
	}
	if len(repo.appended) != 0 {
		t.Errorf("repo.Append must not be called when marshal fails, got %d entries", len(repo.appended))
	}
}

func TestAppendAuditLog_NilPayloadStoresEmptyJSONObject(t *testing.T) {
	repo := &fakeAuditRepo{}
	uc := appacct.NewAppendAuditLog(repo)
	eventID := "evt-nil-payload-1"

	if err := uc.Execute(context.Background(), appacct.AppendInput{
		TenantID: uuid.New(),
		Action:   "pat.created",
		EventID:  eventID,
	}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(repo.appended) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(repo.appended))
	}
	got := repo.appended[0]
	if string(got.Payload) != "{}" {
		t.Errorf("nil Payload must be stored as []byte(\"{}\"), got %q", got.Payload)
	}
	if got.EventID != eventID {
		t.Errorf("EventID must be threaded through for redelivery dedup, got %q want %q", got.EventID, eventID)
	}
}

// ---- ListAuditLog: clamp values actually forwarded to the repo -------------

// clampCaptureAuditRepo captures the limit/offset it was called with so the
// clamp logic in ListAuditLog.Execute can be asserted directly rather than
// merely "no panic".
type clampCaptureAuditRepo struct {
	gotLimit, gotOffset int
}

func (c *clampCaptureAuditRepo) Append(context.Context, *domain.AuditEntry) error { return nil }
func (c *clampCaptureAuditRepo) List(_ context.Context, _ uuid.UUID, limit, offset int) ([]*domain.AuditEntry, int, error) {
	c.gotLimit, c.gotOffset = limit, offset
	return nil, 0, nil
}

func TestListAuditLog_ClampsAndForwardsExactValues(t *testing.T) {
	cases := []struct {
		name          string
		limit, offset int
		wantLimit     int
		wantOffset    int
	}{
		{"non-positive limit clamps to 50", 0, 0, 50, 0},
		{"negative limit clamps to 50", -1, 0, 50, 0},
		{"over-max limit clamps to 50", appacct.AuditLogMax + 1, 0, 50, 0},
		{"at-max limit stays as-is", appacct.AuditLogMax, 0, appacct.AuditLogMax, 0},
		{"valid limit stays as-is", 20, 0, 20, 0},
		{"negative offset clamps to 0", 20, -5, 20, 0},
		{"non-negative offset stays as-is", 20, 7, 20, 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &clampCaptureAuditRepo{}
			uc := appacct.NewListAuditLog(repo)
			if _, _, err := uc.Execute(context.Background(), uuid.New(), tc.limit, tc.offset); err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if repo.gotLimit != tc.wantLimit {
				t.Errorf("limit: got %d want %d", repo.gotLimit, tc.wantLimit)
			}
			if repo.gotOffset != tc.wantOffset {
				t.Errorf("offset: got %d want %d", repo.gotOffset, tc.wantOffset)
			}
		})
	}
}

func TestListAuditLog_TenantScoped(t *testing.T) {
	repo := &fakeAuditRepo{listed: []*domain.AuditEntry{{ID: uuid.New()}}}
	uc := appacct.NewListAuditLog(repo)
	tid := uuid.New()
	entries, total, err := uc.Execute(context.Background(), tid, 10, 0)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if total != 1 || len(entries) != 1 {
		t.Errorf("expected passthrough of repo list (tenant-scoped by the fake's fixed data), got entries=%d total=%d", len(entries), total)
	}
}

// ---- GetProfile: non-ErrNotFound propagation, and tenant/user stamping -----

func TestGetProfile_PropagatesNonNotFoundError(t *testing.T) {
	sentinel := errors.New("boom: profile store down")
	repo := newFakeProfileRepo()
	repo.getErr = sentinel
	uc := appacct.NewGetProfile(repo)

	p, err := uc.Execute(context.Background(), uuid.New(), "user-1")
	if p != nil {
		t.Errorf("expected nil profile on propagated error, got %+v", p)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error to propagate unchanged, got %v", err)
	}
}

func TestGetProfile_MissingStampsCallerTenantAndUser_NoCrossTenantLeak(t *testing.T) {
	repo := newFakeProfileRepo()
	uc := appacct.NewGetProfile(repo)
	tid := uuid.New()

	p, err := uc.Execute(context.Background(), tid, "user-42")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if p.TenantID != tid || p.UserID != "user-42" {
		t.Errorf("zero profile must be stamped with caller's tenant+user, got tenant=%s user=%s", p.TenantID, p.UserID)
	}
}

func TestGetProfile_ExistingRowReturnedAsIs(t *testing.T) {
	// Exercises the err == nil branch: an existing row must be returned
	// unchanged, not replaced by the zero-value stand-in used for missing rows.
	repo := newFakeProfileRepo()
	tid := uuid.New()
	if err := (appacct.NewUpdateProfile(repo)).Execute(context.Background(), tid, "user-99", "Bob", "555"); err != nil {
		t.Fatalf("setup UpdateProfile failed: %v", err)
	}

	uc := appacct.NewGetProfile(repo)
	p, err := uc.Execute(context.Background(), tid, "user-99")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if p.DisplayName != "Bob" || p.Phone != "555" {
		t.Errorf("expected stored profile to be returned as-is, got display_name=%q phone=%q", p.DisplayName, p.Phone)
	}
}

// ---- UpdateProfile: validation guards ---------------------------------------

func TestUpdateProfile_RequiresTenantAndUser(t *testing.T) {
	uc := appacct.NewUpdateProfile(newFakeProfileRepo())

	if err := uc.Execute(context.Background(), uuid.Nil, "user", "name", "phone"); err == nil {
		t.Error("expected error for uuid.Nil tenant")
	}
	if err := uc.Execute(context.Background(), uuid.New(), "", "name", "phone"); err == nil {
		t.Error("expected error for empty user id")
	}
}

func TestUpdateProfile_CJKRuneCountNotByteLength(t *testing.T) {
	// "中" is 3 bytes in UTF-8 but 1 rune. 65 runes must exceed the 64-rune cap
	// even though byte length differs wildly from a 65-char ASCII string. This
	// proves utf8.RuneCountInString is used, not len(string).
	repo := newFakeProfileRepo()
	uc := appacct.NewUpdateProfile(repo)
	tid, uid := uuid.New(), "user-cjk"

	cjk64 := ""
	for i := 0; i < 64; i++ {
		cjk64 += "中"
	}
	cjk65 := cjk64 + "文"

	if err := uc.Execute(context.Background(), tid, uid, cjk64, ""); err != nil {
		t.Fatalf("64 CJK runes must be accepted, got %v", err)
	}
	if err := uc.Execute(context.Background(), tid, uid, cjk65, ""); err == nil {
		t.Fatal("65 CJK runes must be rejected (rune count, not byte length)")
	}
}

func TestUpdateProfile_PhoneRuneCountBoundary(t *testing.T) {
	repo := newFakeProfileRepo()
	uc := appacct.NewUpdateProfile(repo)
	tid, uid := uuid.New(), "user-phone"

	phone32 := ""
	for i := 0; i < 32; i++ {
		phone32 += "1"
	}
	phone33 := phone32 + "9"

	if err := uc.Execute(context.Background(), tid, uid, "", phone32); err != nil {
		t.Fatalf("32-rune phone must be accepted, got %v", err)
	}
	if err := uc.Execute(context.Background(), tid, uid, "", phone33); err == nil {
		t.Fatal("33-rune phone must be rejected")
	}
}

func TestUpdateProfile_TrimsWhitespaceBeforeCounting(t *testing.T) {
	repo := newFakeProfileRepo()
	uc := appacct.NewUpdateProfile(repo)
	tid, uid := uuid.New(), "user-trim"

	// Leading/trailing spaces must be trimmed before the rune-count check and
	// before persistence, so the stored value has no surrounding whitespace.
	if err := uc.Execute(context.Background(), tid, uid, "  Alice  ", "  138  "); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	stored := repo.stored[tid.String()+":"+uid]
	if stored == nil {
		t.Fatal("expected profile to be persisted")
	}
	if stored.DisplayName != "Alice" {
		t.Errorf("expected trimmed display_name %q, got %q", "Alice", stored.DisplayName)
	}
	if stored.Phone != "138" {
		t.Errorf("expected trimmed phone %q, got %q", "138", stored.Phone)
	}
}

// ---- SetAvatar: remaining branches (tenant/user guard, empty body, boundary) ---

func TestSetAvatar_RequiresTenantAndUser(t *testing.T) {
	uc := appacct.NewSetAvatar(newFakeProfileRepo())
	if err := uc.Execute(context.Background(), uuid.Nil, "user", "image/png", []byte{1}); err == nil {
		t.Error("expected error for uuid.Nil tenant")
	}
	if err := uc.Execute(context.Background(), uuid.New(), "", "image/png", []byte{1}); err == nil {
		t.Error("expected error for empty user id")
	}
}

func TestSetAvatar_EmptyBodyRejected(t *testing.T) {
	uc := appacct.NewSetAvatar(newFakeProfileRepo())
	err := uc.Execute(context.Background(), uuid.New(), "user", "image/png", []byte{})
	if err == nil {
		t.Fatal("expected error for empty avatar body")
	}
	if errors.Is(err, appacct.ErrAvatarTooLarge) || errors.Is(err, appacct.ErrAvatarUnsupported) {
		t.Errorf("empty body must be its own error, not %v", err)
	}
}

func TestSetAvatar_ExactlyMaxSizeAccepted(t *testing.T) {
	repo := newFakeProfileRepo()
	uc := appacct.NewSetAvatar(repo)
	data := make([]byte, appacct.AvatarSizeMax) // exactly the cap, not +1
	if err := uc.Execute(context.Background(), uuid.New(), "user", "image/jpeg", data); err != nil {
		t.Fatalf("exactly AvatarSizeMax bytes must be accepted, got %v", err)
	}
	if repo.avatar.ct != "image/jpeg" || len(repo.avatar.data) != appacct.AvatarSizeMax {
		t.Errorf("avatar not persisted as expected: ct=%s len=%d", repo.avatar.ct, len(repo.avatar.data))
	}
}

func TestSetAvatar_WebpAllowed(t *testing.T) {
	repo := newFakeProfileRepo()
	uc := appacct.NewSetAvatar(repo)
	if err := uc.Execute(context.Background(), uuid.New(), "user", "image/webp", []byte{1, 2, 3}); err != nil {
		t.Fatalf("image/webp must be allowed, got %v", err)
	}
}

func TestSetAvatar_GifRejectedAsUnsupported(t *testing.T) {
	uc := appacct.NewSetAvatar(newFakeProfileRepo())
	err := uc.Execute(context.Background(), uuid.New(), "user", "image/gif", []byte{1, 2, 3})
	if !errors.Is(err, appacct.ErrAvatarUnsupported) {
		t.Errorf("expected ErrAvatarUnsupported for image/gif, got %v", err)
	}
}

// ---- GetAvatar ---------------------------------------------------------------

func TestGetAvatar_HappyPath(t *testing.T) {
	repo := newFakeProfileRepo()
	setUC := appacct.NewSetAvatar(repo)
	tid := uuid.New()
	if err := setUC.Execute(context.Background(), tid, "user-1", "image/png", []byte{9, 8, 7}); err != nil {
		t.Fatalf("setup SetAvatar failed: %v", err)
	}

	getUC := appacct.NewGetAvatar(repo)
	ct, data, err := getUC.Execute(context.Background(), tid, "user-1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ct != "image/png" {
		t.Errorf("content-type: got %q want %q", ct, "image/png")
	}
	if len(data) != 3 {
		t.Errorf("expected 3 avatar bytes, got %d", len(data))
	}
}

func TestGetAvatar_NotFoundPropagates(t *testing.T) {
	uc := appacct.NewGetAvatar(newFakeProfileRepo())
	_, _, err := uc.Execute(context.Background(), uuid.New(), "user-none")
	if !errors.Is(err, appacct.ErrNotFound) {
		t.Errorf("expected ErrNotFound passthrough, got %v", err)
	}
}

// ---- RecordSession: rejects nil tenant / empty user (idempotent guard) ------

func TestRecordSession_RejectsNilTenantAndEmptyUserSeparately(t *testing.T) {
	uc := appacct.NewRecordSession(&fakeSessionRepo{})
	if err := uc.Execute(context.Background(), uuid.Nil, "user-1", "ua", nil); err == nil {
		t.Error("expected error for uuid.Nil tenant")
	}
	if err := uc.Execute(context.Background(), uuid.New(), "", "ua", nil); err == nil {
		t.Error("expected error for empty user id")
	}
}
