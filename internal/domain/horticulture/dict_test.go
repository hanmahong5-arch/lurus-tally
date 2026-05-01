package horticulture_test

import (
	"testing"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/horticulture"
)

func TestNurseryDict_Validate_RejectsEmptyName(t *testing.T) {
	d := &domain.NurseryDict{
		TenantID: uuid.New(),
		Name:     "",
		Type:     domain.NurseryTypeTree,
	}
	err := d.Validate()
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
	if err.Error() != "name is required" {
		t.Errorf("expected 'name is required', got %q", err.Error())
	}
}

func TestNurseryDict_Validate_RejectsBadSeason(t *testing.T) {
	d := &domain.NurseryDict{
		TenantID:   uuid.New(),
		Name:       "红枫",
		Type:       domain.NurseryTypeTree,
		BestSeason: [2]int{13, 2},
	}
	err := d.Validate()
	if err == nil {
		t.Fatal("expected error for bad season, got nil")
	}
	got := err.Error()
	want := "best_season month must be between 1 and 12"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestNurseryType_String_AllValuesRoundtrip(t *testing.T) {
	types := []domain.NurseryType{
		domain.NurseryTypeTree,
		domain.NurseryTypeShrub,
		domain.NurseryTypeHerb,
		domain.NurseryTypeVine,
		domain.NurseryTypeBamboo,
		domain.NurseryTypeAquatic,
		domain.NurseryTypeBulb,
		domain.NurseryTypeFruit,
	}
	for _, nt := range types {
		s := nt.String()
		parsed, err := domain.ParseNurseryType(s)
		if err != nil {
			t.Errorf("ParseNurseryType(%q) error: %v", s, err)
			continue
		}
		if parsed != nt {
			t.Errorf("roundtrip failed: got %q, want %q", parsed, nt)
		}
	}
}

func TestNurseryDict_Validate_AcceptsUnsetSeason(t *testing.T) {
	d := &domain.NurseryDict{
		TenantID:   uuid.New(),
		Name:       "银杏",
		Type:       domain.NurseryTypeTree,
		BestSeason: [2]int{0, 0}, // zero value = unset
	}
	if err := d.Validate(); err != nil {
		t.Errorf("expected no error for unset season [0,0], got: %v", err)
	}
}
