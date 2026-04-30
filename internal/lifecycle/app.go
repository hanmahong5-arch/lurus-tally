// Package lifecycle manages the application startup and shutdown sequence.
// App is the dependency injection root: it owns the HTTP server and logger,
// and coordinates ordered start/stop across all subsystems.
package lifecycle

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	handlerai "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/ai"
	handlerAuth "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/auth"
	handlerbill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/bill"
	handlerbilling "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/billing"
	handlercurrency "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/currency"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/health"
	handlerpayment "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/payment"
	handlerproduct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/product"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/router"
	handlerstock "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/stock"
	handlerunit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/unit"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	repoai "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/ai"
	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	repocurrency "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/currency"
	repopayment "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/payment"
	repoproduct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/product"
	repostock "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/stock"
	repotenant "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/tenant"
	repounit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/unit"
	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appbilling "github.com/hanmahong5-arch/lurus-tally/internal/app/billing"
	appcurrency "github.com/hanmahong5-arch/lurus-tally/internal/app/currency"
	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	appproduct "github.com/hanmahong5-arch/lurus-tally/internal/app/product"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	apptenant "github.com/hanmahong5-arch/lurus-tally/internal/app/tenant"
	appunit "github.com/hanmahong5-arch/lurus-tally/internal/app/unit"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/config"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/logger"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/platformclient"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver for database/sql
	"github.com/redis/go-redis/v9"
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
		appproduct.NewRestoreUseCase(productRepo),
	)

	// Wire unit use cases.
	unitRepo := repounit.New(db)
	unitHandler := handlerunit.New(
		appunit.NewCreateUseCase(unitRepo),
		appunit.NewListUseCase(unitRepo),
		appunit.NewDeleteUseCase(unitRepo),
	)

	// Wire tenant onboarding store. The BootstrapStore wraps *sql.DB and
	// provides the atomic tenant+mapping+profile creation needed for first
	// login. Both ChooseProfileUseCase and GetMeUseCase share this store.
	tenantStore := repotenant.NewSQLBootstrapStore(db)

	// Build the platform client up front so the same client serves both
	// ChooseProfile (account provisioning) and the billing handler. Empty
	// PLATFORM_INTERNAL_KEY → nil client → both code paths degrade gracefully.
	var platClient *platformclient.Client
	if cfg.PlatformInternalKey != "" {
		pc, perr := platformclient.New(platformclient.Config{
			BaseURL: cfg.PlatformBaseURL,
			APIKey:  cfg.PlatformInternalKey,
		})
		if perr != nil {
			return nil, fmt.Errorf("lifecycle: cannot init platform client: %w", perr)
		}
		platClient = pc
		l.Info("platform client initialised",
			slog.String("platform_url", cfg.PlatformBaseURL))
	} else {
		l.Warn("platform integration disabled (PLATFORM_INTERNAL_KEY not set)")
	}

	authHandler := handlerAuth.New(
		apptenant.NewChooseProfileUseCase(tenantStore, platClient, l),
		apptenant.NewGetMeUseCase(tenantStore),
	)

	// Wire stock use cases. MVP: single WAC calculator (FIFO routing per-tenant
	// is deferred to V1.5 — a calculator factory keyed on profile.InventoryMethod()
	// will be invoked inside the use case).
	stockRepo := repostock.New(db)
	stockCalculator := appstock.NewCalculator(nil, stockRepo) // nil profile → WAC default
	recordMovementUC := appstock.NewRecordMovementUseCase(stockRepo, stockCalculator, nil, l)
	stockHandler := handlerstock.New(
		recordMovementUC,
		appstock.NewGetSnapshotUseCase(stockRepo),
		appstock.NewListSnapshotsUseCase(stockRepo),
		appstock.NewListMovementsUseCase(stockRepo),
	)

	// Wire bill use cases (Story 6.1: purchase receipt baseline).
	billRepo := repobill.New(db)
	billHandler := handlerbill.New(
		appbill.NewCreatePurchaseDraftUseCase(billRepo),
		appbill.NewUpdatePurchaseDraftUseCase(billRepo),
		appbill.NewApprovePurchaseUseCase(billRepo, recordMovementUC, unitRepo),
		appbill.NewCancelPurchaseUseCase(billRepo),
		appbill.NewListPurchasesUseCase(billRepo),
		appbill.NewGetPurchaseUseCase(billRepo),
		appbill.NewRestorePurchaseUseCase(billRepo),
	)

	// Wire currency use cases (Story 9.1: multi-currency + manual exchange rate entry).
	currencyRepo := repocurrency.New(db)
	currencyHandler := handlercurrency.New(
		appcurrency.NewListCurrenciesUseCase(currencyRepo),
		appcurrency.NewGetRateUseCase(currencyRepo),
		appcurrency.NewCreateRateUseCase(currencyRepo),
		appcurrency.NewListRateHistoryUseCase(currencyRepo),
	)

	// Wire sale + payment use cases (Story 7.1: sales shipment + POS checkout).
	paymentRepo := repopayment.New(db)
	approveSaleUC := appbill.NewApproveSaleUseCase(billRepo, recordMovementUC, unitRepo, paymentRepo)
	quickCheckoutUC := appbill.NewQuickCheckoutUseCase(billRepo, approveSaleUC)
	recordPaymentUC := apppayment.NewRecordPaymentUseCase(billRepo, paymentRepo)
	listPaymentsUC := apppayment.NewListPaymentsUseCase(paymentRepo)

	saleHandler := handlerbill.NewSaleHandler(
		appbill.NewCreateSaleUseCase(billRepo),
		approveSaleUC,
		appbill.NewCancelPurchaseUseCase(billRepo),
		appbill.NewListPurchasesUseCase(billRepo),
		billRepo,
		quickCheckoutUC,
		listPaymentsUC,
	)
	paymentHandler := handlerpayment.New(recordPaymentUC, listPaymentsUC)

	// Wire billing → platform integration. Uses the same platClient already
	// built above for account provisioning. nil client → 501 on /billing/*.
	var billingHandler *handlerbilling.Handler
	if platClient != nil {
		billingHandler = handlerbilling.New(
			appbilling.NewSubscribeUseCase(platClient),
			appbilling.NewOverviewUseCase(platClient),
		)
		l.Info("billing integration enabled")
	}

	// Wire AI assistant. Requires NEWAPI_API_KEY; when absent, AI routes return 501.
	var aiHandler *handlerai.Handler
	if cfg.NewAPIKey != "" {
		rdbOpts, rerr := redis.ParseURL(cfg.RedisURL)
		if rerr != nil {
			return nil, fmt.Errorf("lifecycle: cannot parse REDIS_URL for AI store: %w", rerr)
		}
		rdb := redis.NewClient(rdbOpts)
		planTTL := time.Duration(cfg.AIPlanTTLSeconds) * time.Second
		planStore := repoai.New(repoai.NewGoRedisAdapter(rdb), planTTL)

		llmClient, lerr := llmclient.New(llmclient.Config{
			BaseURL:      cfg.NewAPIBaseURL,
			APIKey:       cfg.NewAPIKey,
			DefaultModel: cfg.DefaultAIModel,
		})
		if lerr != nil {
			return nil, fmt.Errorf("lifecycle: cannot init LLM client: %w", lerr)
		}

		registry := appai.NewRegistry(
			repoai.NewSQLProductRepo(db),
			repoai.NewSQLStockRepo(db),
			repoai.NewSQLSaleRepo(db),
			repoai.NewSQLExchangeRateRepo(db),
		)
		orchestrator := appai.NewOrchestrator(llmClient, registry, planStore, cfg.DefaultAIModel)
		aiHandler = handlerai.New(orchestrator)
		l.Info("AI assistant enabled",
			slog.String("model", cfg.DefaultAIModel),
			slog.String("newapi_url", cfg.NewAPIBaseURL))
	} else {
		l.Warn("AI assistant disabled (NEWAPI_API_KEY not set)")
	}

	// Build AuthMiddleware when ZITADEL_DOMAIN is set. In dev it can be empty;
	// the router will then leave /api/v1 unauthenticated and handlers will
	// surface 401 on identity-required calls.
	var authMW gin.HandlerFunc
	if cfg.ZitadelDomain != "" {
		issuer := "https://" + cfg.ZitadelDomain
		jwksURL := issuer + "/oauth/v2/keys"
		// Resolve tenant_id from user_identity_mapping for already-onboarded
		// users. uuid.Nil is returned for first-time users (no mapping yet) —
		// in that case the middleware lets the request through and only
		// /me + /tenant/profile work; business handlers will return 401.
		tenantLookup := func(ctx context.Context, sub string) (uuid.UUID, error) {
			mapping, err := tenantStore.GetMappingBySub(ctx, sub)
			if err != nil {
				return uuid.Nil, err
			}
			if mapping == nil {
				return uuid.Nil, nil
			}
			return mapping.TenantID, nil
		}
		authMW = middleware.NewAuthMiddleware(jwksURL, issuer, tenantLookup)
		l.Info("auth middleware enabled",
			slog.String("issuer", issuer),
			slog.String("jwks_url", jwksURL))
	} else {
		l.Warn("auth middleware disabled (ZITADEL_DOMAIN not set) — /api/v1 is unauthenticated")
	}

	h := health.New(cfg.ServiceVersion)
	r := router.New(h, authMW, productHandler, unitHandler, authHandler, stockHandler,
		billHandler, currencyHandler, saleHandler, paymentHandler, billingHandler, aiHandler)

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
