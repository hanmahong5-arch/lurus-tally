// Package lifecycle manages the application startup and shutdown sequence.
// App is the dependency injection root: it owns the HTTP server and logger,
// and coordinates ordered start/stop across all subsystems.
package lifecycle

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/health"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/router"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/config"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/logger"
)

// App is the application root. It holds all wired dependencies and manages
// the HTTP server lifecycle. No global variables; all state lives here.
type App struct {
	cfg    *config.Config
	log    *slog.Logger
	engine *gin.Engine
	srv    *http.Server
}

// NewApp wires all dependencies together and returns a ready-to-start App.
// Call Start to begin serving HTTP traffic and Stop to drain and close the server.
func NewApp(cfg *config.Config) (*App, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config must not be nil")
	}

	if cfg.GinMode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	l := logger.New(cfg.LogLevel, "lurus-tally", cfg.ServiceVersion, nil)

	h := health.New(cfg.ServiceVersion)
	r := router.New(h)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	return &App{
		cfg:    cfg,
		log:    l,
		engine: r,
		srv:    srv,
	}, nil
}
