// Package lifecycle manages the application startup and shutdown sequence.
// App is the dependency injection root: it owns the HTTP server and logger,
// and coordinates ordered start/stop across all subsystems.
package lifecycle

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/health"
	handlerproduct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/product"
	handlerunit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/unit"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/router"
	appproduct "github.com/hanmahong5-arch/lurus-tally/internal/app/product"
	appunit "github.com/hanmahong5-arch/lurus-tally/internal/app/unit"
	repoproduct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/product"
	repounit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/unit"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/config"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/logger"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver for database/sql
)

// App is the application root. It holds all wired dependencies and manages
// the HTTP server lifecycle. No global variables; all state lives here.
type App struct {
	cfg    *config.Config
	log    *slog.Logger
	engine *gin.Engine
	srv    *http.Server
	db     *sql.DB
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

	// Open shared database connection pool.
	db, err := sql.Open("pgx", cfg.DatabaseDSN)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: cannot open database: %w; check DATABASE_DSN", err)
	}

	// Wire product use cases.
	productRepo := repoproduct.New(db)
	productHandler := handlerproduct.New(
		appproduct.NewCreateUseCase(productRepo),
		appproduct.NewListUseCase(productRepo),
		appproduct.NewGetUseCase(productRepo),
		appproduct.NewUpdateUseCase(productRepo),
		appproduct.NewDeleteUseCase(productRepo),
	)

	// Wire unit use cases.
	unitRepo := repounit.New(db)
	unitHandler := handlerunit.New(
		appunit.NewCreateUseCase(unitRepo),
		appunit.NewListUseCase(unitRepo),
		appunit.NewDeleteUseCase(unitRepo),
	)

	h := health.New(cfg.ServiceVersion)
	r := router.New(h, productHandler, unitHandler)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	return &App{
		cfg:    cfg,
		log:    l,
		engine: r,
		srv:    srv,
		db:     db,
	}, nil
}
