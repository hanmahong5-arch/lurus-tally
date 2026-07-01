package config_test

import (
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/config"
)

// TestLoad exercises config.Load() end to end across its decision matrix:
// required-field enforcement, default application, and the auth invariants
// (audience required once the OIDC/Zitadel domain is set; auth-disabled boot
// needs an explicit TALLY_DEV_MODE opt-in). It is named after the function under
// test so `go test ./internal/pkg/config/... -run TestLoad` drives the loader
// directly (the older TestConfig_* cases cover the same contract case-by-case;
// this is the consolidated, filter-addressable entry point). Reuses setEnv /
// fullEnv from config_test.go (same external test package).
func TestLoad(t *testing.T) {
	t.Run("AllRequiredSet_AppliesDefaults", func(t *testing.T) {
		setEnv(t, fullEnv())
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg == nil {
			t.Fatal("expected non-nil config")
		}
		if cfg.Port != "18200" {
			t.Errorf("Port default: got %q, want 18200", cfg.Port)
		}
		if cfg.LogLevel != "info" {
			t.Errorf("LogLevel default: got %q, want info", cfg.LogLevel)
		}
		if !cfg.MigrateOnBoot {
			t.Error("MigrateOnBoot default: want true")
		}
	})

	t.Run("MissingRequiredField_Errors", func(t *testing.T) {
		for _, key := range []string{"DATABASE_DSN", "REDIS_URL", "NATS_URL"} {
			env := fullEnv()
			env[key] = ""
			setEnv(t, env)
			if _, err := config.Load(); err == nil {
				t.Errorf("missing %s: expected error, got nil", key)
			}
		}
	})

	t.Run("DomainSetRequiresAudience", func(t *testing.T) {
		env := fullEnv()
		env["ZITADEL_DOMAIN"] = "auth.lurus.cn" // audience left empty on purpose
		setEnv(t, env)
		if _, err := config.Load(); err == nil {
			t.Fatal("domain set without audience: expected fail-fast error, got nil")
		}
	})

	t.Run("DomainAndAudienceSet_OK", func(t *testing.T) {
		env := fullEnv()
		env["ZITADEL_DOMAIN"] = "auth.lurus.cn"
		env["ZITADEL_AUDIENCE"] = "tally-client-id"
		setEnv(t, env)
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.ZitadelAudience != "tally-client-id" {
			t.Errorf("ZitadelAudience: got %q, want tally-client-id", cfg.ZitadelAudience)
		}
	})

	t.Run("AuthDisabledWithoutDevMode_Errors", func(t *testing.T) {
		env := fullEnv()
		env["ZITADEL_DOMAIN"] = ""
		env["TALLY_DEV_MODE"] = ""
		setEnv(t, env)
		if _, err := config.Load(); err == nil {
			t.Fatal("auth disabled without TALLY_DEV_MODE=true: expected error, got nil")
		}
	})

	t.Run("AuthDisabledWithDevMode_OK", func(t *testing.T) {
		env := fullEnv()
		env["ZITADEL_DOMAIN"] = ""
		env["TALLY_DEV_MODE"] = "true"
		setEnv(t, env)
		if _, err := config.Load(); err != nil {
			t.Fatalf("dev-mode auth-disabled boot: unexpected error %v", err)
		}
	})
}
