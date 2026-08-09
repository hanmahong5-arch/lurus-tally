package horticulture_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/horticulture"
)

// TestParseNurseryType_Table covers every valid enum value plus invalid inputs
// (empty string and an arbitrary unknown token), per dict.go:33-41.
func TestParseNurseryType_Table(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    domain.NurseryType
		wantErr bool
	}{
		{"tree", "tree", domain.NurseryTypeTree, false},
		{"shrub", "shrub", domain.NurseryTypeShrub, false},
		{"herb", "herb", domain.NurseryTypeHerb, false},
		{"vine", "vine", domain.NurseryTypeVine, false},
		{"bamboo", "bamboo", domain.NurseryTypeBamboo, false},
		{"aquatic", "aquatic", domain.NurseryTypeAquatic, false},
		{"bulb", "bulb", domain.NurseryTypeBulb, false},
		{"fruit", "fruit", domain.NurseryTypeFruit, false},
		{"empty", "", "", true},
		{"unknown", "bogus", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := domain.ParseNurseryType(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseNurseryType(%q): expected error, got nil", tc.in)
				}
				if !strings.Contains(err.Error(), "invalid nursery type") {
					t.Errorf("ParseNurseryType(%q) error = %q, want to contain %q", tc.in, err.Error(), "invalid nursery type")
				}
				if got != "" {
					t.Errorf("ParseNurseryType(%q) value = %q, want empty on error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseNurseryType(%q): unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseNurseryType(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestNurseryType_String_EmptyAndArbitrary rounds out String() beyond the
// already-tested valid enum values (dict.go:28-30): empty and non-enum values
// must still stringify to their underlying representation.
func TestNurseryType_String_EmptyAndArbitrary(t *testing.T) {
	if got := domain.NurseryType("").String(); got != "" {
		t.Errorf("empty NurseryType.String() = %q, want empty", got)
	}
	if got := domain.NurseryType("whatever").String(); got != "whatever" {
		t.Errorf("NurseryType(\"whatever\").String() = %q, want %q", got, "whatever")
	}
}

// TestNurseryDict_Validate_BestSeasonBoundaries exercises the BestSeason
// invariant (dict.go:71-76) across the sentinel, the fully valid boundary,
// and each way a single field can fall outside [1,12].
func TestNurseryDict_Validate_BestSeasonBoundaries(t *testing.T) {
	cases := []struct {
		name       string
		season     [2]int
		wantErr    bool
	}{
		{"sentinel unset [0,0]", [2]int{0, 0}, false},
		{"full valid range [1,12]", [2]int{1, 12}, false},
		{"start zero, end mid [0,5]", [2]int{0, 5}, true},
		{"start valid, end over max [13,1]", [2]int{13, 1}, true},
		{"start valid, end over max [1,13]", [2]int{1, 13}, true},
		{"start negative [-1,6]", [2]int{-1, 6}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &domain.NurseryDict{
				Name:       "测试植物",
				BestSeason: tc.season,
			}
			err := d.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Validate() with BestSeason=%v: expected error, got nil", tc.season)
				}
				want := "best_season month must be between 1 and 12"
				if err.Error() != want {
					t.Errorf("Validate() with BestSeason=%v error = %q, want %q", tc.season, err.Error(), want)
				}
				return
			}
			if err != nil {
				t.Errorf("Validate() with BestSeason=%v: unexpected error: %v", tc.season, err)
			}
		})
	}
}

// TestNurseryDict_Validate_TypeField covers dict.go:77-81: an empty Type is
// skipped entirely (no validation), while a non-empty invalid Type must
// surface a wrapped ParseNurseryType error.
func TestNurseryDict_Validate_TypeField(t *testing.T) {
	t.Run("empty type is skipped", func(t *testing.T) {
		d := &domain.NurseryDict{
			Name: "空类型植物",
			Type: "",
		}
		if err := d.Validate(); err != nil {
			t.Errorf("expected no error for empty Type, got: %v", err)
		}
	})

	t.Run("invalid type wraps ParseNurseryType error", func(t *testing.T) {
		d := &domain.NurseryDict{
			Name: "坏类型植物",
			Type: domain.NurseryType("bogus"),
		}
		err := d.Validate()
		if err == nil {
			t.Fatal("expected error for invalid Type, got nil")
		}
		want := `invalid type: invalid nursery type: "bogus"`
		if err.Error() != want {
			t.Errorf("Validate() error = %q, want %q", err.Error(), want)
		}
	})

	t.Run("valid type passes", func(t *testing.T) {
		d := &domain.NurseryDict{
			Name: "合法类型植物",
			Type: domain.NurseryTypeShrub,
		}
		if err := d.Validate(); err != nil {
			t.Errorf("expected no error for valid Type, got: %v", err)
		}
	})
}

// TestNurseryDict_Validate_SharedSeedTenantNil documents the multi-tenant
// visibility rule at dict.go:44: TenantID == uuid.Nil denotes a shared seed
// entry, and Validate must not require a non-nil TenantID for it to pass.
func TestNurseryDict_Validate_SharedSeedTenantNil(t *testing.T) {
	d := &domain.NurseryDict{
		TenantID: uuid.Nil,
		Name:     "共享种子植物",
		Type:     domain.NurseryTypeBulb,
	}
	if d.TenantID != uuid.Nil {
		t.Fatalf("test setup broken: TenantID = %v, want uuid.Nil", d.TenantID)
	}
	if err := d.Validate(); err != nil {
		t.Errorf("shared seed (TenantID=uuid.Nil) must pass Validate without requiring TenantID, got error: %v", err)
	}
}

// TestNurseryDict_Validate_AllInvariantsTogether is a happy-path smoke test
// combining every field Validate touches, to guard against regressions that
// only manifest when multiple fields are populated simultaneously.
func TestNurseryDict_Validate_AllInvariantsTogether(t *testing.T) {
	d := &domain.NurseryDict{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		Name:        "五角枫",
		LatinName:   "Acer mono",
		Type:        domain.NurseryTypeTree,
		IsEvergreen: false,
		BestSeason:  [2]int{3, 10},
	}
	if err := d.Validate(); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}
