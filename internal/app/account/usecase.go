// Package account exposes the use cases backing /api/v1/account/*.
//
// The package is split into three small concerns — sessions / audit / profile —
// each with a dedicated repository interface. Handlers compose the use cases
// they need; the audit subscriber composes only AuditStore.
package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/account"
)

// AvatarSizeMax caps the avatar payload at 200KB. The figure is generous for
// a 256x256 PNG/JPEG and small enough that storing in PG bytea remains cheap.
const AvatarSizeMax = 200 * 1024

// AvatarAllowedTypes is the content-type allow list for uploads.
var AvatarAllowedTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
}

// AuditLogMax is the default page size cap for ListAuditLog; the handler
// MAY clamp the requested limit below this. Enforced server-side so a
// pathological client cannot drain the table.
const AuditLogMax = 200

// ---- Repository ports ------------------------------------------------------

// SessionRepo persists user_session rows.
type SessionRepo interface {
	Upsert(ctx context.Context, s *domain.Session) error
	List(ctx context.Context, tenantID uuid.UUID, userID string) ([]*domain.Session, error)
	Revoke(ctx context.Context, tenantID uuid.UUID, id uuid.UUID) error
	Touch(ctx context.Context, tenantID uuid.UUID, userID, userAgent string) error
}

// AuditRepo persists account_audit_log rows.
type AuditRepo interface {
	Append(ctx context.Context, e *domain.AuditEntry) error
	List(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*domain.AuditEntry, int, error)
}

// ProfileRepo persists user_profile rows. Avatar bytes live on this same
// table so writes and reads stay inside one transaction.
type ProfileRepo interface {
	Get(ctx context.Context, tenantID uuid.UUID, userID string) (*domain.Profile, error)
	Upsert(ctx context.Context, tenantID uuid.UUID, userID, displayName, phone string) error
	SetAvatar(ctx context.Context, tenantID uuid.UUID, userID, contentType string, data []byte) error
	GetAvatar(ctx context.Context, tenantID uuid.UUID, userID string) (contentType string, data []byte, err error)
}

// ---- Session use cases -----------------------------------------------------

// RecordSession is called by middleware on each authenticated request: it
// inserts the session row if missing and updates last_active otherwise.
type RecordSession struct{ repo SessionRepo }

// NewRecordSession constructs the use case.
func NewRecordSession(r SessionRepo) *RecordSession { return &RecordSession{repo: r} }

// Execute records or refreshes a session.
func (uc *RecordSession) Execute(ctx context.Context, tenantID uuid.UUID, userID, userAgent string, ip net.IP) error {
	if tenantID == uuid.Nil || userID == "" {
		return fmt.Errorf("record session: tenant_id and user_id required")
	}
	now := time.Now().UTC()
	return uc.repo.Upsert(ctx, &domain.Session{
		ID:         uuid.New(),
		TenantID:   tenantID,
		UserID:     userID,
		UserAgent:  truncate(userAgent, 256),
		IPAddr:     ip,
		CreatedAt:  now,
		LastActive: now,
	})
}

// ListSessions returns all non-revoked sessions for the calling user.
type ListSessions struct{ repo SessionRepo }

// NewListSessions constructs the use case.
func NewListSessions(r SessionRepo) *ListSessions { return &ListSessions{repo: r} }

// Execute returns active sessions.
func (uc *ListSessions) Execute(ctx context.Context, tenantID uuid.UUID, userID string) ([]*domain.Session, error) {
	return uc.repo.List(ctx, tenantID, userID)
}

// RevokeSession marks a session as revoked. Idempotent: revoking a missing
// or already-revoked session returns nil.
type RevokeSession struct{ repo SessionRepo }

// NewRevokeSession constructs the use case.
func NewRevokeSession(r SessionRepo) *RevokeSession { return &RevokeSession{repo: r} }

// Execute revokes the session.
func (uc *RevokeSession) Execute(ctx context.Context, tenantID, id uuid.UUID) error {
	return uc.repo.Revoke(ctx, tenantID, id)
}

// ---- Audit use cases -------------------------------------------------------

// AppendAuditLog persists one audit entry. The use case validates and
// marshals the payload; callers can be terse: pass nil for no payload.
type AppendAuditLog struct{ repo AuditRepo }

// NewAppendAuditLog constructs the use case.
func NewAppendAuditLog(r AuditRepo) *AppendAuditLog { return &AppendAuditLog{repo: r} }

// AppendInput collects the inputs for one audit row.
type AppendInput struct {
	TenantID   uuid.UUID
	ActorID    string
	Action     string
	TargetKind string
	TargetID   string
	Payload    any
}

// Execute marshals payload and persists the row.
func (uc *AppendAuditLog) Execute(ctx context.Context, in AppendInput) error {
	if in.TenantID == uuid.Nil || in.Action == "" {
		return fmt.Errorf("append audit log: tenant_id and action required")
	}
	var raw []byte
	if in.Payload != nil {
		b, err := json.Marshal(in.Payload)
		if err != nil {
			return fmt.Errorf("append audit log: marshal payload: %w", err)
		}
		raw = b
	} else {
		raw = []byte("{}")
	}
	return uc.repo.Append(ctx, &domain.AuditEntry{
		ID:         uuid.New(),
		TenantID:   in.TenantID,
		ActorID:    in.ActorID,
		Action:     in.Action,
		TargetKind: in.TargetKind,
		TargetID:   in.TargetID,
		Payload:    raw,
		CreatedAt:  time.Now().UTC(),
	})
}

// ListAuditLog returns audit entries ordered by created_at DESC. The handler
// is responsible for clamping limit to AuditLogMax.
type ListAuditLog struct{ repo AuditRepo }

// NewListAuditLog constructs the use case.
func NewListAuditLog(r AuditRepo) *ListAuditLog { return &ListAuditLog{repo: r} }

// Execute returns entries and the total count for pagination.
func (uc *ListAuditLog) Execute(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*domain.AuditEntry, int, error) {
	if limit <= 0 || limit > AuditLogMax {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return uc.repo.List(ctx, tenantID, limit, offset)
}

// ---- Profile use cases -----------------------------------------------------

// GetProfile returns the editable profile fields, or a zero-value profile
// when the row is missing. The /me endpoint composes this on top of the
// Zitadel-sourced identity, so a missing row is not an error.
type GetProfile struct{ repo ProfileRepo }

// NewGetProfile constructs the use case.
func NewGetProfile(r ProfileRepo) *GetProfile { return &GetProfile{repo: r} }

// Execute returns the profile (never nil).
func (uc *GetProfile) Execute(ctx context.Context, tenantID uuid.UUID, userID string) (*domain.Profile, error) {
	p, err := uc.repo.Get(ctx, tenantID, userID)
	if err == nil {
		return p, nil
	}
	if isNotFound(err) {
		return &domain.Profile{TenantID: tenantID, UserID: userID}, nil
	}
	return nil, err
}

// UpdateProfile persists display_name and phone overrides.
type UpdateProfile struct{ repo ProfileRepo }

// NewUpdateProfile constructs the use case.
func NewUpdateProfile(r ProfileRepo) *UpdateProfile { return &UpdateProfile{repo: r} }

// Execute writes the row.
func (uc *UpdateProfile) Execute(ctx context.Context, tenantID uuid.UUID, userID, displayName, phone string) error {
	if tenantID == uuid.Nil || userID == "" {
		return fmt.Errorf("update profile: tenant_id and user_id required")
	}
	displayName = strings.TrimSpace(displayName)
	phone = strings.TrimSpace(phone)
	if utf8.RuneCountInString(displayName) > 64 {
		return fmt.Errorf("update profile: display_name too long")
	}
	if utf8.RuneCountInString(phone) > 32 {
		return fmt.Errorf("update profile: phone too long")
	}
	return uc.repo.Upsert(ctx, tenantID, userID, displayName, phone)
}

// SetAvatar uploads / replaces the user's avatar bytes.
type SetAvatar struct{ repo ProfileRepo }

// NewSetAvatar constructs the use case.
func NewSetAvatar(r ProfileRepo) *SetAvatar { return &SetAvatar{repo: r} }

// Execute validates the size + content-type and persists.
func (uc *SetAvatar) Execute(ctx context.Context, tenantID uuid.UUID, userID, contentType string, data []byte) error {
	if tenantID == uuid.Nil || userID == "" {
		return fmt.Errorf("set avatar: tenant_id and user_id required")
	}
	if !AvatarAllowedTypes[contentType] {
		return ErrAvatarUnsupported
	}
	if len(data) > AvatarSizeMax {
		return ErrAvatarTooLarge
	}
	if len(data) == 0 {
		return fmt.Errorf("set avatar: empty body")
	}
	return uc.repo.SetAvatar(ctx, tenantID, userID, contentType, data)
}

// GetAvatar reads the avatar bytes for streaming to the client.
type GetAvatar struct{ repo ProfileRepo }

// NewGetAvatar constructs the use case.
func NewGetAvatar(r ProfileRepo) *GetAvatar { return &GetAvatar{repo: r} }

// Execute returns the content-type and bytes.
func (uc *GetAvatar) Execute(ctx context.Context, tenantID uuid.UUID, userID string) (string, []byte, error) {
	return uc.repo.GetAvatar(ctx, tenantID, userID)
}

// ---- helpers ---------------------------------------------------------------

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func isNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
