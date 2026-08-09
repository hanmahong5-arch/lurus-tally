package onboarding

// zz_coverage_test.go closes the remaining statement-coverage gaps left after
// usecase_test.go + calibration_test.go: the abort paths inside Execute (a
// non-duplicate product-create error and a sales-record error), the
// isDuplicateCode nil-error branch, the indexString/contains empty-substring
// branch, and a handful of independent business-invariant checks (quantity
// conservation, remainder distribution, receipt-before-sale ordering,
// idempotent skip-and-continue, unknown-persona fallback). It lives in
// package onboarding (not onboarding_test) because several targets
// (isDuplicateCode, contains, indexString, demoCatalogue, salesSchedule) are
// unexported.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domainproduct "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// --- local fakes (package-internal; distinct names from usecase_test.go's
// onboarding_test-package fakes, which live in a different package and are
// not visible here) ---

type zzProductCreator struct {
	calls     []domainproduct.CreateInput
	errOnCode string
	errMsg    string
}

func (f *zzProductCreator) Execute(_ context.Context, in domainproduct.CreateInput) (*domainproduct.Product, error) {
	f.calls = append(f.calls, in)
	if f.errOnCode != "" && in.Code == f.errOnCode {
		return nil, errors.New(f.errMsg)
	}
	return &domainproduct.Product{
		ID:       uuid.New(),
		TenantID: in.TenantID,
		Code:     in.Code,
		Name:     in.Name,
		Remark:   in.Remark,
	}, nil
}

type zzStockInitializer struct {
	calls []StockInitRequest
	err   error
}

func (f *zzStockInitializer) Execute(_ context.Context, req StockInitRequest) (*domainstock.Snapshot, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return nil, f.err
	}
	return &domainstock.Snapshot{
		TenantID:    req.TenantID,
		ProductID:   req.ProductID,
		WarehouseID: req.WarehouseID,
		OnHandQty:   req.Qty,
	}, nil
}

// zzSalesRecorder fails starting at call index errAfter (0 = fail on the very
// first RecordSale call) once err is set.
type zzSalesRecorder struct {
	calls    []DemoSaleRequest
	err      error
	errAfter int
}

func (f *zzSalesRecorder) RecordSale(_ context.Context, req DemoSaleRequest) error {
	idx := len(f.calls)
	f.calls = append(f.calls, req)
	if f.err != nil && idx >= f.errAfter {
		return f.err
	}
	return nil
}

// --- Execute abort-path coverage ---

// TestZZExecute_AbortsOnNonDuplicateProductError covers the abort branch
// (usecase.go create-product error path) distinct from the duplicate-skip
// branch already covered elsewhere: a non-duplicate product-create error must
// abort the whole seed, not just skip the SKU.
func TestZZExecute_AbortsOnNonDuplicateProductError(t *testing.T) {
	products := &zzProductCreator{errOnCode: "DEMO-RT-001", errMsg: "connection reset by peer"}
	stock := &zzStockInitializer{}
	sales := &zzSalesRecorder{}
	uc := NewSeedDemoUseCase(products, stock, sales)

	_, err := uc.Execute(context.Background(), SeedInput{
		TenantID:    uuid.New(),
		WarehouseID: uuid.New(),
		Persona:     PersonaRetail,
	})
	if err == nil {
		t.Fatal("want error for non-duplicate product create failure, got nil")
	}
	if !contains(err.Error(), "create product") {
		t.Errorf("want error to mention 'create product', got %q", err.Error())
	}
	// Abort must happen on the first (failing) SKU: no stock or later SKUs touched.
	if len(products.calls) != 1 {
		t.Errorf("want abort after 1 product attempt, got %d", len(products.calls))
	}
	if len(stock.calls) != 0 {
		t.Errorf("want 0 stock calls after abort, got %d", len(stock.calls))
	}
}

// TestZZExecute_AbortsOnSalesRecordError covers the sales.RecordSale error
// abort branch: a failure on the backdated sale leg must abort the seed and
// wrap "record sale" in the error, after the opening receipt already posted.
func TestZZExecute_AbortsOnSalesRecordError(t *testing.T) {
	products := &zzProductCreator{}
	stock := &zzStockInitializer{}
	sales := &zzSalesRecorder{err: errors.New("sales db down"), errAfter: 0}
	uc := NewSeedDemoUseCase(products, stock, sales)

	_, err := uc.Execute(context.Background(), SeedInput{
		TenantID:    uuid.New(),
		WarehouseID: uuid.New(),
		Persona:     PersonaRetail,
	})
	if err == nil {
		t.Fatal("want error when RecordSale fails, got nil")
	}
	if !contains(err.Error(), "record sale") {
		t.Errorf("want error to mention 'record sale', got %q", err.Error())
	}
	// Opening receipt for the first SKU must have posted before the sale failed.
	if len(stock.calls) != 1 {
		t.Errorf("want opening stock recorded for first SKU before abort, got %d calls", len(stock.calls))
	}
	if len(sales.calls) != 1 {
		t.Errorf("want abort on the very first sale attempt, got %d calls", len(sales.calls))
	}
	if len(products.calls) != 1 {
		t.Errorf("want no later SKU attempted after abort, got %d product calls", len(products.calls))
	}
}

// TestZZExecute_DuplicateSkipsMiddleSKUAndContinues reinforces the
// idempotency invariant with the duplicate landing on a *middle* SKU (not the
// first), proving the loop truly continues rather than merely not-yet-having
// reached a later abort.
func TestZZExecute_DuplicateSkipsMiddleSKUAndContinues(t *testing.T) {
	products := &zzProductCreator{
		errOnCode: "DEMO-CB-002",
		errMsg:    `pq: duplicate key value violates unique constraint "products_tenant_code_idx"`,
	}
	stock := &zzStockInitializer{}
	sales := &zzSalesRecorder{}
	uc := NewSeedDemoUseCase(products, stock, sales)

	result, err := uc.Execute(context.Background(), SeedInput{
		TenantID:    uuid.New(),
		WarehouseID: uuid.New(),
		Persona:     PersonaCrossBorder,
	})
	if err != nil {
		t.Fatalf("Execute: unexpected error with duplicate middle SKU: %v", err)
	}
	if result.ProductsCreated != 2 {
		t.Errorf("want 2 products created (1 duplicate skipped), got %d", result.ProductsCreated)
	}
	// All 3 SKUs were attempted (the loop did not abort on the duplicate).
	if len(products.calls) != 3 {
		t.Errorf("want all 3 SKUs attempted, got %d", len(products.calls))
	}
	// Only the 2 non-duplicate SKUs get stock initialized.
	if len(stock.calls) != 2 {
		t.Errorf("want 2 stock calls (duplicate skipped), got %d", len(stock.calls))
	}
}

// --- isDuplicateCode / contains / indexString edge coverage ---

func TestZZIsDuplicateCode_NilErrorReturnsFalse(t *testing.T) {
	if isDuplicateCode(nil) {
		t.Error("want isDuplicateCode(nil) == false")
	}
}

func TestZZIsDuplicateCode_AllKeywordsAndNonMatch(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want bool
	}{
		{"pg_code_23505", "ERROR: duplicate key value (SQLSTATE 23505)", true},
		{"duplicate_key_phrase", "pq: duplicate key value violates unique constraint", true},
		{"unique_constraint_phrase", `violates unique constraint "products_code_key"`, true},
		{"unrelated_error", "connection refused", false},
		{"empty_message", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isDuplicateCode(errors.New(tc.msg))
			if got != tc.want {
				t.Errorf("isDuplicateCode(%q) = %v, want %v", tc.msg, got, tc.want)
			}
		})
	}
}

func TestZZContains_EmptySubstringAndEdgeCases(t *testing.T) {
	// Empty substring: indexString short-circuits to 0, contains reports true.
	if !contains("abc", "") {
		t.Error("want contains(\"abc\", \"\") == true")
	}
	if indexString("abc", "") != 0 {
		t.Errorf("want indexString(\"abc\", \"\") == 0, got %d", indexString("abc", ""))
	}
	// Substring longer than s: contains must short-circuit false without indexing.
	if contains("ab", "abc") {
		t.Error("want contains(\"ab\", \"abc\") == false (sub longer than s)")
	}
	// Match in the middle of s.
	if !contains("abcdef", "cde") {
		t.Error("want contains(\"abcdef\", \"cde\") == true")
	}
	if idx := indexString("abcdef", "cde"); idx != 2 {
		t.Errorf("want indexString(\"abcdef\", \"cde\") == 2, got %d", idx)
	}
	// No match at all.
	if contains("abcdef", "xyz") {
		t.Error("want contains(\"abcdef\", \"xyz\") == false")
	}
}

// --- demoCatalogue ---

func TestZZDemoCatalogue_UnknownPersonaFallsBackToDefault(t *testing.T) {
	got := demoCatalogue(Persona("some_unknown_persona"))
	want := demoCatalogue(PersonaHorticulture)
	if len(got) != len(want) {
		t.Fatalf("want %d SKUs for unknown persona (default set), got %d", len(want), len(got))
	}
	for i := range want {
		if got[i].code != want[i].code {
			t.Errorf("SKU %d: want code %s (default set), got %s", i, want[i].code, got[i].code)
		}
	}
}

func TestZZDemoCatalogue_EachPersonaHasThreeSKUsWithOneLowStock(t *testing.T) {
	for _, p := range []Persona{PersonaCrossBorder, PersonaRetail, PersonaHorticulture} {
		skus := demoCatalogue(p)
		if len(skus) != 3 {
			t.Errorf("persona %s: want 3 SKUs, got %d", p, len(skus))
		}
		lowCount := 0
		for _, s := range skus {
			if s.lowStock {
				lowCount++
				// Calibration intent: the low-stock SKU's monthly velocity must
				// clearly outrun its on-hand qty (the tuning goal), i.e. the
				// ratio is well above 1 — not just barely above.
				if s.monthlySales.LessThanOrEqual(s.qtyOnHand) {
					t.Errorf("persona %s SKU %s: lowStock intent requires monthlySales > qtyOnHand, got sales=%s onHand=%s",
						p, s.code, s.monthlySales, s.qtyOnHand)
				}
			}
		}
		if lowCount != 1 {
			t.Errorf("persona %s: want exactly 1 lowStock SKU, got %d", p, lowCount)
		}
	}
}

// --- salesSchedule: quantity conservation + remainder placement ---

// TestZZSalesSchedule_QuantityConservation_TableDriven locks Σparts == total
// exactly for a spread of totals, including 0 and non-8-divisible values.
func TestZZSalesSchedule_QuantityConservation_TableDriven(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	totals := []int64{0, 1, 7, 8, 9, 24, 36, 54, 72, 90, 96, 150}
	for _, total := range totals {
		tot := decimal.NewFromInt(total)
		sched := salesSchedule(tot, now)
		if len(sched) != demoSalesParts {
			t.Errorf("total %d: want %d parts, got %d", total, demoSalesParts, len(sched))
		}
		sum := decimal.Zero
		for _, s := range sched {
			sum = sum.Add(s.qty)
		}
		if !sum.Equal(tot) {
			t.Errorf("total %d: Σparts = %s, want exactly %s", total, sum, tot)
		}
	}
}

// TestZZSalesSchedule_NegativeTotalProducesNegativeParts guards the defense
// documented in Execute: a negative total (never emitted by demoCatalogue
// today) yields negative parts that Execute's IsPositive guard would skip.
func TestZZSalesSchedule_NegativeTotalProducesNegativeParts(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	tot := decimal.NewFromInt(-16) // evenly divisible by 8: every part == -2
	sched := salesSchedule(tot, now)
	sum := decimal.Zero
	for i, s := range sched {
		if !s.qty.Equal(decimal.NewFromInt(-2)) {
			t.Errorf("part %d: want -2, got %s", i, s.qty)
		}
		if !s.qty.IsNegative() {
			t.Errorf("part %d: want negative part for negative total, got %s", i, s.qty)
		}
		sum = sum.Add(s.qty)
	}
	if !sum.Equal(tot) {
		t.Errorf("Σparts = %s, want %s", sum, tot)
	}
}

// TestZZSalesSchedule_RemainderDistribution_ExtraOnEarliestParts hand-computes
// the expected per-part quantities from the documented algorithm
// (base=floor(total/K), remainder spread +1 across the earliest `remainder`
// parts) and checks the schedule matches exactly — locking "extra 落最早".
func TestZZSalesSchedule_RemainderDistribution_ExtraOnEarliestParts(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)

	// total=9: base=1, remainder=1 → part[0]=2, parts[1..7]=1.
	sched9 := salesSchedule(decimal.NewFromInt(9), now)
	if !sched9[0].qty.Equal(decimal.NewFromInt(2)) {
		t.Errorf("total=9 part[0]: want 2, got %s", sched9[0].qty)
	}
	for i := 1; i < 8; i++ {
		if !sched9[i].qty.Equal(decimal.NewFromInt(1)) {
			t.Errorf("total=9 part[%d]: want 1, got %s", i, sched9[i].qty)
		}
	}

	// total=100: base=12, remainder=4 → parts[0..3]=13, parts[4..7]=12.
	sched100 := salesSchedule(decimal.NewFromInt(100), now)
	for i := 0; i < 4; i++ {
		if !sched100[i].qty.Equal(decimal.NewFromInt(13)) {
			t.Errorf("total=100 part[%d]: want 13, got %s", i, sched100[i].qty)
		}
	}
	for i := 4; i < 8; i++ {
		if !sched100[i].qty.Equal(decimal.NewFromInt(12)) {
			t.Errorf("total=100 part[%d]: want 12, got %s", i, sched100[i].qty)
		}
	}
}

// TestZZSalesSchedule_FractionalRemainder_LandsOnLastPart hand-computes the
// expected parts for a fractional total: base=floor(8.5/8)=1, remainder=0.5,
// extra=0 whole units, frac=0.5 lands entirely on the last part.
func TestZZSalesSchedule_FractionalRemainder_LandsOnLastPart(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	tot, err := decimal.NewFromString("8.5")
	if err != nil {
		t.Fatalf("parse 8.5: %v", err)
	}
	sched := salesSchedule(tot, now)
	for i := 0; i < 7; i++ {
		if !sched[i].qty.Equal(decimal.NewFromInt(1)) {
			t.Errorf("part[%d]: want 1, got %s", i, sched[i].qty)
		}
	}
	want := decimal.RequireFromString("1.5")
	if !sched[7].qty.Equal(want) {
		t.Errorf("part[7] (last, absorbs frac): want 1.5, got %s", sched[7].qty)
	}
}

// TestZZSalesSchedule_Endpoints locks the linear time spread: i=0 lands
// exactly at now-28d, i=K-1 lands exactly at now-2d.
func TestZZSalesSchedule_Endpoints(t *testing.T) {
	now := time.Date(2026, 3, 10, 15, 0, 0, 0, time.UTC)
	sched := salesSchedule(decimal.NewFromInt(24), now)

	want0 := now.Add(-28 * 24 * time.Hour)
	wantLast := now.Add(-2 * 24 * time.Hour)
	if !sched[0].occurredAt.Equal(want0) {
		t.Errorf("part[0].occurredAt: want %v, got %v", want0, sched[0].occurredAt)
	}
	if !sched[demoSalesParts-1].occurredAt.Equal(wantLast) {
		t.Errorf("part[K-1].occurredAt: want %v, got %v", wantLast, sched[demoSalesParts-1].occurredAt)
	}
	// Monotonic non-decreasing across the schedule (linear spread).
	for i := 1; i < len(sched); i++ {
		if sched[i].occurredAt.Before(sched[i-1].occurredAt) {
			t.Errorf("part[%d].occurredAt %v is before part[%d].occurredAt %v — not monotonic",
				i, sched[i].occurredAt, i-1, sched[i-1].occurredAt)
		}
	}
}

// --- SeedDemo inventory conservation (business invariant #2) ---

// TestZZSeedDemo_InventoryConservation_HorticulturePersona drives the full
// Execute path and checks, per SKU: opening receipt Qty == qtyOnHand +
// monthlySales (over-receive), Σsales Qty == monthlySales, so
// opening-Σsales == qtyOnHand exactly (end-state on-hand), and the opening
// receipt is strictly before every one of its sales (receipt-then-sales).
func TestZZSeedDemo_InventoryConservation_HorticulturePersona(t *testing.T) {
	products := &zzProductCreator{}
	stock := &zzStockInitializer{}
	sales := &zzSalesRecorder{}
	uc := NewSeedDemoUseCase(products, stock, sales)

	result, err := uc.Execute(context.Background(), SeedInput{
		TenantID:    uuid.New(),
		WarehouseID: uuid.New(),
		Persona:     PersonaHorticulture,
	})
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if result.ProductsCreated != 3 {
		t.Fatalf("want 3 products created, got %d", result.ProductsCreated)
	}
	if len(stock.calls) != 3 {
		t.Fatalf("want 3 opening-stock calls, got %d", len(stock.calls))
	}

	catalogue := demoCatalogue(PersonaHorticulture)
	for i, sku := range catalogue {
		openingCall := stock.calls[i]

		wantOpeningQty := sku.qtyOnHand.Add(sku.monthlySales)
		if !openingCall.Qty.Equal(wantOpeningQty) {
			t.Errorf("SKU %s: opening Qty = %s, want %s (qtyOnHand+monthlySales)",
				sku.code, openingCall.Qty, wantOpeningQty)
		}

		salesSum := decimal.Zero
		for _, s := range sales.calls {
			if s.ProductID == openingCall.ProductID {
				salesSum = salesSum.Add(s.Qty)
				// Receipt-then-sales: opening receipt must be strictly before
				// every sale for the same product.
				if !openingCall.OccurredAt.Before(s.OccurredAt) {
					t.Errorf("SKU %s: opening receipt at %v not before sale at %v",
						sku.code, openingCall.OccurredAt, s.OccurredAt)
				}
			}
		}
		if !salesSum.Equal(sku.monthlySales) {
			t.Errorf("SKU %s: Σsales = %s, want %s (monthlySales)", sku.code, salesSum, sku.monthlySales)
		}

		endOnHand := openingCall.Qty.Sub(salesSum)
		if !endOnHand.Equal(sku.qtyOnHand) {
			t.Errorf("SKU %s: end-state on-hand = opening(%s) - Σsales(%s) = %s, want qtyOnHand %s",
				sku.code, openingCall.Qty, salesSum, endOnHand, sku.qtyOnHand)
		}
		if endOnHand.IsNegative() {
			t.Errorf("SKU %s: end-state on-hand must never go negative, got %s", sku.code, endOnHand)
		}
	}
}

// --- marshalAttrs ---

func TestZZMarshalAttrs_NilAndEmptyProduceEmptyObject(t *testing.T) {
	raw, err := marshalAttrs(nil)
	if err != nil {
		t.Fatalf("marshalAttrs(nil): unexpected error: %v", err)
	}
	if string(raw) != "{}" {
		t.Errorf("marshalAttrs(nil) = %s, want {}", raw)
	}

	raw2, err := marshalAttrs(map[string]string{})
	if err != nil {
		t.Fatalf("marshalAttrs(empty map): unexpected error: %v", err)
	}
	if string(raw2) != "{}" {
		t.Errorf("marshalAttrs(empty map) = %s, want {}", raw2)
	}
}

func TestZZMarshalAttrs_NonEmptyRoundTrips(t *testing.T) {
	in := map[string]string{"hs_code": "8518300090", "origin": "CN"}
	raw, err := marshalAttrs(in)
	if err != nil {
		t.Fatalf("marshalAttrs: unexpected error: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got["hs_code"] != "8518300090" || got["origin"] != "CN" {
		t.Errorf("round-tripped attrs = %v, want %v", got, in)
	}
}

// --- ClearDemo: Nil tenant guard, already covered elsewhere, plus a fresh
// DemoDeleter fake exercised only within this file (no reuse across
// packages). ---

type zzDemoDeleter struct {
	called   bool
	tenantID uuid.UUID
	err      error
}

func (f *zzDemoDeleter) DeleteDemoProducts(_ context.Context, tenantID uuid.UUID) error {
	f.called = true
	f.tenantID = tenantID
	return f.err
}

func TestZZClearDemoUseCase_NilTenantNeverCallsRepo(t *testing.T) {
	del := &zzDemoDeleter{}
	uc := NewClearDemoUseCase(del)
	if err := uc.Execute(context.Background(), uuid.Nil); err == nil {
		t.Fatal("want error for nil tenant_id, got nil")
	}
	if del.called {
		t.Error("want repo.DeleteDemoProducts NOT called when tenant_id is nil")
	}
}
