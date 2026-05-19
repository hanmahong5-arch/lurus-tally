package tenant

import (
	"testing"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
)

// TestDemoEntityNames pins the profile → preset-names mapping so a future
// silent change to either name (which would surprise a customer mid-onboarding)
// requires updating the test, not just the table. Internal test (same package)
// because demoEntityNames is unexported by design — these names are an
// implementation detail of Bootstrap, not a public contract.
func TestDemoEntityNames(t *testing.T) {
	tests := []struct {
		name        string
		profileType domain.ProfileType
		wantWh      string
		wantSup     string
	}{
		{"retail preset", "retail", "门店仓", "现金采购"},
		{"cross_border preset", "cross_border", "海外仓", "默认供应商"},
		{"horticulture preset", "horticulture", "苗圃仓", "苗木供应商"},
		{"hybrid falls through to generic", "hybrid", "主仓库", "默认供应商"},
		{"empty falls through to generic", "", "主仓库", "默认供应商"},
		{"unknown future profile falls through", "warehousing", "主仓库", "默认供应商"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotWh, gotSup := demoEntityNames(tc.profileType)
			if gotWh != tc.wantWh {
				t.Errorf("warehouse: got %q, want %q", gotWh, tc.wantWh)
			}
			if gotSup != tc.wantSup {
				t.Errorf("supplier: got %q, want %q", gotSup, tc.wantSup)
			}
		})
	}
}
