package lifecycle

import (
	"database/sql"
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/blob"
	handlerintake "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/intake"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/intakebridge"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/intakespeech"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/intakevision"
	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	repointake "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/intake"
	repoinvoice "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/invoice"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appintake "github.com/hanmahong5-arch/lurus-tally/internal/app/intake"
	appinv "github.com/hanmahong5-arch/lurus-tally/internal/app/invoice"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/config"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
)

// mountIntake wires the capture inbox onto the stock handler's router group so
// router.New keeps its signature. Vision runs only when the model gateway is
// configured; a missing key still accepts QR codes and stored files.
func mountIntake(log *slog.Logger, db *sql.DB, cfg *config.Config) func(*gin.RouterGroup) {
	dir := os.Getenv("INTAKE_BLOB_DIR")
	store, err := blob.NewLocal(dir)
	if err != nil {
		log.Warn("intake files disabled", slog.String("error", err.Error()))
	}
	svc := &appintake.Service{
		Repo:     repointake.New(db),
		Blobs:    store,
		Invoices: intakebridge.Invoices{UC: appinv.NewCreateUseCase(repoinvoice.New(db))},
		Drafts:   intakebridge.Drafts{UC: appbill.NewCreatePurchaseDraftUseCase(repobill.New(db))},
	}
	if cfg.NewAPIKey != "" && cfg.NewAPIBaseURL != "" && cfg.DefaultAIModel != "" {
		client, err := llmclient.New(llmclient.Config{
			BaseURL: cfg.NewAPIBaseURL, APIKey: cfg.NewAPIKey, DefaultModel: cfg.DefaultAIModel,
		})
		if err != nil {
			log.Warn("intake vision disabled", slog.String("error", err.Error()))
		} else {
			svc.Vision = &intakevision.Parser{Client: client}
			log.Info("intake vision enabled")
		}
	} else {
		log.Info("intake vision disabled")
	}
	if url := os.Getenv("SPEECH_URL"); url != "" {
		svc.Speech = &intakespeech.Parser{URL: url}
		log.Info("intake speech enabled")
	}
	return handlerintake.New(svc).RegisterRoutes
}
