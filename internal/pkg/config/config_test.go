package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/config"
)

// setEnv sets env vars for duration of test and restores originals on cleanup.
func setEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	orig := make(map[string]string, len(vars))
	for k := range vars {
		orig[k] = os.Getenv(k)
	}
	for k, v := range vars {
		if v == "" {
			os.Unsetenv(k)
		} else {
			os.Setenv(k, v)
		}
	}
	t.Cleanup(func() {
		for k, v := range orig {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	})
}

// fullEnv returns a map with all required env vars set to placeholder values.
// ZITADEL_* are explicitly cleared so auth stays disabled (dev default) unless
// a test opts in — this keeps the suite independent of a polluted local env.
// TALLY_DEV_MODE=true is set because running with auth disabled (empty
// ZITADEL_DOMAIN) now requires an explicit dev-mode opt-in.
func fullEnv() map[string]string {
	return map[string]string{
		"DATABASE_DSN":     "postgres://placeholder:placeholder@localhost/placeholder?sslmode=disable",
		"REDIS_URL":        "redis://localhost:6379/5",
		"NATS_URL":         "nats://localhost:4222",
		"ZITADEL_DOMAIN":   "",
		"ZITADEL_AUDIENCE": "",
		"TALLY_DEV_MODE":   "true",
	}
}

func TestConfig_MissingDatabaseDSN_ReturnsError(t *testing.T) {
	env := fullEnv()
	env["DATABASE_DSN"] = ""
	setEnv(t, env)

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error when DATABASE_DSN is missing, got nil")
	}
	if err.Error() == "" {
		t.Fatal("error message must not be empty")
	}
}

func TestConfig_MissingRedisURL_ReturnsError(t *testing.T) {
	env := fullEnv()
	env["REDIS_URL"] = ""
	setEnv(t, env)

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error when REDIS_URL is missing, got nil")
	}
}

func TestConfig_MissingNATSURL_ReturnsError(t *testing.T) {
	env := fullEnv()
	env["NATS_URL"] = ""
	setEnv(t, env)

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error when NATS_URL is missing, got nil")
	}
}

func TestConfig_AllSet_ReturnsConfig(t *testing.T) {
	setEnv(t, fullEnv())

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil Config")
	}
	if cfg.DatabaseDSN == "" {
		t.Error("DatabaseDSN must not be empty")
	}
	if cfg.RedisURL == "" {
		t.Error("RedisURL must not be empty")
	}
	if cfg.NATSURL == "" {
		t.Error("NATSURL must not be empty")
	}
	// Defaults
	if cfg.Port != "18200" {
		t.Errorf("default Port: want 18200, got %s", cfg.Port)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("default LogLevel: want info, got %s", cfg.LogLevel)
	}
}

func TestConfig_ZitadelDomainSet_RequiresAudience(t *testing.T) {
	env := fullEnv()
	env["ZITADEL_DOMAIN"] = "identity.lurus.cn"
	// ZITADEL_AUDIENCE intentionally left unset.
	setEnv(t, env)

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error when ZITADEL_DOMAIN is set but ZITADEL_AUDIENCE is missing, got nil")
	}
}

func TestConfig_ZitadelDomainAndAudienceSet_ReturnsConfig(t *testing.T) {
	env := fullEnv()
	env["ZITADEL_DOMAIN"] = "identity.lurus.cn"
	env["ZITADEL_AUDIENCE"] = "tally-client-id"
	setEnv(t, env)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error when both ZITADEL_DOMAIN and ZITADEL_AUDIENCE are set, got: %v", err)
	}
	if cfg.ZitadelDomain != "identity.lurus.cn" {
		t.Errorf("ZitadelDomain: want identity.lurus.cn, got %q", cfg.ZitadelDomain)
	}
	if cfg.ZitadelAudience != "tally-client-id" {
		t.Errorf("ZitadelAudience: want tally-client-id, got %q", cfg.ZitadelAudience)
	}
}

func TestConfig_ZitadelDomainEmpty_AudienceOptional(t *testing.T) {
	env := fullEnv()
	// Both ZITADEL_* unset (dev / auth disabled) — Load must succeed.
	setEnv(t, env)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error when ZITADEL_DOMAIN is empty, got: %v", err)
	}
	if cfg.ZitadelAudience != "" {
		t.Errorf("ZitadelAudience: want empty when auth disabled, got %q", cfg.ZitadelAudience)
	}
}

func TestConfig_AuthDisabledWithoutDevMode_ReturnsError(t *testing.T) {
	env := fullEnv()
	// Auth disabled (no ZITADEL_DOMAIN) AND no explicit dev-mode opt-in: this is
	// the misconfiguration the gate must catch before it reaches stage/prod.
	env["ZITADEL_DOMAIN"] = ""
	env["TALLY_DEV_MODE"] = ""
	setEnv(t, env)

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error when auth is disabled without TALLY_DEV_MODE=true, got nil")
	}
}

func TestConfig_AuthDisabledWithDevMode_ReturnsConfig(t *testing.T) {
	env := fullEnv()
	env["ZITADEL_DOMAIN"] = ""
	env["TALLY_DEV_MODE"] = "true"
	setEnv(t, env)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error when TALLY_DEV_MODE=true permits auth-disabled boot, got: %v", err)
	}
	if cfg.ZitadelDomain != "" {
		t.Errorf("ZitadelDomain: want empty in dev mode, got %q", cfg.ZitadelDomain)
	}
}

func TestConfig_MigrateOnBoot_DefaultTrue(t *testing.T) {
	env := fullEnv()
	env["MIGRATE_ON_BOOT"] = ""
	setEnv(t, env)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !cfg.MigrateOnBoot {
		t.Error("MigrateOnBoot: want true (default), got false")
	}
}

func TestConfig_MigrateOnBoot_FalseWhenSet(t *testing.T) {
	env := fullEnv()
	env["MIGRATE_ON_BOOT"] = "false"
	setEnv(t, env)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.MigrateOnBoot {
		t.Error("MigrateOnBoot: want false when MIGRATE_ON_BOOT=false, got true")
	}
}

func TestConfig_ShutdownTimeout_DefaultIs5s(t *testing.T) {
	env := fullEnv()
	env["SHUTDOWN_TIMEOUT"] = ""
	setEnv(t, env)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.ShutdownTimeout != 5*time.Second {
		t.Errorf("default ShutdownTimeout: want 5s, got %s", cfg.ShutdownTimeout)
	}
}

func TestConfig_ShutdownTimeout_CustomValueParsed(t *testing.T) {
	env := fullEnv()
	env["SHUTDOWN_TIMEOUT"] = "30s"
	setEnv(t, env)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.ShutdownTimeout != 30*time.Second {
		t.Errorf("ShutdownTimeout: want 30s, got %s", cfg.ShutdownTimeout)
	}
}

func TestConfig_ShutdownTimeout_InvalidReturnsError(t *testing.T) {
	env := fullEnv()
	env["SHUTDOWN_TIMEOUT"] = "not-a-duration"
	setEnv(t, env)

	if _, err := config.Load(); err == nil {
		t.Fatal("expected error when SHUTDOWN_TIMEOUT is not a valid duration, got nil")
	}
}
