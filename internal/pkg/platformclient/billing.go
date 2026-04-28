package platformclient

import (
	"context"
	"net/http"
	"net/url"
)

// Account is the subset of platform's account record Tally cares about.
type Account struct {
	ID          int64  `json:"id"`
	ZitadelSub  string `json:"zitadel_sub"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name,omitempty"`
}

// SubscriptionSnapshot is the active subscription stub used for entitlement checks.
type SubscriptionSnapshot struct {
	PlanCode  string `json:"plan_code"`
	Status    string `json:"status"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// Wallet mirrors the wallet block in /internal/v1/accounts/:id/overview.
type Wallet struct {
	Available float64 `json:"available"`
	Frozen    float64 `json:"frozen"`
	Total     float64 `json:"total"`
}

// AccountOverview is what /internal/v1/accounts/:id/overview returns.
type AccountOverview struct {
	Account struct {
		ID         int64  `json:"id"`
		Username   string `json:"username"`
		Email      string `json:"email"`
		VipTier    string `json:"vip_tier"`
		VipExpires string `json:"vip_expires_at,omitempty"`
	} `json:"account"`
	Wallet       *Wallet               `json:"wallet,omitempty"`
	Subscription *SubscriptionSnapshot `json:"subscription,omitempty"`
	Entitlements map[string]string     `json:"entitlements,omitempty"`
}

// SubscriptionCheckoutRequest mirrors POST /internal/v1/subscriptions/checkout body.
type SubscriptionCheckoutRequest struct {
	AccountID     int64  `json:"account_id"`
	ProductID     string `json:"product_id"`
	PlanCode      string `json:"plan_code"`
	BillingCycle  string `json:"billing_cycle"`
	PaymentMethod string `json:"payment_method"` // "wallet" | "alipay" | "wechat" | "stripe"
	ReturnURL     string `json:"return_url,omitempty"`
}

// SubscriptionCheckoutResponse covers both immediate-activation (wallet) and
// external-payment (alipay/wechat/stripe) shapes — fields are mutually exclusive
// in practice but we keep both so callers can branch on presence.
type SubscriptionCheckoutResponse struct {
	OrderNo      string                `json:"order_no,omitempty"`
	PayURL       string                `json:"pay_url,omitempty"`
	Subscription *SubscriptionSnapshot `json:"subscription,omitempty"`
}

// GetAccountByZitadelSub looks up the platform account record for a Zitadel sub.
// Returns *Error{Code: ErrCodeNotFound} when no account is provisioned yet.
func (c *Client) GetAccountByZitadelSub(ctx context.Context, sub string) (*Account, error) {
	if sub == "" {
		return nil, &Error{Code: ErrCodeInvalidParameter, Message: "zitadel_sub is required"}
	}
	var out Account
	if err := c.do(ctx, http.MethodGet,
		"/internal/v1/accounts/by-zitadel-sub/"+url.PathEscape(sub),
		nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAccountOverview fetches wallet + subscription + entitlements scoped to a product.
func (c *Client) GetAccountOverview(ctx context.Context, accountID int64, productID string) (*AccountOverview, error) {
	if accountID <= 0 {
		return nil, &Error{Code: ErrCodeInvalidParameter, Message: "account_id must be positive"}
	}
	if productID == "" {
		return nil, &Error{Code: ErrCodeInvalidParameter, Message: "product_id is required"}
	}
	q := url.Values{}
	q.Set("product_id", productID)
	var out AccountOverview
	if err := c.do(ctx, http.MethodGet,
		"/internal/v1/accounts/"+itoa(accountID)+"/overview?"+q.Encode(),
		nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SubscriptionCheckout posts a checkout intent and returns either an activated
// subscription (wallet) or a payment URL the user should be redirected to.
func (c *Client) SubscriptionCheckout(ctx context.Context, req SubscriptionCheckoutRequest) (*SubscriptionCheckoutResponse, error) {
	if req.AccountID <= 0 {
		return nil, &Error{Code: ErrCodeInvalidParameter, Message: "account_id is required"}
	}
	if req.ProductID == "" || req.PlanCode == "" || req.BillingCycle == "" || req.PaymentMethod == "" {
		return nil, &Error{Code: ErrCodeInvalidParameter, Message: "product_id, plan_code, billing_cycle, payment_method are required"}
	}
	var out SubscriptionCheckoutResponse
	if err := c.do(ctx, http.MethodPost, "/internal/v1/subscriptions/checkout", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// itoa formats an int64 without dragging in strconv at every call site.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
