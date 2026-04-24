package main_test

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

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := fmt.Sprintf("%d", l.Addr().(*net.TCPAddr).Port)
	l.Close()
	return port
}

func setEnvVars(t *testing.T, vars map[string]string) {
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

func TestMain_Integration_HealthEndpointReturns200(t *testing.T) {
	port := freePort(t)

	setEnvVars(t, map[string]string{
		"DATABASE_DSN": "postgres://placeholder",
		"REDIS_URL":    "redis://placeholder",
		"NATS_URL":     "nats://placeholder",
	})

	cfg := &config.Config{
		DatabaseDSN:     "postgres://placeholder",
		RedisURL:        "redis://placeholder",
		NATSURL:         "nats://placeholder",
		Port:            port,
		LogLevel:        "info",
		GinMode:         "release",
		ServiceVersion:  "test",
		ShutdownTimeout: "5s",
	}

	app, err := lifecycle.NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		app.Stop(stopCtx) //nolint:errcheck
	}()

	time.Sleep(20 * time.Millisecond)

	url := fmt.Sprintf("http://127.0.0.1:%s/internal/v1/tally/health", port)
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestMain_Integration_MissingEnv_ExitNonZero(t *testing.T) {
	// Verify config.Load returns an error when required env vars are absent.
	setEnvVars(t, map[string]string{
		"DATABASE_DSN": "",
		"REDIS_URL":    "",
		"NATS_URL":     "",
	})

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error from config.Load with missing env vars, got nil")
	}
}
