// Package account holds the domain types backing the /api/v1/account/* surface.
//
// Three companion records are exposed:
//   - Session     — one row per browser login; lets the user see and revoke
//                   active devices
//   - AuditEntry  — append-only flow of business-significant events the
//                   tenant should be able to inspect ("活动日志" tab)
//   - Profile     — per-user editable display_name / phone / avatar
//
// The types are intentionally simple value structs; the persistence and use
// case layers own validation and lifecycle.
package account

import (
	"net"
	"time"

	"github.com/google/uuid"
)

// Session represents one authenticated browser/device session.
// RevokedAt = nil means active. Once set, the session is treated as
// revoked by the auth middleware Redis cache and cannot be reactivated —
// the user must sign in again.
type Session struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	UserID     string // Zitadel sub
	UserAgent  string
	IPAddr     net.IP
	CreatedAt  time.Time
	LastActive time.Time
	RevokedAt  *time.Time
}

// IsActive reports whether the session is still usable at the given moment.
func (s *Session) IsActive() bool { return s.RevokedAt == nil }

// AuditEntry is one row in the per-tenant audit_log. Action is a dotted
// string like "pat.created" or "bill.approved". Payload is opaque JSON.
type AuditEntry struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	ActorID    string
	Action     string
	TargetKind string
	TargetID   string
	Payload    []byte // marshalled JSON
	CreatedAt  time.Time
}

// Profile is the editable per-user record. DisplayName here overrides the
// Zitadel-provided value when non-empty.
type Profile struct {
	TenantID          uuid.UUID
	UserID            string
	DisplayName       string
	Phone             string
	AvatarContentType string
	// AvatarBytes is intentionally left untyped here. Handlers should not
	// load it eagerly when reading the profile for /me; use the dedicated
	// avatar endpoint to stream the bytes.
	HasAvatar bool
	UpdatedAt time.Time
}
