package payment

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// WithReceipts enables the lump-sum receipt and statement routes.
func (h *Handler) WithReceipts(receipt *apppayment.ReceiptUseCase, statement *apppayment.StatementUseCase) *Handler {
	h.receiptUC, h.statementUC = receipt, statement
	return h
}

// businessZone cuts statement days at Asia/Shanghai wall-clock days; a fixed zone avoids
// LoadLocation, which needs tzdata that scratch images lack.
var businessZone = time.FixedZone("CST", 8*3600)

type receiptRequest struct {
	Direction     string `json:"direction"`
	PartnerID     string `json:"partner_id"`
	Amount        string `json:"amount"`
	PaymentMethod string `json:"payment_method" binding:"max=128"`
	Remark        string `json:"remark,omitempty" binding:"max=500"`
}

// parseReceiptCommon validates the fields shared by preview and create.
func parseReceiptCommon(c *gin.Context, direction, partner, amountStr string) (uuid.UUID, decimal.Decimal, bool) {
	if direction != apppayment.DirectionReceivable && direction != apppayment.DirectionPayable {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "请选择是收款还是付款", ""))
		return uuid.Nil, decimal.Zero, false
	}
	partnerID, err := uuid.Parse(partner)
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "请选择客户或供应商", ""))
		return uuid.Nil, decimal.Zero, false
	}
	amount, err := decimalutil.Parse(amountStr, "amount")
	if !moneyRe.MatchString(amountStr) || err != nil || !amount.IsPositive() || !amount.Equal(amount.Round(2)) {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "金额必须是大于 0 的数字，最多两位小数", ""))
		return uuid.Nil, decimal.Zero, false
	}
	if amount.GreaterThan(decimal.NewFromFloat(maxPaymentAmount)) {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "单笔金额不能超过 100 亿", ""))
		return uuid.Nil, decimal.Zero, false
	}
	return partnerID, amount, true
}

// PreviewReceipt handles GET /api/v1/receipts/preview?direction&partner_id&amount
func (h *Handler) PreviewReceipt(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "请先登录", ""))
		return
	}
	partnerID, amount, ok := parseReceiptCommon(c, c.Query("direction"), c.Query("partner_id"), c.Query("amount"))
	if !ok {
		return
	}
	res, err := h.receiptUC.Preview(c.Request.Context(), tenantID, c.Query("direction"), partnerID, amount)
	if err != nil {
		writeReceiptError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// CreateReceipt handles POST /api/v1/receipts
func (h *Handler) CreateReceipt(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "请先登录", ""))
		return
	}
	creatorID := resolveCreatorID(c)
	if creatorID == uuid.Nil {
		creatorID = tenantID
	}
	var req receiptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "请求内容不正确，请检查后重试", ""))
		return
	}
	partnerID, amount, ok := parseReceiptCommon(c, req.Direction, req.PartnerID, req.Amount)
	if !ok {
		return
	}
	res, err := h.receiptUC.Execute(c.Request.Context(), apppayment.ReceiptRequest{
		TenantID: tenantID, CreatorID: creatorID, Direction: req.Direction, PartnerID: partnerID,
		Amount: amount, PayType: req.PaymentMethod, Remark: req.Remark,
	})
	if err != nil {
		writeReceiptError(c, err)
		return
	}
	for range res.Allocations {
		middleware.IncPaymentCreated("CNY", tenantID.String())
	}
	c.JSON(http.StatusCreated, res)
}

// Statement handles GET /api/v1/partners/:id/statement?direction&from&to[&format=csv]
// from / to are calendar days (YYYY-MM-DD), both inclusive; they default to the current month.
func (h *Handler) Statement(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "请先登录", ""))
		return
	}
	partnerID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "请选择客户或供应商", ""))
		return
	}
	direction := c.DefaultQuery("direction", apppayment.DirectionReceivable)
	y, m, d := time.Now().In(businessZone).Date()
	from := time.Date(y, m, 1, 0, 0, 0, 0, businessZone)
	to := time.Date(y, m, d, 0, 0, 0, 0, businessZone)
	if s := c.Query("from"); s != "" {
		if from, err = time.ParseInLocation("2006-01-02", s, businessZone); err != nil {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "开始日期格式不正确", ""))
			return
		}
	}
	if s := c.Query("to"); s != "" {
		if to, err = time.ParseInLocation("2006-01-02", s, businessZone); err != nil {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "结束日期格式不正确", ""))
			return
		}
	}
	st, err := h.statementUC.Execute(c.Request.Context(), apppayment.StatementQuery{
		TenantID: tenantID, Direction: direction, PartnerID: partnerID, From: from, To: to,
	})
	if err != nil {
		writeReceiptError(c, err)
		return
	}
	if c.Query("format") == "csv" {
		writeStatementCSV(c, st)
		return
	}
	c.JSON(http.StatusOK, st)
}

var methodLabels = map[string]string{
	"cash": "现金", "wechat": "微信", "alipay": "支付宝", "card": "银行卡", "credit": "赊账", "transfer": "转账",
}

// csvText neutralises spreadsheet formulas in free text.
func csvText(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

var moneyRe = regexp.MustCompile(`^\d+(\.\d{1,2})?$`)

// statementDisposition names the download "对账单-<partner>-<from>-<to>.csv" (RFC 5987), with
// an ASCII fallback for old clients.
func statementDisposition(st *apppayment.Statement) string {
	name := strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, st.PartnerName)
	full := fmt.Sprintf("对账单-%s-%s-%s.csv", name, st.From.Format("20060102"), st.To.Format("20060102"))
	fallback := fmt.Sprintf("statement-%s-%s.csv", st.From.Format("20060102"), st.To.Format("20060102"))
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, fallback, strings.ReplaceAll(url.QueryEscape(full), "+", "%20"))
}

func writeStatementCSV(c *gin.Context, st *apppayment.Statement) {
	kindLabel := map[string]string{apppayment.LineBill: "发货", apppayment.LineReturn: "退货", apppayment.LinePayment: "收款"}
	if st.Direction == apppayment.DirectionPayable {
		kindLabel[apppayment.LineReturn] = "退货冲抵"
		kindLabel[apppayment.LineBill] = "进货"
		kindLabel[apppayment.LinePayment] = "付款"
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", statementDisposition(st))
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{"对账单", csvText(st.PartnerName), st.From.Format("2006-01-02") + " 至 " + st.To.Format("2006-01-02")})
	_ = w.Write([]string{"期初欠款", st.Opening.StringFixed(2)})
	_ = w.Write([]string{"日期", "类型", "单号", "方式", "备注", "金额", "欠款余额"})
	for _, l := range st.Lines {
		_ = w.Write([]string{l.Date.In(businessZone).Format("2006-01-02"), kindLabel[l.Kind], csvText(l.BillNo),
			methodLabels[l.Method], csvText(l.Remark), l.Delta.StringFixed(2), l.Balance.StringFixed(2)})
	}
	if !st.Adjustment.IsZero() {
		_ = w.Write([]string{"其他调整", st.Adjustment.StringFixed(2)})
	}
	_ = w.Write([]string{"期末欠款", st.Closing.StringFixed(2)})
	w.Flush()
}

func writeReceiptError(c *gin.Context, err error) {
	var exceeds *apppayment.ExceedsPartnerDebtError
	switch {
	case errors.As(err, &exceeds):
		c.JSON(http.StatusUnprocessableEntity, errResp("receipt_exceeds_debt", exceeds.Error(), ""))
	case errors.Is(err, apppayment.ErrInvalidPayType):
		c.JSON(http.StatusBadRequest, errResp("validation_error", "不支持的收付款方式，请从列表中选择", ""))
	case errors.Is(err, apppayment.ErrCreditNotPayment):
		c.JSON(http.StatusBadRequest, errResp("validation_error", "赊账不是收款方式，请选择现金、微信、支付宝等实际收到钱的方式", ""))
	case errors.Is(err, apppayment.ErrInvalidReceiptDirection):
		c.JSON(http.StatusBadRequest, errResp("validation_error", "请选择是收款还是付款", ""))
	case errors.Is(err, apppayment.ErrReceiptAmount):
		c.JSON(http.StatusBadRequest, errResp("validation_error", "金额必须是大于 0 的数字，最多两位小数", ""))
	case errors.Is(err, apppayment.ErrInvalidStatementRange):
		c.JSON(http.StatusBadRequest, errResp("validation_error", "结束日期不能早于开始日期", ""))
	case errors.Is(err, apppayment.ErrPartnerNotFound):
		c.JSON(http.StatusNotFound, errResp("partner_not_found", "找不到这个客户或供应商", ""))
	default:
		httperr.WriteInternal(c, err)
	}
}
