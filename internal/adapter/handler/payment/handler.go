// Package payment implements Gin HTTP handlers for payment endpoints.
package payment

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// Handler groups payment Gin handlers.
type Handler struct {
	recordUC *apppayment.RecordPaymentUseCase
	listUC   *apppayment.ListPaymentsUseCase

	receiptUC   *apppayment.ReceiptUseCase
	statementUC *apppayment.StatementUseCase
	moneyUC     *apppayment.MoneyUseCase
}

// New creates a Handler.
func New(recordUC *apppayment.RecordPaymentUseCase, listUC *apppayment.ListPaymentsUseCase) *Handler {
	return &Handler{recordUC: recordUC, listUC: listUC}
}

func (h *Handler) WithMoney(uc *apppayment.MoneyUseCase) *Handler {
	h.moneyUC = uc
	return h
}

// RegisterRoutes mounts payment routes.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/payments", h.Record)
	rg.GET("/payments", h.List)
	if h.receiptUC != nil && h.statementUC != nil {
		rg.GET("/receipts/preview", h.PreviewReceipt)
		rg.POST("/receipts", h.CreateReceipt)
		rg.GET("/partners/:id/statement", h.Statement)
	}
	if h.moneyUC != nil {
		rg.POST("/money-lines", h.ImportMoney)
		rg.GET("/money-lines", h.ListMoney)
		rg.POST("/money-lines/:id/match", h.MatchMoney)
		rg.POST("/money-lines/:id/ignore", h.IgnoreMoney)
	}
}

// maxPaymentAmount is the upper bound for a single payment (10 billion).
// Any amount above this is almost certainly a data-entry error or an integer
// overflow upstream, and decimal precision degrades past ~1e15 in float64.
const maxPaymentAmount = 1e10

// ----- request types -----

type recordRequest struct {
	BillID        string `json:"bill_id"`
	Amount        string `json:"amount"`
	PaymentMethod string `json:"payment_method"  binding:"max=128"`
	Remark        string `json:"remark,omitempty" binding:"max=500"`
}

// ----- handlers -----

// Record handles POST /api/v1/payments
func (h *Handler) Record(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "请先登录", ""))
		return
	}
	creatorID := resolveCreatorID(c)
	if creatorID == uuid.Nil {
		creatorID = tenantID
	}

	var req recordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "请求内容不正确，请检查后重试", ""))
		return
	}

	billID, err := uuid.Parse(req.BillID)
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "单据编号不正确", ""))
		return
	}

	amount, err := decimalutil.Parse(req.Amount, "amount")
	if err != nil || !moneyRe.MatchString(strings.TrimSpace(req.Amount)) || !amount.IsPositive() || !amount.Equal(amount.Round(2)) {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "金额必须是大于 0 的数字，最多两位小数", ""))
		return
	}
	if amount.GreaterThan(decimal.NewFromFloat(maxPaymentAmount)) {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "单笔金额不能超过 100 亿", ""))
		return
	}

	payment, err := h.recordUC.ExecuteReturning(c.Request.Context(), apppayment.RecordPaymentRequest{
		TenantID:  tenantID,
		BillID:    billID,
		CreatorID: creatorID,
		Amount:    amount,
		PayType:   req.PaymentMethod,
		Remark:    req.Remark,
	})
	if err != nil {
		writeRecordError(c, err)
		return
	}

	// Use a stable currency label. The payment domain does not carry ISO currency
	// in this endpoint yet; emit "CNY" as the canonical default so the metric is
	// immediately queryable. When multi-currency is wired, replace with the resolved value.
	middleware.IncPaymentCreated("CNY", tenantID.String())

	c.JSON(http.StatusCreated, payment)
}

// List handles GET /api/v1/payments?bill_id=...
func (h *Handler) List(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "请先登录", ""))
		return
	}

	billIDStr := c.Query("bill_id")
	if billIDStr == "" {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "请指定单据", ""))
		return
	}
	billID, err := uuid.Parse(billIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "单据编号不正确", ""))
		return
	}

	payments, err := h.listUC.Execute(c.Request.Context(), tenantID, billID)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": payments})
}

// ----- helpers -----

// writeRecordError maps a record-payment failure to a plain-Chinese response.
func writeRecordError(c *gin.Context, err error) {
	var exceeds *apppayment.ExceedsOutstandingError
	switch {
	case errors.Is(err, apppayment.ErrInvalidPayType):
		c.JSON(http.StatusBadRequest, errResp("validation_error", "不支持的收付款方式，请从列表中选择", ""))
	case errors.Is(err, apppayment.ErrCreditNotPayment):
		c.JSON(http.StatusBadRequest, errResp("validation_error", "赊账不是收款方式，请选择现金、微信、支付宝等实际收到钱的方式", ""))
	case errors.Is(err, apppayment.ErrReceiptAmount):
		c.JSON(http.StatusBadRequest, errResp("validation_error", "金额必须是大于 0 的数字，最多两位小数", ""))
	case errors.Is(err, apppayment.ErrBillNotPayable):
		c.JSON(http.StatusUnprocessableEntity, errResp("bill_not_payable", "采购退货单不需要付款，请在原采购单上处理", ""))
	case errors.Is(err, appbill.ErrBillNotFound):
		c.JSON(http.StatusNotFound, errResp("bill_not_found", "找不到这张单据", ""))
	case errors.Is(err, apppayment.ErrBillNotApproved):
		c.JSON(http.StatusUnprocessableEntity, errResp("bill_not_approved", "单据还没有审核，不能登记收付款", ""))
	case errors.As(err, &exceeds):
		c.JSON(http.StatusUnprocessableEntity, errResp("payment_exceeds_outstanding", exceeds.Error(), ""))
	default:
		httperr.WriteInternal(c, err)
	}
}

func errResp(code, message, action string) gin.H {
	h := gin.H{"error": code, "message": message}
	if action != "" {
		h["action"] = action
	}
	return h
}

// resolveTenantID returns the tenant UUID injected by AuthMiddleware.
// uuid.Nil → caller MUST return 401. No header fallback (see bill/handler.go).
func resolveTenantID(c *gin.Context) uuid.UUID {
	return middleware.GetTenantID(c)
}

// resolveCreatorID reads the creator UUID from the OIDC subject injected by
// AuthMiddleware. The X-User-ID header fallback was removed (UAT-3 Bug 2)
// because clients could spoof payment.creator_id by setting it.
func resolveCreatorID(c *gin.Context) uuid.UUID {
	return middleware.ActorUUID(c)
}

const maxMoneyFile = 1_500_000

func (h *Handler) ImportMoney(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "请先登录", ""))
		return
	}
	var req struct {
		FileBase64 string `json:"file_base64"`
		AccountRef string `json:"account_ref"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.FileBase64 == "" {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "请上传账单文件", ""))
		return
	}
	raw, err := base64.StdEncoding.DecodeString(req.FileBase64)
	if err != nil || len(raw) == 0 || len(raw) > maxMoneyFile {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "账单文件无法读取，请按月份重新导出后再上传", ""))
		return
	}
	channel, inserted, skipped, err := h.moneyUC.Import(c.Request.Context(), tenantID, req.AccountRef, raw)
	if err != nil {
		writeMoneyError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"channel": channel, "inserted": inserted, "skipped": skipped})
}

func (h *Handler) ListMoney(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "请先登录", ""))
		return
	}
	items, err := h.moneyUC.List(c.Request.Context(), tenantID)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *Handler) MatchMoney(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "请先登录", ""))
		return
	}
	lineID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "这笔流水的编号不正确", ""))
		return
	}
	var req struct {
		BillID string `json:"bill_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "请选择要记入的单据", ""))
		return
	}
	billID, err := uuid.Parse(req.BillID)
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "单据编号不正确", ""))
		return
	}
	creatorID := resolveCreatorID(c)
	if creatorID == uuid.Nil {
		creatorID = tenantID
	}
	if err := h.moneyUC.Confirm(c.Request.Context(), tenantID, creatorID, lineID, billID); err != nil {
		writeMoneyError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) IgnoreMoney(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "请先登录", ""))
		return
	}
	lineID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "这笔流水的编号不正确", ""))
		return
	}
	if err := h.moneyUC.Ignore(c.Request.Context(), tenantID, lineID); err != nil {
		writeMoneyError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
