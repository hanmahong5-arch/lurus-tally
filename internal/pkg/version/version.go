// Package version exposes the build-time version string.
// The Version variable is overridden at link time via:
//
//	-ldflags "-X github.com/hanmahong5-arch/lurus-tally/internal/pkg/version.Version=<tag>"
package version

// Version is the service version, injected at build time via -ldflags.
// Falls back to "dev" when not provided.
var Version = "dev"

// Get returns the current build version string.
func Get() string {
	return Version
}
