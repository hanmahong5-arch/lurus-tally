// Package billing wires Tally's "subscribe to a plan" flow on top of the
// platform internal API.
//
// tally's plan is per-TENANT: the subscription lives on the tenant's pinned
// platform account (tenant.platform_account_id, migration 000051 — the
// bootstrap owner's account). The entitlement gate and the usage reporter
// already resolve through that pin, so billing must too: checking out on the
// caller's personal account would charge a member and leave the tenant on the
// free plan. Only the tenant owner may change the plan; every member may read
// the tenant's billing overview.
package billing

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/platformclient"
)

// ProductID is the cross-service constant Tally always passes when calling
// platform billing endpoints. Defined in doc/coord/contracts.md.
const ProductID = "tally"

// PlatformPort is the surface of platformclient.Client we depend on.
// Captured as an interface so use cases can be unit-tested with a stub.
type PlatformPort interface {
	UpsertAccount(ctx context.Context, req platformclient.UpsertAccountRequest) (*platformclient.Account, error)
	GetAccountOverview(ctx context.Context, accountID int64, productID string) (*platformclient.AccountOverview, error)
	GetEntitlements(ctx context.Context, accountID int64, productID string) (map[string]string, error)
	SubscriptionCheckout(ctx context.Context, req platformclient.SubscriptionCheckoutRequest, idempotencyKey string) (*platformclient.SubscriptionCheckoutResponse, error)
	GetCheckoutStatus(ctx context.Context, orderNo string) (*platformclient.CheckoutStatus, error)
}

// TenantStore resolves who the caller is inside the tenant and which platform
// account holds the tenant's plan. Lifecycle wiring composes it from the
// mapping store and the tenant repo.
type TenantStore interface {
	GetMappingBySub(ctx context.Context, sub string) (*domain.UserIdentityMapping, error)
	GetPlatformAccountID(ctx context.Context, tenantID uuid.UUID) (int64, bool, error)
	SetPlatformAccountID(ctx context.Context, tenantID uuid.UUID, accountID int64) error
}

// Caller identifies the signed-in user and the tenant the request runs in.
type Caller struct {
	IDPSubject  string
	TenantID    uuid.UUID
	Email       string
	DisplayName string
}

// ErrUnauthenticated indicates the caller is not signed in (no idp_subject or tenant).
var ErrUnauthenticated = errors.New("billing: caller is not authenticated")

// ErrInvalidInput indicates the caller-supplied plan/cycle/method is missing.
var ErrInvalidInput = errors.New("billing: plan_code, billing_cycle and payment_method are required")

// ErrOwnerOnly is returned when a non-owner member tries to change the plan.
var ErrOwnerOnly = errors.New("billing: only the tenant owner can change the plan")

// ErrNotLinked is returned when the tenant has no platform account yet and the
// caller is not in a position to create it (a member, not the owner).
var ErrNotLinked = errors.New("billing: tenant is not linked to a billing account yet")

// membership loads the caller's mapping and checks it belongs to the tenant
// the request runs in. A sub mapped to another tenant is treated as
// unauthenticated for this tenant rather than leaking that tenant's billing.
func membership(ctx context.Context, store TenantStore, c Caller) (*domain.UserIdentityMapping, error) {
	if c.IDPSubject == "" || c.TenantID == uuid.Nil {
		return nil, ErrUnauthenticated
	}
	m, err := store.GetMappingBySub(ctx, c.IDPSubject)
	if err != nil {
		return nil, fmt.Errorf("resolve membership: %w", err)
	}
	if m == nil || m.TenantID != c.TenantID {
		return nil, ErrUnauthenticated
	}
	return m, nil
}

// tenantAccount returns the tenant's plan-holder account. When the tenant was
// never pinned (onboarded before migration 000051, or the onboarding upsert hit
// a platform blip) and the caller is the owner, it provisions the owner's
// account and pins it — the same heal ChooseProfile performs — so the owner is
// never stuck unable to pay.
func tenantAccount(ctx context.Context, store TenantStore, platform PlatformPort, c Caller, m *domain.UserIdentityMapping) (int64, error) {
	id, ok, err := store.GetPlatformAccountID(ctx, c.TenantID)
	if err != nil {
		return 0, fmt.Errorf("resolve tenant account: %w", err)
	}
	if ok {
		return id, nil
	}
	if !m.IsOwner {
		return 0, ErrNotLinked
	}
	email := c.Email
	if email == "" {
		email = c.IDPSubject + "@idp.local"
	}
	acc, err := platform.UpsertAccount(ctx, platformclient.UpsertAccountRequest{
		IDPSubject:  c.IDPSubject,
		Email:       email,
		DisplayName: c.DisplayName,
	})
	if err != nil {
		return 0, fmt.Errorf("provision tenant account: %w", err)
	}
	if err := store.SetPlatformAccountID(ctx, c.TenantID, acc.ID); err != nil {
		return 0, fmt.Errorf("pin tenant account: %w", err)
	}
	return acc.ID, nil
}

// SubscribeInput is what the HTTP handler hands to the use case.
type SubscribeInput struct {
	Caller
	PlanCode     string
	BillingCycle string
	// PaymentMethod is what the buyer picked: "wallet", a provider family
	// ("alipay", "wechat") or a concrete platform method id. Families are
	// resolved to the scene that suits the buyer's device (see ResolveMethod).
	PaymentMethod string
	// UserAgent is the buyer's browser UA, used to pick the payment scene.
	UserAgent string
	// ReturnURL must be absolute; the handler builds it from a same-site path.
	ReturnURL string
	// IdempotencyKey is forwarded to platform's checkout so a Tally-side retry
	// cannot double-charge; platform mandates it on financial mutations.
	IdempotencyKey string
}

// SubscribeOutput is one of three shapes: an activated subscription (wallet),
// a pay URL to redirect to, or a QR payload to render (WeChat Native on desktop).
type SubscribeOutput struct {
	OrderNo      string
	PayURL       string
	QRCode       string
	Subscription *platformclient.SubscriptionSnapshot
}

// SubscribeUseCase checks out a plan for the caller's tenant.
type SubscribeUseCase struct {
	platform PlatformPort
	store    TenantStore
}

// NewSubscribeUseCase constructs the use case.
func NewSubscribeUseCase(p PlatformPort, s TenantStore) *SubscribeUseCase {
	return &SubscribeUseCase{platform: p, store: s}
}

// Execute verifies the caller owns the tenant, resolves the tenant's plan
// holder and posts the checkout intent.
func (uc *SubscribeUseCase) Execute(ctx context.Context, in SubscribeInput) (*SubscribeOutput, error) {
	if in.IDPSubject == "" || in.TenantID == uuid.Nil {
		return nil, ErrUnauthenticated
	}
	if in.PlanCode == "" || in.BillingCycle == "" || in.PaymentMethod == "" {
		return nil, ErrInvalidInput
	}
	m, err := membership(ctx, uc.store, in.Caller)
	if err != nil {
		return nil, err
	}
	if !m.IsOwner {
		return nil, ErrOwnerOnly
	}
	accountID, err := tenantAccount(ctx, uc.store, uc.platform, in.Caller, m)
	if err != nil {
		return nil, err
	}

	method := ResolveMethod(in.PaymentMethod, in.UserAgent)
	resp, err := uc.platform.SubscriptionCheckout(ctx, platformclient.SubscriptionCheckoutRequest{
		AccountID:     accountID,
		ProductID:     ProductID,
		PlanCode:      in.PlanCode,
		BillingCycle:  in.BillingCycle,
		PaymentMethod: method,
		ReturnURL:     in.ReturnURL,
	}, in.IdempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("checkout: %w", err)
	}

	out := &SubscribeOutput{OrderNo: resp.OrderNo, Subscription: resp.Subscription}
	if IsQRMethod(method) {
		out.QRCode = resp.PayURL
	} else {
		out.PayURL = resp.PayURL
	}
	return out, nil
}

// Overview is tally's billing read model for the current tenant. It is shaped
// for the tally UI rather than passing platform's wire format through, so a
// platform-side field rename breaks one mapping here instead of the screen.
type Overview struct {
	AccountID    int64                                `json:"account_id"`
	Wallet       OverviewWallet                       `json:"wallet"`
	VIP          OverviewVIP                          `json:"vip"`
	Subscription *platformclient.SubscriptionSnapshot `json:"subscription"`
	Entitlements map[string]string                    `json:"entitlements"`
	TopupURL     string                               `json:"topup_url,omitempty"`
	// CanManage is true for the tenant owner — the only member allowed to
	// change the plan. The UI hides checkout for everyone else.
	CanManage bool `json:"can_manage"`
}

// OverviewWallet is the tenant plan holder's wallet in CNY.
type OverviewWallet struct {
	Available float64 `json:"available"`
	Frozen    float64 `json:"frozen"`
	Total     float64 `json:"total"`
}

// OverviewVIP is the plan holder's VIP tier.
type OverviewVIP struct {
	Level     int    `json:"level"`
	LevelName string `json:"level_name"`
}

// OverviewUseCase returns the tenant's wallet/subscription/entitlement snapshot.
type OverviewUseCase struct {
	platform PlatformPort
	store    TenantStore
}

// NewOverviewUseCase constructs the use case.
func NewOverviewUseCase(p PlatformPort, s TenantStore) *OverviewUseCase {
	return &OverviewUseCase{platform: p, store: s}
}

// Execute returns the overview of the tenant the caller belongs to. A tenant
// with no plan holder yet reads as the free plan with an empty wallet rather
// than an error, so the plans screen still renders for brand-new tenants.
func (uc *OverviewUseCase) Execute(ctx context.Context, c Caller) (*Overview, error) {
	m, err := membership(ctx, uc.store, c)
	if err != nil {
		return nil, err
	}
	out := &Overview{CanManage: m.IsOwner, Entitlements: map[string]string{"plan_code": "free"}}

	accountID, ok, err := uc.store.GetPlatformAccountID(ctx, c.TenantID)
	if err != nil {
		return nil, fmt.Errorf("resolve tenant account: %w", err)
	}
	if !ok {
		return out, nil
	}
	ov, err := uc.platform.GetAccountOverview(ctx, accountID, ProductID)
	if err != nil {
		return nil, fmt.Errorf("overview: %w", err)
	}
	ents, err := uc.platform.GetEntitlements(ctx, accountID, ProductID)
	if err != nil {
		return nil, fmt.Errorf("entitlements: %w", err)
	}

	out.AccountID = accountID
	out.Wallet = OverviewWallet{
		Available: ov.Wallet.Balance,
		Frozen:    ov.Wallet.Frozen,
		Total:     ov.Wallet.Balance + ov.Wallet.Frozen,
	}
	out.VIP = OverviewVIP{Level: ov.VIP.Level, LevelName: ov.VIP.LevelName}
	out.Subscription = ov.Subscription
	if len(ents) > 0 {
		out.Entitlements = ents
	}
	out.TopupURL = ov.TopupURL
	return out, nil
}

// OrderStatus is what the UI needs to confirm a payment. Amount and pay URL
// are deliberately left out: platform's status endpoint is not scoped to an
// account, so tally exposes only whether the order settled.
type OrderStatus struct {
	OrderNo string `json:"order_no"`
	Status  string `json:"status"`
	Paid    bool   `json:"paid"`
	// Final is true once the order can no longer change (paid, failed,
	// cancelled, expired), so the UI knows to stop polling.
	Final bool `json:"final"`
}

// OrderStatusUseCase reports a checkout order's payment status so the UI can
// confirm a payment after the buyer returns from Alipay or scans a QR code.
type OrderStatusUseCase struct {
	platform PlatformPort
	store    TenantStore
}

// NewOrderStatusUseCase constructs the use case.
func NewOrderStatusUseCase(p PlatformPort, s TenantStore) *OrderStatusUseCase {
	return &OrderStatusUseCase{platform: p, store: s}
}

// Execute returns the order status for a member of the caller's tenant.
func (uc *OrderStatusUseCase) Execute(ctx context.Context, c Caller, orderNo string) (*OrderStatus, error) {
	if _, err := membership(ctx, uc.store, c); err != nil {
		return nil, err
	}
	if orderNo == "" {
		return nil, ErrInvalidInput
	}
	st, err := uc.platform.GetCheckoutStatus(ctx, orderNo)
	if err != nil {
		return nil, fmt.Errorf("order status: %w", err)
	}
	switch st.Status {
	case "paid":
		return &OrderStatus{OrderNo: st.OrderNo, Status: st.Status, Paid: true, Final: true}, nil
	case "failed", "cancelled", "expired", "refunded":
		return &OrderStatus{OrderNo: st.OrderNo, Status: st.Status, Final: true}, nil
	default:
		return &OrderStatus{OrderNo: st.OrderNo, Status: st.Status}, nil
	}
}
