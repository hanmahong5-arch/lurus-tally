// Package lifecycle manages the application startup and shutdown sequence.
// App is the dependency injection root: it owns the HTTP server and logger,
// and coordinates ordered start/stop across all subsystems.
package lifecycle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	handleracct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/account"
	handlerai "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/ai"
	handlerAuth "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/auth"
	handlerbill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/bill"
	handlerbilling "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/billing"
	handlercurrency "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/currency"
	handlerdigest "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/digest"
	handlerexport "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/export"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/health"
	handlerhorticulture "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/horticulture"
	handlerimporting "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/importing"
	handlermetrics "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/metrics"
	handleronboarding "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/onboarding"
	handlerpayment "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/payment"
	handlerproduct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/product"
	handlerproject "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/project"
	handlerreplenish "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/replenish"
	handlerreports "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/reports"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/router"
	handlersearch "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/search"
	handlershopify "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/shopify"
	handlerstock "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/stock"
	handlersupp "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/supplier"
	handlertelemetry "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/telemetry"
	handlerunit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/unit"
	handlerwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/warehouse"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	adapternats "github.com/hanmahong5-arch/lurus-tally/internal/adapter/nats"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/platform"
	repoacct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/account"
	repoai "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/ai"
	repoauth "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/auth"
	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	repocurrency "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/currency"
	repodigest "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/digest"
	repooutbox "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/event_outbox"
	repohorticulture "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/horticulture"
	repoimporting "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/importing"
	repoonboarding "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/onboarding"
	repopayment "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/payment"
	repoproduct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/product"
	repoprojectrepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/project"
	reporepl "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/replenish"
	reporeports "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/reports"
	reposearch "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/search"
	reposhopify "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/shopify"
	repostock "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/stock"
	reposupplier "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/supplier"
	repotenant "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/tenant"
	repounit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/unit"
	repowarehouse "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/warehouse"
	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	appauth "github.com/hanmahong5-arch/lurus-tally/internal/app/auth"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appbilling "github.com/hanmahong5-arch/lurus-tally/internal/app/billing"
	appcurrency "github.com/hanmahong5-arch/lurus-tally/internal/app/currency"
	appdigest "github.com/hanmahong5-arch/lurus-tally/internal/app/digest"
	appexport "github.com/hanmahong5-arch/lurus-tally/internal/app/export"
	apphorticulture "github.com/hanmahong5-arch/lurus-tally/internal/app/horticulture"
	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	appproduct "github.com/hanmahong5-arch/lurus-tally/internal/app/product"
	appprojectuc "github.com/hanmahong5-arch/lurus-tally/internal/app/project"
	appreplenish "github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
	appreports "github.com/hanmahong5-arch/lurus-tally/internal/app/reports"
	appsearch "github.com/hanmahong5-arch/lurus-tally/internal/app/search"
	appshopify "github.com/hanmahong5-arch/lurus-tally/internal/app/shopify"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	appsupp "github.com/hanmahong5-arch/lurus-tally/internal/app/supplier"
	apptenant "github.com/hanmahong5-arch/lurus-tally/internal/app/tenant"
	appunit "github.com/hanmahong5-arch/lurus-tally/internal/app/unit"
	appwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/app/warehouse"
	domainauth "github.com/hanmahong5-arch/lurus-tally/internal/domain/auth"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/config"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmgateway"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/logger"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver for database/sql
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

// App is the application root. It holds all wired dependencies and manages
// the HTTP server lifecycle. No global variables; all state lives here.
type App struct {
	cfg        *config.Config
	log        *slog.Logger
	engine     *gin.Engine
	srv        *http.Server
	db         *sql.DB
	stopOutbox context.CancelFunc // cancels the outbox worker goroutine on Stop
	auditSub   *adapternats.AuditSubscriber
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
	var platClient *platform.Client
	if cfg.PlatformInternalKey != "" {
		pc, perr := platform.New(platform.Config{
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

	// Wire NATS publisher + notification client. When NATS_URL is empty or
	// unreachable, NoOpFallback keeps the service bootable in dev.
	natsNoOp := cfg.NATSURL == ""
	natsPub, natsPubErr := adapternats.NewPublisher(adapternats.Config{
		URL:          cfg.NATSURL,
		NoOpFallback: natsNoOp,
	})
	if natsPubErr != nil {
		// NATS is non-critical — degrade to noop and log the failure.
		l.Warn("NATS publisher unavailable, falling back to noop",
			slog.String("nats_url", cfg.NATSURL),
			slog.String("error", natsPubErr.Error()))
		natsPub, _ = adapternats.NewPublisher(adapternats.Config{NoOpFallback: true})
	}
	notifyMode := "nats"
	if natsNoOp {
		notifyMode = "noop"
	}
	notifyClient := platform.NewNotificationClient(platform.NotificationConfig{
		NATSPublisher: natsPub,
		NotifyURL:     cfg.PlatformNotifyURL,
		APIKey:        cfg.PlatformInternalKey,
	})
	l.Info("notification: enabled", slog.String("mode", notifyMode))
	_ = notifyClient // capability ready; business events wired in subsequent stories

	// Wire account-center (Phase 3): user_session + account_audit_log + user_profile.
	// All three share a single repo pool against the shared *sql.DB.
	acctSessionRepo := repoacct.NewSessionRepo(db)
	acctAuditRepo := repoacct.NewAuditRepo(db)
	acctProfileRepo := repoacct.NewProfileRepo(db)

	acctListSessions := appacct.NewListSessions(acctSessionRepo)
	acctRevokeSession := appacct.NewRevokeSession(acctSessionRepo)
	acctRecordSession := appacct.NewRecordSession(acctSessionRepo)
	acctAppendAudit := appacct.NewAppendAuditLog(acctAuditRepo)
	acctListAudit := appacct.NewListAuditLog(acctAuditRepo)
	acctGetProfile := appacct.NewGetProfile(acctProfileRepo)
	acctUpdateProfile := appacct.NewUpdateProfile(acctProfileRepo)
	acctSetAvatar := appacct.NewSetAvatar(acctProfileRepo)
	acctGetAvatar := appacct.NewGetAvatar(acctProfileRepo)

	accountHandler := handleracct.New(
		acctListSessions,
		acctRevokeSession,
		acctListAudit,
		acctGetProfile,
		acctUpdateProfile,
		acctSetAvatar,
		acctGetAvatar,
	)

	authHandler := handlerAuth.New(
		apptenant.NewChooseProfileUseCase(tenantStore, platClient, l),
		apptenant.NewGetMeUseCase(tenantStore).WithProfileGetter(acctGetProfile),
	)

	// Wire transactional outbox store. Shared between the use case (Enqueue) and
	// the background drain worker. Using the same *sql.DB pool is safe — Enqueue
	// participates in the caller's tx, while Drain opens its own tx per poll.
	outboxStore := repooutbox.New(db)

	// Wire stock use cases. MVP: single WAC calculator (FIFO routing per-tenant
	// is deferred to V1.5 — a calculator factory keyed on profile.InventoryMethod()
	// will be invoked inside the use case).
	stockRepo := repostock.New(db)
	stockCalculator := appstock.NewCalculator(nil, stockRepo) // nil profile → WAC default
	recordMovementUC := appstock.NewRecordMovementUseCase(stockRepo, stockCalculator, outboxStore, l)
	stockHandler := handlerstock.New(
		recordMovementUC,
		appstock.NewGetSnapshotUseCase(stockRepo),
		appstock.NewListSnapshotsUseCase(stockRepo),
		appstock.NewListMovementsUseCase(stockRepo),
		appstock.NewListLowStockUseCase(stockRepo),
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
	// rdb is hoisted so the readiness probe can ping it when AI is enabled. When
	// AI is disabled, Redis is not opened at all and is not part of the readiness
	// contract — degraded but bootable.
	var (
		aiHandler *handlerai.Handler
		rdb       *redis.Client
	)
	if cfg.NewAPIKey != "" {
		rdbOpts, rerr := redis.ParseURL(cfg.RedisURL)
		if rerr != nil {
			return nil, fmt.Errorf("lifecycle: cannot parse REDIS_URL for AI store: %w", rerr)
		}
		rdb = redis.NewClient(rdbOpts)
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

		aiProductRepo := repoai.NewSQLProductRepo(db)
		registry := appai.NewRegistry(
			aiProductRepo,
			repoai.NewSQLStockRepo(db),
			repoai.NewSQLSaleRepo(db),
			repoai.NewSQLExchangeRateRepo(db),
		)
		orchestrator := appai.NewOrchestrator(llmClient, registry, planStore, cfg.DefaultAIModel)

		// Wire the plan executor so confirming an AI plan performs real side
		// effects (PO draft / price change / stock adjust) instead of a no-op.
		executor := buildPlanExecutor(db, billRepo, recordMovementUC, aiProductRepo)
		// Attach price-before snapshot so price_change plans can be undone within 30 s.
		executor.WithPriceSnapshot(buildPriceCapturerAdapter(db), newAIPriceSnapshotStore(rdb))
		orchestrator.WithExecutor(executor)
		// Audit every AI plan execution into the account activity log (red-line:
		// each AI write must be auditable).
		orchestrator.WithAudit(newAIAuditWriter(acctAuditRepo, l))

		// Wire memorus memory client when both env vars are set.
		// Returns (nil, nil) when either is empty — orchestrator degrades gracefully.
		if cfg.MemoryAPIKey != "" {
			memClient, merr := memorusclient.New(memorusclient.Config{
				BaseURL: cfg.MemoryBaseURL,
				APIKey:  cfg.MemoryAPIKey,
			})
			if merr != nil {
				return nil, fmt.Errorf("lifecycle: cannot init memorus client: %w", merr)
			}
			if memClient != nil {
				orchestrator.WithMemory(memClient)
				l.Info("memorus memory enabled",
					slog.String("memorus_url", cfg.MemoryBaseURL))
			}
		} else {
			l.Info("memorus: disabled (MEMORUS_API_KEY not set)")
		}

		// Per-tenant LLM rate limiter (W0.A5). Backed by the AI Redis client we
		// already opened; degrades open on Redis failure. Limit/window are
		// hard-coded for now — operator override via cfg lands once a clear
		// per-tier budget surface exists.
		var limiterStore llmgateway.RedisIncrer
		if rdb != nil {
			limiterStore = newRedisLimiterAdapter(rdb)
		}
		limiter := llmgateway.NewRateLimiter(
			limiterStore,
			llmgateway.DefaultRateLimit,
			llmgateway.DefaultRateWindow,
		)
		reverter := buildReverter(db, repostock.New(db), recordMovementUC, planStore, rdb)
		aiHandler = handlerai.NewWithLimiter(orchestrator, limiter).WithReverter(reverter)
		// Attach LLM span tracer (env LANGFUSE_* → OTLP exporter; missing → no-op).
		WireTracer(orchestrator, BuildTracer(cfg))
		l.Info("AI assistant enabled",
			slog.String("model", cfg.DefaultAIModel),
			slog.String("newapi_url", cfg.NewAPIBaseURL),
			slog.Int("llm_rate_limit_per_min", limiter.Limit()))
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
		// PAT resolver — short-circuits before JWT path when bearer starts
		// with tally_pat_. See domain/auth and migration 000031.
		patRepo := repoauth.New(db)
		patResolver := func(ctx context.Context, bearer string) (uuid.UUID, []string, error) {
			prefix, secret, ok := domainauth.ParseBearer(bearer)
			if !ok {
				return uuid.Nil, nil, middleware.ErrInvalidPAT
			}
			pat, err := patRepo.GetByPrefix(ctx, prefix)
			if err != nil {
				if errors.Is(err, appauth.ErrNotFound) {
					return uuid.Nil, nil, middleware.ErrInvalidPAT
				}
				return uuid.Nil, nil, err
			}
			if !domainauth.Verify(prefix, secret, pat.Hash) {
				return uuid.Nil, nil, middleware.ErrInvalidPAT
			}
			if !pat.IsActive(time.Now()) {
				return uuid.Nil, nil, middleware.ErrInvalidPAT
			}
			// Best-effort last_used_at touch — don't block the request, don't
			// fail the auth on a transient DB hiccup. Detached context so the
			// goroutine survives request cancellation.
			go func(id uuid.UUID) {
				bg, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if err := patRepo.TouchLastUsed(bg, id); err != nil {
					slog.Debug("auth: touch last_used_at failed", slog.Any("error", err))
				}
			}(pat.ID)
			return pat.TenantID, pat.Scopes, nil
		}

		authMW = middleware.NewAuthMiddleware(jwksURL, issuer, tenantLookup, patResolver)
		l.Info("auth middleware enabled",
			slog.String("issuer", issuer),
			slog.String("jwks_url", jwksURL))

		// Chain session-record after auth so every authenticated request
		// upserts a tally.user_session row. Best-effort: a session repo
		// hiccup must not interrupt the request path (recorder swallows err).
		sessionMW := middleware.SessionRecord(acctRecordSession.Execute)
		baseAuthMW := authMW
		authMW = func(c *gin.Context) {
			baseAuthMW(c)
			if c.IsAborted() {
				return
			}
			sessionMW(c)
		}
	} else {
		l.Warn("auth middleware disabled (ZITADEL_DOMAIN not set) — /api/v1 is unauthenticated")
	}

	// Wire horticulture nursery dictionary (Story 28.1).
	dictRepo := repohorticulture.New(db)
	dictHandler := handlerhorticulture.NewDictHandler(
		apphorticulture.NewCreateUseCase(dictRepo),
		apphorticulture.NewGetByIDUseCase(dictRepo),
		apphorticulture.NewListUseCase(dictRepo),
		apphorticulture.NewUpdateUseCase(dictRepo),
		apphorticulture.NewDeleteUseCase(dictRepo),
		apphorticulture.NewRestoreUseCase(dictRepo),
	)

	// Wire project CRUD (Story 28.2).
	projectRepo := repoprojectrepo.New(db)
	projectHandler := handlerproject.NewProjectHandler(
		appprojectuc.NewCreateUseCase(projectRepo),
		appprojectuc.NewGetByIDUseCase(projectRepo),
		appprojectuc.NewListUseCase(projectRepo),
		appprojectuc.NewUpdateUseCase(projectRepo),
		appprojectuc.NewDeleteUseCase(projectRepo),
		appprojectuc.NewRestoreUseCase(projectRepo),
	)

	// Wire supplier CRUD (W3.D1).
	suppRepo := reposupplier.New(db)
	supplierHandler := handlersupp.New(
		appsupp.NewCreateUseCase(suppRepo),
		appsupp.NewGetByIDUseCase(suppRepo),
		appsupp.NewListUseCase(suppRepo),
		appsupp.NewUpdateUseCase(suppRepo),
		appsupp.NewDeleteUseCase(suppRepo),
		appsupp.NewRestoreUseCase(suppRepo),
	)

	// Wire warehouse CRUD (W3.D1).
	whRepo := repowarehouse.New(db)
	warehouseHandler := handlerwarehouse.New(
		appwarehouse.NewCreateUseCase(whRepo),
		appwarehouse.NewGetByIDUseCase(whRepo),
		appwarehouse.NewListUseCase(whRepo),
		appwarehouse.NewUpdateUseCase(whRepo),
		appwarehouse.NewDeleteUseCase(whRepo),
		appwarehouse.NewRestoreUseCase(whRepo),
	)

	// Wire CSV export (W5.F3). All three use cases read directly from the
	// shared DB pool; the handler streams CSV through io.Pipe so memory is
	// bounded regardless of tenant size.
	exportHandler := handlerexport.New(
		appexport.NewBillsExportUseCase(db, l),
		appexport.NewStockExportUseCase(db, l),
		appexport.NewPaymentsExportUseCase(db, l),
		l,
	)

	// Build readiness probe deps. DB is required (service can't function without it);
	// Redis is optional — only present when AI is enabled, and even then a Redis
	// outage should not pull the pod from k8s endpoints because non-AI endpoints
	// are still serviceable.
	healthDeps := []health.Dep{
		{Name: "db", Pinger: dbPinger{db}, Required: true},
	}
	if rdb != nil {
		healthDeps = append(healthDeps, health.Dep{Name: "redis", Pinger: redisPinger{rdb}, Required: false})
	}
	h := health.New(cfg.ServiceVersion, healthDeps...)

	// Idempotency middleware is opt-in based on Redis availability. Without
	// Redis the dedup layer is a no-op and the Idempotency-Key header from
	// the frontend is silently ignored — request semantics are unchanged.
	var idempotencyMW gin.HandlerFunc
	if rdb != nil {
		idempotencyMW = middleware.Idempotency(middleware.NewIdempotencyRedisStore(rdb))
	}

	// PAT CRUD handler — reuses the same patRepo wired into the auth middleware.
	patHandler := handlerAuth.NewPATHandler(repoauth.New(db))

	// /internal/v1/metrics — LLM cost + token counters (S0.Q2). Bearer-gated when
	// PLATFORM_INTERNAL_KEY is set; open when blank for dev/test.
	metricsHandler := handlermetrics.NewMetricsHandler(cfg.PlatformInternalKey)

	// Swarm batch handlers — replenishment (Req 3), reports (Req 10), entity
	// search (Req 6), multi-platform import (Req 5).
	replenishHandler := handlerreplenish.NewWithBatch(
		appreplenish.NewListSuggestionsUseCase(reporepl.NewSQLSuggestionRepo(db)),
		// Draft-batch: groups suggestions by supplier and creates one purchase
		// draft per group. Supplier name resolver is nil — names are resolved
		// client-side from the suggestions list, avoiding an extra round-trip.
		// TODO(P1 #4): wire SupplierNameResolver when per-supplier pricing lands.
		appreplenish.NewCreateDraftBatchUseCase(
			appbill.NewCreatePurchaseDraftUseCase(billRepo),
			nil, // no server-side name resolution needed (client has the names)
		),
	)
	reportsHandler := handlerreports.New(appreports.New(reporeports.New(db)))
	searchHandler := handlersearch.New(appsearch.NewSearchEntitiesUseCase(reposearch.New(db)))
	importUC := appimporting.NewImportOrdersUseCase(
		repoimporting.New(db),
		importSaleCreator{uc: appbill.NewCreateSaleUseCase(billRepo)},
		importSaleApprover{uc: approveSaleUC},
		importStockChecker{uc: appstock.NewGetSnapshotUseCase(stockRepo)},
		importWarehouseChecker{repo: whRepo},
		importCurrencyRater{uc: appcurrency.NewGetRateUseCase(currencyRepo)},
		"CNY",
	)
	importUC.WithReturnHandlers(
		importReturnCreator{uc: appbill.NewCreateReturnBillUseCase(billRepo)},
		importReturnApprover{uc: appbill.NewApproveReturnBillUseCase(billRepo, recordMovementUC)},
	)
	importHandler := handlerimporting.New(importUC, uuid.Nil)

	// Wave-2 handlers — weekly digest (Req 9) + onboarding wizard (Req 7).
	digestHandler := handlerdigest.New(appdigest.NewWeeklySummaryUseCase(repodigest.New(db)))
	onboardingHandler := handleronboarding.New(
		appproduct.NewCreateUseCase(productRepo),
		recordMovementUC,
		repoonboarding.New(db),
	)

	r := router.New(h, authMW, idempotencyMW, productHandler, unitHandler, authHandler, patHandler, stockHandler,
		billHandler, currencyHandler, saleHandler, paymentHandler, billingHandler, aiHandler, dictHandler, projectHandler, metricsHandler, supplierHandler, warehouseHandler, exportHandler, accountHandler,
		replenishHandler, reportsHandler, searchHandler, importHandler, digestHandler, onboardingHandler)

	// POST /internal/v1/telemetry/web — browser-side product telemetry → NATS
	// PSI_TELEMETRY.web.* (S0.Q3). Bearer-gated via the same key as metrics.
	telemetryHandler := handlertelemetry.New(natsPub, cfg.PlatformInternalKey, "anonymous")
	telemetryHandler.Register(r)

	// POST /webhooks/shopify/orders — public (HMAC-verified) e-commerce ingest.
	// Mounted on the root engine so it bypasses the /api/v1 auth middleware.
	shopifyHandler := BuildShopifyHandler(db, importUC, cfg.ShopifyWebhookSecret, l)
	shopifyHandler.RegisterRoutes(r)

	// /api/v1/shopify/shops — tenant-scoped shop-binding admin (auth-gated).
	// Mounted via a dedicated /api/v1 group so the route inherits the same
	// authMW + idempotencyMW stack that router.New attaches to its own group.
	shopifyAdminGroup := r.Group("/api/v1")
	if authMW != nil {
		shopifyAdminGroup.Use(authMW)
	}
	if idempotencyMW != nil {
		shopifyAdminGroup.Use(idempotencyMW)
	}
	shopRepo := newShopRepoAdapter(reposhopify.New(db))
	whGetter := appwarehouse.NewGetByIDUseCase(whRepo)
	shopifyAdminHandler := handlershopify.New(
		appshopify.NewBindShopUseCase(shopRepo, appshopify.NewWarehouseCheckerAdapter(whGetter)),
		appshopify.NewListShopsUseCase(shopRepo),
		appshopify.NewUnbindShopUseCase(shopRepo),
	)
	shopifyAdminHandler.RegisterRoutes(shopifyAdminGroup)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	// Start the outbox drain worker in a background goroutine.
	// It polls every 30s, publishing any pending event_outbox rows to NATS.
	// Graceful shutdown is handled by Stop() cancelling outboxCtx.
	outboxCtx, outboxCancel := context.WithCancel(context.Background())
	outboxWorker := adapternats.NewOutboxWorker(outboxStore, natsPub, l)
	go outboxWorker.Run(outboxCtx)
	l.Info("outbox worker started", slog.String("poll_interval", "30s"))

	// Audit subscriber (Phase 3) — best-effort consumer that mirrors business
	// PSI_EVENTS into tally.account_audit_log. Skipped when NATS is in noop mode
	// (dev) or when the connection can't be opened.
	var auditSub *adapternats.AuditSubscriber
	if cfg.NATSURL != "" {
		nc, ncErr := nats.Connect(cfg.NATSURL)
		if ncErr != nil {
			l.Warn("audit subscriber: NATS connect failed, account_audit_log will not be populated",
				slog.String("error", ncErr.Error()))
		} else {
			sub, subErr := adapternats.NewAuditSubscriber(nc, acctAppendAudit, l)
			if subErr != nil {
				l.Warn("audit subscriber: init failed",
					slog.String("error", subErr.Error()))
			} else if sub != nil {
				if startErr := sub.Start(outboxCtx); startErr != nil {
					l.Warn("audit subscriber: start failed",
						slog.String("error", startErr.Error()))
				} else {
					auditSub = sub
				}
			}
		}
	}

	return &App{
		cfg:        cfg,
		log:        l,
		engine:     r,
		srv:        srv,
		db:         db,
		stopOutbox: outboxCancel,
		auditSub:   auditSub,
	}, nil
}

// dbPinger adapts *sql.DB to the health.Pinger interface.
type dbPinger struct{ db *sql.DB }

func (d dbPinger) Ping(ctx context.Context) error { return d.db.PingContext(ctx) }

// redisPinger adapts *redis.Client to the health.Pinger interface.
type redisPinger struct{ c *redis.Client }

func (p redisPinger) Ping(ctx context.Context) error { return p.c.Ping(ctx).Err() }

// redisLimiterAdapter satisfies llmgateway.RedisIncrer using *redis.Client.
// A nil underlying client yields a nil adapter so the limiter degrades to a
// permissive (no-op) implementation in dev.
type redisLimiterAdapter struct{ c *redis.Client }

func newRedisLimiterAdapter(c *redis.Client) *redisLimiterAdapter {
	if c == nil {
		return nil
	}
	return &redisLimiterAdapter{c: c}
}

func (a *redisLimiterAdapter) Incr(ctx context.Context, key string) (int64, error) {
	if a == nil || a.c == nil {
		return 0, fmt.Errorf("redis client unavailable")
	}
	return a.c.Incr(ctx, key).Result()
}

func (a *redisLimiterAdapter) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if a == nil || a.c == nil {
		return fmt.Errorf("redis client unavailable")
	}
	return a.c.Expire(ctx, key, ttl).Err()
}
