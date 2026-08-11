package replenish_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/notify"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
	"github.com/shopspring/decimal"
)

// ----- usecase.go gaps -----

// TestForecast_Direct_WeeksZeroFallsBackToDefault exercises Forecast (not via
// Execute) with weeks<=0, hitting the DefaultWeeks fallback inside the pure
// function itself (usecase.go:220-222).
//
// avgDaily=5, leadTime=7 → safetyStock = 1.65 × (5×0.3) × √7 ≈ 6.5431...
// target = 5 × 7 × 2 (DefaultWeeks=2, since weeks=0 falls back) = 70
// suggested = ceil(70 + 6.5431 − 0 − 0) = ceil(76.5431...) = 77
func TestForecast_Direct_WeeksZeroFallsBackToDefault(t *testing.T) {
	raw := replenish.RawRow{
		ProductID:     uuid.New(),
		ProductName:   "WeeksZero",
		ProductCode:   "WZ",
		AvailableQty:  d("0"),
		AvgDailySales: d("5"),
		UnitCost:      d("10"),
		LeadTimeDays:  7,
	}
	f := replenish.Forecast(raw, 0)

	ss := expectedSafetyStock(5, 7)
	target := decimal.NewFromFloat(5 * 7 * 2)
	want := target.Add(ss).Ceil()
	if !f.SuggestedQty.Equal(want) {
		t.Errorf("SuggestedQty = %s, want %s (weeks=0 must fall back to DefaultWeeks=2)", f.SuggestedQty, want)
	}
}

// TestForecast_LearnedLeadTime_ZeroRoundsUpToOne hits the `learned < 1` branch
// (usecase.go:243-246) directly: math.Round(0) == 0, which is < 1, so the
// formula must floor it to 1 to keep the ROP horizon non-zero.
func TestForecast_LearnedLeadTime_ZeroRoundsUpToOne(t *testing.T) {
	raw := replenish.RawRow{
		ProductID:       uuid.New(),
		ProductName:     "SubDayZero",
		ProductCode:     "SZ",
		AvailableQty:    d("0"),
		AvgDailySales:   d("5"),
		UnitCost:        d("10"),
		LeadTimeDays:    7,
		LearnedLeadDays: 0.0, // rounds to 0, must floor to 1
		LeadTimeSamples: 2,
	}
	f := replenish.Forecast(raw, 2)
	if f.LeadTimeDays != 1 {
		t.Errorf("LeadTimeDays = %d, want 1 (learned median 0 must floor to 1)", f.LeadTimeDays)
	}
	if f.LeadTimeSource != replenish.LeadTimeSourceLearned {
		t.Errorf("LeadTimeSource = %q, want %q", f.LeadTimeSource, replenish.LeadTimeSourceLearned)
	}
}

// TestListSuggestions_Execute_RepoError_Propagated verifies a repo failure is
// returned as-is (usecase.go:168-170) — no ledger write is attempted either.
func TestListSuggestions_Execute_RepoError_Propagated(t *testing.T) {
	ledger := &stubLedger{}
	uc := replenish.NewListSuggestionsUseCase(&stubRepo{err: errors.New("db down")}).WithLedger(ledger, nil)

	out, err := uc.Execute(context.Background(), uuid.New(), 2)
	if err == nil {
		t.Fatal("expected error from repo, got nil")
	}
	if out != nil {
		t.Errorf("expected nil rows on repo error, got %+v", out)
	}
	if ledger.calls != 0 {
		t.Errorf("expected no ledger call when repo errors, got %d", ledger.calls)
	}
}

// TestListSuggestions_WithLedger_CustomLoggerWired verifies WithLedger's
// `log != nil` branch (usecase.go:123-125): a caller-supplied logger is
// actually used instead of falling back to slog.Default. We can't intercept
// slog.Default output cheaply, so instead we assert indirectly: passing a
// non-nil logger plus a failing ledger must still not crash or fail the read
// (the fact that log.Warn is called through OUR logger and does not panic is
// the observable behavior here alongside the existing swallow-error contract).
func TestListSuggestions_WithLedger_CustomLoggerWired(t *testing.T) {
	rows := []replenish.RawRow{
		{ProductID: uuid.New(), ProductName: "L", ProductCode: "L", AvailableQty: d("0"), AvgDailySales: d("5"), UnitCost: d("10"), LeadTimeDays: 7},
	}
	ledger := &stubLedger{err: errors.New("ledger down")}
	customLog := slog.Default() // non-nil, exercises the `if log != nil` true branch
	uc := replenish.NewListSuggestionsUseCase(&stubRepo{rows: rows}).WithLedger(ledger, customLog)

	out, err := uc.Execute(context.Background(), uuid.New(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 row, got %d", len(out))
	}
	if ledger.calls != 1 {
		t.Errorf("expected ledger to be attempted once, got %d", ledger.calls)
	}
}

// ----- low_stock.go gaps -----

// TestListLowStock_LimitZero_DefaultsTo200 verifies limit<=0 falls back to
// the documented default of 200 (low_stock.go:66-68) rather than returning 0
// rows.
func TestListLowStock_LimitZero_DefaultsTo200(t *testing.T) {
	alert := lowStockRaw("A", "5", "2", "0", 7) // below ROP, would alert
	uc := replenish.NewListLowStockUseCase(&stubRepo{rows: []replenish.RawRow{alert}})
	rows, err := uc.Execute(context.Background(), uuid.New(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row with default limit, got %d", len(rows))
	}
}

// TestListLowStock_RepoError_Propagated verifies repo failures are wrapped
// (low_stock.go:71-73).
func TestListLowStock_RepoError_Propagated(t *testing.T) {
	uc := replenish.NewListLowStockUseCase(&stubRepo{err: errors.New("db down")})
	_, err := uc.Execute(context.Background(), uuid.New(), 50)
	if err == nil {
		t.Fatal("expected error from repo, got nil")
	}
}

// TestListLowStock_TruncatesToLimit verifies more-than-limit alert rows are
// truncated after sorting (low_stock.go:91-93), keeping the most urgent ones.
func TestListLowStock_TruncatesToLimit(t *testing.T) {
	urgent := lowStockRaw("U", "1", "2", "0", 7)   // DoS = 0.5, most urgent
	middle := lowStockRaw("M", "4", "2", "0", 7)   // DoS = 2
	distant := lowStockRaw("D", "10", "2", "0", 7) // DoS = 5, still below ROP≈16.62
	uc := replenish.NewListLowStockUseCase(&stubRepo{rows: []replenish.RawRow{distant, urgent, middle}})

	rows, err := uc.Execute(context.Background(), uuid.New(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected truncation to limit=2, got %d rows: %+v", len(rows), rows)
	}
	if rows[0].ProductCode != "U" || rows[1].ProductCode != "M" {
		t.Errorf("expected [U, M] (most urgent first, truncated), got [%s, %s]", rows[0].ProductCode, rows[1].ProductCode)
	}
}

// ----- batch_draft.go gaps -----

// TestCreateDraftBatch_NilTenant_Rejected verifies the tenant guard
// (batch_draft.go:125-127).
func TestCreateDraftBatch_NilTenant_Rejected(t *testing.T) {
	creator := &stubDraftCreator{}
	uc := replenish.NewCreateDraftBatchUseCase(creator, nil)

	_, err := uc.Execute(context.Background(), replenish.DraftBatchRequest{
		TenantID: uuid.Nil,
		Lines:    []replenish.DraftBatchLine{{ProductID: uuid.New(), Qty: decimal.NewFromInt(1)}},
	})
	if err == nil {
		t.Fatal("expected error for nil tenant_id, got nil")
	}
	if len(creator.calls) != 0 {
		t.Errorf("creator must not be called for nil tenant_id, got %d calls", len(creator.calls))
	}
}

// TestCreateDraftBatch_AllLinesZeroOrNegative_Errors verifies the "nothing
// left after filtering" guard (batch_draft.go:154-156): every line has
// zero/negative qty, so no group survives and the use case must error rather
// than silently create an empty draft.
func TestCreateDraftBatch_AllLinesZeroOrNegative_Errors(t *testing.T) {
	creator := &stubDraftCreator{}
	uc := replenish.NewCreateDraftBatchUseCase(creator, nil)

	_, err := uc.Execute(context.Background(), replenish.DraftBatchRequest{
		TenantID:  uuid.New(),
		CreatorID: uuid.New(),
		Lines: []replenish.DraftBatchLine{
			{ProductID: uuid.New(), Qty: decimal.Zero},
			{ProductID: uuid.New(), Qty: decimal.NewFromInt(-3)},
		},
	})
	if err == nil {
		t.Fatal("expected error when all lines are zero/negative qty, got nil")
	}
	if len(creator.calls) != 0 {
		t.Errorf("creator must not be called when no group survives filtering, got %d calls", len(creator.calls))
	}
}

// stubSupplierNameResolver is a test double for SupplierNameResolver.
type stubSupplierNameResolver struct {
	names map[uuid.UUID]string
	err   error
	calls int
}

func (s *stubSupplierNameResolver) NameByID(_ context.Context, _ uuid.UUID, supplierID uuid.UUID) (string, error) {
	s.calls++
	if s.err != nil {
		return "", s.err
	}
	return s.names[supplierID], nil
}

// TestCreateDraftBatch_ResolverSuccess_SetsSupplierName verifies a successful
// resolver call populates DraftResult.SupplierName (batch_draft.go:239-242
// success path).
func TestCreateDraftBatch_ResolverSuccess_SetsSupplierName(t *testing.T) {
	sid := uuid.New()
	resolver := &stubSupplierNameResolver{names: map[uuid.UUID]string{sid: "Acme Supply Co."}}
	creator := &stubDraftCreator{}
	uc := replenish.NewCreateDraftBatchUseCase(creator, resolver)

	out, err := uc.Execute(context.Background(), replenish.DraftBatchRequest{
		TenantID:  uuid.New(),
		CreatorID: uuid.New(),
		Lines: []replenish.DraftBatchLine{
			{ProductID: uuid.New(), SupplierID: &sid, Qty: decimal.NewFromInt(5)},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolver.calls != 1 {
		t.Fatalf("expected resolver to be called once, got %d", resolver.calls)
	}
	if out.Drafts[0].SupplierName != "Acme Supply Co." {
		t.Errorf("SupplierName = %q, want %q", out.Drafts[0].SupplierName, "Acme Supply Co.")
	}
}

// TestCreateDraftBatch_ResolverError_NameStaysEmpty verifies a failing
// resolver leaves SupplierName empty without failing the whole draft
// creation (batch_draft.go:239-242 error path — "best-effort").
func TestCreateDraftBatch_ResolverError_NameStaysEmpty(t *testing.T) {
	sid := uuid.New()
	resolver := &stubSupplierNameResolver{err: errors.New("lookup failed")}
	creator := &stubDraftCreator{}
	uc := replenish.NewCreateDraftBatchUseCase(creator, resolver)

	out, err := uc.Execute(context.Background(), replenish.DraftBatchRequest{
		TenantID:  uuid.New(),
		CreatorID: uuid.New(),
		Lines: []replenish.DraftBatchLine{
			{ProductID: uuid.New(), SupplierID: &sid, Qty: decimal.NewFromInt(5)},
		},
	})
	if err != nil {
		t.Fatalf("draft creation must not fail on resolver error, got: %v", err)
	}
	if out.Drafts[0].SupplierName != "" {
		t.Errorf("expected empty SupplierName on resolver error, got %q", out.Drafts[0].SupplierName)
	}
}

// TestCreateDraftBatch_WithAdoptionMarker_CustomLoggerWired exercises the
// `log != nil` branch inside WithAdoptionMarker (batch_draft.go:115-117) by
// passing a real, non-nil logger alongside a failing marker: the call must
// still swallow the marker error and return the draft.
func TestCreateDraftBatch_WithAdoptionMarker_CustomLoggerWired(t *testing.T) {
	sid := uuid.New()
	creator := &stubDraftCreator{}
	marker := &stubAdoptionMarker{err: errors.New("marker down")}
	customLog := slog.Default()
	uc := replenish.NewCreateDraftBatchUseCase(creator, nil).WithAdoptionMarker(marker, customLog)

	out, err := uc.Execute(context.Background(), replenish.DraftBatchRequest{
		TenantID:  uuid.New(),
		CreatorID: uuid.New(),
		Lines: []replenish.DraftBatchLine{
			{ProductID: uuid.New(), SupplierID: &sid, Qty: decimal.NewFromInt(2)},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Drafts) != 1 {
		t.Fatalf("expected 1 draft, got %d", len(out.Drafts))
	}
	if len(marker.calls) != 1 {
		t.Errorf("expected marker to be attempted once, got %d", len(marker.calls))
	}
}

// ----- notify_seam.go (0% covered) -----

// TestNotifyLines_FieldMapping verifies the straight field copy from
// LowStockRow to notify.LowStockLine.
func TestNotifyLines_FieldMapping(t *testing.T) {
	rows := []replenish.LowStockRow{
		{
			ProductID:     uuid.New(),
			ProductCode:   "C-1",
			ProductName:   "Widget",
			AvailableQty:  "5",
			ReorderPoint:  "16.62",
			AvgDailySales: "2",
			DaysOfSupply:  "2.5",
		},
	}
	lines := replenish.NotifyLines(rows)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	l := lines[0]
	if l.ProductName != "Widget" || l.ProductCode != "C-1" || l.AvailableQty != "5" ||
		l.ReorderPoint != "16.62" || l.DaysOfSupply != "2.5" {
		t.Errorf("field mapping mismatch: %+v", l)
	}
}

// TestNotifyLines_Empty verifies an empty input yields an empty (non-nil)
// slice.
func TestNotifyLines_Empty(t *testing.T) {
	lines := replenish.NotifyLines(nil)
	if len(lines) != 0 {
		t.Errorf("expected 0 lines for nil input, got %d", len(lines))
	}
}

// TestAlertLowStock_NilDispatcher_NoOp verifies a nil dispatcher short-
// circuits without touching the use case (notify_seam.go:41-44, nil branch).
func TestAlertLowStock_NilDispatcher_NoOp(t *testing.T) {
	uc := replenish.NewListLowStockUseCase(&stubRepo{err: errors.New("must not be called")})
	if err := replenish.AlertLowStock(context.Background(), uc, nil, uuid.New(), "Acme", 50); err != nil {
		t.Fatalf("expected nil error for nil dispatcher, got: %v", err)
	}
}

// TestAlertLowStock_UnconfiguredDispatcher_NoOp verifies a Dispatcher with no
// live notifiers is treated as unconfigured and short-circuits before
// calling the use case (notify_seam.go:41-44, !Configured() branch).
func TestAlertLowStock_UnconfiguredDispatcher_NoOp(t *testing.T) {
	uc := replenish.NewListLowStockUseCase(&stubRepo{err: errors.New("must not be called")})
	dispatcher := notify.NewDispatcher(nil) // no notifiers passed → Configured()==false
	if err := replenish.AlertLowStock(context.Background(), uc, dispatcher, uuid.New(), "Acme", 50); err != nil {
		t.Fatalf("expected nil error for unconfigured dispatcher, got: %v", err)
	}
}

// TestAlertLowStock_UseCaseError_Propagated verifies a failing low-stock
// query is returned as-is (notify_seam.go:45-48) and the dispatcher is never
// invoked.
func TestAlertLowStock_UseCaseError_Propagated(t *testing.T) {
	uc := replenish.NewListLowStockUseCase(&stubRepo{err: errors.New("db down")})
	dispatcher := notify.NewDispatcher(nil, notify.NewLogNotifier(nil))
	err := replenish.AlertLowStock(context.Background(), uc, dispatcher, uuid.New(), "Acme", 50)
	if err == nil {
		t.Fatal("expected error propagated from use case, got nil")
	}
}

// TestAlertLowStock_ConfiguredDispatcher_Dispatches drives the full happy
// path end-to-end (notify_seam.go:45-50): a real low-stock row is found and
// handed to a real (dry-run) Dispatcher, exercising NotifyLines +
// DispatchLowStock together.
func TestAlertLowStock_ConfiguredDispatcher_Dispatches(t *testing.T) {
	alert := lowStockRaw("A", "5", "2", "0", 7) // below ROP → produces a row
	uc := replenish.NewListLowStockUseCase(&stubRepo{rows: []replenish.RawRow{alert}})
	dispatcher := notify.NewDispatcher(nil, notify.NewLogNotifier(nil))

	if err := replenish.AlertLowStock(context.Background(), uc, dispatcher, uuid.New(), "Acme", 50); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestAlertLowStock_ConfiguredDispatcher_NoRows_NoPanic verifies the seam is
// safe to call even when there is nothing to alert on (DispatchLowStock's own
// no-op path, invoked transitively).
func TestAlertLowStock_ConfiguredDispatcher_NoRows_NoPanic(t *testing.T) {
	uc := replenish.NewListLowStockUseCase(&stubRepo{rows: nil})
	dispatcher := notify.NewDispatcher(nil, notify.NewLogNotifier(nil))

	if err := replenish.AlertLowStock(context.Background(), uc, dispatcher, uuid.New(), "Acme", 50); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
