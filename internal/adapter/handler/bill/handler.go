// Package bill implements the Gin HTTP handlers for purchase bill REST endpoints.
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
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// Handler groups all purchase bill Gin handlers.
type Handler struct {
	create  *appbill.CreatePurchaseDraftUseCase
	update  *appbill.UpdatePurchaseDraftUseCase
	approve *appbill.ApprovePurchaseUseCase
	cancel  *appbill.CancelPurchaseUseCase
	list    *appbill.ListPurchasesUseCase
	get     *appbill.GetPurchaseUseCase
	restore *appbill.RestorePurchaseUseCase
	// receivables is optional; see WithReceivables.
	receivables *appbill.ReceivablesUseCase
	// lastPrices is optional; see WithLastPrices.
	lastPrices *appbill.LastPricesUseCase
	// purchaseReturn is optional; see WithPurchaseReturn.
	purchaseReturn *appbill.PurchaseReturnUseCase
	// names is optional; see WithProductNames.
	names ProductNamer
}

// ProductNamer resolves product names for the zero-price refusal. Without it the
// message names the lines only.
type ProductNamer interface {
	ProductNames(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error)
}

// WithProductNames lets the zero-price refusal name the products still at price 0.
func (h *Handler) WithProductNames(n ProductNamer) *Handler {
	h.names = n
	return h
}

// New creates a Handler wired to the provided use cases.
func New(
	create *appbill.CreatePurchaseDraftUseCase,
	update *appbill.UpdatePurchaseDraftUseCase,
	approve *appbill.ApprovePurchaseUseCase,
	cancel *appbill.CancelPurchaseUseCase,
	list *appbill.ListPurchasesUseCase,
	get *appbill.GetPurchaseUseCase,
	restore *appbill.RestorePurchaseUseCase,
) *Handler {
	return &Handler{
		create:  create,
		update:  update,
		approve: approve,
		cancel:  cancel,
		list:    list,
		get:     get,
		restore: restore,
	}
}

// RegisterRoutes mounts all purchase bill routes onto the given router group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/purchase-bills", h.Create)
	rg.PUT("/purchase-bills/:id", h.Update)
	rg.POST("/purchase-bills/:id/approve", h.Approve)
	rg.POST("/purchase-bills/:id/cancel", h.Cancel)
	rg.POST("/purchase-bills/:id/restore", h.RestorePurchase)
	rg.POST("/purchase-bills/:id/return", h.Return)
	rg.GET("/purchase-bills", h.List)
	rg.GET("/purchase-bills/:id", h.Get)
	rg.GET("/receivables", h.Receivables)
	rg.GET("/last-prices", h.LastPrices)
}

// ----- Request / Response types -----

type itemInput struct {
	ProductID string `json:"product_id"`
	UnitID    string `json:"unit_id,omitempty"   binding:"max=128"`
	UnitName  string `json:"unit_name,omitempty" binding:"max=128"`
	LineNo    int    `json:"line_no"`
	Qty       string `json:"qty"`
	UnitPrice string `json:"unit_price"`
}

type createRequest struct {
	PartnerID   string `json:"partner_id,omitempty"`
	WarehouseID string `json:"warehouse_id,omitempty"`
	BillDate    string `json:"bill_date,omitempty"` // RFC3339
	ShippingFee string `json:"shipping_fee,omitempty"`
	TaxAmount   string `json:"tax_amount,omitempty"`
	Remark      string `json:"remark,omitempty"    binding:"max=500"`
	// Currency is the 3-letter invoice currency; empty means CNY. ExchangeRate is
	// 1 unit of Currency in CNY and is required for any non-CNY currency.
	// Updating a draft does not support changing currency: Update ignores both.
	Currency     string      `json:"currency,omitempty"`
	ExchangeRate string      `json:"exchange_rate,omitempty"`
	Items        []itemInput `json:"items"               binding:"max=200,dive"`
}

// ----- Handlers -----

// Create handles POST /api/v1/purchase-bills
func (h *Handler) Create(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", msgNeedLogin, ""))
		return
	}
	creatorID := resolveCreatorID(c)
	if creatorID == uuid.Nil {
		creatorID = tenantID // fallback to tenant when JWT is not available (dev mode)
	}

	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", bindErrMsg(err), ""))
		return
	}
	if len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, errResp("validation_error", msgNoItems, ""))
		return
	}

	ucReq, err := buildCreateRequest(tenantID, creatorID, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", validationMsg(err), ""))
		return
	}
	if !middleware.CanViewCost(c) {
		dropPurchaseMoney(&ucReq)
	}

	out, err := h.create.Execute(c.Request.Context(), ucReq)
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

// Update handles PUT /api/v1/purchase-bills/:id
func (h *Handler) Update(c *gin.Context) {
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

	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "请求内容格式不对，请刷新页面后重试", ""))
		return
	}

	// Currency and rate are fixed at creation; ignore them here instead of validating.
	req.Currency, req.ExchangeRate = "", ""
	ucReq, err := buildCreateRequest(tenantID, uuid.Nil, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", validationMsg(err), ""))
		return
	}

	// Without cost.view: only the caller's own drafts, no prices written, no amounts back.
	hideMoney := !middleware.CanViewCost(c)
	var onlyCreator uuid.UUID
	if hideMoney {
		var ok bool
		if onlyCreator, ok = ownerOf(c); !ok {
			return
		}
	}

	// An omitted date keeps the draft's own date instead of jumping to now.
	billDate := ucReq.BillDate
	if req.BillDate == "" {
		billDate = time.Time{}
	}
	upReq := appbill.UpdatePurchaseDraftRequest{
		TenantID:      tenantID,
		BillID:        billID,
		OnlyCreatorID: onlyCreator,
		KeepPrices:    hideMoney,
		PartnerID:     ucReq.PartnerID,
		WarehouseID:   ucReq.WarehouseID,
		BillDate:      billDate,
		ShippingFee:   ucReq.ShippingFee,
		TaxAmount:     ucReq.TaxAmount,
		Remark:        ucReq.Remark,
		Items:         ucReq.Items,
	}

	head, err := h.update.Execute(c.Request.Context(), upReq)
	if err != nil {
		if errors.Is(err, appbill.ErrBillNotFound) {
			c.JSON(http.StatusNotFound, errResp("bill_not_found", msgPurchaseGone, ""))
			return
		}
		if errors.Is(err, appbill.ErrValidation) {
			c.JSON(http.StatusBadRequest, errResp("validation_error", validationMsg(err), ""))
			return
		}
		if errors.Is(err, appbill.ErrInvalidBillStatus) {
			c.JSON(http.StatusUnprocessableEntity, errResp("invalid_bill_status", "只有草稿采购单可以修改；已审核的单据请用「采购退货」更正", ""))
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	if hideMoney {
		c.JSON(http.StatusOK, gin.H{"bill_id": head.ID, "bill_no": head.BillNo})
		return
	}
	c.JSON(http.StatusOK, head)
}

// approvePurchaseRequest is the optional body of the approve call: pay on approval.
type approvePurchaseRequest struct {
	PaidAmount    string `json:"paid_amount,omitempty"`
	PaymentMethod string `json:"payment_method,omitempty" binding:"max=128"`
	// AllowZeroPrice confirms lines with a zero unit price are gifts booked at zero cost.
	AllowZeroPrice bool `json:"allow_zero_price,omitempty"`
}

// zeroPriceNames resolves the names of the zero-priced lines' products; a lookup
// failure only costs the names, never the refusal.
func (h *Handler) zeroPriceNames(ctx context.Context, tenantID uuid.UUID, lines []appbill.ZeroPriceLine) map[uuid.UUID]string {
	if h.names == nil || len(lines) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(lines))
	for _, l := range lines {
		ids = append(ids, l.ProductID)
	}
	m, err := h.names.ProductNames(ctx, tenantID, ids)
	if err != nil {
		return nil
	}
	return m
}

// Approve handles POST /api/v1/purchase-bills/:id/approve
func (h *Handler) Approve(c *gin.Context) {
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

	approvedBy := resolveCreatorID(c)
	if approvedBy == uuid.Nil {
		approvedBy = tenantID // fallback
	}

	// The body is optional: an empty body means "approve without payment", but a
	// malformed one must not be silently read as zero paid.
	var req approvePurchaseRequest
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "请求内容不正确，请检查后重试", ""))
			return
		}
	}
	paidAmount := decimal.Zero
	if req.PaidAmount != "" {
		paidAmount, err = decimalutil.Parse(req.PaidAmount, "paid_amount")
		if err != nil || paidAmount.IsNegative() {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "付款金额必须是不小于 0 的数字", ""))
			return
		}
	}

	if err := h.approve.ExecuteWithPayment(c.Request.Context(), appbill.ApprovePurchaseRequest{
		TenantID:       tenantID,
		BillID:         billID,
		ApprovedBy:     approvedBy,
		PaidAmount:     paidAmount,
		PayType:        req.PaymentMethod,
		AllowZeroPrice: req.AllowZeroPrice,
	}); err != nil {
		var zeroErr *appbill.ZeroPriceLinesError
		if errors.As(err, &zeroErr) {
			names := h.zeroPriceNames(c.Request.Context(), tenantID, zeroErr.Lines)
			lines := make([]gin.H, 0, len(zeroErr.Lines))
			for _, l := range zeroErr.Lines {
				lines = append(lines, gin.H{"line_no": l.LineNo, "product_id": l.ProductID.String(), "product_name": names[l.ProductID]})
			}
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":   "zero_price_lines",
				"message": zeroPriceMsg(zeroErr.Lines, names),
				"lines":   lines,
			})
			return
		}
		if errors.Is(err, appbill.ErrInvalidPayMethod) {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "不支持的收付款方式，请从列表中选择", ""))
			return
		}
		if errors.Is(err, appbill.ErrPaidExceedsTotal) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":   "paid_exceeds_total",
				"message": "付款金额 " + paidAmount.String() + " 超过单据合计",
			})
			return
		}
		if errors.Is(err, appbill.ErrBillAlreadyApproved) {
			c.JSON(http.StatusConflict, gin.H{
				"error":   "bill_already_approved",
				"message": "单据已审核，本次填写的付款未入账，请到付款中登记",
			})
			return
		}
		if errors.Is(err, appbill.ErrBillNotFound) {
			c.JSON(http.StatusNotFound, errResp("bill_not_found", msgPurchaseGone, ""))
			return
		}
		if errors.Is(err, appbill.ErrInvalidBillStatus) {
			c.JSON(http.StatusUnprocessableEntity, errResp("invalid_bill_status", msgNotDraftApprv, ""))
			return
		}
		if errors.Is(err, appbill.ErrBillApprovalConflict) {
			c.JSON(http.StatusConflict, errResp("bill_approval_conflict", msgApproveBusy, ""))
			return
		}
		if errors.Is(err, appbill.ErrInvalidUnitForProduct) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":   "invalid_unit_for_product",
				"message": "所选单位没有配置与商品基本单位的换算关系，请改用基本单位",
			})
			return
		}
		if errors.Is(err, appbill.ErrWarehouseRequired) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":   "warehouse_required",
				"message": "单据未指定仓库，且没有默认仓库。请在「仓库」中设置默认仓，或在单据上选择仓库",
			})
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "approved"})
}

// Cancel handles POST /api/v1/purchase-bills/:id/cancel
func (h *Handler) Cancel(c *gin.Context) {
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

	if err := h.cancel.Execute(c.Request.Context(), tenantID, billID); err != nil {
		if errors.Is(err, appbill.ErrBillNotFound) {
			c.JSON(http.StatusNotFound, errResp("bill_not_found", msgPurchaseGone, ""))
			return
		}
		if errors.Is(err, appbill.ErrCannotCancelApproved) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":   "cannot_cancel_approved_bill",
				"message": "已审核的采购单不能直接作废，请用「采购退货」处理",
				"action":  "POST /api/v1/purchase-bills/" + billID.String() + "/return",
			})
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}

// RestorePurchase handles POST /api/v1/purchase-bills/:id/restore.
// Sets a cancelled purchase bill back to draft status.
// Returns 409 when the bill is approved (use the purchase-return flow instead).
func (h *Handler) RestorePurchase(c *gin.Context) {
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

	// 入库 also holds customer sale returns and stock-ins that are not purchases;
	// sale drafts live under 出库. Only a purchase bill restores here.
	head, err := h.get.Head(c.Request.Context(), tenantID, billID)
	if err == nil && (head.BillType != domain.BillTypePurchase || head.SubType != domain.BillSubTypePurchase) {
		err = appbill.ErrBillNotFound
	}
	if err == nil {
		err = h.restore.Execute(c.Request.Context(), tenantID, billID)
	}
	if err != nil {
		if errors.Is(err, appbill.ErrBillNotFound) {
			c.JSON(http.StatusNotFound, errResp("bill_not_found", msgPurchaseGone, ""))
			return
		}
		if errors.Is(err, appbill.ErrCannotRestoreApproved) {
			c.JSON(http.StatusConflict, errResp(
				"cannot_restore_approved_bill",
				"已审核的采购单不能直接恢复，请用「采购退货」处理",
				"POST /api/v1/purchase-bills/"+billID.String()+"/return",
			))
			return
		}
		if errors.Is(err, appbill.ErrBillAlreadyRestoredOnce) {
			c.JSON(http.StatusConflict, errResp("bill_already_restored", "这张采购单已经恢复过一次，请重新开一张", ""))
			return
		}
		if errors.Is(err, appbill.ErrInvalidBillStatus) {
			c.JSON(http.StatusConflict, errResp("invalid_bill_status", "这张采购单的状态已变化，请刷新后再看", ""))
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "draft"})
}

// List handles GET /api/v1/purchase-bills
func (h *Handler) List(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", msgNeedLogin, ""))
		return
	}

	page := parseIntQuery(c, "page", 1)
	size := middleware.ParseLimitQuery(c, "size", 20, middleware.DefaultMaxPageLimit)

	f := appbill.BillListFilter{
		TenantID: tenantID,
		Page:     page,
		Size:     size,
	}
	if err := parseListFilter(c.Request.URL.Query(), resolveCreatorID(c), &f); err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", validationMsg(err), ""))
		return
	}
	// Without cost.view the list is "my drafts": the caller's own, drafts only
	// (mine is implied) plus what was approved in the last 7 days, and no price or amount leaves the server.
	hideMoney := !middleware.CanViewCost(c)
	if hideMoney {
		me, ok := ownerOf(c)
		if !ok {
			return
		}
		since := time.Now().Add(-ownApprovedWindow)
		f.CreatorID, f.Status, f.OwnRecentApprovedSince = &me, nil, &since
		// Payment and invoice state are hidden too; filtering by them would reveal it.
		f.PaymentStatus, f.InvoiceStatus = "", ""
	}

	out, err := h.list.Execute(c.Request.Context(), f)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	items := make([]listItem, len(out.Items))
	for i, b := range out.Items {
		items[i] = newListItem(b)
	}
	writePurchaseJSON(c, http.StatusOK, gin.H{"items": items, "total": out.Total, "summary": listSummaryJSON(out.Summary)}, hideMoney)
}

// Get handles GET /api/v1/purchase-bills/:id
func (h *Handler) Get(c *gin.Context) {
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

	out, err := h.get.Execute(c.Request.Context(), tenantID, billID)
	if err != nil {
		if errors.Is(err, appbill.ErrBillNotFound) {
			c.JSON(http.StatusNotFound, errResp("bill_not_found", msgPurchaseGone, ""))
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	hideMoney := !middleware.CanViewCost(c)
	if hideMoney {
		me, ok := ownerOf(c)
		if !ok {
			return
		}
		if !isOwnVisible(out.Head, me, time.Now()) {
			c.JSON(http.StatusNotFound, errResp("bill_not_found", "这张采购单只有老板或店长能看", ""))
			return
		}
	}
	resp := gin.H{"head": out.Head, "items": out.Items}
	if h.purchaseReturn != nil {
		returned, err := h.purchaseReturn.Summary(c.Request.Context(), tenantID, out.Head)
		if err != nil {
			httperr.WriteInternal(c, err)
			return
		}
		lines := make([]purchaseDetailItem, len(out.Items))
		for i, it := range out.Items {
			lines[i] = purchaseDetailItem{BillItem: it, ReturnedQty: returned[it.ID].String()}
		}
		resp["items"] = lines
	}
	writePurchaseJSON(c, http.StatusOK, resp, hideMoney)
}

// purchaseDetailItem is a purchase line plus how much of it has gone back to the supplier.
type purchaseDetailItem struct {
	*domain.BillItem
	ReturnedQty string `json:"returned_qty"`
}

// ----- Helpers -----

func errResp(code, message, action string) gin.H {
	h := gin.H{"error": code, "message": message}
	if action != "" {
		h["action"] = action
	}
	return h
}

// resolveTenantID returns the tenant UUID injected by AuthMiddleware (Story 2.1).
// Returns uuid.Nil when AuthMiddleware did not run or did not resolve a tenant —
// callers MUST treat this as 401. No header fallback: a misconfigured deploy
// without AuthMiddleware would otherwise let clients spoof any tenant_id.
func resolveTenantID(c *gin.Context) uuid.UUID {
	return middleware.GetTenantID(c)
}

// resolveCreatorID reads the creator UUID from the OIDC subject injected by
// AuthMiddleware. The X-User-ID header fallback was removed (UAT-3 Bug 2)
// because clients could spoof bill_head.creator_id by setting it.
func resolveCreatorID(c *gin.Context) uuid.UUID {
	return middleware.ActorUUID(c)
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
		if id != uuid.Nil {
			partnerID = &id
		}
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
		f, err := decimalutil.Parse(req.ShippingFee, "shipping_fee")
		if err != nil {
			return appbill.CreatePurchaseDraftRequest{}, errWithField("shipping_fee", "must be a valid decimal")
		}
		shippingFee = f
	}

	taxAmount := decimal.Zero
	if req.TaxAmount != "" {
		f, err := decimalutil.Parse(req.TaxAmount, "tax_amount")
		if err != nil {
			return appbill.CreatePurchaseDraftRequest{}, errWithField("tax_amount", "must be a valid decimal")
		}
		taxAmount = f
	}

	currency := req.Currency
	exchangeRate := decimal.Zero
	if currency != "" && !isUpperAlpha3(currency) {
		return appbill.CreatePurchaseDraftRequest{}, errWithField("currency", "must be a 3-letter uppercase code")
	}
	if req.ExchangeRate != "" {
		r, err := decimalutil.Parse(req.ExchangeRate, "exchange_rate")
		if err != nil || !r.IsPositive() {
			return appbill.CreatePurchaseDraftRequest{}, errWithField("exchange_rate", "must be a positive decimal")
		}
		if r.GreaterThan(maxExchangeRate) || !r.Equal(r.Round(8)) {
			return appbill.CreatePurchaseDraftRequest{}, errWithField("exchange_rate", "must be at most 1000000 with at most 8 decimal places")
		}
		exchangeRate = r
	}
	if currency != "" && currency != "CNY" && exchangeRate.IsZero() {
		return appbill.CreatePurchaseDraftRequest{}, errWithField("exchange_rate", "is required for a non-CNY currency")
	}

	items := make([]appbill.CreatePurchaseItemInput, 0, len(req.Items))
	for i, it := range req.Items {
		productID, err := uuid.Parse(it.ProductID)
		if err != nil {
			return appbill.CreatePurchaseDraftRequest{}, errWithField("items["+strconv.Itoa(i)+"].product_id", "must be a valid UUID")
		}
		qty, err := decimalutil.Parse(it.Qty, "qty")
		if err != nil || qty.IsZero() || qty.IsNegative() {
			return appbill.CreatePurchaseDraftRequest{}, errWithField("items["+strconv.Itoa(i)+"].qty", "must be a positive decimal")
		}
		unitPrice := decimal.Zero
		if it.UnitPrice != "" {
			unitPrice, err = decimalutil.Parse(it.UnitPrice, "unit_price")
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
		TenantID:     tenantID,
		CreatorID:    creatorID,
		PartnerID:    partnerID,
		WarehouseID:  warehouseID,
		BillDate:     billDate,
		ShippingFee:  shippingFee,
		TaxAmount:    taxAmount,
		Remark:       req.Remark,
		Currency:     currency,
		ExchangeRate: exchangeRate,
		Items:        items,
	}, nil
}

func isUpperAlpha3(s string) bool {
	if len(s) != 3 {
		return false
	}
	for i := 0; i < 3; i++ {
		if s[i] < 'A' || s[i] > 'Z' {
			return false
		}
	}
	return true
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

// maxExchangeRate bounds exchange_rate so the value fits NUMERIC(20,8).
var maxExchangeRate = decimal.NewFromInt(1000000)
