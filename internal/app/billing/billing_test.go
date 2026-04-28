package billing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/billing"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/platformclient"
)

type stubPlatform struct {
	account         *platformclient.Account
	accountErr      error
	overview        *platformclient.AccountOverview
	overviewErr     error
	checkoutResp    *platformclient.SubscriptionCheckoutResponse
	checkoutErr     error
	gotCheckoutReq  platformclient.SubscriptionCheckoutRequest
	gotOverviewArgs struct {
		ID        int64
		ProductID string
	}
}

func (s *stubPlatform) GetAccountByZitadelSub(_ context.Context, _ string) (*platformclient.Account, error) {
	return s.account, s.accountErr
}

func (s *stubPlatform) GetAccountOverview(_ context.Context, id int64, productID string) (*platformclient.AccountOverview, error) {
	s.gotOverviewArgs.ID = id
	s.gotOverviewArgs.ProductID = productID
	return s.overview, s.overviewErr
}

func (s *stubPlatform) SubscriptionCheckout(_ context.Context, req platformclient.SubscriptionCheckoutRequest) (*platformclient.SubscriptionCheckoutResponse, error) {
	s.gotCheckoutReq = req
	return s.checkoutResp, s.checkoutErr
}

func TestSubscribe_RejectsMissingZitadelSub(t *testing.T) {
	uc := billing.NewSubscribeUseCase(&stubPlatform{})
	_, err := uc.Execute(context.Background(), billing.SubscribeInput{
		PlanCode:      "pro",
		BillingCycle:  "monthly",
		PaymentMethod: "wallet",
	})
	if !errors.Is(err, billing.ErrUnauthenticated) {
		t.Errorf("want ErrUnauthenticated, got %v", err)
	}
}

func TestSubscribe_RejectsMissingPlanFields(t *testing.T) {
	uc := billing.NewSubscribeUseCase(&stubPlatform{})
	_, err := uc.Execute(context.Background(), billing.SubscribeInput{ZitadelSub: "sub"})
	if !errors.Is(err, billing.ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput, got %v", err)
	}
}

func TestSubscribe_PropagatesProductIDAndAccountID(t *testing.T) {
	stub := &stubPlatform{
		account: &platformclient.Account{ID: 7, Email: "u@x"},
		checkoutResp: &platformclient.SubscriptionCheckoutResponse{
			Subscription: &platformclient.SubscriptionSnapshot{PlanCode: "pro", Status: "active"},
		},
	}
	uc := billing.NewSubscribeUseCase(stub)

	out, err := uc.Execute(context.Background(), billing.SubscribeInput{
		ZitadelSub:    "sub-abc",
		PlanCode:      "pro",
		BillingCycle:  "monthly",
		PaymentMethod: "wallet",
		ReturnURL:     "/subscription",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stub.gotCheckoutReq.AccountID != 7 {
		t.Errorf("AccountID propagation wrong: %d", stub.gotCheckoutReq.AccountID)
	}
	if stub.gotCheckoutReq.ProductID != billing.ProductID {
		t.Errorf("ProductID must be %s, got %s", billing.ProductID, stub.gotCheckoutReq.ProductID)
	}
	if out.Subscription == nil || out.Subscription.PlanCode != "pro" {
		t.Errorf("subscription not propagated: %+v", out)
	}
}

func TestSubscribe_PropagatesCheckoutError(t *testing.T) {
	platErr := &platformclient.Error{Code: platformclient.ErrCodeInsufficientBalance, HTTPStatus: 402, Message: "broke"}
	stub := &stubPlatform{
		account:     &platformclient.Account{ID: 7},
		checkoutErr: platErr,
	}
	uc := billing.NewSubscribeUseCase(stub)
	_, err := uc.Execute(context.Background(), billing.SubscribeInput{
		ZitadelSub:    "sub-abc",
		PlanCode:      "pro",
		BillingCycle:  "monthly",
		PaymentMethod: "wallet",
	})
	if !platformclient.IsCode(err, platformclient.ErrCodeInsufficientBalance) {
		t.Errorf("typed error must survive wrap: %v", err)
	}
}

func TestOverview_PropagatesProductScope(t *testing.T) {
	stub := &stubPlatform{
		account:  &platformclient.Account{ID: 9},
		overview: &platformclient.AccountOverview{},
	}
	uc := billing.NewOverviewUseCase(stub)
	if _, err := uc.Execute(context.Background(), "sub-9"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stub.gotOverviewArgs.ID != 9 || stub.gotOverviewArgs.ProductID != billing.ProductID {
		t.Errorf("scope wrong: %+v", stub.gotOverviewArgs)
	}
}

func TestOverview_RejectsMissingZitadelSub(t *testing.T) {
	uc := billing.NewOverviewUseCase(&stubPlatform{})
	_, err := uc.Execute(context.Background(), "")
	if !errors.Is(err, billing.ErrUnauthenticated) {
		t.Errorf("want ErrUnauthenticated, got %v", err)
	}
}
