package bill

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/permission"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// purchaseMoneyKeys are the purchase-bill keys a person without cost.view never
// receives: every price and amount. Quantities, products and dates stay.
var purchaseMoneyKeys = map[string]bool{
	"subtotal":        true,
	"shipping_fee":    true,
	"tax_amount":      true,
	"total_amount":    true,
	"paid_amount":     true,
	"returned_amount": true,
	"unpaid_amount":   true,
	"invoiced_amount": true,
	"payment_status":  true,
	"invoice_status":  true,
	"exchange_rate":   true,
	"amount_local":    true,
	"unit_price":      true,
	"line_amount":     true,
}

// ownerOf is the signed-in person a price-blind caller's "mine" is judged by. With
// no identifiable person it answers false and writes the refusal (fail closed): the
// tenant fallback bills are stamped with must never count as anyone's own.
func ownerOf(c *gin.Context) (uuid.UUID, bool) {
	id := resolveCreatorID(c)
	if id == uuid.Nil {
		c.JSON(http.StatusForbidden, errResp("forbidden", "无法确认你的身份，请重新登录后再试", ""))
		return uuid.Nil, false
	}
	return id, true
}

// dropPurchaseMoney zeroes every price and fee a person without cost.view sent on a
// new draft: they cannot see purchase prices, so they cannot write them either. The
// currency and rate go too (the draft is CNY at rate 1), so a rate they cannot see
// never multiplies the prices the boss fills in later. The boss or manager fills the
// prices in before approving. (An edit keeps stored prices
// instead, see UpdatePurchaseDraftRequest.KeepPrices.)
func dropPurchaseMoney(req *appbill.CreatePurchaseDraftRequest) {
	req.ShippingFee, req.TaxAmount = decimal.Zero, decimal.Zero
	req.Currency, req.ExchangeRate = "CNY", decimal.Zero
	for i := range req.Items {
		req.Items[i].UnitPrice = decimal.Zero
	}
}

// trimPurchaseMoney removes the amount keys from a purchase response.
func trimPurchaseMoney(v any) (any, error) { return permission.StripNamed(v, purchaseMoneyKeys) }

// writePurchaseJSON writes a purchase response; when trim is set, every price and
// amount is removed first.
func writePurchaseJSON(c *gin.Context, status int, v any, trim bool) {
	if !trim {
		c.JSON(status, v)
		return
	}
	out, err := trimPurchaseMoney(v)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	alignSummaryCount(out)
	c.JSON(status, out)
}

// alignSummaryCount makes a trimmed list's summary.count equal its total. The summary
// counts approved bills only, but the price-blind list holds the person's drafts and
// their bills approved in the last 7 days, so the count would fall short of the list.
func alignSummaryCount(out any) {
	body, ok := out.(map[string]any)
	if !ok {
		return
	}
	summary, ok := body["summary"].(map[string]any)
	if !ok {
		return
	}
	if total, ok := body["total"]; ok {
		summary["count"] = total
	}
}

// ownApprovedWindow is how long a person's own approved bill stays in their "my drafts".
// The list and the detail both honour it, and the page shows a status tag so a clerk on a
// phone can tell a draft from an approved bill.
const ownApprovedWindow = 7 * 24 * time.Hour

// isOwnVisible reports whether head is something owner may open without cost.view:
// their own draft, or their own bill approved within the last 7 days.
func isOwnVisible(head *domain.BillHead, owner uuid.UUID, now time.Time) bool {
	if head == nil || head.CreatorID != owner {
		return false
	}
	switch head.Status {
	case domain.StatusDraft:
		return true
	case domain.StatusApproved:
		return head.ApprovedAt != nil && !head.ApprovedAt.Before(now.Add(-ownApprovedWindow))
	}
	return false
}
