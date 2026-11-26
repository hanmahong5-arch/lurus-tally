package bill

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
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
	case writeBatchDrawError(c, err):
		return
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

func saleLineSources(repo appbill.BillRepo, ctx context.Context, tenantID, billID uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	linker, ok := repo.(interface {
		ReturnLinks(context.Context, uuid.UUID, uuid.UUID) (map[uuid.UUID]uuid.UUID, error)
	})
	if !ok {
		return nil, nil
	}
	return linker.ReturnLinks(ctx, tenantID, billID)
}

func saleSourcePurchase(ctx context.Context, repo appbill.BillRepo, tenantID, billID uuid.UUID) (*appbill.SupplyBill, error) {
	reader, ok := repo.(appbill.SupplyReader)
	if !ok {
		return nil, nil
	}
	return reader.SourcePurchase(ctx, tenantID, billID)
}

func writeBatchDrawError(c *gin.Context, err error) bool {
	var short *appbill.BatchShortError
	if errors.As(err, &short) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":   "batch_short",
			"message": "这一批只剩 " + short.Remaining.String() + "，这行要出 " + short.Requested.String(),
		})
		return true
	}
	var wh *appbill.BatchWarehouseError
	if errors.As(err, &wh) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":   "batch_warehouse",
			"message": "这批货不在这张单的仓库里",
		})
		return true
	}
	if errors.Is(err, appbill.ErrBatchOutOfSync) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":   "batch_out_of_sync",
			"message": "批次和库存对不上，这张单没有出库。请先盘点",
		})
		return true
	}
	return false
}

// WithSupply shows which sales were opened from a purchase, and how much of each receipt is left.
func (h *Handler) WithSupply(s appbill.SupplyReader) *Handler {
	h.supply = s
	return h
}

// purchaseDetailItem is a purchase line plus how much of it has gone back to the supplier
// and, once the receipt is tracked, how much of that receipt is still unsold.
type purchaseDetailItem struct {
	*domain.BillItem
	ReturnedQty    string `json:"returned_qty"`
	BatchRemaining string `json:"batch_remaining,omitempty"`
}

func (h *Handler) attachPurchaseSupply(c *gin.Context, resp gin.H, tenantID, billID uuid.UUID, out *appbill.GetPurchaseOutput) error {
	if h.purchaseReturn != nil {
		returned, err := h.purchaseReturn.Summary(c.Request.Context(), tenantID, out.Head)
		if err != nil {
			return err
		}
		var remaining map[uuid.UUID]decimal.Decimal
		if h.supply != nil && out.Head.Status == domain.StatusApproved && out.Head.SubType == domain.BillSubTypePurchase {
			ids := make([]uuid.UUID, len(out.Items))
			for i, it := range out.Items {
				ids[i] = it.ID
			}
			remaining, err = h.supply.BatchRemaining(c.Request.Context(), tenantID, ids)
			if err != nil {
				return err
			}
		}
		lines := make([]purchaseDetailItem, len(out.Items))
		for i, it := range out.Items {
			lines[i] = purchaseDetailItem{BillItem: it, ReturnedQty: returned[it.ID].String()}
			if remaining != nil {
				if left, ok := remaining[it.ID]; ok {
					lines[i].BatchRemaining = left.String()
				}
			}
		}
		resp["items"] = lines
	}
	if h.supply == nil || out.Head.SubType != domain.BillSubTypePurchase {
		return nil
	}
	sales, err := h.supply.OpenedSales(c.Request.Context(), tenantID, billID)
	if err != nil {
		return err
	}
	if sales == nil {
		sales = []appbill.SupplyBill{}
	}
	resp["opened_sales"] = sales
	return nil
}

// saleDetailItem is a sale line plus how much of it has come back, and the purchase line it was opened from.
type saleDetailItem struct {
	*domain.BillItem
	ReturnedQty  string `json:"returned_qty"`
	SourceItemID string `json:"source_item_id,omitempty"`
}

// applyReturnFigures adds the money side of returns to a sale detail response. Goods the
// customer sent back reduce what they owe (receivable_amount is the net figure); money
// they had already paid beyond the reduced total becomes refund_due_amount, owed back to
// them and not paid out by the system.
func applyReturnFigures(resp gin.H, head *domain.BillHead, returned decimal.Decimal) {
	netTotal := decimal.Max(head.TotalAmount.Sub(returned), decimal.Zero)
	resp["returned_amount"] = returned.String()
	resp["receivable_amount"] = decimal.Max(netTotal.Sub(head.PaidAmount), decimal.Zero).String()
	resp["refund_due_amount"] = decimal.Max(head.PaidAmount.Sub(netTotal), decimal.Zero).String()
}

func attachSaleSupply(c *gin.Context, resp gin.H, repo appbill.BillRepo, ret *appbill.ReturnSaleUseCase, tenantID, billID uuid.UUID, head *domain.BillHead, items []*domain.BillItem) error {
	if ret != nil {
		sum, err := ret.Summary(c.Request.Context(), tenantID, head, items)
		if err != nil {
			return err
		}
		sources, err := saleLineSources(repo, c.Request.Context(), tenantID, billID)
		if err != nil {
			return err
		}
		lines := make([]saleDetailItem, len(items))
		for i, it := range items {
			lines[i] = saleDetailItem{BillItem: it, ReturnedQty: sum.ReturnedQty[it.ID].String()}
			if src, ok := sources[it.ID]; ok {
				lines[i].SourceItemID = src.String()
			}
		}
		resp["items"] = lines
		applyReturnFigures(resp, head, sum.ReturnedAmount)
	}
	src, err := saleSourcePurchase(c.Request.Context(), repo, tenantID, billID)
	if err != nil {
		return err
	}
	if src != nil {
		resp["source_purchase"] = src
	}
	return nil
}
