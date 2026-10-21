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

// TestDefaultUnitsFor pins the per-profile unit presets: nursery tenants need
// 株/棵/丛/袋/平方米 on day one, retail tenants the hardware-store set. Codes must be
// unique within a preset (UNIQUE(tenant_id, code) on unit_def) and avoid the
// system codes (pcs/box/kg/g/m/cm/l) that every tenant already sees.
func TestDefaultUnitsFor(t *testing.T) {
	names := func(us []defaultUnit) []string {
		out := make([]string, 0, len(us))
		for _, u := range us {
			out = append(out, u.Name)
		}
		return out
	}
	tests := []struct {
		pt   domain.ProfileType
		want []string
	}{
		{"horticulture", []string{"株", "棵", "丛", "袋", "平方米"}},
		{"retail", []string{"个", "把", "根", "盒", "套", "包", "卷"}},
		{"cross_border", nil},
		{"hybrid", nil},
		{"", nil},
	}
	system := map[string]bool{"pcs": true, "box": true, "kg": true, "g": true, "m": true, "cm": true, "l": true}
	for _, tc := range tests {
		t.Run(string(tc.pt), func(t *testing.T) {
			us := defaultUnitsFor(tc.pt)
			got := names(us)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			seen := map[string]bool{}
			for i, u := range us {
				if got[i] != tc.want[i] {
					t.Errorf("unit %d: got %q, want %q", i, got[i], tc.want[i])
				}
				if u.Code == "" || seen[u.Code] || system[u.Code] {
					t.Errorf("unit %q: bad code %q", u.Name, u.Code)
				}
				seen[u.Code] = true
			}
		})
	}
}
