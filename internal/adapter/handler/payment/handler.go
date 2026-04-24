// Package payment implements Gin HTTP handlers for payment endpoints.
package payment

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
)

// Handler groups payment Gin handlers.
type Handler struct {
	recordUC *apppayment.RecordPaymentUseCase
	listUC   *apppayment.ListPaymentsUseCase
}

// New creates a Handler.
func New(recordUC *apppayment.RecordPaymentUseCase, listUC *apppayment.ListPaymentsUseCase) *Handler {
	return &Handler{recordUC: recordUC, listUC: listUC}
}

// RegisterRoutes mounts payment routes.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/payments", h.Record)
	rg.GET("/payments", h.List)
}

// ----- request types -----

type recordRequest struct {
	BillID        string `json:"bill_id"`
	Amount        string `json:"amount"`
	PaymentMethod string `json:"payment_method"`
	Remark        string `json:"remark,omitempty"`
}

// ----- handlers -----

// Record handles POST /api/v1/payments
func (h *Handler) Record(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "tenant_id required", ""))
		return
	}
	creatorID := resolveCreatorID(c)
	if creatorID == uuid.Nil {
		creatorID = tenantID
	}

	var req recordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "invalid request body: "+err.Error(), ""))
		return
	}

	billID, err := uuid.Parse(req.BillID)
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "bill_id must be a valid UUID", ""))
		return
	}

	amount, err := decimal.NewFromString(req.Amount)
	if err != nil || amount.IsZero() || amount.IsNegative() {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "amount must be a positive decimal", ""))
		return
	}

	if err := h.recordUC.Execute(c.Request.Context(), apppayment.RecordPaymentRequest{
		TenantID:  tenantID,
		BillID:    billID,
		CreatorID: creatorID,
		Amount:    amount,
		PayType:   req.PaymentMethod,
		Remark:    req.Remark,
	}); err != nil {
		c.JSON(http.StatusUnprocessableEntity, errResp("payment_error", err.Error(), ""))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "recorded"})
}

// List handles GET /api/v1/payments?bill_id=...
func (h *Handler) List(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "tenant_id required", ""))
		return
	}

	billIDStr := c.Query("bill_id")
	if billIDStr == "" {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "bill_id query parameter is required", ""))
		return
	}
	billID, err := uuid.Parse(billIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "bill_id must be a valid UUID", ""))
		return
	}

	payments, err := h.listUC.Execute(c.Request.Context(), tenantID, billID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errResp("internal_error", err.Error(), ""))
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": payments})
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

func resolveCreatorID(c *gin.Context) uuid.UUID {
	if sub, exists := c.Get(middleware.CtxKeyZitadelSub); exists {
		if s, ok := sub.(string); ok {
			if id, err := uuid.Parse(s); err == nil {
				return id
			}
		}
	}
	if raw := c.GetHeader("X-User-ID"); raw != "" {
		if parsed, err := uuid.Parse(raw); err == nil {
			return parsed
		}
	}
	return uuid.Nil
}

// errorsIs placeholder to satisfy the import.
var _ = errors.New
