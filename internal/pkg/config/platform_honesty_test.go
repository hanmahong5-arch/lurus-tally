// Honesty lock test (TALLY-04, config link) — converts the verified claim
//
//	"env PLATFORM_INTERNAL_KEY + PLATFORM_BASE_URL 默认 svc:18104; 空→501"
//
// into a contract on the FIRST link of the env→nil-client→501 chain: config.
// The router-layer 501 contract is locked in router/billing_honesty_test.go;
// here we lock that when PLATFORM_INTERNAL_KEY is unset, config.Load yields an
// EMPTY key (which lifecycle/app.go turns into a nil platform client → nil
// billing handler → 501), and that PLATFORM_BASE_URL defaults to the documented
// in-cluster address platform-core.lurus-platform.svc:18104.
//
// Reuses setEnv + fullEnv from config_test.go (same config_test package).
// config.Load only reads env — it opens no DB/Redis/NATS connection — so these
// tests are hermetic.
package config_test

import (
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/config"
)

// expectedPlatformDefaultBaseURL is the documented in-cluster billing address.
// Asserting the exact literal pins the "默认 svc:18104" half of the claim.
const expectedPlatformDefaultBaseURL = "http://platform-core.lurus-platform.svc:18104"

// TestConfig_PlatformKeyEmpty_DefaultBaseURL locks "空 key" + "默认 svc:18104":
// with PLATFORM_INTERNAL_KEY / PLATFORM_BASE_URL unset, the key is empty (the
// trigger for the downstream nil-client → 501 degradation) and the base URL
// falls back to the documented default.
func TestConfig_PlatformKeyEmpty_DefaultBaseURL(t *testing.T) {
	env := fullEnv()
	env["PLATFORM_INTERNAL_KEY"] = "" // unset → empty
	env["PLATFORM_BASE_URL"] = ""     // unset → default
	setEnv(t, env)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PlatformInternalKey != "" {
		t.Errorf("PlatformInternalKey: want empty (→ nil client → 501), got non-empty")
	}
	if cfg.PlatformBaseURL != expectedPlatformDefaultBaseURL {
		t.Errorf("PlatformBaseURL default: want %q, got %q", expectedPlatformDefaultBaseURL, cfg.PlatformBaseURL)
	}
}

// TestConfig_PlatformKeySet_Propagates locks the enabled path: when both env
// vars are set, config.Load surfaces them unchanged (this is what produces a
// non-nil platform client → billing routes served, not 501).
func TestConfig_PlatformKeySet_Propagates(t *testing.T) {
	env := fullEnv()
	env["PLATFORM_INTERNAL_KEY"] = "test-internal-key" // non-secret placeholder
	env["PLATFORM_BASE_URL"] = "http://platform.test.local:18104"
	setEnv(t, env)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PlatformInternalKey != "test-internal-key" {
		t.Errorf("PlatformInternalKey not propagated: got %q", cfg.PlatformInternalKey)
	}
	if cfg.PlatformBaseURL != "http://platform.test.local:18104" {
		t.Errorf("PlatformBaseURL not propagated: got %q", cfg.PlatformBaseURL)
	}
}
