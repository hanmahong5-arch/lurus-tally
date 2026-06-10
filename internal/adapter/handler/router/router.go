// Package router wires all HTTP routes onto a Gin engine.
package router

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
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
	handlersearch "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/search"
	handlerstock "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/stock"
	handlersupp "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/supplier"
	handlerunit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/unit"
	handlerwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/warehouse"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
)

const (
	// maxRequestBodyBytes is a generous global backstop (10 MiB) against
	// pathological request bodies. Routes needing a tighter bound keep their
	// own MaxBytesReader (e.g. the Shopify webhook); the avatar upload
	// (200 KiB) and JSON endpoints sit far below this.
	maxRequestBodyBytes = 10 << 20
	// requestTimeout bounds processing of non-streaming requests so a stuck
	// handler cannot pin a connection forever.
	requestTimeout = 30 * time.Second
)

// isStreamingRoute reports whether the matched route streams its response and
// therefore must NOT be wrapped by the buffering Timeout middleware: the SSE
// chat endpoint (POST /api/v1/ai/chat) and the CSV exports (*.csv), which write
// incrementally via io.Copy.
func isStreamingRoute(c *gin.Context) bool {
	p := c.FullPath()
	return p == "/api/v1/ai/chat" || strings.HasSuffix(p, ".csv")
}

// notImplemented is a placeholder handler returned when a handler struct is nil.
// This allows the router to register routes for integration testing even when
// handler dependencies (DB) are not available.
func notImplemented(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "handler not configured"})
}

// New creates and configures a Gin engine with all registered routes.
// All handler pointers may be nil in test environments; routes will still be registered
// and respond with 501 instead of panicking.
// authMW may be nil — in that case the /api/v1 group has no auth middleware
// and handlers will see no sub/tenant_id in context (returns 401).
// tenantDBMW may be nil — when nil no tenant connection is pinned (dev / no-DB).
// It MUST run after authMW (needs tenant_id) and before idempotencyMW.
// idempotencyMW may be nil — when nil the dedup layer is skipped (dev / no-Redis).
// It MUST run after authMW so the tenant_id is in context.
// The engine mode (release/debug) is controlled by GIN_MODE or gin.SetMode.
//
//nolint:cyclop // router wiring is intentionally long
func New(h *health.Handler, authMW gin.HandlerFunc, tenantDBMW gin.HandlerFunc, idempotencyMW gin.HandlerFunc, ph *handlerproduct.Handler, uh *handlerunit.Handler, ah *handlerAuth.Handler, pat *handlerAuth.PATHandler, sh *handlerstock.Handler, bh *handlerbill.Handler, ch *handlercurrency.Handler, saleh *handlerbill.SaleHandler, payh *handlerpayment.Handler, bilh *handlerbilling.Handler, aih *handlerai.Handler, dh *handlerhorticulture.DictHandler, projh *handlerproject.ProjectHandler, mh *handlermetrics.MetricsHandler, suph *handlersupp.Handler, wh *handlerwarehouse.Handler, exh *handlerexport.Handler, acct *handleracct.Handler, replh *handlerreplenish.Handler, reph *handlerreports.Handler, srch *handlersearch.Handler, imph *handlerimporting.Handler, digh *handlerdigest.Handler, onh *handleronboarding.Handler) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.RequestID())
	r.Use(middleware.RequestMetrics())
	// Resource bounds (T0 hardening): cap request body size and per-request
	// processing time. Timeout skips streaming routes it cannot buffer.
	r.Use(middleware.BodyLimit(maxRequestBodyBytes))
	r.Use(middleware.Timeout(requestTimeout, isStreamingRoute))

	internal := r.Group("/internal/v1/tally")
	{
		internal.GET("/health", h.Healthz)
		internal.GET("/ready", h.Readyz)
	}

	// LLM observability (S0.Q2). Mounted at /internal/v1 (not /tally) so
	// Prometheus ServiceMonitor picks it up under a stable path. The
	// MetricsHandler enforces the bearer-token gate when expectedKey is set.
	if mh != nil {
		r.GET("/internal/v1/metrics", mh.Serve)
	}

	// API v1 — business endpoints.
	api := r.Group("/api/v1")
	if authMW != nil {
		api.Use(authMW)
	}
	if tenantDBMW != nil {
		api.Use(tenantDBMW)
	}
	if idempotencyMW != nil {
		api.Use(idempotencyMW)
	}
	{
		// Auth + tenant profile routes (Story 2.1).
		// Production setup: AuthMiddleware is applied at the lifecycle layer before
		// these routes are reached. In development, handlers accept X-Tenant-ID header.
		if ah != nil {
			ah.RegisterRoutes(api)
		} else {
			api.GET("/me", notImplemented)
			api.POST("/tenant/profile", notImplemented)
			api.POST("/auth/logout", notImplemented)
		}

		// Personal Access Token CRUD (ADR-0011 Phase 2b).
		if pat != nil {
			pat.RegisterRoutes(api)
		} else {
			api.POST("/auth/pats", notImplemented)
			api.GET("/auth/pats", notImplemented)
			api.DELETE("/auth/pats/:id", notImplemented)
		}

		products := api.Group("/products")
		{
			products.GET("", productHandler(ph, (*handlerproduct.Handler).List))
			products.POST("", productHandler(ph, (*handlerproduct.Handler).Create))
			products.GET("/:id", productHandler(ph, (*handlerproduct.Handler).GetByID))
			products.PUT("/:id", productHandler(ph, (*handlerproduct.Handler).Update))
			products.DELETE("/:id", productHandler(ph, (*handlerproduct.Handler).Delete))
			products.POST("/:id/restore", productHandler(ph, (*handlerproduct.Handler).Restore))
		}

		units := api.Group("/units")
		{
			units.GET("", unitHandler(uh, (*handlerunit.Handler).List))
			units.POST("", unitHandler(uh, (*handlerunit.Handler).Create))
			units.DELETE("/:id", unitHandler(uh, (*handlerunit.Handler).Delete))
		}

		// Stock query routes (read-only; mutations go through bill approval).
		if sh != nil {
			sh.RegisterRoutes(api)
		} else {
			api.GET("/stock/snapshots", notImplemented)
			api.GET("/stock/snapshots/:product_id/:warehouse_id", notImplemented)
			api.GET("/stock/movements", notImplemented)
			api.GET("/stock/alerts/low-stock", notImplemented)
		}

		// Purchase bill routes (Story 6.1).
		if bh != nil {
			bh.RegisterRoutes(api)
		} else {
			// Stub routes so integration tests can verify registration.
			api.POST("/purchase-bills", notImplemented)
			api.PUT("/purchase-bills/:id", notImplemented)
			api.POST("/purchase-bills/:id/approve", notImplemented)
			api.POST("/purchase-bills/:id/cancel", notImplemented)
			api.POST("/purchase-bills/:id/restore", notImplemented)
			api.GET("/purchase-bills", notImplemented)
			api.GET("/purchase-bills/:id", notImplemented)
		}

		// Currency and exchange rate routes (Story 9.1).
		if ch != nil {
			ch.RegisterRoutes(api)
		} else {
			api.GET("/currencies", notImplemented)
			api.GET("/exchange-rates", notImplemented)
			api.POST("/exchange-rates", notImplemented)
			api.GET("/exchange-rates/history", notImplemented)
		}

		// Sale bill routes (Story 7.1).
		if saleh != nil {
			saleh.RegisterRoutes(api)
		} else {
			api.POST("/sale-bills/quick-checkout", notImplemented)
			api.POST("/sale-bills", notImplemented)
			api.PUT("/sale-bills/:id", notImplemented)
			api.POST("/sale-bills/:id/approve", notImplemented)
			api.POST("/sale-bills/:id/cancel", notImplemented)
			api.GET("/sale-bills", notImplemented)
			api.GET("/sale-bills/:id", notImplemented)
		}

		// Payment routes (Story 7.1).
		if payh != nil {
			payh.RegisterRoutes(api)
		} else {
			api.POST("/payments", notImplemented)
			api.GET("/payments", notImplemented)
		}

		// Billing routes — Tally → platform subscription checkout (Story 10.1).
		if bilh != nil {
			bilh.RegisterRoutes(api)
		} else {
			api.GET("/billing/overview", notImplemented)
			api.POST("/billing/subscribe", notImplemented)
		}

		// AI assistant routes (Story 11.1: AI Drawer + ⌘K palette).
		if aih != nil {
			aih.RegisterRoutes(api)
		} else {
			api.POST("/ai/chat", notImplemented)
			api.GET("/ai/plans", notImplemented)
			api.POST("/ai/plans/:plan_id/confirm", notImplemented)
			api.POST("/ai/plans/:plan_id/cancel", notImplemented)
		}

		// Horticulture — nursery dictionary (Story 28.1).
		if dh != nil {
			dh.RegisterRoutes(api)
		} else {
			api.GET("/nursery-dict", notImplemented)
			api.POST("/nursery-dict", notImplemented)
			api.GET("/nursery-dict/:id", notImplemented)
			api.PUT("/nursery-dict/:id", notImplemented)
			api.DELETE("/nursery-dict/:id", notImplemented)
			api.POST("/nursery-dict/:id/restore", notImplemented)
		}

		// Project CRUD (Story 28.2).
		if projh != nil {
			projh.RegisterRoutes(api)
		} else {
			api.GET("/projects", notImplemented)
			api.POST("/projects", notImplemented)
			api.GET("/projects/:id", notImplemented)
			api.PUT("/projects/:id", notImplemented)
			api.DELETE("/projects/:id", notImplemented)
			api.POST("/projects/:id/restore", notImplemented)
		}

		// Supplier CRUD (W3.D1).
		if suph != nil {
			suph.RegisterRoutes(api)
		} else {
			api.GET("/suppliers", notImplemented)
			api.POST("/suppliers", notImplemented)
			api.GET("/suppliers/:id", notImplemented)
			api.PUT("/suppliers/:id", notImplemented)
			api.DELETE("/suppliers/:id", notImplemented)
			api.POST("/suppliers/:id/restore", notImplemented)
		}

		// Warehouse CRUD (W3.D1).
		if wh != nil {
			wh.RegisterRoutes(api)
		} else {
			api.GET("/warehouses", notImplemented)
			api.POST("/warehouses", notImplemented)
			api.GET("/warehouses/:id", notImplemented)
			api.PUT("/warehouses/:id", notImplemented)
			api.DELETE("/warehouses/:id", notImplemented)
			api.POST("/warehouses/:id/restore", notImplemented)
		}

		// CSV export (W5.F3). When exh is nil the routes return 501 so the
		// FE download button degrades gracefully in dev / smoke environments.
		if exh != nil {
			exh.RegisterRoutes(api)
		} else {
			api.GET("/exports/bills.csv", notImplemented)
			api.GET("/exports/stock.csv", notImplemented)
			api.GET("/exports/payments.csv", notImplemented)
		}

		// Account-center (Phase 3). When acct is nil the routes return 501
		// so dev / migration-pending environments don't 404 the new tabs.
		if acct != nil {
			acct.RegisterRoutes(api)
		} else {
			api.GET("/account/sessions", notImplemented)
			api.DELETE("/account/sessions/:id", notImplemented)
			api.GET("/account/audit-log", notImplemented)
			api.GET("/account/profile", notImplemented)
			api.PUT("/account/profile", notImplemented)
			api.POST("/account/avatar", notImplemented)
			api.GET("/account/avatar", notImplemented)
		}

		// Replenishment decision page (Req 3) — weekly suggestions + batch draft creation.
		if replh != nil {
			replh.RegisterRoutes(api)
		} else {
			api.GET("/replenish/suggestions", notImplemented)
			api.POST("/replenish/draft-batch", notImplemented)
		}

		// Reports — surfaced AI analytics (Req 10).
		if reph != nil {
			reph.RegisterRoutes(api)
		} else {
			api.GET("/reports/gross-margin", notImplemented)
			api.GET("/reports/abc", notImplemented)
			api.GET("/reports/dead-stock", notImplemented)
			api.GET("/reports/sales-top", notImplemented)
		}

		// ⌘K entity search (Req 6).
		if srch != nil {
			srch.RegisterRoutes(api)
		} else {
			api.GET("/search", notImplemented)
		}

		// Multi-platform order import (Req 5).
		if imph != nil {
			imph.RegisterRoutes(api)
		} else {
			api.POST("/imports/orders", notImplemented)
			api.GET("/imports/mappings", notImplemented)
		}

		// Weekly summary "Monday card" (Req 9).
		if digh != nil {
			digh.RegisterRoutes(api)
		} else {
			api.GET("/weekly-summary", notImplemented)
		}

		// Onboarding first-run wizard (Req 7).
		if onh != nil {
			onh.RegisterRoutes(api)
		} else {
			api.POST("/onboarding/seed-demo", notImplemented)
			api.POST("/onboarding/clear-demo", notImplemented)
		}
	}

	return r
}

// productHandler returns a gin.HandlerFunc that delegates to the method on h,
// or notImplemented if h is nil.
func productHandler(h *handlerproduct.Handler, fn func(*handlerproduct.Handler, *gin.Context)) gin.HandlerFunc {
	if h == nil {
		return notImplemented
	}
	return func(c *gin.Context) { fn(h, c) }
}

// unitHandler returns a gin.HandlerFunc that delegates to the method on h,
// or notImplemented if h is nil.
func unitHandler(h *handlerunit.Handler, fn func(*handlerunit.Handler, *gin.Context)) gin.HandlerFunc {
	if h == nil {
		return notImplemented
	}
	return func(c *gin.Context) { fn(h, c) }
}
