// Package bill also contains sale bill Gin HTTP handlers (Story 7.1).
package bill

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// SaleHandler groups all sale bill Gin handlers.
type SaleHandler struct {
	createUC        *appbill.CreateSaleUseCase
	approveUC       *appbill.ApproveSaleUseCase
	cancelUC        *appbill.CancelPurchaseUseCase // reuse cancel use case; it's type-agnostic
	listUC          *appbill.ListPurchasesUseCase  // will filter by BillTypeSale
	billRepo        appbill.BillRepo
	quickCheckoutUC *appbill.QuickCheckoutUseCase
	listPaymentsUC  *apppayment.ListPaymentsUseCase
}

// NewSaleHandler wires a SaleHandler.
func NewSaleHandler(
	createUC *appbill.CreateSaleUseCase,
	approveUC *appbill.ApproveSaleUseCase,
	cancelUC *appbill.CancelPurchaseUseCase,
	listUC *appbill.ListPurchasesUseCase,
	billRepo appbill.BillRepo,
	quickCheckoutUC *appbill.QuickCheckoutUseCase,
	listPaymentsUC *apppayment.ListPaymentsUseCase,
) *SaleHandler {
	return &SaleHandler{
		createUC:        createUC,
		approveUC:       approveUC,
		cancelUC:        cancelUC,
		listUC:          listUC,
		billRepo:        billRepo,
		quickCheckoutUC: quickCheckoutUC,
		listPaymentsUC:  listPaymentsUC,
	}
}

// RegisterRoutes mounts all sale bill routes onto the given router group.
func (h *SaleHandler) RegisterRoutes(rg *gin.RouterGroup) {
	// quick-checkout must be registered before :id to avoid conflict
	rg.POST("/sale-bills/quick-checkout", h.QuickCheckout)
	rg.POST("/sale-bills", h.Create)
	rg.PUT("/sale-bills/:id", h.Update)
	rg.POST("/sale-bills/:id/approve", h.Approve)
	rg.POST("/sale-bills/:id/cancel", h.Cancel)
	rg.GET("/sale-bills", h.List)
	rg.GET("/sale-bills/:id", h.Get)
}

// ----- request / response types -----

type saleItemInput struct {
	ProductID   string `json:"product_id"`
	WarehouseID string `json:"warehouse_id,omitempty"`
	UnitID      string `json:"unit_id,omitempty"`
	UnitName    string `json:"unit_name,omitempty"`
	LineNo      int    `json:"line_no"`
	Qty         string `json:"qty"`
	UnitPrice   string `json:"unit_price"`
}

type createSaleRequest struct {
	PartnerID   string          `json:"partner_id,omitempty"`
	WarehouseID string          `json:"warehouse_id,omitempty"`
	BillDate    string          `json:"bill_date,omitempty"`
	ShippingFee string          `json:"shipping_fee,omitempty"`
	TaxAmount   string          `json:"tax_amount,omitempty"`
	Remark      string          `json:"remark,omitempty"`
	Items       []saleItemInput `json:"items"`
}

type approveSaleRequest struct {
	PaidAmount    string `json:"paid_amount,omitempty"`
	PaymentMethod string `json:"payment_method,omitempty"`
}

type quickCheckoutRequest struct {
	CustomerName  string          `json:"customer_name,omitempty"`
	PaymentMethod string          `json:"payment_method"`
	PaidAmount    string          `json:"paid_amount"`
	Items         []saleItemInput `json:"items"`
}

// ----- handlers -----

// Create handles POST /api/v1/sale-bills
func (h *SaleHandler) Create(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "tenant_id required", ""))
		return
	}
	creatorID := resolveCreatorID(c)
	if creatorID == uuid.Nil {
		creatorID = tenantID
	}

	var req createSaleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "invalid request body: "+err.Error(), ""))
		return
	}
	if len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "items must not be empty", ""))
		return
	}

	ucReq, err := buildCreateSaleRequest(tenantID, creatorID, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", err.Error(), ""))
		return
	}

	out, err := h.createUC.Execute(c.Request.Context(), ucReq)
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

// Update handles PUT /api/v1/sale-bills/:id — not implemented in Story 7.1.
func (h *SaleHandler) Update(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not_implemented", "message": "sale bill update coming in Story 7.2"})
}

// Approve handles POST /api/v1/sale-bills/:id/approve
func (h *SaleHandler) Approve(c *gin.Context) {
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
	creatorID := resolveCreatorID(c)
	if creatorID == uuid.Nil {
		creatorID = tenantID
	}

	var req approveSaleRequest
	_ = c.ShouldBindJSON(&req) // optional body

	paidAmount := decimal.Zero
	if req.PaidAmount != "" {
		paidAmount, err = decimal.NewFromString(req.PaidAmount)
		if err != nil {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "invalid paid_amount", ""))
			return
		}
	}

	if err := h.approveUC.Execute(c.Request.Context(), appbill.ApproveSaleRequest{
		TenantID:   tenantID,
		BillID:     billID,
		CreatorID:  creatorID,
		PaidAmount: paidAmount,
		PayType:    req.PaymentMethod,
	}); err != nil {
		if errors.Is(err, appbill.ErrBillNotFound) {
			c.JSON(http.StatusNotFound, errResp("bill_not_found", "sale bill not found", ""))
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
		var ise *appstock.InsufficientStockError
		if errors.As(err, &ise) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":     "insufficient_stock",
				"message":   err.Error(),
				"available": ise.Available.String(),
				"requested": ise.Requested.String(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, errResp("internal_error", err.Error(), ""))
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "approved"})
}

// Cancel handles POST /api/v1/sale-bills/:id/cancel
func (h *SaleHandler) Cancel(c *gin.Context) {
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

	if err := h.cancelUC.Execute(c.Request.Context(), tenantID, billID); err != nil {
		if errors.Is(err, appbill.ErrBillNotFound) {
			c.JSON(http.StatusNotFound, errResp("bill_not_found", "sale bill not found", ""))
			return
		}
		if errors.Is(err, appbill.ErrCannotCancelApproved) {
			c.JSON(http.StatusUnprocessableEntity, errResp("cannot_cancel_approved_bill", "approved 单据不可直接取消", ""))
			return
		}
		c.JSON(http.StatusInternalServerError, errResp("internal_error", err.Error(), ""))
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}

// List handles GET /api/v1/sale-bills
func (h *SaleHandler) List(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "tenant_id required", ""))
		return
	}

	page := parseIntQuery(c, "page", 1)
	size := parseIntQuery(c, "size", 20)

	f := appbill.BillListFilter{
		TenantID: tenantID,
		BillType: domain.BillTypeSale,
		Page:     page,
		Size:     size,
	}
	if s := c.Query("status"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			st := domain.BillStatus(n)
			f.Status = &st
		}
	}
	if pid := c.Query("partner_id"); pid != "" {
		if id, err := uuid.Parse(pid); err == nil {
			f.PartnerID = &id
		}
	}

	bills, total, err := h.billRepo.ListBills(c.Request.Context(), f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errResp("internal_error", err.Error(), ""))
		return
	}

	type saleListItem struct {
		domain.BillHead
		ReceivableAmount string `json:"receivable_amount"`
	}
	items := make([]saleListItem, len(bills))
	for i, b := range bills {
		items[i] = saleListItem{BillHead: b, ReceivableAmount: b.ReceivableAmount().String()}
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total})
}

// Get handles GET /api/v1/sale-bills/:id
func (h *SaleHandler) Get(c *gin.Context) {
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

	head, err := h.billRepo.GetBill(c.Request.Context(), tenantID, billID)
	if err != nil {
		if errors.Is(err, appbill.ErrBillNotFound) {
			c.JSON(http.StatusNotFound, errResp("bill_not_found", "sale bill not found", ""))
			return
		}
		c.JSON(http.StatusInternalServerError, errResp("internal_error", err.Error(), ""))
		return
	}

	items, err := h.billRepo.GetBillItems(c.Request.Context(), tenantID, billID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errResp("internal_error", err.Error(), ""))
		return
	}

	payments, err := h.listPaymentsUC.Execute(c.Request.Context(), tenantID, billID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errResp("internal_error", err.Error(), ""))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"head":              head,
		"items":             items,
		"payments":          payments,
		"receivable_amount": head.ReceivableAmount().String(),
	})
}

// QuickCheckout handles POST /api/v1/sale-bills/quick-checkout
func (h *SaleHandler) QuickCheckout(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "tenant_id required", ""))
		return
	}
	creatorID := resolveCreatorID(c)
	if creatorID == uuid.Nil {
		creatorID = tenantID
	}

	var req quickCheckoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "invalid request body: "+err.Error(), ""))
		return
	}
	if len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "items must not be empty", ""))
		return
	}

	paidAmount, err := decimal.NewFromString(req.PaidAmount)
	if err != nil {
		paidAmount = decimal.Zero
	}

	items, err := parseSaleItems(req.Items)
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", err.Error(), ""))
		return
	}

	result, err := h.quickCheckoutUC.Execute(c.Request.Context(), appbill.QuickCheckoutRequest{
		TenantID:      tenantID,
		CreatorID:     creatorID,
		CustomerName:  req.CustomerName,
		Items:         items,
		PaymentMethod: req.PaymentMethod,
		PaidAmount:    paidAmount,
	})
	if err != nil {
		if errors.Is(err, appbill.ErrValidation) {
			c.JSON(http.StatusBadRequest, errResp("validation_error", err.Error(), ""))
			return
		}
		var ise *appstock.InsufficientStockError
		if errors.As(err, &ise) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":     "insufficient_stock",
				"message":   err.Error(),
				"available": ise.Available.String(),
				"requested": ise.Requested.String(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, errResp("internal_error", err.Error(), ""))
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"bill_id":           result.BillID,
		"bill_no":           result.BillNo,
		"total_amount":      result.TotalAmount.String(),
		"receivable_amount": result.ReceivableAmount.String(),
	})
}

// ----- helpers -----

func buildCreateSaleRequest(tenantID, creatorID uuid.UUID, req createSaleRequest) (appbill.CreateSaleRequest, error) {
	var partnerID *uuid.UUID
	if req.PartnerID != "" {
		id, err := uuid.Parse(req.PartnerID)
		if err != nil {
			return appbill.CreateSaleRequest{}, errWithField("partner_id", "must be a valid UUID")
		}
		partnerID = &id
	}

	var warehouseID *uuid.UUID
	if req.WarehouseID != "" {
		id, err := uuid.Parse(req.WarehouseID)
		if err != nil {
			return appbill.CreateSaleRequest{}, errWithField("warehouse_id", "must be a valid UUID")
		}
		warehouseID = &id
	}

	billDate := time.Now().UTC()
	if req.BillDate != "" {
		t, err := time.Parse(time.RFC3339, req.BillDate)
		if err != nil {
			return appbill.CreateSaleRequest{}, errWithField("bill_date", "must be RFC3339 format")
		}
		billDate = t
	}

	shippingFee := decimal.Zero
	if req.ShippingFee != "" {
		f, err := decimal.NewFromString(req.ShippingFee)
		if err != nil {
			return appbill.CreateSaleRequest{}, errWithField("shipping_fee", "must be a valid decimal")
		}
		shippingFee = f
	}

	taxAmount := decimal.Zero
	if req.TaxAmount != "" {
		f, err := decimal.NewFromString(req.TaxAmount)
		if err != nil {
			return appbill.CreateSaleRequest{}, errWithField("tax_amount", "must be a valid decimal")
		}
		taxAmount = f
	}

	items, err := parseSaleItems(req.Items)
	if err != nil {
		return appbill.CreateSaleRequest{}, err
	}

	return appbill.CreateSaleRequest{
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

func parseSaleItems(raw []saleItemInput) ([]appbill.SaleItem, error) {
	items := make([]appbill.SaleItem, 0, len(raw))
	for i, it := range raw {
		productID, err := uuid.Parse(it.ProductID)
		if err != nil {
			return nil, errWithField("items["+strconv.Itoa(i)+"].product_id", "must be a valid UUID")
		}
		qty, err := decimal.NewFromString(it.Qty)
		if err != nil || qty.IsZero() || qty.IsNegative() {
			return nil, errWithField("items["+strconv.Itoa(i)+"].qty", "must be a positive decimal")
		}
		unitPrice := decimal.Zero
		if it.UnitPrice != "" {
			unitPrice, err = decimal.NewFromString(it.UnitPrice)
			if err != nil || unitPrice.IsNegative() {
				return nil, errWithField("items["+strconv.Itoa(i)+"].unit_price", "must be a non-negative decimal")
			}
		}
		var unitID *uuid.UUID
		if it.UnitID != "" {
			id, err := uuid.Parse(it.UnitID)
			if err != nil {
				return nil, errWithField("items["+strconv.Itoa(i)+"].unit_id", "must be a valid UUID")
			}
			unitID = &id
		}
		var warehouseID uuid.UUID
		if it.WarehouseID != "" {
			warehouseID, err = uuid.Parse(it.WarehouseID)
			if err != nil {
				return nil, errWithField("items["+strconv.Itoa(i)+"].warehouse_id", "must be a valid UUID")
			}
		}
		lineNo := it.LineNo
		if lineNo <= 0 {
			lineNo = i + 1
		}
		items = append(items, appbill.SaleItem{
			ProductID:   productID,
			WarehouseID: warehouseID,
			UnitID:      unitID,
			UnitName:    it.UnitName,
			LineNo:      lineNo,
			Qty:         qty,
			UnitPrice:   unitPrice,
		})
	}
	return items, nil
}
