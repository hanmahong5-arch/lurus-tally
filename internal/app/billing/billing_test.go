package billing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/billing"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/platformclient"
)

type stubPlatform struct {
	upserted          *platformclient.Account
	upsertErr         error
	upsertCalls       int
	overview          *platformclient.AccountOverview
	overviewErr       error
	ents              map[string]string
	entsErr           error
	checkoutResp      *platformclient.SubscriptionCheckoutResponse
	checkoutErr       error
	gotCheckoutReq    platformclient.SubscriptionCheckoutRequest
	gotIdempotencyKey string
	checkoutCalls     int
	gotOverviewID     int64
	status            *platformclient.CheckoutStatus
	statusErr         error
}

func (s *stubPlatform) UpsertAccount(_ context.Context, _ platformclient.UpsertAccountRequest) (*platformclient.Account, error) {
	s.upsertCalls++
	return s.upserted, s.upsertErr
}

func (s *stubPlatform) GetAccountOverview(_ context.Context, id int64, _ string) (*platformclient.AccountOverview, error) {
	s.gotOverviewID = id
	return s.overview, s.overviewErr
}

func (s *stubPlatform) GetEntitlements(_ context.Context, _ int64, _ string) (map[string]string, error) {
	return s.ents, s.entsErr
}

func (s *stubPlatform) SubscriptionCheckout(_ context.Context, req platformclient.SubscriptionCheckoutRequest, key string) (*platformclient.SubscriptionCheckoutResponse, error) {
	s.checkoutCalls++
	s.gotCheckoutReq = req
	s.gotIdempotencyKey = key
	return s.checkoutResp, s.checkoutErr
}

func (s *stubPlatform) GetCheckoutStatus(_ context.Context, _ string) (*platformclient.CheckoutStatus, error) {
	return s.status, s.statusErr
}

type stubStore struct {
	mapping  *domain.UserIdentityMapping
	pinned   int64
	pinErr   error
	setCalls int
}

func (s *stubStore) GetMappingBySub(_ context.Context, _ string) (*domain.UserIdentityMapping, error) {
	return s.mapping, nil
}

func (s *stubStore) GetPlatformAccountID(_ context.Context, _ uuid.UUID) (int64, bool, error) {
	return s.pinned, s.pinned > 0, s.pinErr
}

func (s *stubStore) SetPlatformAccountID(_ context.Context, _ uuid.UUID, id int64) error {
	s.setCalls++
	s.pinned = id
	return nil
}

var tenantA = uuid.MustParse("11111111-1111-1111-1111-111111111111")

func owner() *domain.UserIdentityMapping {
	return &domain.UserIdentityMapping{TenantID: tenantA, IDPSubject: "sub-owner", IsOwner: true}
}

func member() *domain.UserIdentityMapping {
	return &domain.UserIdentityMapping{TenantID: tenantA, IDPSubject: "sub-member"}
}

func subscribeIn(sub string) billing.SubscribeInput {
	return billing.SubscribeInput{
		Caller:         billing.Caller{IDPSubject: sub, TenantID: tenantA},
		PlanCode:       "pro",
		BillingCycle:   "monthly",
		PaymentMethod:  "wallet",
		ReturnURL:      "https://tally.lurus.cn/subscription",
		IdempotencyKey: "idem-1",
	}
}

func TestSubscribe_ChecksOutOnTenantAccountNotCaller(t *testing.T) {
	plat := &stubPlatform{checkoutResp: &platformclient.SubscriptionCheckoutResponse{
		Subscription: &platformclient.SubscriptionSnapshot{PlanCode: "pro", Status: "active"},
	}}
	store := &stubStore{mapping: owner(), pinned: 7}
	out, err := billing.NewSubscribeUseCase(plat, store).Execute(context.Background(), subscribeIn("sub-owner"))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if plat.gotCheckoutReq.AccountID != 7 || plat.gotCheckoutReq.ProductID != billing.ProductID {
		t.Errorf("checkout must target the tenant's pinned account: %+v", plat.gotCheckoutReq)
	}
	if plat.gotIdempotencyKey != "idem-1" {
		t.Errorf("idempotency key not forwarded: %q", plat.gotIdempotencyKey)
	}
	if out.Subscription == nil || out.Subscription.PlanCode != "pro" {
		t.Errorf("subscription not propagated: %+v", out)
	}
}

func TestSubscribe_MemberIsRefused(t *testing.T) {
	plat := &stubPlatform{}
	store := &stubStore{mapping: member(), pinned: 7}
	_, err := billing.NewSubscribeUseCase(plat, store).Execute(context.Background(), subscribeIn("sub-member"))
	if !errors.Is(err, billing.ErrOwnerOnly) {
		t.Fatalf("want ErrOwnerOnly, got %v", err)
	}
	if plat.checkoutCalls != 0 {
		t.Error("member must not reach platform checkout")
	}
}

func TestSubscribe_OtherTenantsMemberIsUnauthenticated(t *testing.T) {
	other := owner()
	other.TenantID = uuid.New()
	_, err := billing.NewSubscribeUseCase(&stubPlatform{}, &stubStore{mapping: other, pinned: 7}).
		Execute(context.Background(), subscribeIn("sub-owner"))
	if !errors.Is(err, billing.ErrUnauthenticated) {
		t.Fatalf("want ErrUnauthenticated, got %v", err)
	}
}

func TestSubscribe_UnpinnedOwnerIsProvisionedAndPinned(t *testing.T) {
	plat := &stubPlatform{
		upserted:     &platformclient.Account{ID: 42},
		checkoutResp: &platformclient.SubscriptionCheckoutResponse{OrderNo: "LO1", PayURL: "https://openapi.alipay.com/x"},
	}
	store := &stubStore{mapping: owner()}
	if _, err := billing.NewSubscribeUseCase(plat, store).Execute(context.Background(), subscribeIn("sub-owner")); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if plat.upsertCalls != 1 || store.pinned != 42 || plat.gotCheckoutReq.AccountID != 42 {
		t.Errorf("unpinned owner must be provisioned, pinned and charged: upserts=%d pinned=%d checkout=%d",
			plat.upsertCalls, store.pinned, plat.gotCheckoutReq.AccountID)
	}
}

func TestSubscribe_WechatOnDesktopReturnsQRCode(t *testing.T) {
	plat := &stubPlatform{checkoutResp: &platformclient.SubscriptionCheckoutResponse{OrderNo: "LO2", PayURL: "weixin://wxpay/bizpayurl?pr=abc"}}
	in := subscribeIn("sub-owner")
	in.PaymentMethod = "wechat"
	in.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"
	out, err := billing.NewSubscribeUseCase(plat, &stubStore{mapping: owner(), pinned: 7}).Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if plat.gotCheckoutReq.PaymentMethod != "wechat_native" {
		t.Errorf("wechat on desktop must resolve to wechat_native, got %q", plat.gotCheckoutReq.PaymentMethod)
	}
	if out.QRCode != "weixin://wxpay/bizpayurl?pr=abc" || out.PayURL != "" || out.OrderNo != "LO2" {
		t.Errorf("native pay must come back as a QR code, not a redirect: %+v", out)
	}
}

func TestSubscribe_PropagatesTypedCheckoutError(t *testing.T) {
	plat := &stubPlatform{checkoutErr: &platformclient.Error{Code: platformclient.ErrCodeInsufficientBalance, HTTPStatus: 402}}
	_, err := billing.NewSubscribeUseCase(plat, &stubStore{mapping: owner(), pinned: 7}).Execute(context.Background(), subscribeIn("sub-owner"))
	if !platformclient.IsCode(err, platformclient.ErrCodeInsufficientBalance) {
		t.Errorf("typed error must survive wrap: %v", err)
	}
}

func TestSubscribe_RejectsMissingFields(t *testing.T) {
	uc := billing.NewSubscribeUseCase(&stubPlatform{}, &stubStore{mapping: owner(), pinned: 7})
	if _, err := uc.Execute(context.Background(), billing.SubscribeInput{PlanCode: "pro", BillingCycle: "monthly", PaymentMethod: "wallet"}); !errors.Is(err, billing.ErrUnauthenticated) {
		t.Errorf("missing caller: want ErrUnauthenticated, got %v", err)
	}
	in := subscribeIn("sub-owner")
	in.PaymentMethod = ""
	if _, err := uc.Execute(context.Background(), in); !errors.Is(err, billing.ErrInvalidInput) {
		t.Errorf("missing method: want ErrInvalidInput, got %v", err)
	}
}

func TestOverview_MemberSeesTenantPlan(t *testing.T) {
	plat := &stubPlatform{
		overview: &platformclient.AccountOverview{
			Wallet:       platformclient.Wallet{Balance: 120, Frozen: 30},
			VIP:          platformclient.VIP{Level: 2, LevelName: "黄金"},
			Subscription: &platformclient.SubscriptionSnapshot{PlanCode: "pro", Status: "active"},
			TopupURL:     "https://identity.lurus.cn/wallet/topup",
		},
		ents: map[string]string{"plan_code": "pro", "ai_assistant": "true"},
	}
	ov, err := billing.NewOverviewUseCase(plat, &stubStore{mapping: member(), pinned: 7}).
		Execute(context.Background(), billing.Caller{IDPSubject: "sub-member", TenantID: tenantA})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if plat.gotOverviewID != 7 {
		t.Errorf("overview must read the tenant's account, got %d", plat.gotOverviewID)
	}
	if ov.CanManage {
		t.Error("member must not be allowed to manage billing")
	}
	if ov.Wallet.Available != 120 || ov.Wallet.Frozen != 30 || ov.Wallet.Total != 150 {
		t.Errorf("wallet mapping wrong: %+v", ov.Wallet)
	}
	if ov.VIP.Level != 2 || ov.Subscription.PlanCode != "pro" || ov.Entitlements["ai_assistant"] != "true" || ov.TopupURL == "" {
		t.Errorf("overview mapping wrong: %+v", ov)
	}
}

func TestOverview_UnpinnedTenantReadsAsFree(t *testing.T) {
	plat := &stubPlatform{}
	ov, err := billing.NewOverviewUseCase(plat, &stubStore{mapping: owner()}).
		Execute(context.Background(), billing.Caller{IDPSubject: "sub-owner", TenantID: tenantA})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !ov.CanManage || ov.Entitlements["plan_code"] != "free" || ov.Subscription != nil {
		t.Errorf("unpinned tenant must read as free and manageable by the owner: %+v", ov)
	}
	if plat.gotOverviewID != 0 {
		t.Error("no platform read expected for an unpinned tenant")
	}
}

func TestOrderStatus_ClassifiesFinalStates(t *testing.T) {
	cases := map[string]struct{ paid, final bool }{
		"pending": {false, false},
		"paid":    {true, true},
		"expired": {false, true},
		"failed":  {false, true},
	}
	for status, want := range cases {
		plat := &stubPlatform{status: &platformclient.CheckoutStatus{OrderNo: "LO1", Status: status, AmountCNY: 199}}
		st, err := billing.NewOrderStatusUseCase(plat, &stubStore{mapping: member()}).
			Execute(context.Background(), billing.Caller{IDPSubject: "sub-member", TenantID: tenantA}, "LO1")
		if err != nil {
			t.Fatalf("%s: %v", status, err)
		}
		if st.Paid != want.paid || st.Final != want.final {
			t.Errorf("%s: got paid=%v final=%v", status, st.Paid, st.Final)
		}
	}
}
