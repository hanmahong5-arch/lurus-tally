package config_test

import (
	"os"
	"testing"

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
func fullEnv() map[string]string {
	return map[string]string{
		"DATABASE_DSN": "postgres://placeholder:placeholder@localhost/placeholder?sslmode=disable",
		"REDIS_URL":    "redis://localhost:6379/5",
		"NATS_URL":     "nats://localhost:4222",
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
