// Package config loads and validates service configuration from environment variables.
// All required fields must be set before the service starts; missing fields cause Load to return
// a descriptive error so operators know exactly what to fix.
package config

import (
	"fmt"
	"os"
)

// Config holds all runtime configuration for lurus-tally.
// Required fields must be set via environment variables before the service can start.
type Config struct {
	// Required
	DatabaseDSN string // DATABASE_DSN: PostgreSQL connection string
	RedisURL    string // REDIS_URL: Redis connection URL
	NATSURL     string // NATS_URL: NATS server URL

	// Optional with defaults
	Port            string // PORT: HTTP listen port (default "18200")
	LogLevel        string // LOG_LEVEL: log verbosity debug|info|warn|error (default "info")
	GinMode         string // GIN_MODE: gin mode release|debug (default "release")
	ServiceVersion  string // SERVICE_VERSION: build version label (default "dev")
	ShutdownTimeout string // SHUTDOWN_TIMEOUT: graceful shutdown deadline (default "5s")
	MigrateOnBoot   bool   // MIGRATE_ON_BOOT: run migrations on startup, default true

	// Billing — Tally calls into 2l-svc-platform /internal/v1/* for subscription
	// checkout. When PlatformInternalKey is empty the billing routes return 503
	// instead of failing fast at boot so dev clusters without platform stay
	// bootable.
	PlatformBaseURL     string // PLATFORM_BASE_URL: e.g. http://platform-core.lurus-platform.svc:18104
	PlatformInternalKey string // PLATFORM_INTERNAL_KEY: bearer token for platform internal API
}

// required reads an environment variable and returns a descriptive error when absent.
func required(name, hint string) (string, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf(
			"%s is required: set it to %s (e.g. %s)",
			name, hint, hint,
		)
	}
	return v, nil
}

// optional reads an environment variable and returns the default value when absent.
func optional(name, defaultVal string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return defaultVal
}

// Load reads configuration from the environment, validates all required fields,
// and returns a fully populated Config or a descriptive error.
func Load() (*Config, error) {
	dbDSN, err := required("DATABASE_DSN",
		"PostgreSQL DSN (e.g. postgres://user:pass@host/dbname?sslmode=disable)")
	if err != nil {
		return nil, err
	}

	redisURL, err := required("REDIS_URL",
		"Redis URL (e.g. redis://localhost:6379/5)")
	if err != nil {
		return nil, err
	}

	natsURL, err := required("NATS_URL",
		"NATS URL (e.g. nats://localhost:4222)")
	if err != nil {
		return nil, err
	}

	return &Config{
		DatabaseDSN:     dbDSN,
		RedisURL:        redisURL,
		NATSURL:         natsURL,
		Port:            optional("PORT", "18200"),
		LogLevel:        optional("LOG_LEVEL", "info"),
		GinMode:         optional("GIN_MODE", "release"),
		ServiceVersion:  optional("SERVICE_VERSION", "dev"),
		ShutdownTimeout: optional("SHUTDOWN_TIMEOUT", "5s"),
		MigrateOnBoot:   optional("MIGRATE_ON_BOOT", "true") != "false",
		PlatformBaseURL: optional("PLATFORM_BASE_URL",
			"http://platform-core.lurus-platform.svc:18104"),
		PlatformInternalKey: optional("PLATFORM_INTERNAL_KEY", ""),
	}, nil
}
