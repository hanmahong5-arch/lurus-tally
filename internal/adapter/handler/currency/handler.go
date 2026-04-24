// Package currency implements Gin HTTP handlers for currency and exchange rate endpoints.
package currency

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appcurrency "github.com/hanmahong5-arch/lurus-tally/internal/app/currency"
)

// Handler groups all currency and exchange rate Gin handlers.
type Handler struct {
	listCurrencies  *appcurrency.ListCurrenciesUseCase
	getRate         *appcurrency.GetRateUseCase
	createRate      *appcurrency.CreateRateUseCase
	listRateHistory *appcurrency.ListRateHistoryUseCase
}

// New creates a Handler wired to the provided use cases.
func New(
	listCurrencies *appcurrency.ListCurrenciesUseCase,
	getRate *appcurrency.GetRateUseCase,
	createRate *appcurrency.CreateRateUseCase,
	listRateHistory *appcurrency.ListRateHistoryUseCase,
) *Handler {
	return &Handler{
		listCurrencies:  listCurrencies,
		getRate:         getRate,
		createRate:      createRate,
		listRateHistory: listRateHistory,
	}
}

// RegisterRoutes mounts all currency routes onto the given router group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/currencies", h.ListCurrencies)
	rg.GET("/exchange-rates", h.GetRate)
	rg.POST("/exchange-rates", h.CreateRate)
	rg.GET("/exchange-rates/history", h.ListRateHistory)
}

// ListCurrencies handles GET /api/v1/currencies
func (h *Handler) ListCurrencies(c *gin.Context) {
	currencies, err := h.listCurrencies.Execute(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, errResp("internal_error", err.Error(), ""))
		return
	}
	c.JSON(http.StatusOK, gin.H{"currencies": currencies})
}

// GetRate handles GET /api/v1/exchange-rates?from=USD&to=CNY&date=2026-04-23
func (h *Handler) GetRate(c *gin.Context) {
	tenantID := resolveTenantID(c)
	from := c.Query("from")
	to := c.Query("to")
	if from == "" || to == "" {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "from and to are required", "provide ?from=USD&to=CNY"))
		return
	}

	date := time.Now().UTC()
	if ds := c.Query("date"); ds != "" {
		t, err := time.Parse("2006-01-02", ds)
		if err != nil {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "date must be YYYY-MM-DD", ""))
			return
		}
		// Use end-of-day to include rates effective on that date.
		date = t.Add(23*time.Hour + 59*time.Minute + 59*time.Second).UTC()
	}

	result, err := h.getRate.Execute(c.Request.Context(), tenantID, from, to, date)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errResp("internal_error", err.Error(), ""))
		return
	}
	c.JSON(http.StatusOK, result)
}

// createRateRequest is the request body for POST /api/v1/exchange-rates.
type createRateRequest struct {
	FromCurrency string `json:"from_currency"`
	ToCurrency   string `json:"to_currency"`
	Rate         string `json:"rate"`
	EffectiveAt  string `json:"effective_at"` // RFC3339
}

// CreateRate handles POST /api/v1/exchange-rates
func (h *Handler) CreateRate(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "tenant_id required", ""))
		return
	}

	var req createRateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "invalid request body: "+err.Error(), ""))
		return
	}

	rate, err := decimal.NewFromString(req.Rate)
	if err != nil || rate.IsZero() || rate.IsNegative() {
		c.JSON(http.StatusBadRequest, errResp("invalid_rate", "rate must be a positive decimal", ""))
		return
	}

	var effectiveAt time.Time
	if req.EffectiveAt != "" {
		effectiveAt, err = time.Parse(time.RFC3339, req.EffectiveAt)
		if err != nil {
			// Also try date-only format.
			effectiveAt, err = time.Parse("2006-01-02", req.EffectiveAt)
			if err != nil {
				c.JSON(http.StatusBadRequest, errResp("validation_error", "effective_at must be RFC3339 or YYYY-MM-DD", ""))
				return
			}
		}
	} else {
		effectiveAt = time.Now().UTC().Truncate(24 * time.Hour)
	}

	result, err := h.createRate.Execute(c.Request.Context(), appcurrency.CreateRateRequest{
		TenantID:     tenantID,
		FromCurrency: req.FromCurrency,
		ToCurrency:   req.ToCurrency,
		Rate:         rate,
		EffectiveAt:  effectiveAt,
	})
	if err != nil {
		if errors.Is(err, appcurrency.ErrValidation) {
			c.JSON(http.StatusBadRequest, errResp("validation_error", err.Error(), ""))
			return
		}
		c.JSON(http.StatusInternalServerError, errResp("internal_error", err.Error(), ""))
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ListRateHistory handles GET /api/v1/exchange-rates/history?from=USD&to=CNY&days=30
func (h *Handler) ListRateHistory(c *gin.Context) {
	tenantID := resolveTenantID(c)
	from := c.Query("from")
	to := c.Query("to")
	if from == "" || to == "" {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "from and to are required", ""))
		return
	}

	days := 30
	if ds := c.Query("days"); ds != "" {
		if n, err := strconv.Atoi(ds); err == nil && n > 0 {
			days = n
		}
	}

	rates, err := h.listRateHistory.Execute(c.Request.Context(), tenantID, from, to, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errResp("internal_error", err.Error(), ""))
		return
	}
	c.JSON(http.StatusOK, gin.H{"rates": rates})
}

// ----- helpers -----

func errResp(code, message, action string) gin.H {
	h := gin.H{"error": code, "message": message}
	if action != "" {
		h["action"] = action
	}
	return h
}

func resolveTenantID(c *gin.Context) uuid.UUID {
	id := middleware.GetTenantID(c)
	if id != uuid.Nil {
		return id
	}
	if raw := c.GetHeader("X-Tenant-ID"); raw != "" {
		if parsed, err := uuid.Parse(raw); err == nil {
			return parsed
		}
	}
	return uuid.Nil
}
