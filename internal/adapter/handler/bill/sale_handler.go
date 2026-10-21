// Package bill also contains sale bill Gin HTTP handlers (Story 7.1).
package bill

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/permission"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
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
	returnUC        *appbill.ReturnSaleUseCase // optional; nil = /return answers 501
	updateUC        *appbill.UpdateSaleDraftUseCase
	refundUC        *appbill.SaleRefundUseCase      // optional; nil = a return cannot carry a refund
	restoreUC       *appbill.RestorePurchaseUseCase // type-agnostic like cancel; the handler checks the bill is a sale
	memberNames     MemberNamer                     // optional; nil = pos-summary carries no cashier split
}

// MemberNamer names the people of one shop by the creator id their bills carry.
type MemberNamer interface {
	MemberNames(ctx context.Context, tenantID uuid.UUID) (map[uuid.UUID]string, error)
}

// WithMemberNames lets the shop-wide pos-summary list its cashiers by name. Returns the
// receiver for chaining.
func (h *SaleHandler) WithMemberNames(n MemberNamer) *SaleHandler {
	h.memberNames = n
	return h
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
		updateUC:        appbill.NewUpdateSaleDraftUseCase(billRepo),
		restoreUC:       appbill.NewRestorePurchaseUseCase(billRepo),
	}
}

// WithRefunds lets POST /sale-bills/:id/return carry refund_amount / refund_method, booking
// the money handed back as a negative payment on the sale. Returns the receiver for chaining.
func (h *SaleHandler) WithRefunds(uc *appbill.SaleRefundUseCase) *SaleHandler {
	h.refundUC = uc
	return h
}

// WithReturn enables POST /sale-bills/:id/return and the returned-quantity fields on the
// detail response. Returns the receiver for chaining.
func (h *SaleHandler) WithReturn(uc *appbill.ReturnSaleUseCase) *SaleHandler {
	h.returnUC = uc
	return h
}

// RegisterRoutes mounts all sale bill routes onto the given router group.
func (h *SaleHandler) RegisterRoutes(rg *gin.RouterGroup) {
	// quick-checkout must be registered before :id to avoid conflict
	rg.POST("/sale-bills/quick-checkout", h.QuickCheckout)
	rg.GET("/sale-bills/pos-summary", h.POSSummary)
	rg.POST("/sale-bills", h.Create)
	rg.PUT("/sale-bills/:id", h.Update)
	rg.POST("/sale-bills/:id/approve", h.Approve)
	rg.POST("/sale-bills/:id/cancel", h.Cancel)
	rg.POST("/sale-bills/:id/restore", h.Restore)
	rg.POST("/sale-bills/:id/return", h.Return)
	rg.GET("/sale-bills", h.List)
	rg.GET("/sale-bills/:id", h.Get)
}

// ----- request / response types -----

type saleItemInput struct {
	ProductID   string `json:"product_id"`
	WarehouseID string `json:"warehouse_id,omitempty"`
	UnitID      string `json:"unit_id,omitempty"    binding:"max=128"`
	UnitName    string `json:"unit_name,omitempty"  binding:"max=128"`
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
	Remark      string          `json:"remark,omitempty"     binding:"max=500"`
	Items       []saleItemInput `json:"items"                binding:"max=200,dive"`
}

type approveSaleRequest struct {
	PaidAmount    string `json:"paid_amount,omitempty"`
	PaymentMethod string `json:"payment_method,omitempty" binding:"max=128"`
}

type quickCheckoutRequest struct {
	PartnerID    string `json:"partner_id,omitempty"`
	CustomerName string `json:"customer_name,omitempty" binding:"max=128"`
	// Remark is the cashier's free note, kept on the bill for cash and credit sales alike.
	Remark        string `json:"remark,omitempty"        binding:"max=200"`
	PaymentMethod string `json:"payment_method"          binding:"max=128"`
	PaidAmount    string `json:"paid_amount"`
	// DiscountAmount is the whole-bill discount as a NUMERIC string; omitted = no discount.
	DiscountAmount string          `json:"discount_amount,omitempty"`
	Items          []saleItemInput `json:"items"                   binding:"max=200,dive"`

	// Offline-till replay support. ClientRef is the till's idempotency reference (a repeat is
	// answered 409 checkout_already_exists with the booked bill); Source is "pos" (default) or
	// "pos_offline" (may take stock below zero); OccurredAt is the RFC3339 time of the sale.
	ClientRef  string `json:"client_ref,omitempty"  binding:"max=64"`
	Source     string `json:"source,omitempty"      binding:"max=20"`
	OccurredAt string `json:"occurred_at,omitempty" binding:"max=64"`
}

// ----- handlers -----

// Create handles POST /api/v1/sale-bills
func (h *SaleHandler) Create(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", msgNeedLogin, ""))
		return
	}
	creatorID := resolveCreatorID(c)
	if creatorID == uuid.Nil {
		creatorID = tenantID
	}

	var req createSaleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", bindErrMsg(err), ""))
		return
	}
	if len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, errResp("validation_error", msgNoItems, ""))
		return
	}

	ucReq, err := buildCreateSaleRequest(tenantID, creatorID, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", validationMsg(err), ""))
		return
	}

	out, err := h.createUC.Execute(c.Request.Context(), ucReq)
	if err != nil {
		if errors.Is(err, appbill.ErrValidation) {
			c.JSON(http.StatusBadRequest, errResp("validation_error", validationMsg(err), ""))
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"bill_id": out.BillID, "bill_no": out.BillNo})
}

// Update handles PUT /api/v1/sale-bills/:id — replaces the lines of a draft. An approved
// sale has moved stock and money and is corrected by a return instead.
func (h *SaleHandler) Update(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", msgNeedLogin, ""))
		return
	}
	billID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", msgBadBillID, ""))
		return
	}
	var req createSaleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "请求内容格式不对，请刷新页面后重试", ""))
		return
	}
	if len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "请至少添加一行商品", ""))
		return
	}
	ucReq, err := buildCreateSaleRequest(tenantID, uuid.Nil, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", validationMsg(err), ""))
		return
	}
	// An omitted date keeps the draft's own date instead of jumping to now.
	billDate := ucReq.BillDate
	if req.BillDate == "" {
		billDate = time.Time{}
	}

	head, err := h.updateUC.Execute(c.Request.Context(), appbill.UpdateSaleDraftRequest{
		TenantID:    tenantID,
		BillID:      billID,
		PartnerID:   ucReq.PartnerID,
		WarehouseID: ucReq.WarehouseID,
		BillDate:    billDate,
		ShippingFee: ucReq.ShippingFee,
		TaxAmount:   ucReq.TaxAmount,
		Remark:      ucReq.Remark,
		Items:       ucReq.Items,
	})
	if err != nil {
		switch {
		case errors.Is(err, appbill.ErrBillNotFound):
			c.JSON(http.StatusNotFound, errResp("bill_not_found", "找不到这张销售单", ""))
		case errors.Is(err, appbill.ErrInvalidBillStatus):
			c.JSON(http.StatusUnprocessableEntity, errResp("invalid_bill_status", "只有草稿销售单可以修改；已审核的单据请用「退货」更正", ""))
		case errors.Is(err, appbill.ErrValidation), errors.Is(err, appbill.ErrNegativeFee):
			c.JSON(http.StatusBadRequest, errResp("validation_error", validationMsg(err), ""))
		default:
			httperr.WriteInternal(c, err)
		}
		return
	}
	c.JSON(http.StatusOK, head)
}

// Approve handles POST /api/v1/sale-bills/:id/approve
func (h *SaleHandler) Approve(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", msgNeedLogin, ""))
		return
	}
	billID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", msgBadBillID, ""))
		return
	}
	creatorID := resolveCreatorID(c)
	if creatorID == uuid.Nil {
		creatorID = tenantID
	}

	// The body is optional: an empty body means "approve without payment", but a
	// malformed one must not be silently read as zero paid.
	var req approveSaleRequest
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, errResp("validation_error", bindErrMsg(err), ""))
			return
		}
	}

	paidAmount := decimal.Zero
	if req.PaidAmount != "" {
		paidAmount, err = decimalutil.Parse(req.PaidAmount, "paid_amount")
		if err != nil || paidAmount.IsNegative() {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "实收金额要填不小于 0 的数字", ""))
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
			c.JSON(http.StatusNotFound, errResp("bill_not_found", msgSaleGone, ""))
			return
		}
		if errors.Is(err, appbill.ErrInvalidBillStatus) {
			c.JSON(http.StatusUnprocessableEntity, errResp("invalid_bill_status", msgNotDraftApprv, ""))
			return
		}
		if errors.Is(err, appbill.ErrInvalidPayMethod) {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "不支持的收付款方式，请从列表中选择", ""))
			return
		}
		if errors.Is(err, appbill.ErrValidation) {
			c.JSON(http.StatusBadRequest, errResp("validation_error", validationMsg(err), ""))
			return
		}
		if errors.Is(err, appbill.ErrPaidExceedsTotal) {
			writePaidExceedsTotal(c, billTotal(c, h.billRepo, tenantID, billID), paidAmount)
			return
		}
		if errors.Is(err, appbill.ErrBillAlreadyApproved) {
			c.JSON(http.StatusConflict, gin.H{
				"error":   "bill_already_approved",
				"message": "单据已审核，本次填写的收款未入账，请到收款中登记",
			})
			return
		}
		if errors.Is(err, appbill.ErrBillApprovalConflict) {
			c.JSON(http.StatusConflict, errResp("bill_approval_conflict", msgApproveBusy, ""))
			return
		}
		if errors.Is(err, appbill.ErrWarehouseRequired) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":   "warehouse_required",
				"message": "单据未指定仓库，且没有默认仓库。请在「仓库」中设置默认仓，或在单据上选择仓库",
			})
			return
		}
		if errors.Is(err, appbill.ErrInvalidUnitForProduct) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":   "invalid_unit_for_product",
				"message": "所选单位没有配置与商品基本单位的换算关系，请改用基本单位",
			})
			return
		}
		var bise *appstock.BatchInsufficientStockError
		if errors.As(err, &bise) {
			type detail struct {
				ProductID    string `json:"product_id"`
				AvailableQty string `json:"available_qty"`
				RequestedQty string `json:"requested_qty"`
			}
			d := make([]detail, len(bise.Shortages))
			for i, s := range bise.Shortages {
				d[i] = detail{
					ProductID:    s.ProductID.String(),
					AvailableQty: s.Available.String(),
					RequestedQty: s.Requested.String(),
				}
			}
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":   "insufficient_stock",
				"message": msgShortOfStock,
				"details": d,
			})
			return
		}
		// Legacy single-shortage path (stock handler or other callers).
		var ise *appstock.InsufficientStockError
		if errors.As(err, &ise) {
			type detail struct {
				ProductID    string `json:"product_id"`
				AvailableQty string `json:"available_qty"`
				RequestedQty string `json:"requested_qty"`
			}
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":   "insufficient_stock",
				"message": msgShortOfStock,
				"details": []detail{{
					ProductID:    ise.ProductID.String(),
					AvailableQty: ise.Available.String(),
					RequestedQty: ise.Requested.String(),
				}},
			})
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "approved"})
}

// Cancel handles POST /api/v1/sale-bills/:id/cancel
func (h *SaleHandler) Cancel(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", msgNeedLogin, ""))
		return
	}
	billID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", msgBadBillID, ""))
		return
	}

	if err := h.cancelUC.Execute(c.Request.Context(), tenantID, billID); err != nil {
		if errors.Is(err, appbill.ErrBillNotFound) {
			c.JSON(http.StatusNotFound, errResp("bill_not_found", msgSaleGone, ""))
			return
		}
		if errors.Is(err, appbill.ErrCannotCancelApproved) {
			c.JSON(http.StatusUnprocessableEntity, errResp("cannot_cancel_approved_bill",
				"已审核的销售单不能直接取消，客户退货请在销售单详情里点“退货”",
				"POST /api/v1/sale-bills/"+billID.String()+"/return"))
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	middleware.IncBillCancelled("sale", tenantID.String())
	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}

// Restore handles POST /api/v1/sale-bills/:id/restore.
// Sets a cancelled sale draft back to draft (the "撤销" toast after cancelling).
// Returns 409 when the bill is approved; a return goes through the return flow instead.
func (h *SaleHandler) Restore(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", msgNeedLogin, ""))
		return
	}
	billID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", msgBadBillID, ""))
		return
	}

	head, err := h.billRepo.GetBill(c.Request.Context(), tenantID, billID)
	// 出库 also holds supplier returns (采购退货): those are not sales and are not restorable here.
	// A customer sale return is stored as 入库, so it never reaches this endpoint either.
	if err == nil && (head.BillType != domain.BillTypeSale || head.SubType != domain.BillSubTypeSale) {
		err = appbill.ErrBillNotFound
	}
	if err == nil {
		err = h.restoreUC.Execute(c.Request.Context(), tenantID, billID)
	}
	if err != nil {
		if errors.Is(err, appbill.ErrBillNotFound) {
			c.JSON(http.StatusNotFound, errResp("bill_not_found", msgSaleGone, ""))
			return
		}
		if errors.Is(err, appbill.ErrCannotRestoreApproved) {
			c.JSON(http.StatusConflict, errResp("cannot_restore_approved_bill",
				"已审核的销售单不能直接恢复，客户退货请在销售单详情里点“退货”",
				"POST /api/v1/sale-bills/"+billID.String()+"/return"))
			return
		}
		if errors.Is(err, appbill.ErrBillAlreadyRestoredOnce) {
			c.JSON(http.StatusConflict, errResp("bill_already_restored",
				"这张销售单已经恢复过一次，请重新开一张", ""))
			return
		}
		if errors.Is(err, appbill.ErrInvalidBillStatus) {
			c.JSON(http.StatusConflict, errResp("invalid_bill_status",
				"这张销售单的状态已变化，请刷新后再看", ""))
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "draft"})
}

// List handles GET /api/v1/sale-bills
func (h *SaleHandler) List(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", msgNeedLogin, ""))
		return
	}

	page := parseIntQuery(c, "page", 1)
	size := middleware.ParseLimitQuery(c, "size", 20, middleware.DefaultMaxPageLimit)

	f := appbill.BillListFilter{
		TenantID: tenantID,
		BillType: domain.BillTypeSale,
		// Outbound bills also hold returns to suppliers; only sales belong in this list.
		SubType: domain.BillSubTypeSale,
		Page:    page,
		Size:    size,
	}
	if err := parseListFilter(c.Request.URL.Query(), resolveCreatorID(c), &f); err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", validationMsg(err), ""))
		return
	}

	bills, total, err := h.billRepo.ListBills(c.Request.Context(), f)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	summary, err := h.billRepo.SummarizeBills(c.Request.Context(), f)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}

	items := make([]saleListItem, len(bills))
	for i, b := range bills {
		items[i] = saleListItem{listItem: newListItem(b), ReceivableAmount: decimal.Max(decimal.Max(b.TotalAmount.Sub(b.ReturnedAmount), decimal.Zero).Sub(b.PaidAmount), decimal.Zero).String()}
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "summary": listSummaryJSON(summary)})
}

// Get handles GET /api/v1/sale-bills/:id
func (h *SaleHandler) Get(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", msgNeedLogin, ""))
		return
	}
	billID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", msgBadBillID, ""))
		return
	}

	head, err := h.billRepo.GetBill(c.Request.Context(), tenantID, billID)
	if err != nil {
		if errors.Is(err, appbill.ErrBillNotFound) {
			c.JSON(http.StatusNotFound, errResp("bill_not_found", msgSaleGone, ""))
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	if head.SubType != domain.BillSubTypeSale && head.SubType != domain.BillSubTypeSaleReturn {
		// Purchase bills and supplier returns carry purchase prices; they are read
		// through the purchase side, which is gated by cost permission. Only sales
		// and sale returns (incl. their print views) are served here.
		c.JSON(http.StatusNotFound, errResp("bill_not_found", "找不到这张销售单", ""))
		return
	}

	items, err := h.billRepo.GetBillItems(c.Request.Context(), tenantID, billID)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}

	payments, err := h.listPaymentsUC.Execute(c.Request.Context(), tenantID, billID)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}

	resp := gin.H{
		"head":              head,
		"items":             items,
		"payments":          payments,
		"receivable_amount": head.ReceivableAmount().String(),
	}
	if h.returnUC != nil {
		sum, err := h.returnUC.Summary(c.Request.Context(), tenantID, head, items)
		if err != nil {
			httperr.WriteInternal(c, err)
			return
		}
		lines := make([]saleDetailItem, len(items))
		for i, it := range items {
			lines[i] = saleDetailItem{BillItem: it, ReturnedQty: sum.ReturnedQty[it.ID].String()}
		}
		resp["items"] = lines
		applyReturnFigures(resp, head, sum.ReturnedAmount)
	}
	c.JSON(http.StatusOK, resp)
}

// saleDetailItem is a sale line plus how much of it has come back.
type saleDetailItem struct {
	*domain.BillItem
	ReturnedQty string `json:"returned_qty"`
}

// applyReturnFigures adds the money side of returns to a sale detail response. Goods the
// customer sent back reduce what they owe (receivable_amount is the net figure); money
// they had already paid beyond the reduced total becomes refund_due_amount, owed back to
// them and not paid out by the system (see docs/plans/2026-10-10-return-receivable-decision.md).
func applyReturnFigures(resp gin.H, head *domain.BillHead, returned decimal.Decimal) {
	netTotal := decimal.Max(head.TotalAmount.Sub(returned), decimal.Zero)
	resp["returned_amount"] = returned.String()
	resp["receivable_amount"] = decimal.Max(netTotal.Sub(head.PaidAmount), decimal.Zero).String()
	resp["refund_due_amount"] = decimal.Max(head.PaidAmount.Sub(netTotal), decimal.Zero).String()
}

// QuickCheckout handles POST /api/v1/sale-bills/quick-checkout
func (h *SaleHandler) QuickCheckout(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", msgNeedLogin, ""))
		return
	}
	creatorID := resolveCreatorID(c)
	if creatorID == uuid.Nil {
		creatorID = tenantID
	}

	var req quickCheckoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", bindErrMsg(err), ""))
		return
	}
	if len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, errResp("validation_error", msgNoItems, ""))
		return
	}

	paidAmount := decimal.Zero
	if req.PaidAmount != "" {
		p, err := decimalutil.Parse(req.PaidAmount, "paid_amount")
		if err != nil || p.IsNegative() {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "实收金额要填不小于 0 的数字", ""))
			return
		}
		paidAmount = p
	}

	discountAmount := decimal.Zero
	if req.DiscountAmount != "" {
		d, err := decimalutil.Parse(req.DiscountAmount, "discount_amount")
		if err != nil || d.IsNegative() {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "优惠金额要填不小于 0 的数字", ""))
			return
		}
		discountAmount = d
	}

	var partnerID *uuid.UUID
	if req.PartnerID != "" {
		id, err := uuid.Parse(req.PartnerID)
		if err != nil {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "客户选择有误，请刷新页面后重试", ""))
			return
		}
		if id != uuid.Nil {
			partnerID = &id
		}
	}

	// A credit sale without a customer would book a debt nobody owes.
	if req.PaymentMethod == "credit" && partnerID == nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "赊账必须选择客户", ""))
		return
	}

	var occurredAt time.Time
	if req.OccurredAt != "" {
		t, perr := time.Parse(time.RFC3339, req.OccurredAt)
		if perr != nil {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "收银时间格式不正确", ""))
			return
		}
		occurredAt = t
	}

	items, err := parseSaleItems(req.Items)
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", validationMsg(err), ""))
		return
	}

	result, err := h.quickCheckoutUC.Execute(c.Request.Context(), appbill.QuickCheckoutRequest{
		TenantID:      tenantID,
		CreatorID:     creatorID,
		PartnerID:     partnerID,
		CustomerName:  req.CustomerName,
		Remark:        req.Remark,
		Items:         items,
		PaymentMethod: req.PaymentMethod,
		PaidAmount:    paidAmount,

		DiscountAmount: discountAmount,
		ClientRef:      req.ClientRef,
		Source:         req.Source,
		OccurredAt:     occurredAt,
	})
	if err != nil {
		var dup *appbill.DuplicateCheckoutError
		if errors.As(err, &dup) {
			// The till replayed a checkout that is already booked: hand back the bill so the
			// queue entry can be marked done.
			c.JSON(http.StatusConflict, gin.H{
				"error":   "checkout_already_exists",
				"message": "这笔收银已经入账，无需重复提交",
				"id":      dup.BillID,
				"bill_no": dup.BillNo,
			})
			return
		}
		if errors.Is(err, appbill.ErrInvalidPayMethod) {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "不支持的收付款方式，请从列表中选择", ""))
			return
		}
		if errors.Is(err, appbill.ErrValidation) {
			c.JSON(http.StatusBadRequest, errResp("validation_error", validationMsg(err), ""))
			return
		}
		if errors.Is(err, appbill.ErrWarehouseRequired) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":   "warehouse_required",
				"message": "单据未指定仓库，且没有默认仓库。请在「仓库」中设置默认仓，或在单据上选择仓库",
			})
			return
		}
		if errors.Is(err, appbill.ErrInvalidUnitForProduct) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":   "invalid_unit_for_product",
				"message": "所选单位没有配置与商品基本单位的换算关系，请改用基本单位",
			})
			return
		}
		var bise *appstock.BatchInsufficientStockError
		if errors.As(err, &bise) {
			type detail struct {
				ProductID    string `json:"product_id"`
				AvailableQty string `json:"available_qty"`
				RequestedQty string `json:"requested_qty"`
			}
			d := make([]detail, len(bise.Shortages))
			for i, s := range bise.Shortages {
				d[i] = detail{
					ProductID:    s.ProductID.String(),
					AvailableQty: s.Available.String(),
					RequestedQty: s.Requested.String(),
				}
			}
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":   "insufficient_stock",
				"message": msgShortOfStock,
				"details": d,
			})
			return
		}
		var ise *appstock.InsufficientStockError
		if errors.As(err, &ise) {
			type detail struct {
				ProductID    string `json:"product_id"`
				AvailableQty string `json:"available_qty"`
				RequestedQty string `json:"requested_qty"`
			}
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":   "insufficient_stock",
				"message": msgShortOfStock,
				"details": []detail{{
					ProductID:    ise.ProductID.String(),
					AvailableQty: ise.Available.String(),
					RequestedQty: ise.Requested.String(),
				}},
			})
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"bill_id":           result.BillID,
		"bill_no":           result.BillNo,
		"total_amount":      result.TotalAmount.String(),
		"receivable_amount": result.ReceivableAmount.String(),
		"change_amount":     result.ChangeAmount.String(),
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
		f, err := decimalutil.Parse(req.ShippingFee, "shipping_fee")
		if err != nil {
			return appbill.CreateSaleRequest{}, errWithField("shipping_fee", "must be a valid decimal")
		}
		shippingFee = f
	}

	taxAmount := decimal.Zero
	if req.TaxAmount != "" {
		f, err := decimalutil.Parse(req.TaxAmount, "tax_amount")
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
		qty, err := decimalutil.Parse(it.Qty, "qty")
		if err != nil || qty.IsZero() || qty.IsNegative() {
			return nil, errWithField("items["+strconv.Itoa(i)+"].qty", "must be a positive decimal")
		}
		unitPrice := decimal.Zero
		if it.UnitPrice != "" {
			unitPrice, err = decimalutil.Parse(it.UnitPrice, "unit_price")
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

// billTotal reads the bill total for the over-payment message; it returns "" when the
// bill cannot be read, in which case the message falls back to a generic wording.
func billTotal(c *gin.Context, repo appbill.BillRepo, tenantID, billID uuid.UUID) string {
	head, err := repo.GetBill(c.Request.Context(), tenantID, billID)
	if err != nil {
		return ""
	}
	return head.TotalAmount.String()
}

// writePaidExceedsTotal answers 422 for a tendered amount larger than the bill total.
func writePaidExceedsTotal(c *gin.Context, total string, paid decimal.Decimal) {
	msg := "实收金额 " + paid.String() + " 超过单据合计 " + total
	if total == "" {
		msg = "实收金额 " + paid.String() + " 超过单据合计"
	}
	c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "paid_exceeds_total", "message": msg})
}

// POSSummary handles GET /api/v1/sale-bills/pos-summary?date=YYYY-MM-DD[&mine=1]
//
// mine=1 narrows the shift to the signed-in person's own bills, payments and returns.
// cashier=<key> (only for whoever holds bill.return) narrows it to that person instead; anyone
// else's cashier is ignored. A shop-wide answer for a bill.return holder also carries
// cashiers[], the per-creator split; key is the creator id and is only ever a parameter.
//
// Shift-end totals for one Beijing calendar day (default today): approved sale bills,
// what each payment method collected, the unpaid (on-credit) remainder net of returns, and the approved
// sales returns dated that day. Sales amounts are gross of returns, which are reported apart.
func (h *SaleHandler) POSSummary(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", msgNeedLogin, ""))
		return
	}
	repo, ok := h.billRepo.(appbill.POSSummaryRepo)
	if !ok {
		c.JSON(http.StatusNotImplemented, errResp("not_implemented", "今日结单暂未开通", ""))
		return
	}
	from, to, err := appbill.ParsePOSDate(c.Query("date"), time.Now())
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "日期格式不正确，应为 年-月-日", ""))
		return
	}
	// Someone who cannot take returns for any bill (cashier) only ever balances their
	// own till, whatever mine says: the shop-wide takings are not theirs to see.
	m := c.Query("mine")
	if m != "" && m != "0" && m != "1" {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "“只看我的”参数不正确", ""))
		return
	}
	canSeeAll := middleware.HasPerm(c, permission.BillReturn)
	cashierKey := c.Query("cashier")
	var other uuid.UUID
	if canSeeAll && cashierKey != "" {
		id, perr := uuid.Parse(cashierKey)
		if perr != nil {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "收银员参数不正确", ""))
			return
		}
		other = id
	}
	var only *uuid.UUID
	switch {
	case m == "1" || !canSeeAll:
		me := resolveCreatorID(c)
		if me == uuid.Nil {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "无法识别当前用户，不能只看我的结单", ""))
			return
		}
		only = &me
	case cashierKey != "":
		only = &other
	}
	out, err := appbill.GetPOSSummary(c.Request.Context(), repo, tenantID, from, to, only)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	credit := out.CreditAmount
	byPay := make([]gin.H, len(out.ByPayType))
	for i, t := range out.ByPayType {
		byPay[i] = gin.H{"pay_type": t.PayType, "count": t.Count, "amount": t.Amount.StringFixed(2)}
	}
	resp := gin.H{
		"date":          out.Date,
		"count":         out.Count,
		"total_amount":  out.TotalAmount.StringFixed(2),
		"paid_amount":   out.PaidAmount.StringFixed(2),
		"credit_amount": credit.StringFixed(2),
		"by_pay_type":   byPay,
		"return_count":  out.ReturnCount,
		"refund_count":  out.RefundCount,
		"refund_amount": out.RefundAmount.StringFixed(2),
		"return_amount": out.ReturnAmount.StringFixed(2),
	}
	if canSeeAll && m != "1" && cashierKey == "" {
		if list, ok, cerr := h.posCashiers(c, tenantID, from, to); cerr != nil {
			httperr.WriteInternal(c, cerr)
			return
		} else if ok {
			resp["cashiers"] = list
		}
	}
	c.JSON(http.StatusOK, resp)
}

// posCashiers builds the per-creator split of the day. ok is false when the repo or the name
// source is not wired, in which case the response simply has no cashiers key.
func (h *SaleHandler) posCashiers(c *gin.Context, tenantID uuid.UUID, from, to time.Time) ([]gin.H, bool, error) {
	cr, ok := h.billRepo.(appbill.POSCashierRepo)
	if !ok || h.memberNames == nil {
		return nil, false, nil
	}
	rows, err := appbill.GetPOSCashiers(c.Request.Context(), cr, tenantID, from, to)
	if err != nil {
		return nil, false, err
	}
	names, err := h.memberNames.MemberNames(c.Request.Context(), tenantID)
	if err != nil {
		return nil, false, err
	}
	list := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		name, known := names[r.CreatorID]
		if !known || r.CreatorID == uuid.Nil {
			name = "已移出的成员"
		}
		list = append(list, gin.H{
			"key":           r.CreatorID.String(),
			"name":          name,
			"count":         r.Count,
			"total_amount":  r.TotalAmount.StringFixed(2),
			"paid_amount":   r.PaidAmount.StringFixed(2),
			"refund_amount": r.RefundAmount.StringFixed(2),
		})
	}
	return list, true, nil
}
