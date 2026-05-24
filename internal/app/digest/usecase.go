// Package digest provides the weekly summary ("Monday card") use case.
// It aggregates three signals across a tenant's inventory:
//   - Replenishment candidates (available < safety, positive velocity)
//   - Oversell risk (available_qty < 0 or on_hand < reserved)
//   - Dead stock (on_hand > 0, no movement in 90 days)
//
// The counting / threshold logic lives here (testable with a stub repo).
// SQL queries are kept in the adapter/repo/digest layer.
package digest

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ReplenishRow is one candidate product returned by the repo.
type ReplenishRow struct {
	ProductID    uuid.UUID
	AvailableQty decimal.Decimal
	SafetyQty    decimal.Decimal
	// AvgDailySales over the past 30 days (out-direction movements / 30).
	AvgDailySales decimal.Decimal
	UnitCost      decimal.Decimal
}

// Summary is the computed output of WeeklySummaryUseCase.
type Summary struct {
	// ReplenishCount is the number of SKUs below safety line with positive velocity.
	ReplenishCount int
	// ReplenishAmountCNY is the estimated total purchase cost in CNY.
	ReplenishAmountCNY decimal.Decimal
	// OversellCount is SKUs where available_qty < 0.
	OversellCount int
	// DeadStockCount is SKUs with on_hand > 0 and no movement in 90 days.
	DeadStockCount int
	GeneratedAt    time.Time
}

// DigestRepo is the read-only port the repo layer must satisfy.
type DigestRepo interface {
	// ListReplenishCandidates returns products where available < low_safe_qty
	// AND avg_daily_sales > 0 (velocity measured over the past 30 days).
	ListReplenishCandidates(ctx context.Context, tenantID uuid.UUID) ([]ReplenishRow, error)
	// CountOversell returns the number of products whose available_qty < 0.
	CountOversell(ctx context.Context, tenantID uuid.UUID) (int, error)
	// CountDeadStock returns the number of products with on_hand > 0 and no
	// stock movement in the past 90 days.
	CountDeadStock(ctx context.Context, tenantID uuid.UUID) (int, error)
}

// WeeklySummaryUseCase computes the Monday-card summary for a tenant.
type WeeklySummaryUseCase struct {
	repo DigestRepo
	// coverageDays is how many days of stock to target per suggested order (default 14).
	coverageDays int
}

// NewWeeklySummaryUseCase constructs the use case with a 14-day coverage default.
func NewWeeklySummaryUseCase(repo DigestRepo) *WeeklySummaryUseCase {
	return &WeeklySummaryUseCase{repo: repo, coverageDays: 14}
}

// Execute runs all three aggregate queries concurrently and assembles the summary.
func (uc *WeeklySummaryUseCase) Execute(ctx context.Context, tenantID uuid.UUID) (Summary, error) {
	// Run the three queries in parallel using goroutines + a simple error
	// aggregation pattern. All three reads are idempotent; failures on one
	// should not suppress the others.
	type replenishResult struct {
		rows []ReplenishRow
		err  error
	}
	type intResult struct {
		n   int
		err error
	}

	replCh := make(chan replenishResult, 1)
	oversellCh := make(chan intResult, 1)
	deadCh := make(chan intResult, 1)

	go func() {
		rows, err := uc.repo.ListReplenishCandidates(ctx, tenantID)
		replCh <- replenishResult{rows, err}
	}()
	go func() {
		n, err := uc.repo.CountOversell(ctx, tenantID)
		oversellCh <- intResult{n, err}
	}()
	go func() {
		n, err := uc.repo.CountDeadStock(ctx, tenantID)
		deadCh <- intResult{n, err}
	}()

	replRes := <-replCh
	if replRes.err != nil {
		return Summary{}, replRes.err
	}
	oversellRes := <-oversellCh
	if oversellRes.err != nil {
		return Summary{}, oversellRes.err
	}
	deadRes := <-deadCh
	if deadRes.err != nil {
		return Summary{}, deadRes.err
	}

	coverage := decimal.NewFromInt(int64(uc.coverageDays))
	totalAmount := decimal.Zero
	for _, r := range replRes.rows {
		// suggestedQty = avgDailySales × coverageDays − available (floor 0)
		suggested := r.AvgDailySales.Mul(coverage).Sub(r.AvailableQty)
		if suggested.IsNegative() {
			suggested = decimal.Zero
		}
		totalAmount = totalAmount.Add(suggested.Mul(r.UnitCost))
	}

	return Summary{
		ReplenishCount:     len(replRes.rows),
		ReplenishAmountCNY: totalAmount,
		OversellCount:      oversellRes.n,
		DeadStockCount:     deadRes.n,
		GeneratedAt:        time.Now().UTC(),
	}, nil
}
