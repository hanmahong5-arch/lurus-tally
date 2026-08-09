package version

import "testing"

// TestGet_DefaultIsDev asserts the package-level default: without ldflags
// overriding Version, Get() must return "dev" (see version.go var Version = "dev").
func TestGet_DefaultIsDev(t *testing.T) {
	// Guard: if a previous test in this run mutated Version and failed to
	// restore it, this test's premise (default value) would be violated.
	// Save/restore keeps this test isolated regardless of run order.
	original := Version
	defer func() { Version = original }()

	Version = "dev"

	if got := Get(); got != "dev" {
		t.Fatalf("Get() = %q, want %q (default)", got, "dev")
	}
}

// TestGet_ReflectsReassignedVersion proves Get() reads the live var Version
// rather than a captured/constant value at build time.
func TestGet_ReflectsReassignedVersion(t *testing.T) {
	original := Version
	defer func() { Version = original }()

	tests := []struct {
		name string
		set  string
		want string
	}{
		{name: "semver tag", set: "v1.2.3", want: "v1.2.3"},
		{name: "empty string", set: "", want: ""},
		{name: "sha-based tag", set: "main-abc1234", want: "main-abc1234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Version = tt.set
			if got := Get(); got != tt.want {
				t.Fatalf("Get() = %q, want %q after Version = %q", got, tt.want, tt.set)
			}
		})
	}
}
