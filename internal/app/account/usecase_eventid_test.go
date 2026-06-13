package account_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	account "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/account"
)

// captureAuditRepo records the last AuditEntry handed to Append so we can assert
// the use case threads EventID through to the persistence layer.
type captureAuditRepo struct{ last *domain.AuditEntry }

func (c *captureAuditRepo) Append(_ context.Context, e *domain.AuditEntry) error {
	c.last = e
	return nil
}

func (c *captureAuditRepo) List(_ context.Context, _ uuid.UUID, _, _ int) ([]*domain.AuditEntry, int, error) {
	return nil, 0, nil
}

// TestAppendAuditLog_PropagatesEventID proves the dedup key supplied on the
// input reaches the domain entry (and thus the ON CONFLICT insert). Without
// this thread, redelivered events could never be de-duplicated.
func TestAppendAuditLog_PropagatesEventID(t *testing.T) {
	repo := &captureAuditRepo{}
	uc := account.NewAppendAuditLog(repo)

	in := account.AppendInput{
		TenantID: uuid.New(),
		ActorID:  "tally",
		Action:   "bill.created",
		EventID:  "evt-abc-123",
	}
	if err := uc.Execute(context.Background(), in); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.last == nil {
		t.Fatal("Append was not called")
	}
	if repo.last.EventID != "evt-abc-123" {
		t.Errorf("EventID: got %q, want %q", repo.last.EventID, "evt-abc-123")
	}
}

// TestAppendAuditLog_EmptyEventID_StaysEmpty proves synchronous writes (no NATS
// envelope) carry an empty dedup key, so they keep their insert-always
// behaviour (a NULL event_id never conflicts).
func TestAppendAuditLog_EmptyEventID_StaysEmpty(t *testing.T) {
	repo := &captureAuditRepo{}
	uc := account.NewAppendAuditLog(repo)

	if err := uc.Execute(context.Background(), account.AppendInput{
		TenantID: uuid.New(),
		Action:   "pat.created",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.last.EventID != "" {
		t.Errorf("EventID: got %q, want empty", repo.last.EventID)
	}
}
