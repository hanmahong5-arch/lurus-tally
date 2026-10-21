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
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/permission"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

const msgRefundCents = "退款金额最多到分，请改成两位小数以内"

type returnItemInput struct {
	BillItemID string `json:"bill_item_id"`
	Qty        string `json:"qty"`
}

type returnSaleRequest struct {
	Items       []returnItemInput `json:"items"                  binding:"max=200"`
	WarehouseID string            `json:"warehouse_id,omitempty"`
	Remark      string            `json:"remark,omitempty"       binding:"max=500"`
	// RefundAmount is the money handed back with the return (NUMERIC string); omitted = none.
	RefundAmount string `json:"refund_amount,omitempty"`
	// RefundMethod is how it is handed back (cash, wechat, ...); empty = cash.
	RefundMethod string `json:"refund_method,omitempty" binding:"max=32"`
	// RefundOnly books the refund of a return that already went through (补记退款): no goods move.
	RefundOnly bool `json:"refund_only,omitempty"`
	// ReturnBillID is the return the missing refund belongs to; the server checks it is a
	// return of this sale and takes its number for the payment label.
	ReturnBillID string `json:"return_bill_id,omitempty" binding:"max=64"`
}

// Return handles POST /api/v1/sale-bills/:id/return
//
// The customer gives goods back: the listed lines go back into stock at the cost they
// left at, and the return is approved in the same call. Quantities are in the unit the
// line was sold in and are capped by what earlier returns left.
func (h *SaleHandler) Return(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", msgNeedLogin, ""))
		return
	}
	if h.returnUC == nil {
		c.JSON(http.StatusNotImplemented, errResp("not_implemented", "退货功能暂未开通", ""))
		return
	}
	creatorID := resolveCreatorID(c)
	if creatorID == uuid.Nil {
		creatorID = tenantID
	}
	saleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "单据编号不对，请返回列表重新打开", ""))
		return
	}

	var body returnSaleRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "请求内容格式不对，请刷新页面后重试", ""))
		return
	}
	// A cashier (return.own without bill.return) may only take back a sale they
	// rang up themselves today; everything else goes to the boss or manager.
	if !middleware.HasPerm(c, permission.BillReturn) && !h.ownSaleToday(c, tenantID, resolveCreatorID(c), saleID) {
		return
	}
	if body.RefundOnly {
		h.recordRefundOnly(c, tenantID, creatorID, saleID, body)
		return
	}
	req := appbill.ReturnSaleRequest{
		TenantID:   tenantID,
		CreatorID:  creatorID,
		SaleBillID: saleID,
		Remark:     body.Remark,
	}
	for _, it := range body.Items {
		itemID, err := uuid.Parse(it.BillItemID)
		if err != nil {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "要退的商品信息不对，请刷新页面后重试", ""))
			return
		}
		qty, err := decimalutil.Parse(it.Qty, "qty")
		if err != nil || !qty.IsPositive() {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "退货数量要填大于 0 的数字", ""))
			return
		}
		req.Items = append(req.Items, appbill.ReturnSaleLine{BillItemID: itemID, Qty: qty})
	}
	if len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, errResp("validation_error", msgNoItems, ""))
		return
	}
	if body.WarehouseID != "" {
		wh, err := uuid.Parse(body.WarehouseID)
		if err != nil {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "仓库信息不对，请刷新页面后重试", ""))
			return
		}
		req.WarehouseID = &wh
	}

	refund := decimal.Zero
	if body.RefundAmount != "" {
		if refund, err = decimalutil.Parse(body.RefundAmount, "refund_amount"); err != nil || refund.IsNegative() {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "退款金额要填不小于 0 的数字", ""))
			return
		}
		if !refund.Equal(refund.Round(2)) {
			c.JSON(http.StatusBadRequest, errResp("validation_error", msgRefundCents, ""))
			return
		}
	}
	if refund.IsPositive() {
		if h.refundUC == nil {
			c.JSON(http.StatusNotImplemented, errResp("not_implemented", "退货退款功能暂未开通", ""))
			return
		}
		out, err := h.refundUC.Execute(c.Request.Context(), appbill.SaleRefundRequest{Return: req, Amount: refund, Method: body.RefundMethod})
		if err != nil {
			writeReturnError(c, err)
			return
		}
		resp := gin.H{
			"bill_id":         out.BillID,
			"bill_no":         out.BillNo,
			"return_amount":   out.ReturnAmount.String(),
			"refunded_amount": out.Refunded.String(),
		}
		if out.RefundErr != nil {
			resp["refund_error"] = "货已退回入库，但退款没有记上，请先把这笔钱当面退给客户，再点「补记退款」把这笔钱记上"
		}
		c.JSON(http.StatusCreated, resp)
		return
	}

	out, err := h.returnUC.Execute(c.Request.Context(), req)
	if err != nil {
		writeReturnError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"bill_id":       out.BillID,
		"bill_no":       out.BillNo,
		"return_amount": out.ReturnAmount.String(),
	})
}

// msgNotYourSaleToday is what a cashier is told for a sale outside their own day.
const msgNotYourSaleToday = "这张单不是你今天开的，请找老板或店长"

// ownSaleToday checks that the sale was created by creatorID and dated today
// (Beijing time). It answers the request itself (404 / 403 / 500) and reports false
// when the return must not go ahead. Quick-checkout (POS) sales are ordinary
// sales created by the cashier, so they pass the same check.
func (h *SaleHandler) ownSaleToday(c *gin.Context, tenantID, creatorID, saleID uuid.UUID) bool {
	head, err := h.billRepo.GetBill(c.Request.Context(), tenantID, saleID)
	if err != nil {
		writeReturnError(c, err)
		return false
	}
	if creatorID == uuid.Nil || head.SubType != domain.BillSubTypeSale || head.CreatorID != creatorID || !sameBeijingDay(head.BillDate, time.Now()) {
		httperr.Write(c, httperr.Forbidden("not_own_sale_today", msgNotYourSaleToday))
		c.Abort()
		return false
	}
	return true
}

// sameBeijingDay reports whether a and b fall on the same calendar day in Beijing time.
func sameBeijingDay(a, b time.Time) bool {
	ay, am, ad := a.In(listLocation).Date()
	by, bm, bd := b.In(listLocation).Date()
	return ay == by && am == bm && ad == bd
}

// recordRefundOnly books the refund that failed to record with a return that already went through.
func (h *SaleHandler) recordRefundOnly(c *gin.Context, tenantID, creatorID, saleID uuid.UUID, body returnSaleRequest) {
	if h.refundUC == nil {
		c.JSON(http.StatusNotImplemented, errResp("not_implemented", "退货退款功能暂未开通", ""))
		return
	}
	amount, err := decimalutil.Parse(body.RefundAmount, "refund_amount")
	if err != nil || !amount.IsPositive() {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "退款金额要填大于 0 的数字", ""))
		return
	}
	if !amount.Equal(amount.Round(2)) {
		c.JSON(http.StatusBadRequest, errResp("validation_error", msgRefundCents, ""))
		return
	}
	returnID, err := uuid.Parse(body.ReturnBillID)
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "找不到这笔退款对应的退货单，请刷新页面后重试", ""))
		return
	}
	if err := h.refundUC.RecordRefund(c.Request.Context(), appbill.SaleRefundOnlyRequest{
		TenantID: tenantID, CreatorID: creatorID, SaleBillID: saleID,
		Amount: amount, Method: body.RefundMethod, ReturnBillID: returnID,
	}); err != nil {
		writeReturnError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"refunded_amount": amount.String()})
}

// writeReturnError maps the return use case's errors to HTTP statuses and shop-floor wording.
func writeReturnError(c *gin.Context, err error) {
	var over *appbill.ReturnExceededError
	var refundOver *appbill.RefundExceededError
	switch {
	case errors.Is(err, appbill.ErrInvalidPayMethod):
		c.JSON(http.StatusBadRequest, errResp("invalid_refund_method", "退款方式不支持，请选现金、微信、支付宝、刷卡或转账", ""))
	case errors.As(err, &refundOver):
		c.JSON(http.StatusUnprocessableEntity, errResp("refund_exceeds_paid",
			"退款超过客户实际付过的钱，最多能退 "+refundOver.Max.Truncate(2).String()+" 元", ""))
	case errors.Is(err, appbill.ErrRefundReturnMismatch):
		c.JSON(http.StatusBadRequest, errResp("return_not_of_sale", "这笔退款对应的退货单不属于这张销售单，请刷新页面后重试", ""))
	case errors.Is(err, appbill.ErrBillApprovalConflict):
		c.JSON(http.StatusConflict, errResp("bill_busy", "另一笔操作正在处理这张单，请稍后再试", ""))
	case errors.Is(err, appbill.ErrBillNotFound):
		c.JSON(http.StatusNotFound, errResp("bill_not_found", "找不到这张销售单", ""))
	case errors.Is(err, appbill.ErrSaleNotReturnable):
		c.JSON(http.StatusUnprocessableEntity, errResp("sale_not_returnable", "只有已审核的销售单才能退货；草稿或已作废的单据没有出过库", ""))
	case errors.Is(err, appbill.ErrReturnItemNotFound):
		c.JSON(http.StatusBadRequest, errResp("invalid_bill_item", "要退的商品不在这张销售单里，请刷新页面后重试", ""))
	case errors.As(err, &over):
		c.JSON(http.StatusUnprocessableEntity, errResp("return_exceeds_sold",
			"退货数量超过可退数量，第 "+strconv.Itoa(over.LineNo)+" 行最多还能退 "+over.Remaining.Truncate(4).String(), ""))
	case errors.Is(err, appbill.ErrNothingToReverse):
		c.JSON(http.StatusUnprocessableEntity, errResp("nothing_to_return", "这张销售单的货已经全部退完了", ""))
	case errors.Is(err, appbill.ErrWarehouseRequired):
		c.JSON(http.StatusUnprocessableEntity, errResp("warehouse_required", "单据未指定仓库，且没有默认仓库。请在「仓库」中设置默认仓，或在单据上选择仓库", ""))
	case errors.Is(err, appbill.ErrWarehouseNotFound):
		c.JSON(http.StatusBadRequest, errResp("warehouse_not_found", "找不到所选的仓库，请重新选择", ""))
	case errors.Is(err, appbill.ErrValidation):
		c.JSON(http.StatusBadRequest, errResp("validation_error", "退货内容不符合要求，请检查数量和金额后重试", ""))
	default:
		httperr.WriteInternal(c, err)
	}
}
