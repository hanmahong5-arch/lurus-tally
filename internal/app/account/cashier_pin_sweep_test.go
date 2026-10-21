package account

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPinAttempts_ExpiredEntriesAreSwept(t *testing.T) {
	uc := NewSetCashierMode(nil)
	t0 := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	stale := pinKey{uuid.New(), "gone"}
	if !uc.reserve(stale, t0) {
		t.Fatal("first attempt must be counted")
	}
	if len(uc.tries) != 1 {
		t.Fatalf("tries = %d, want 1", len(uc.tries))
	}
	// A later attempt by someone else, past the lock window, sweeps the idle entry.
	other := pinKey{uuid.New(), "here"}
	uc.reserve(other, t0.Add(pinLockFor+2*time.Minute))
	if _, ok := uc.tries[stale]; ok {
		t.Fatal("idle entry was not swept")
	}
	if _, ok := uc.tries[other]; !ok {
		t.Fatal("live entry must stay")
	}
}

func TestPinAttempts_LockedEntryNotSweptEarly(t *testing.T) {
	uc := NewSetCashierMode(nil)
	t0 := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	k := pinKey{uuid.New(), "locked"}
	for i := 0; i < pinMaxFails; i++ {
		uc.reserve(k, t0)
	}
	uc.reserve(pinKey{uuid.New(), "x"}, t0.Add(2*time.Minute))
	if _, ok := uc.tries[k]; !ok {
		t.Fatal("entry still locked must be kept")
	}
	if uc.reserve(k, t0.Add(2*time.Minute)) {
		t.Fatal("must still be locked")
	}
}
