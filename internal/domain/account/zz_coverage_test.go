package account

import (
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestSession_IsActive_RevokedAtStates asserts the business invariant at
// account.go:37 — IsActive() returns true iff RevokedAt == nil. Once
// RevokedAt is set (even to a zero-value time), the session must report
// inactive; the auth middleware Redis cache treats this as unrecoverable
// (comment at account.go:22-24).
func TestSession_IsActive_RevokedAtStates(t *testing.T) {
	zeroTime := time.Time{}
	now := time.Now()

	tests := []struct {
		name      string
		revokedAt *time.Time
		want      bool
	}{
		{
			name:      "nil RevokedAt is active",
			revokedAt: nil,
			want:      true,
		},
		{
			name:      "non-nil RevokedAt (now) is inactive",
			revokedAt: &now,
			want:      false,
		},
		{
			name:      "non-nil RevokedAt pointing at zero-value time is still inactive",
			revokedAt: &zeroTime,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Session{
				ID:        uuid.New(),
				TenantID:  uuid.New(),
				UserID:    "sub-123",
				RevokedAt: tt.revokedAt,
			}
			got := s.IsActive()
			if got != tt.want {
				t.Errorf("IsActive() = %v, want %v (RevokedAt=%v)", got, tt.want, tt.revokedAt)
			}
		})
	}
}

// TestSession_IsActive_ZeroValueSession asserts a completely zero-value
// Session (as would exist before any field is populated) is active, since
// RevokedAt defaults to nil.
func TestSession_IsActive_ZeroValueSession(t *testing.T) {
	var s Session
	if !s.IsActive() {
		t.Errorf("zero-value Session.IsActive() = false, want true (RevokedAt defaults to nil)")
	}
}

// TestSession_FieldAssignment exercises every field of Session to cover the
// struct literal/field-access statements the compiler generates, and pins
// down that field values round-trip unchanged (no hidden mutation/aliasing
// in this pure value struct).
func TestSession_FieldAssignment(t *testing.T) {
	id := uuid.New()
	tenantID := uuid.New()
	ip := net.ParseIP("10.0.0.1")
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	lastActive := created.Add(time.Hour)

	s := &Session{
		ID:         id,
		TenantID:   tenantID,
		UserID:     "sub-abc",
		UserAgent:  "Mozilla/5.0",
		IPAddr:     ip,
		CreatedAt:  created,
		LastActive: lastActive,
		RevokedAt:  nil,
	}

	if s.ID != id {
		t.Errorf("ID = %v, want %v", s.ID, id)
	}
	if s.TenantID != tenantID {
		t.Errorf("TenantID = %v, want %v", s.TenantID, tenantID)
	}
	if s.UserID != "sub-abc" {
		t.Errorf("UserID = %q, want %q", s.UserID, "sub-abc")
	}
	if s.UserAgent != "Mozilla/5.0" {
		t.Errorf("UserAgent = %q, want %q", s.UserAgent, "Mozilla/5.0")
	}
	if !s.IPAddr.Equal(ip) {
		t.Errorf("IPAddr = %v, want %v", s.IPAddr, ip)
	}
	if !s.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", s.CreatedAt, created)
	}
	if !s.LastActive.Equal(lastActive) {
		t.Errorf("LastActive = %v, want %v", s.LastActive, lastActive)
	}
	if s.RevokedAt != nil {
		t.Errorf("RevokedAt = %v, want nil", s.RevokedAt)
	}
}

// TestAuditEntry_EventID_DedupKey asserts the business invariant documented
// at account.go:50-53: EventID carries the source NATS envelope id and acts
// as the dedup key for at-least-once redelivery. Two entries constructed
// with the same EventID but different CreatedAt must both report the same
// EventID value — the struct itself does not dedup (that's the repo/NATS
// subscriber's job), but it must faithfully carry the field so that layer
// can. We also assert the empty-string case, which the comment states means
// "synchronous (non-event) write" — i.e. EventID is NOT force-populated.
func TestAuditEntry_EventID_DedupKey(t *testing.T) {
	tests := []struct {
		name    string
		eventID string
	}{
		{name: "synchronous write has empty EventID", eventID: ""},
		{name: "async write carries NATS envelope id as EventID", eventID: "nats-envelope-abc123"},
		{name: "duplicate redelivery carries identical EventID", eventID: "nats-envelope-abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := AuditEntry{
				ID:         uuid.New(),
				TenantID:   uuid.New(),
				ActorID:    "user-1",
				Action:     "pat.created",
				TargetKind: "pat",
				TargetID:   "pat-1",
				Payload:    []byte(`{"k":"v"}`),
				CreatedAt:  time.Now(),
				EventID:    tt.eventID,
			}
			if entry.EventID != tt.eventID {
				t.Errorf("EventID = %q, want %q", entry.EventID, tt.eventID)
			}
		})
	}
}

// TestAuditEntry_FieldAssignment exercises every remaining AuditEntry field
// (Action as a dotted string, opaque Payload bytes, TargetKind/TargetID).
func TestAuditEntry_FieldAssignment(t *testing.T) {
	id := uuid.New()
	tenantID := uuid.New()
	created := time.Date(2026, 6, 1, 12, 30, 0, 0, time.UTC)
	payload := []byte(`{"amount":100}`)

	entry := AuditEntry{
		ID:         id,
		TenantID:   tenantID,
		ActorID:    "actor-9",
		Action:     "bill.approved",
		TargetKind: "bill",
		TargetID:   "bill-42",
		Payload:    payload,
		CreatedAt:  created,
		EventID:    "evt-1",
	}

	if entry.ID != id {
		t.Errorf("ID = %v, want %v", entry.ID, id)
	}
	if entry.TenantID != tenantID {
		t.Errorf("TenantID = %v, want %v", entry.TenantID, tenantID)
	}
	if entry.ActorID != "actor-9" {
		t.Errorf("ActorID = %q, want %q", entry.ActorID, "actor-9")
	}
	if entry.Action != "bill.approved" {
		t.Errorf("Action = %q, want %q", entry.Action, "bill.approved")
	}
	if entry.TargetKind != "bill" {
		t.Errorf("TargetKind = %q, want %q", entry.TargetKind, "bill")
	}
	if entry.TargetID != "bill-42" {
		t.Errorf("TargetID = %q, want %q", entry.TargetID, "bill-42")
	}
	if string(entry.Payload) != string(payload) {
		t.Errorf("Payload = %q, want %q", entry.Payload, payload)
	}
	if !entry.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", entry.CreatedAt, created)
	}
}

// TestProfile_ZeroValue_HasAvatarFalse is a defensive assertion: a freshly
// zero-valued Profile (e.g. before any avatar upload) must report
// HasAvatar == false, since the zero value of bool is false and no
// constructor exists in this package to override it.
func TestProfile_ZeroValue_HasAvatarFalse(t *testing.T) {
	var p Profile
	if p.HasAvatar {
		t.Errorf("zero-value Profile.HasAvatar = true, want false")
	}
	if p.DisplayName != "" {
		t.Errorf("zero-value Profile.DisplayName = %q, want empty", p.DisplayName)
	}
	if p.Phone != "" {
		t.Errorf("zero-value Profile.Phone = %q, want empty", p.Phone)
	}
	if p.AvatarContentType != "" {
		t.Errorf("zero-value Profile.AvatarContentType = %q, want empty", p.AvatarContentType)
	}
}

// TestProfile_FieldAssignment exercises the fully populated Profile,
// including the HasAvatar=true path (avatar bytes themselves are
// intentionally not modeled here per the comment at account.go:64-66).
func TestProfile_FieldAssignment(t *testing.T) {
	tenantID := uuid.New()
	updated := time.Date(2026, 3, 15, 9, 0, 0, 0, time.UTC)

	p := Profile{
		TenantID:          tenantID,
		UserID:            "sub-xyz",
		DisplayName:       "Alice",
		Phone:             "+8613800000000",
		AvatarContentType: "image/png",
		HasAvatar:         true,
		UpdatedAt:         updated,
	}

	if p.TenantID != tenantID {
		t.Errorf("TenantID = %v, want %v", p.TenantID, tenantID)
	}
	if p.UserID != "sub-xyz" {
		t.Errorf("UserID = %q, want %q", p.UserID, "sub-xyz")
	}
	if p.DisplayName != "Alice" {
		t.Errorf("DisplayName = %q, want %q", p.DisplayName, "Alice")
	}
	if p.Phone != "+8613800000000" {
		t.Errorf("Phone = %q, want %q", p.Phone, "+8613800000000")
	}
	if p.AvatarContentType != "image/png" {
		t.Errorf("AvatarContentType = %q, want %q", p.AvatarContentType, "image/png")
	}
	if !p.HasAvatar {
		t.Errorf("HasAvatar = false, want true")
	}
	if !p.UpdatedAt.Equal(updated) {
		t.Errorf("UpdatedAt = %v, want %v", p.UpdatedAt, updated)
	}
}
