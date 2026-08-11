package billing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/billing"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/platformclient"
)

// zzStubPlatform is a second fake, kept local to this file so it never
// collides with the identically-shaped stubPlatform in billing_test.go
// (that file must not be edited per task constraints).
type zzStubPlatform struct {
	account      *platformclient.Account
	accountErr   error
	overview     *platformclient.AccountOverview
	overviewErr  error
	checkoutResp *platformclient.SubscriptionCheckoutResponse
	checkoutErr  error
}

func (s *zzStubPlatform) GetAccountByIDPSubject(_ context.Context, _ string) (*platformclient.Account, error) {
	return s.account, s.accountErr
}

func (s *zzStubPlatform) GetAccountOverview(_ context.Context, _ int64, _ string) (*platformclient.AccountOverview, error) {
	return s.overview, s.overviewErr
}

func (s *zzStubPlatform) SubscriptionCheckout(_ context.Context, _ platformclient.SubscriptionCheckoutRequest, _ string) (*platformclient.SubscriptionCheckoutResponse, error) {
	return s.checkoutResp, s.checkoutErr
}

// errResolveAccount is the sentinel returned by the fake's account lookup so
// we can assert it survives fmt.Errorf("resolve account: %w", err) wrapping
// via errors.Is, independent of any platformclient.Error typing.
var errResolveAccount = errors.New("zz: account lookup failed")

func TestZZSubscribe_ResolveAccountErrorWrapped(t *testing.T) {
	uc := billing.NewSubscribeUseCase(&zzStubPlatform{accountErr: errResolveAccount})
	out, err := uc.Execute(context.Background(), billing.SubscribeInput{
		IDPSubject:    "sub-1",
		PlanCode:      "pro",
		BillingCycle:  "monthly",
		PaymentMethod: "wallet",
	})
	if out != nil {
		t.Errorf("want nil output on error, got %+v", out)
	}
	if !errors.Is(err, errResolveAccount) {
		t.Errorf("resolve account error must survive %%w wrapping, got %v", err)
	}
}

func TestZZOverview_ResolveAccountErrorWrapped(t *testing.T) {
	uc := billing.NewOverviewUseCase(&zzStubPlatform{accountErr: errResolveAccount})
	out, err := uc.Execute(context.Background(), "sub-1")
	if out != nil {
		t.Errorf("want nil output on error, got %+v", out)
	}
	if !errors.Is(err, errResolveAccount) {
		t.Errorf("resolve account error must survive %%w wrapping, got %v", err)
	}
}

func TestZZOverview_GetOverviewErrorWrapped(t *testing.T) {
	platErr := &platformclient.Error{Code: platformclient.ErrCodeInsufficientBalance, HTTPStatus: 402, Message: "broke"}
	stub := &zzStubPlatform{
		account:     &platformclient.Account{ID: 3},
		overviewErr: platErr,
	}
	uc := billing.NewOverviewUseCase(stub)
	out, err := uc.Execute(context.Background(), "sub-3")
	if out != nil {
		t.Errorf("want nil output on error, got %+v", out)
	}
	if !platformclient.IsCode(err, platformclient.ErrCodeInsufficientBalance) {
		t.Errorf("typed platform error must survive overview wrap: %v", err)
	}
}

// TestZZSubscribe_GuardOrdering asserts empty IDPSubject is checked before the
// empty plan/cycle/method fields: when both are empty/unset the caller must
// see ErrUnauthenticated, never ErrInvalidInput.
func TestZZSubscribe_GuardOrdering(t *testing.T) {
	uc := billing.NewSubscribeUseCase(&zzStubPlatform{})
	_, err := uc.Execute(context.Background(), billing.SubscribeInput{})
	if !errors.Is(err, billing.ErrUnauthenticated) {
		t.Errorf("guard order: want ErrUnauthenticated when subject and plan fields are both empty, got %v", err)
	}
	if errors.Is(err, billing.ErrInvalidInput) {
		t.Errorf("guard order: ErrInvalidInput must not fire before the auth guard")
	}
}

// TestZZSubscribe_RejectsEachMissingPlanField exercises each of the three
// individually-required fields so the `||` guard's short-circuit branches are
// all hit, not just the all-empty case.
func TestZZSubscribe_RejectsEachMissingPlanField(t *testing.T) {
	cases := []struct {
		name  string
		input billing.SubscribeInput
	}{
		{
			name: "missing plan code",
			input: billing.SubscribeInput{
				IDPSubject:    "sub-1",
				BillingCycle:  "monthly",
				PaymentMethod: "wallet",
			},
		},
		{
			name: "missing billing cycle",
			input: billing.SubscribeInput{
				IDPSubject:    "sub-1",
				PlanCode:      "pro",
				PaymentMethod: "wallet",
			},
		},
		{
			name: "missing payment method",
			input: billing.SubscribeInput{
				IDPSubject:   "sub-1",
				PlanCode:     "pro",
				BillingCycle: "monthly",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc := billing.NewSubscribeUseCase(&zzStubPlatform{})
			_, err := uc.Execute(context.Background(), tc.input)
			if !errors.Is(err, billing.ErrInvalidInput) {
				t.Errorf("%s: want ErrInvalidInput, got %v", tc.name, err)
			}
		})
	}
}

// TestZZSubscribe_ShapeTable covers the two documented SubscribeOutput shapes:
// wallet-activation (Subscription populated, PayURL empty) vs redirect-pay
// (PayURL populated). Expected fields are copied verbatim from the fake's
// canned response, matching billing.go's direct field-for-field copy.
func TestZZSubscribe_ShapeTable(t *testing.T) {
	cases := []struct {
		name         string
		checkoutResp *platformclient.SubscriptionCheckoutResponse
	}{
		{
			name: "wallet activation shape",
			checkoutResp: &platformclient.SubscriptionCheckoutResponse{
				OrderNo:      "ORD-WALLET-1",
				PayURL:       "",
				Subscription: &platformclient.SubscriptionSnapshot{PlanCode: "pro", Status: "active"},
			},
		},
		{
			name: "redirect pay shape",
			checkoutResp: &platformclient.SubscriptionCheckoutResponse{
				OrderNo:      "ORD-REDIRECT-1",
				PayURL:       "https://pay.example.com/checkout/ORD-REDIRECT-1",
				Subscription: nil,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &zzStubPlatform{
				account:      &platformclient.Account{ID: 42},
				checkoutResp: tc.checkoutResp,
			}
			uc := billing.NewSubscribeUseCase(stub)
			out, err := uc.Execute(context.Background(), billing.SubscribeInput{
				IDPSubject:    "sub-42",
				PlanCode:      "pro",
				BillingCycle:  "monthly",
				PaymentMethod: "wallet",
			})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if out.OrderNo != tc.checkoutResp.OrderNo {
				t.Errorf("OrderNo: want %q got %q", tc.checkoutResp.OrderNo, out.OrderNo)
			}
			if out.PayURL != tc.checkoutResp.PayURL {
				t.Errorf("PayURL: want %q got %q", tc.checkoutResp.PayURL, out.PayURL)
			}
			if out.Subscription != tc.checkoutResp.Subscription {
				t.Errorf("Subscription: want same pointer copied through, got %+v want %+v", out.Subscription, tc.checkoutResp.Subscription)
			}
		})
	}
}
