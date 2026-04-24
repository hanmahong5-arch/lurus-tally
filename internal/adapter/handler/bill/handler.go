// Package bill implements the Gin HTTP handlers for purchase bill REST endpoints.
package bill

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// Handler groups all purchase bill Gin handlers.
type Handler struct {
	create  *appbill.CreatePurchaseDraftUseCase
	update  *appbill.UpdatePurchaseDraftUseCase
	approve *appbill.ApprovePurchaseUseCase
	cancel  *appbill.CancelPurchaseUseCase
	list    *appbill.ListPurchasesUseCase
	get     *appbill.GetPurchaseUseCase
}

// New creates a Handler wired to the provided use cases.
func New(
	create *appbill.CreatePurchaseDraftUseCase,
	update *appbill.UpdatePurchaseDraftUseCase,
	approve *appbill.ApprovePurchaseUseCase,
	cancel *appbill.CancelPurchaseUseCase,
	list *appbill.ListPurchasesUseCase,
	get *appbill.GetPurchaseUseCase,
) *Handler {
	return &Handler{
		create:  create,
		update:  update,
		approve: approve,
		cancel:  cancel,
		list:    list,
		get:     get,
	}
}

// RegisterRoutes mounts all purchase bill routes onto the given router group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/purchase-bills", h.Create)
	rg.PUT("/purchase-bills/:id", h.Update)
	rg.POST("/purchase-bills/:id/approve", h.Approve)
	rg.POST("/purchase-bills/:id/cancel", h.Cancel)
	rg.GET("/purchase-bills", h.List)
	rg.GET("/purchase-bills/:id", h.Get)
}

// ----- Request / Response types -----

type itemInput struct {
	ProductID string `json:"product_id"`
	UnitID    string `json:"unit_id,omitempty"`
	UnitName  string `json:"unit_name,omitempty"`
	LineNo    int    `json:"line_no"`
	Qty       string `json:"qty"`
	UnitPrice string `json:"unit_price"`
}

type createRequest struct {
	PartnerID   string      `json:"partner_id,omitempty"`
	WarehouseID string      `json:"warehouse_id,omitempty"`
	BillDate    string      `json:"bill_date,omitempty"` // RFC3339
	ShippingFee string      `json:"shipping_fee,omitempty"`
	TaxAmount   string      `json:"tax_amount,omitempty"`
	Remark      string      `json:"remark,omitempty"`
	Items       []itemInput `json:"items"`
}

// ----- Handlers -----

// Create handles POST /api/v1/purchase-bills
func (h *Handler) Create(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "tenant_id required", ""))
		return
	}
	creatorID := resolveCreatorID(c)
	if creatorID == uuid.Nil {
		creatorID = tenantID // fallback to tenant when JWT is not available (dev mode)
	}

	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "invalid request body: "+err.Error(), ""))
		return
	}
	if len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "items must not be empty", ""))
		return
	}

	ucReq, err := buildCreateRequest(tenantID, creatorID, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", err.Error(), ""))
		return
	}

	out, err := h.create.Execute(c.Request.Context(), ucReq)
	if err != nil {
		if errors.Is(err, appbill.ErrValidation) {
			c.JSON(http.StatusBadRequest, errResp("validation_error", err.Error(), ""))
			return
		}
		c.JSON(http.StatusInternalServerError, errResp("internal_error", err.Error(), ""))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"bill_id": out.BillID, "bill_no": out.BillNo})
}

// Update handles PUT /api/v1/purchase-bills/:id
func (h *Handler) Update(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "tenant_id required", ""))
		return
	}
	billID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "invalid bill id", ""))
		return
	}

	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "invalid request body: "+err.Error(), ""))
		return
	}

	ucReq, err := buildCreateRequest(tenantID, uuid.Nil, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", err.Error(), ""))
		return
	}

	upReq := appbill.UpdatePurchaseDraftRequest{
		TenantID:    tenantID,
		BillID:      billID,
		PartnerID:   ucReq.PartnerID,
		WarehouseID: ucReq.WarehouseID,
		BillDate:    ucReq.BillDate,
		ShippingFee: ucReq.ShippingFee,
		TaxAmount:   ucReq.TaxAmount,
		Remark:      ucReq.Remark,
		Items:       ucReq.Items,
	}

	head, err := h.update.Execute(c.Request.Context(), upReq)
	if err != nil {
		if errors.Is(err, appbill.ErrBillNotFound) {
			c.JSON(http.StatusNotFound, errResp("bill_not_found", "bill not found", ""))
			return
		}
		if errors.Is(err, appbill.ErrInvalidBillStatus) {
			c.JSON(http.StatusUnprocessableEntity, errResp("invalid_bill_status", err.Error(), "only draft bills can be updated"))
			return
		}
		c.JSON(http.StatusInternalServerError, errResp("internal_error", err.Error(), ""))
		return
	}
	c.JSON(http.StatusOK, head)
}

// Approve handles POST /api/v1/purchase-bills/:id/approve
func (h *Handler) Approve(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "tenant_id required", ""))
		return
	}
	billID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "invalid bill id", ""))
		return
	}

	approvedBy := resolveCreatorID(c)
	if approvedBy == uuid.Nil {
		approvedBy = tenantID // fallback
	}

	if err := h.approve.Execute(c.Request.Context(), tenantID, billID, approvedBy); err != nil {
		if errors.Is(err, appbill.ErrBillNotFound) {
			c.JSON(http.StatusNotFound, errResp("bill_not_found", "bill not found", ""))
			return
		}
		if errors.Is(err, appbill.ErrInvalidBillStatus) {
			c.JSON(http.StatusUnprocessableEntity, errResp("invalid_bill_status", err.Error(), "bill must be in draft status to approve"))
			return
		}
		if errors.Is(err, appbill.ErrBillApprovalConflict) {
			c.JSON(http.StatusConflict, errResp("bill_approval_conflict", "concurrent approval in progress", "retry later"))
			return
		}
		if errors.Is(err, appbill.ErrInvalidUnitForProduct) {
			c.JSON(http.StatusUnprocessableEntity, errResp("invalid_unit_for_product", err.Error(), "check unit configuration for each product"))
			return
		}
		c.JSON(http.StatusInternalServerError, errResp("internal_error", err.Error(), ""))
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "approved"})
}

// Cancel handles POST /api/v1/purchase-bills/:id/cancel
func (h *Handler) Cancel(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "tenant_id required", ""))
		return
	}
	billID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "invalid bill id", ""))
		return
	}

	if err := h.cancel.Execute(c.Request.Context(), tenantID, billID); err != nil {
		if errors.Is(err, appbill.ErrBillNotFound) {
			c.JSON(http.StatusNotFound, errResp("bill_not_found", "bill not found", ""))
			return
		}
		if errors.Is(err, appbill.ErrCannotCancelApproved) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":   "cannot_cancel_approved_bill",
				"message": "approved 单据不可直接取消，需走采购退货流程",
				"action":  "POST /api/v1/purchase-bills/" + billID.String() + "/return",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, errResp("internal_error", err.Error(), ""))
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}

// List handles GET /api/v1/purchase-bills
func (h *Handler) List(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "tenant_id required", ""))
		return
	}

	page := parseIntQuery(c, "page", 1)
	size := parseIntQuery(c, "size", 20)

	f := appbill.BillListFilter{
		TenantID: tenantID,
		Page:     page,
		Size:     size,
	}
	if s := c.Query("status"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			status := domain.BillStatus(n)
			f.Status = &status
		}
	}
	if pid := c.Query("partner_id"); pid != "" {
		if id, err := uuid.Parse(pid); err == nil {
			f.PartnerID = &id
		}
	}

	out, err := h.list.Execute(c.Request.Context(), f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errResp("internal_error", err.Error(), ""))
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": out.Items, "total": out.Total})
}

// Get handles GET /api/v1/purchase-bills/:id
func (h *Handler) Get(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "tenant_id required", ""))
		return
	}
	billID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "invalid bill id", ""))
		return
	}

	out, err := h.get.Execute(c.Request.Context(), tenantID, billID)
	if err != nil {
		if errors.Is(err, appbill.ErrBillNotFound) {
			c.JSON(http.StatusNotFound, errResp("bill_not_found", "bill not found", ""))
			return
		}
		c.JSON(http.StatusInternalServerError, errResp("internal_error", err.Error(), ""))
		return
	}
	c.JSON(http.StatusOK, gin.H{"head": out.Head, "items": out.Items})
}

// ----- Helpers -----

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

func parseIntQuery(c *gin.Context, key string, def int) int {
	if s := c.Query(key); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func buildCreateRequest(tenantID, creatorID uuid.UUID, req createRequest) (appbill.CreatePurchaseDraftRequest, error) {
	var partnerID *uuid.UUID
	if req.PartnerID != "" {
		id, err := uuid.Parse(req.PartnerID)
		if err != nil {
			return appbill.CreatePurchaseDraftRequest{}, errWithField("partner_id", "must be a valid UUID")
		}
		partnerID = &id
	}

	var warehouseID *uuid.UUID
	if req.WarehouseID != "" {
		id, err := uuid.Parse(req.WarehouseID)
		if err != nil {
			return appbill.CreatePurchaseDraftRequest{}, errWithField("warehouse_id", "must be a valid UUID")
		}
		warehouseID = &id
	}

	billDate := time.Now().UTC()
	if req.BillDate != "" {
		t, err := time.Parse(time.RFC3339, req.BillDate)
		if err != nil {
			return appbill.CreatePurchaseDraftRequest{}, errWithField("bill_date", "must be RFC3339 format")
		}
		billDate = t
	}

	shippingFee := decimal.Zero
	if req.ShippingFee != "" {
		f, err := decimal.NewFromString(req.ShippingFee)
		if err != nil {
			return appbill.CreatePurchaseDraftRequest{}, errWithField("shipping_fee", "must be a valid decimal")
		}
		shippingFee = f
	}

	taxAmount := decimal.Zero
	if req.TaxAmount != "" {
		f, err := decimal.NewFromString(req.TaxAmount)
		if err != nil {
			return appbill.CreatePurchaseDraftRequest{}, errWithField("tax_amount", "must be a valid decimal")
		}
		taxAmount = f
	}

	items := make([]appbill.CreatePurchaseItemInput, 0, len(req.Items))
	for i, it := range req.Items {
		productID, err := uuid.Parse(it.ProductID)
		if err != nil {
			return appbill.CreatePurchaseDraftRequest{}, errWithField("items["+strconv.Itoa(i)+"].product_id", "must be a valid UUID")
		}
		qty, err := decimal.NewFromString(it.Qty)
		if err != nil || qty.IsZero() || qty.IsNegative() {
			return appbill.CreatePurchaseDraftRequest{}, errWithField("items["+strconv.Itoa(i)+"].qty", "must be a positive decimal")
		}
		unitPrice := decimal.Zero
		if it.UnitPrice != "" {
			unitPrice, err = decimal.NewFromString(it.UnitPrice)
			if err != nil || unitPrice.IsNegative() {
				return appbill.CreatePurchaseDraftRequest{}, errWithField("items["+strconv.Itoa(i)+"].unit_price", "must be a non-negative decimal")
			}
		}
		var unitID *uuid.UUID
		if it.UnitID != "" {
			id, err := uuid.Parse(it.UnitID)
			if err != nil {
				return appbill.CreatePurchaseDraftRequest{}, errWithField("items["+strconv.Itoa(i)+"].unit_id", "must be a valid UUID")
			}
			unitID = &id
		}
		lineNo := it.LineNo
		if lineNo <= 0 {
			lineNo = i + 1
		}
		items = append(items, appbill.CreatePurchaseItemInput{
			ProductID: productID,
			UnitID:    unitID,
			UnitName:  it.UnitName,
			LineNo:    lineNo,
			Qty:       qty,
			UnitPrice: unitPrice,
		})
	}

	return appbill.CreatePurchaseDraftRequest{
		TenantID:    tenantID,
		CreatorID:   creatorID,
		PartnerID:   partnerID,
		WarehouseID: warehouseID,
		BillDate:    billDate,
		ShippingFee: shippingFee,
		TaxAmount:   taxAmount,
		Remark:      req.Remark,
		Items:       items,
	}, nil
}

func errWithField(field, msg string) error {
	return &fieldError{field: field, msg: msg}
}

type fieldError struct {
	field string
	msg   string
}

func (e *fieldError) Error() string {
	return e.field + ": " + e.msg
}
