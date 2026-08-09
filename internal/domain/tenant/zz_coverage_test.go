package tenant

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestNewTenantProfile_ValidTypes_Behavior asserts business invariant: for each
// user-selectable ProfileType, NewTenantProfile returns a profile with the
// correct default InventoryMethod (hand-computed, not read back from the
// function under test), a non-nil UUID, tenant_id passthrough, and
// CreatedAt==UpdatedAt in UTC.
func TestNewTenantProfile_ValidTypes_Behavior(t *testing.T) {
	tests := []struct {
		name       string
		in         ProfileType
		wantMethod InventoryMethod
	}{
		// cross_border is the only profile that defaults to FIFO costing.
		{name: "cross_border defaults to FIFO", in: ProfileTypeCrossBorder, wantMethod: InventoryMethodFIFO},
		// retail and horticulture fall through defaultInventoryMethod's default branch (WAC).
		{name: "retail defaults to WAC", in: ProfileTypeRetail, wantMethod: InventoryMethodWAC},
		{name: "horticulture defaults to WAC", in: ProfileTypeHorticulture, wantMethod: InventoryMethodWAC},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tenantID := uuid.New()

			before := time.Now().UTC()
			profile, err := NewTenantProfile(tenantID, tc.in)
			after := time.Now().UTC()

			if err != nil {
				t.Fatalf("NewTenantProfile(%q) returned unexpected error: %v", tc.in, err)
			}
			if profile == nil {
				t.Fatalf("NewTenantProfile(%q) returned nil profile with nil error", tc.in)
			}

			if profile.ID == uuid.Nil {
				t.Error("profile.ID must not be uuid.Nil")
			}
			if profile.TenantID != tenantID {
				t.Errorf("profile.TenantID = %v, want %v (passthrough)", profile.TenantID, tenantID)
			}
			if profile.ProfileType != tc.in {
				t.Errorf("profile.ProfileType = %v, want %v", profile.ProfileType, tc.in)
			}
			if profile.InventoryMethod != tc.wantMethod {
				t.Errorf("profile.InventoryMethod = %v, want %v", profile.InventoryMethod, tc.wantMethod)
			}

			if !profile.CreatedAt.Equal(profile.UpdatedAt) {
				t.Errorf("CreatedAt (%v) must equal UpdatedAt (%v) on creation", profile.CreatedAt, profile.UpdatedAt)
			}
			if profile.CreatedAt.Location() != time.UTC {
				t.Errorf("profile.CreatedAt.Location() = %v, want time.UTC", profile.CreatedAt.Location())
			}
			if profile.UpdatedAt.Location() != time.UTC {
				t.Errorf("profile.UpdatedAt.Location() = %v, want time.UTC", profile.UpdatedAt.Location())
			}

			// Sanity bound: timestamp must fall within the call window (not zero/stale).
			if profile.CreatedAt.Before(before) || profile.CreatedAt.After(after) {
				t.Errorf("profile.CreatedAt = %v, want within [%v, %v]", profile.CreatedAt, before, after)
			}
		})
	}
}

// TestNewTenantProfile_Hybrid_NotUserSelectable asserts the core business
// invariant: hybrid is admin-only and must never be constructible via
// NewTenantProfile, even though it is a valid ProfileType constant.
func TestNewTenantProfile_Hybrid_NotUserSelectable(t *testing.T) {
	profile, err := NewTenantProfile(uuid.New(), ProfileTypeHybrid)

	if !errors.Is(err, ErrInvalidProfileType) {
		t.Fatalf("NewTenantProfile(hybrid) error = %v, want ErrInvalidProfileType", err)
	}
	if profile != nil {
		t.Fatalf("NewTenantProfile(hybrid) profile = %+v, want nil", profile)
	}
}

// TestNewTenantProfile_InvalidTypes_ReturnsError covers every invalid-input
// branch: empty string, unknown garbage string, and hybrid (redundant with
// the dedicated hybrid test above but kept here for table completeness).
func TestNewTenantProfile_InvalidTypes_ReturnsError(t *testing.T) {
	tests := []struct {
		name string
		in   ProfileType
	}{
		{name: "empty string", in: ProfileType("")},
		{name: "unknown garbage string", in: ProfileType("unknown")},
		{name: "hybrid reserved", in: ProfileTypeHybrid},
		{name: "whitespace", in: ProfileType(" ")},
		{name: "case mismatch", in: ProfileType("Retail")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			profile, err := NewTenantProfile(uuid.New(), tc.in)

			if !errors.Is(err, ErrInvalidProfileType) {
				t.Errorf("NewTenantProfile(%q) error = %v, want ErrInvalidProfileType", tc.in, err)
			}
			if profile != nil {
				t.Errorf("NewTenantProfile(%q) profile = %+v, want nil", tc.in, profile)
			}
		})
	}
}

// TestNewTenantProfile_NilTenantID confirms a nil (zero-value) tenant UUID is
// passed through unchanged — NewTenantProfile does not validate TenantID
// itself, only ProfileType.
func TestNewTenantProfile_NilTenantID(t *testing.T) {
	profile, err := NewTenantProfile(uuid.Nil, ProfileTypeRetail)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.TenantID != uuid.Nil {
		t.Errorf("profile.TenantID = %v, want uuid.Nil (passthrough, not validated)", profile.TenantID)
	}
}

// TestDefaultInventoryMethod_TableDriven directly exercises
// defaultInventoryMethod's switch statement, covering the single explicit
// case (cross_border -> FIFO) and multiple inputs that all land in the
// default branch (-> WAC), including inputs that are not valid ProfileType
// constants at all.
func TestDefaultInventoryMethod_TableDriven(t *testing.T) {
	tests := []struct {
		name string
		in   ProfileType
		want InventoryMethod
	}{
		{name: "cross_border -> FIFO", in: ProfileTypeCrossBorder, want: InventoryMethodFIFO},
		{name: "retail -> WAC (default branch)", in: ProfileTypeRetail, want: InventoryMethodWAC},
		{name: "horticulture -> WAC (default branch)", in: ProfileTypeHorticulture, want: InventoryMethodWAC},
		{name: "hybrid -> WAC (default branch)", in: ProfileTypeHybrid, want: InventoryMethodWAC},
		{name: "empty string -> WAC (default branch)", in: ProfileType(""), want: InventoryMethodWAC},
		{name: "unknown -> WAC (default branch)", in: ProfileType("unknown"), want: InventoryMethodWAC},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := defaultInventoryMethod(tc.in)
			if got != tc.want {
				t.Errorf("defaultInventoryMethod(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestIsUserSelectableProfile_TableDriven asserts the business invariant:
// only cross_border, retail, and horticulture are user-selectable; hybrid
// and any unknown/empty string must report false.
func TestIsUserSelectableProfile_TableDriven(t *testing.T) {
	tests := []struct {
		name string
		in   ProfileType
		want bool
	}{
		{name: "cross_border is selectable", in: ProfileTypeCrossBorder, want: true},
		{name: "retail is selectable", in: ProfileTypeRetail, want: true},
		{name: "horticulture is selectable", in: ProfileTypeHorticulture, want: true},
		{name: "hybrid is NOT selectable (admin-only)", in: ProfileTypeHybrid, want: false},
		{name: "empty string is NOT selectable", in: ProfileType(""), want: false},
		{name: "garbage string is NOT selectable", in: ProfileType("zzz-not-a-real-profile"), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsUserSelectableProfile(tc.in)
			if got != tc.want {
				t.Errorf("IsUserSelectableProfile(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestNewTenantProfile_ConcurrentCalls exercises NewTenantProfile under
// concurrent access (-race safety). The function has no shared mutable
// state, so this also acts as a regression guard should one be introduced.
func TestNewTenantProfile_ConcurrentCalls(t *testing.T) {
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	ids := make([]uuid.UUID, goroutines)
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			profile, err := NewTenantProfile(uuid.New(), ProfileTypeRetail)
			errs[idx] = err
			if profile != nil {
				ids[idx] = profile.ID
			}
		}(i)
	}
	wg.Wait()

	seen := make(map[uuid.UUID]bool, goroutines)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: unexpected error: %v", i, err)
		}
		if ids[i] == uuid.Nil {
			t.Fatalf("goroutine %d: profile.ID is nil", i)
		}
		if seen[ids[i]] {
			t.Fatalf("goroutine %d: duplicate ID generated: %v", i, ids[i])
		}
		seen[ids[i]] = true
	}
}

// TestErrorSentinels_AreDistinctAndWrappable guards the two sentinel errors:
// they must be distinguishable via errors.Is and carry the documented
// message content used by callers/handlers.
func TestErrorSentinels_AreDistinctAndWrappable(t *testing.T) {
	if errors.Is(ErrProfileAlreadySet, ErrInvalidProfileType) {
		t.Fatal("ErrProfileAlreadySet and ErrInvalidProfileType must be distinct sentinels")
	}

	wrapped := errors.Join(errors.New("context"), ErrInvalidProfileType)
	if !errors.Is(wrapped, ErrInvalidProfileType) {
		t.Fatal("ErrInvalidProfileType must remain matchable through errors.Is after wrapping")
	}
}
