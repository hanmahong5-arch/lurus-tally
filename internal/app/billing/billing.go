// Package billing wires Tally's "subscribe to a plan" flow on top of the
// platform internal API. Use cases here translate Tally-side identity (Zitadel sub)
// into platform account IDs and forward the checkout intent.
package billing

import (
	"context"
	"errors"
	"fmt"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/platformclient"
)

// ProductID is the cross-service constant Tally always passes when calling
// platform billing endpoints. Defined in doc/coord/contracts.md.
const ProductID = "lurus-tally"

// PlatformPort is the surface of platformclient.Client we depend on.
// Captured as an interface so use cases can be unit-tested with a stub.
type PlatformPort interface {
	GetAccountByZitadelSub(ctx context.Context, sub string) (*platformclient.Account, error)
	GetAccountOverview(ctx context.Context, accountID int64, productID string) (*platformclient.AccountOverview, error)
	SubscriptionCheckout(ctx context.Context, req platformclient.SubscriptionCheckoutRequest) (*platformclient.SubscriptionCheckoutResponse, error)
}

// SubscribeInput is what the HTTP handler hands to the use case.
type SubscribeInput struct {
	ZitadelSub    string
	PlanCode      string
	BillingCycle  string
	PaymentMethod string
	ReturnURL     string
}

// SubscribeOutput captures both the wallet-activation and the redirect-pay shapes.
type SubscribeOutput struct {
	OrderNo      string
	PayURL       string
	Subscription *platformclient.SubscriptionSnapshot
}

// SubscribeUseCase resolves the caller's account and fires a checkout.
type SubscribeUseCase struct {
	platform PlatformPort
}

// NewSubscribeUseCase constructs the use case.
func NewSubscribeUseCase(p PlatformPort) *SubscribeUseCase {
	return &SubscribeUseCase{platform: p}
}

// ErrUnauthenticated indicates the caller is not signed in (no zitadel_sub).
var ErrUnauthenticated = errors.New("billing: caller is not authenticated")

// ErrInvalidInput indicates the caller-supplied plan/cycle/method is missing.
var ErrInvalidInput = errors.New("billing: plan_code, billing_cycle and payment_method are required")

// Execute resolves the platform account_id for the caller's Zitadel sub and posts
// the checkout intent. It returns SubscribeOutput on success and surfaces typed
// platform errors so the handler can map them to HTTP statuses cleanly.
func (uc *SubscribeUseCase) Execute(ctx context.Context, in SubscribeInput) (*SubscribeOutput, error) {
	if in.ZitadelSub == "" {
		return nil, ErrUnauthenticated
	}
	if in.PlanCode == "" || in.BillingCycle == "" || in.PaymentMethod == "" {
		return nil, ErrInvalidInput
	}

	acc, err := uc.platform.GetAccountByZitadelSub(ctx, in.ZitadelSub)
	if err != nil {
		return nil, fmt.Errorf("resolve account: %w", err)
	}

	resp, err := uc.platform.SubscriptionCheckout(ctx, platformclient.SubscriptionCheckoutRequest{
		AccountID:     acc.ID,
		ProductID:     ProductID,
		PlanCode:      in.PlanCode,
		BillingCycle:  in.BillingCycle,
		PaymentMethod: in.PaymentMethod,
		ReturnURL:     in.ReturnURL,
	})
	if err != nil {
		return nil, fmt.Errorf("checkout: %w", err)
	}

	return &SubscribeOutput{
		OrderNo:      resp.OrderNo,
		PayURL:       resp.PayURL,
		Subscription: resp.Subscription,
	}, nil
}

// OverviewUseCase returns the wallet/subscription/entitlement snapshot for
// the caller, scoped to the lurus-tally product.
type OverviewUseCase struct {
	platform PlatformPort
}

// NewOverviewUseCase constructs the use case.
func NewOverviewUseCase(p PlatformPort) *OverviewUseCase {
	return &OverviewUseCase{platform: p}
}

// Execute returns the platform overview for the caller. Callers only need to
// pass the Zitadel sub; the use case resolves the platform account_id internally.
func (uc *OverviewUseCase) Execute(ctx context.Context, zitadelSub string) (*platformclient.AccountOverview, error) {
	if zitadelSub == "" {
		return nil, ErrUnauthenticated
	}
	acc, err := uc.platform.GetAccountByZitadelSub(ctx, zitadelSub)
	if err != nil {
		return nil, fmt.Errorf("resolve account: %w", err)
	}
	overview, err := uc.platform.GetAccountOverview(ctx, acc.ID, ProductID)
	if err != nil {
		return nil, fmt.Errorf("overview: %w", err)
	}
	return overview, nil
}
