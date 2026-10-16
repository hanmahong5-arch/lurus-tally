package platform

import (
	"context"
	"net/http"
	"net/url"
)

// Account is the subset of platform's account record Tally cares about.
type Account struct {
	ID int64 `json:"id"`
	// IDPSubject is the upstream OIDC `sub`. Vendor-neutral wire field
	// `idp_subject`, aligned with platform's canonical naming.
	IDPSubject  string `json:"idp_subject"`
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

// Wallet mirrors the wallet block in /internal/v1/accounts/:id/overview
// (platform internal/app/overview_service.go WalletSummary). Balance is the
// spendable CNY amount; Frozen is held by open pre-authorisations.
type Wallet struct {
	Balance      float64 `json:"balance"`
	Frozen       float64 `json:"frozen"`
	CreditsCents int64   `json:"credits_cents"`
	DiscountRate float64 `json:"discount_rate"`
	DiscountTier string  `json:"discount_tier"`
}

// VIP mirrors the vip block of the overview (platform VIPSummary).
type VIP struct {
	Level     int    `json:"level"`
	LevelName string `json:"level_name"`
}

// AccountOverview is what /internal/v1/accounts/:id/overview returns
// (platform internal/app/overview_service.go AccountOverview). It carries no
// entitlements — read those from GetEntitlements.
type AccountOverview struct {
	Account struct {
		ID          int64  `json:"id"`
		LurusID     string `json:"lurus_id"`
		DisplayName string `json:"display_name"`
	} `json:"account"`
	VIP          VIP                   `json:"vip"`
	Wallet       Wallet                `json:"wallet"`
	Subscription *SubscriptionSnapshot `json:"subscription,omitempty"`
	TopupURL     string                `json:"topup_url,omitempty"`
}

// CheckoutStatus mirrors GET /internal/v1/checkout/:order_no/status.
type CheckoutStatus struct {
	OrderNo   string  `json:"order_no"`
	Status    string  `json:"status"` // pending | paid | failed | cancelled | expired | refunded
	AmountCNY float64 `json:"amount_cny"`
}

// SubscriptionCheckoutRequest mirrors POST /internal/v1/subscriptions/checkout body.
type SubscriptionCheckoutRequest struct {
	AccountID     int64  `json:"account_id"`
	ProductID     string `json:"product_id"`
	PlanCode      string `json:"plan_code"`
	BillingCycle  string `json:"billing_cycle"`
	PaymentMethod string `json:"payment_method"` // "wallet" or a concrete platform method id (alipay, alipay_wap, wechat_native, wechat_h5, ...)
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

// UpsertAccountRequest is the body for POST /internal/v1/accounts/upsert.
// Platform requires idp_subject + email; the rest are best-effort metadata.
type UpsertAccountRequest struct {
	IDPSubject      string `json:"idp_subject"`
	Email           string `json:"email"`
	DisplayName     string `json:"display_name,omitempty"`
	AvatarURL       string `json:"avatar_url,omitempty"`
	ReferrerAffCode string `json:"referrer_aff_code,omitempty"`
}

// UpsertAccount creates or updates the platform account record keyed by
// idp_subject. Idempotent — second call with the same sub returns the same
// account row. Tally calls this on first /setup so platform owns the
// canonical wallet / subscription / VIP record from the very first login.
func (c *Client) UpsertAccount(ctx context.Context, req UpsertAccountRequest) (*Account, error) {
	if req.IDPSubject == "" {
		return nil, &Error{Code: ErrCodeInvalidParameter, Message: "idp_subject is required"}
	}
	if req.Email == "" {
		return nil, &Error{Code: ErrCodeInvalidParameter, Message: "email is required"}
	}
	var out Account
	if err := c.do(ctx, http.MethodPost, "/internal/v1/accounts/upsert", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAccountByIDPSubject looks up the platform account record for an OIDC IdP
// subject. Returns *Error{Code: ErrCodeNotFound} when no account is provisioned yet.
func (c *Client) GetAccountByIDPSubject(ctx context.Context, sub string) (*Account, error) {
	if sub == "" {
		return nil, &Error{Code: ErrCodeInvalidParameter, Message: "idp_subject is required"}
	}
	var out Account
	if err := c.do(ctx, http.MethodGet,
		"/internal/v1/accounts/by-idp-sub/"+url.PathEscape(sub),
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

// GetEntitlements fetches the entitlement key→value map for an account+product
// from platform's dedicated endpoint (Redis-cached server-side). Platform
// returns {"plan_code":"free"} for accounts with no active paid entitlements,
// so a successful call never yields a nil map for a known account.
func (c *Client) GetEntitlements(ctx context.Context, accountID int64, productID string) (map[string]string, error) {
	if accountID <= 0 {
		return nil, &Error{Code: ErrCodeInvalidParameter, Message: "account_id must be positive"}
	}
	if productID == "" {
		return nil, &Error{Code: ErrCodeInvalidParameter, Message: "product_id is required"}
	}
	var out map[string]string
	if err := c.do(ctx, http.MethodGet,
		"/internal/v1/accounts/"+itoa(accountID)+"/entitlements/"+url.PathEscape(productID),
		nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SubscriptionCheckout posts a checkout intent and returns either an activated
// subscription (wallet) or a payment URL the user should be redirected to.
func (c *Client) SubscriptionCheckout(ctx context.Context, req SubscriptionCheckoutRequest, idempotencyKey string) (*SubscriptionCheckoutResponse, error) {
	if req.AccountID <= 0 {
		return nil, &Error{Code: ErrCodeInvalidParameter, Message: "account_id is required"}
	}
	if req.ProductID == "" || req.PlanCode == "" || req.BillingCycle == "" || req.PaymentMethod == "" {
		return nil, &Error{Code: ErrCodeInvalidParameter, Message: "product_id, plan_code, billing_cycle, payment_method are required"}
	}
	var out SubscriptionCheckoutResponse
	if err := c.doWithIdem(ctx, http.MethodPost, "/internal/v1/subscriptions/checkout", req, idempotencyKey, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetCheckoutStatus reads a checkout order's payment status.
func (c *Client) GetCheckoutStatus(ctx context.Context, orderNo string) (*CheckoutStatus, error) {
	if orderNo == "" {
		return nil, &Error{Code: ErrCodeInvalidParameter, Message: "order_no is required"}
	}
	var out CheckoutStatus
	if err := c.do(ctx, http.MethodGet,
		"/internal/v1/checkout/"+url.PathEscape(orderNo)+"/status",
		nil, &out); err != nil {
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
