package sku_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
)

// fakePriceRepo is a configurable PriceRepo double: it seeds ListDefaultSKUs
// results, can fail ListDefaultSKUs or UpdateRetailPrice (optionally only
// after N successful updates, to exercise the "partial affected count on
// mid-loop failure" invariant), and captures every UpdateRetailPrice call so
// assertions can hand-verify per-SKU new prices instead of trusting the
// returned count alone.
type fakePriceRepo struct {
	skus        []appsku.DefaultSKU
	listErr     error
	updateErrAt int // 1-based call index at which UpdateRetailPrice fails; 0 = never
	updateCalls int
	updates     map[uuid.UUID]decimal.Decimal
	updateOrder []uuid.UUID
}

func (f *fakePriceRepo) ListDefaultSKUs(_ context.Context, _ uuid.UUID, _ []uuid.UUID) ([]appsku.DefaultSKU, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.skus, nil
}

func (f *fakePriceRepo) UpdateRetailPrice(_ context.Context, _, skuID uuid.UUID, newPrice decimal.Decimal) error {
	f.updateCalls++
	if f.updateErrAt != 0 && f.updateCalls == f.updateErrAt {
		return errors.New("write failed")
	}
	if f.updates == nil {
		f.updates = make(map[uuid.UUID]decimal.Decimal)
	}
	f.updates[skuID] = newPrice
	f.updateOrder = append(f.updateOrder, skuID)
	return nil
}

// --- ApplyAction: exhaustive error/edge table ---

func TestApplyAction_Table(t *testing.T) {
	cases := []struct {
		name    string
		current decimal.Decimal
		action  string
		want    decimal.Decimal
		wantErr bool
	}{
		{
			name:    "empty action errors",
			current: decimal.NewFromInt(100),
			action:  "",
			wantErr: true,
		},
		{
			name:    "whitespace-only action errors (trimmed to empty)",
			current: decimal.NewFromInt(100),
			action:  "   ",
			wantErr: true,
		},
		{
			name:    "bad percentage numeric part errors",
			current: decimal.NewFromInt(100),
			action:  "+x%",
			wantErr: true,
		},
		{
			name:    "bad absolute numeric part errors",
			current: decimal.NewFromInt(100),
			action:  "=abc",
			wantErr: true,
		},
		{
			name:    "unrecognised bare non-numeric action errors",
			current: decimal.NewFromInt(100),
			action:  "foo",
			wantErr: true,
		},
		{
			name:    "leading/trailing whitespace trimmed around percentage",
			current: decimal.NewFromInt(100),
			action:  "  +5%  ",
			want:    decimal.NewFromInt(105),
		},
		{
			name:    "relative increase: 100 * 1.05 = 105.000000",
			current: decimal.NewFromInt(100),
			action:  "+5%",
			want:    decimal.NewFromInt(105),
		},
		{
			name:    "relative decrease: 200 * 0.90 = 180",
			current: decimal.NewFromInt(200),
			action:  "-10%",
			want:    decimal.NewFromInt(180),
		},
		{
			name:    "absolute with '=' prefix sets 199.00 exactly",
			current: decimal.NewFromInt(100),
			action:  "=199.00",
			want:    decimal.RequireFromString("199"),
		},
		{
			name:    "bare absolute number (no '=' no '%') sets value directly",
			current: decimal.NewFromInt(100),
			action:  "199.00",
			want:    decimal.RequireFromString("199"),
		},
		{
			name:    "negative percentage beyond -100% clamps to zero, never negative",
			current: decimal.NewFromInt(100),
			action:  "-150%",
			want:    decimal.Zero,
		},
		{
			name:    "negative absolute action clamps to zero",
			current: decimal.NewFromInt(100),
			action:  "=-50",
			want:    decimal.Zero,
		},
		{
			name:    "relative rounding: 100 * 1.033333 rounds to 6 decimals",
			current: decimal.NewFromInt(100),
			action:  "+3.3333333%",
			// 100 * (1 + 3.3333333/100) = 103.33333333 -> round(6) = 103.333333
			want: decimal.RequireFromString("103.333333"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := appsku.ApplyAction(tc.current, tc.action)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (result=%s)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("got %s, want %s", got, tc.want)
			}
			if got.IsNegative() {
				t.Errorf("result %s is negative; money invariant violated", got)
			}
		})
	}
}

func TestApplyAction_UnrecognisedMessageMentionsExamples(t *testing.T) {
	_, err := appsku.ApplyAction(decimal.NewFromInt(100), "cheaper please")
	if err == nil {
		t.Fatal("expected error for unrecognised action")
	}
	msg := err.Error()
	if !containsAll(msg, "+5%", "-10%", "=199.00") {
		t.Errorf("error message %q should mention usage examples", msg)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// --- UpdatePriceUseCase: validation, zero-write short-circuits, repo error wrapping ---

func TestUpdatePriceUseCase_NilTenantID_ErrorsBeforeAnyRepoCall(t *testing.T) {
	repo := &fakePriceRepo{skus: []appsku.DefaultSKU{{SKUID: uuid.New(), RetailPrice: decimal.NewFromInt(100)}}}
	uc := appsku.NewUpdatePriceUseCase(repo)

	affected, err := uc.Execute(context.Background(), uuid.Nil, []uuid.UUID{uuid.New()}, "+5%")
	if err == nil {
		t.Fatal("expected error for nil tenant_id")
	}
	if affected != 0 {
		t.Errorf("affected=%d, want 0", affected)
	}
	if repo.updateCalls != 0 {
		t.Errorf("expected no repo writes, got %d", repo.updateCalls)
	}
}

func TestUpdatePriceUseCase_EmptyProductIDs_NoRepoCallAtAll(t *testing.T) {
	repo := &fakePriceRepo{skus: []appsku.DefaultSKU{{SKUID: uuid.New(), RetailPrice: decimal.NewFromInt(100)}}}
	uc := appsku.NewUpdatePriceUseCase(repo)

	affected, err := uc.Execute(context.Background(), uuid.New(), []uuid.UUID{}, "+5%")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if affected != 0 {
		t.Errorf("affected=%d, want 0", affected)
	}
	if repo.updateCalls != 0 {
		t.Errorf("expected ListDefaultSKUs/UpdateRetailPrice never called for empty productIDs, got %d update calls", repo.updateCalls)
	}
}

func TestUpdatePriceUseCase_InvalidAction_NoRepoWritesAtAll(t *testing.T) {
	repo := &fakePriceRepo{skus: []appsku.DefaultSKU{{SKUID: uuid.New(), RetailPrice: decimal.NewFromInt(100)}}}
	uc := appsku.NewUpdatePriceUseCase(repo)

	affected, err := uc.Execute(context.Background(), uuid.New(), []uuid.UUID{uuid.New()}, "not-a-price-action")
	if err == nil {
		t.Fatal("expected error for invalid action")
	}
	if affected != 0 {
		t.Errorf("affected=%d, want 0 (validated before any write)", affected)
	}
	if repo.updateCalls != 0 {
		t.Errorf("expected zero UpdateRetailPrice calls for invalid action, got %d", repo.updateCalls)
	}
}

func TestUpdatePriceUseCase_ListDefaultSKUsError_WrappedAndZeroAffected(t *testing.T) {
	underlying := errors.New("db unreachable")
	repo := &fakePriceRepo{listErr: underlying}
	uc := appsku.NewUpdatePriceUseCase(repo)

	affected, err := uc.Execute(context.Background(), uuid.New(), []uuid.UUID{uuid.New()}, "+5%")
	if err == nil {
		t.Fatal("expected wrapped error from ListDefaultSKUs failure")
	}
	if !errors.Is(err, underlying) {
		t.Errorf("expected error chain to wrap underlying error via %%w, got: %v", err)
	}
	if affected != 0 {
		t.Errorf("affected=%d, want 0", affected)
	}
}

func TestUpdatePriceUseCase_UpdateRetailPriceErrorMidLoop_ReturnsPartialAffectedAndWrappedError(t *testing.T) {
	tenantID := uuid.New()
	sku1, sku2, sku3 := uuid.New(), uuid.New(), uuid.New()
	repo := &fakePriceRepo{
		skus: []appsku.DefaultSKU{
			{SKUID: sku1, RetailPrice: decimal.NewFromInt(100)},
			{SKUID: sku2, RetailPrice: decimal.NewFromInt(200)},
			{SKUID: sku3, RetailPrice: decimal.NewFromInt(300)},
		},
		updateErrAt: 2, // fail on the 2nd UpdateRetailPrice call (sku2)
	}
	uc := appsku.NewUpdatePriceUseCase(repo)

	affected, err := uc.Execute(context.Background(), tenantID, []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}, "+10%")
	if err == nil {
		t.Fatal("expected error from UpdateRetailPrice failure")
	}
	// Only sku1 (the row processed before the failing 2nd call) should have
	// been recorded as affected; the loop must stop, not continue past sku3.
	if affected != 1 {
		t.Errorf("affected=%d, want 1 (partial progress before mid-loop failure)", affected)
	}
	if got := repo.updates[sku1]; !got.Equal(decimal.NewFromInt(110)) {
		t.Errorf("sku1 price=%s, want 110 (100 * 1.10)", got)
	}
	if _, ok := repo.updates[sku3]; ok {
		t.Error("sku3 should never have been written; loop must stop at first failure")
	}
	if repo.updateCalls != 2 {
		t.Errorf("expected exactly 2 UpdateRetailPrice attempts (stop after failure), got %d", repo.updateCalls)
	}
}

func TestUpdatePriceUseCase_RelativePercentagePerSKU_HandComputedPrices(t *testing.T) {
	tenantID := uuid.New()
	skuA, skuB, skuC := uuid.New(), uuid.New(), uuid.New()
	repo := &fakePriceRepo{
		skus: []appsku.DefaultSKU{
			{SKUID: skuA, RetailPrice: decimal.NewFromInt(100)}, // 100 * 1.05 = 105
			{SKUID: skuB, RetailPrice: decimal.NewFromInt(50)},  // 50  * 1.05 = 52.5
			{SKUID: skuC, RetailPrice: decimal.RequireFromString("33.333333")},
		},
	}
	uc := appsku.NewUpdatePriceUseCase(repo)

	affected, err := uc.Execute(context.Background(), tenantID, []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}, "+5%")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if affected != 3 {
		t.Errorf("affected=%d, want 3", affected)
	}
	if got := repo.updates[skuA]; !got.Equal(decimal.NewFromInt(105)) {
		t.Errorf("skuA price=%s, want 105", got)
	}
	if got := repo.updates[skuB]; !got.Equal(decimal.RequireFromString("52.5")) {
		t.Errorf("skuB price=%s, want 52.5", got)
	}
	// 33.333333 * 1.05 = 34.99999965 -> round(6) = 35.000000 (rounds up at 7th decimal 5)
	wantC := decimal.RequireFromString("33.333333").Mul(decimal.RequireFromString("1.05")).Round(6)
	if got := repo.updates[skuC]; !got.Equal(wantC) {
		t.Errorf("skuC price=%s, want %s", got, wantC)
	}
}

func TestUpdatePriceUseCase_NegativePercentageClampsAcrossAllSKUs(t *testing.T) {
	tenantID := uuid.New()
	sku1, sku2 := uuid.New(), uuid.New()
	repo := &fakePriceRepo{
		skus: []appsku.DefaultSKU{
			{SKUID: sku1, RetailPrice: decimal.NewFromInt(10)},
			{SKUID: sku2, RetailPrice: decimal.NewFromInt(999)},
		},
	}
	uc := appsku.NewUpdatePriceUseCase(repo)

	affected, err := uc.Execute(context.Background(), tenantID, []uuid.UUID{uuid.New(), uuid.New()}, "-500%")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if affected != 2 {
		t.Errorf("affected=%d, want 2", affected)
	}
	for _, id := range []uuid.UUID{sku1, sku2} {
		got := repo.updates[id]
		if !got.IsZero() {
			t.Errorf("sku %s price=%s, want 0 (clamped, money non-negative invariant)", id, got)
		}
		if got.IsNegative() {
			t.Errorf("sku %s price=%s is negative; invariant violated", id, got)
		}
	}
}

func TestUpdatePriceUseCase_AbsoluteSetAppliesSameValueRegardlessOfCurrentPrice(t *testing.T) {
	tenantID := uuid.New()
	sku1, sku2 := uuid.New(), uuid.New()
	repo := &fakePriceRepo{
		skus: []appsku.DefaultSKU{
			{SKUID: sku1, RetailPrice: decimal.NewFromInt(10)},
			{SKUID: sku2, RetailPrice: decimal.NewFromInt(9999)},
		},
	}
	uc := appsku.NewUpdatePriceUseCase(repo)

	affected, err := uc.Execute(context.Background(), tenantID, []uuid.UUID{uuid.New(), uuid.New()}, "=199.00")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if affected != 2 {
		t.Errorf("affected=%d, want 2", affected)
	}
	want := decimal.RequireFromString("199")
	for _, id := range []uuid.UUID{sku1, sku2} {
		if got := repo.updates[id]; !got.Equal(want) {
			t.Errorf("sku %s price=%s, want 199", id, got)
		}
	}
}
