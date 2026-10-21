package bill

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// WithPurchaseReturn enables POST /purchase-bills/:id/return and the returned-quantity
// field on the purchase detail. Without it the route answers 501.
func (h *Handler) WithPurchaseReturn(uc *appbill.PurchaseReturnUseCase) *Handler {
	h.purchaseReturn = uc
	return h
}

type returnPurchaseRequest struct {
	Items  []returnItemInput `json:"items"            binding:"max=200"`
	Remark string            `json:"remark,omitempty" binding:"max=500"`
}

// Return handles POST /api/v1/purchase-bills/:id/return
//
// The goods go back to the supplier: the listed lines leave stock at the prevailing average
// cost and what is still owed on the purchase drops by their value, in one step.
// Quantities are in the unit the line was bought in and capped by what earlier returns left.
func (h *Handler) Return(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", msgNeedLogin, ""))
		return
	}
	if h.purchaseReturn == nil {
		c.JSON(http.StatusNotImplemented, errResp("not_implemented", "采购退货功能暂未开通", ""))
		return
	}
	creatorID := resolveCreatorID(c)
	if creatorID == uuid.Nil {
		creatorID = tenantID
	}
	billID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "单据编号不对，请返回列表重新打开", ""))
		return
	}
	var body returnPurchaseRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "请求内容格式不对，请刷新页面后重试", ""))
		return
	}
	req := appbill.PurchaseReturnRequest{
		TenantID:       tenantID,
		CreatorID:      creatorID,
		PurchaseBillID: billID,
		Remark:         body.Remark,
	}
	for i, it := range body.Items {
		itemID, err := uuid.Parse(it.BillItemID)
		if err != nil {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "退货明细有误，请刷新页面后重试", ""))
			return
		}
		qty, err := decimalutil.Parse(it.Qty, "qty")
		if err != nil || !qty.IsPositive() {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "第 "+strconv.Itoa(i+1)+" 行的退货数量要大于 0", ""))
			return
		}
		req.Items = append(req.Items, appbill.PurchaseReturnLine{BillItemID: itemID, Qty: qty})
	}
	if len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "请至少选择一行要退的商品", ""))
		return
	}

	out, err := h.purchaseReturn.Execute(c.Request.Context(), req)
	if err != nil {
		writePurchaseReturnError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"bill_id":         out.BillID,
		"bill_no":         out.BillNo,
		"return_amount":   out.ReturnAmount.String(),
		"payable_reduced": out.PayableReduced.String(),
		"supplier_owes":   out.SupplierOwes.String(),
	})
}

// writePurchaseReturnError maps the purchase return's errors to HTTP statuses and shop-floor wording.
func writePurchaseReturnError(c *gin.Context, err error) {
	var over *appbill.ReturnExceededError
	var short *appstock.InsufficientStockError
	switch {
	case errors.Is(err, appbill.ErrBillNotFound):
		c.JSON(http.StatusNotFound, errResp("bill_not_found", "找不到这张采购单", ""))
	case errors.Is(err, appbill.ErrPurchaseNotReturnable):
		c.JSON(http.StatusUnprocessableEntity, errResp("purchase_not_returnable", "只有已审核的采购单才能退货；草稿或已作废的单据没有入过库", ""))
	case errors.Is(err, appbill.ErrReturnItemNotFound):
		c.JSON(http.StatusBadRequest, errResp("invalid_bill_item", "要退的商品不在这张采购单里，请刷新页面后重试", ""))
	case errors.As(err, &over):
		c.JSON(http.StatusUnprocessableEntity, errResp("return_exceeds_bought",
			"退货数量超过可退数量，第 "+strconv.Itoa(over.LineNo)+" 行最多还能退 "+over.Remaining.Truncate(4).String(), ""))
	case errors.As(err, &short):
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":   "insufficient_stock",
			"message": "库存不够退，这批货可能已经卖出去了一部分",
			"details": []gin.H{{
				"product_id":    short.ProductID.String(),
				"available_qty": short.Available.String(),
				"requested_qty": short.Requested.String(),
			}},
		})
	case errors.Is(err, appbill.ErrWarehouseRequired):
		c.JSON(http.StatusUnprocessableEntity, errResp("warehouse_required", "单据未指定仓库，且没有默认仓库。请在「仓库」中设置默认仓，或在单据上选择仓库", ""))
	case errors.Is(err, appbill.ErrValidation):
		c.JSON(http.StatusBadRequest, errResp("validation_error", "退货内容不符合要求，请检查数量后重试", ""))
	default:
		httperr.WriteInternal(c, err)
	}
}
