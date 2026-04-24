package lifecycle_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-tally/internal/lifecycle"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/config"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// freePort returns an available TCP port on localhost.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not find free port: %v", err)
	}
	port := fmt.Sprintf("%d", l.Addr().(*net.TCPAddr).Port)
	l.Close()
	return port
}

// testConfig builds a minimal Config pointing to a random free port.
func testConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		DatabaseDSN:     "postgres://placeholder",
		RedisURL:        "redis://placeholder",
		NATSURL:         "nats://placeholder",
		Port:            freePort(t),
		LogLevel:        "info",
		GinMode:         "release",
		ServiceVersion:  "test",
		ShutdownTimeout: "5s",
	}
}

func setRequiredEnv(t *testing.T) {
	t.Helper()
	vars := map[string]string{
		"DATABASE_DSN": "postgres://placeholder",
		"REDIS_URL":    "redis://placeholder",
		"NATS_URL":     "nats://placeholder",
	}
	for k, v := range vars {
		prev := os.Getenv(k)
		os.Setenv(k, v)
		t.Cleanup(func() { os.Setenv(k, prev) })
	}
}

func TestLifecycle_Start_ListensOnConfiguredPort(t *testing.T) {
	setRequiredEnv(t)
	cfg := testConfig(t)

	app, err := lifecycle.NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		app.Stop(stopCtx) //nolint:errcheck
	}()

	// Give the server a moment to bind.
	time.Sleep(20 * time.Millisecond)

	url := fmt.Sprintf("http://127.0.0.1:%s/internal/v1/tally/health", cfg.Port)
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		t.Fatalf("health check request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestLifecycle_Start_SkipsMigrationWhenDisabled verifies that Start does not attempt
// database migration when MigrateOnBoot is false, even if DatabaseDSN is unreachable.
// This ensures operators can start the service without auto-migration in production.
func TestLifecycle_Start_SkipsMigrationWhenDisabled(t *testing.T) {
	setRequiredEnv(t)
	cfg := testConfig(t)
	cfg.MigrateOnBoot = false
	// Use an intentionally unreachable DSN; if migration ran it would fail.
	cfg.DatabaseDSN = "postgres://invalid:invalid@127.0.0.1:1/none?sslmode=disable&connect_timeout=1"

	app, err := lifecycle.NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Start must succeed because migration is skipped.
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start with MigrateOnBoot=false must not fail, got: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		app.Stop(stopCtx) //nolint:errcheck
	}()
}

func TestLifecycle_Stop_GracefulShutdown(t *testing.T) {
	setRequiredEnv(t)
	cfg := testConfig(t)

	app, err := lifecycle.NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}

	startCtx := context.Background()
	if err := app.Start(startCtx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	done := make(chan error, 1)
	go func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		done <- app.Stop(stopCtx)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Stop returned error: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Error("Stop did not complete within 6 seconds")
	}
}
