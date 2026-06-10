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

// ScorecardCounts is the past-week suggestion track record (F4): how many
// suggestions were made, how many were adopted, and how many ignored
// suggestions ended in a stockout. The window is 7 days — the Monday card
// reports "last week", unlike the replenish scorecard's 28-day view.
type ScorecardCounts struct {
	Suggested      int
	Adopted        int
	MissedStockout int
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
	// Suggested / Adopted / MissedStockout surface last week's suggestion
	// track record on the Monday card (zero values when the ledger is empty).
	Suggested      int
	Adopted        int
	MissedStockout int
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
	// SuggestionScorecard returns the 7-day suggestion track record from the
	// replenish suggestion ledger. An empty ledger yields zero counts, not an
	// error, so tenants without history still get a Monday card.
	SuggestionScorecard(ctx context.Context, tenantID uuid.UUID) (ScorecardCounts, error)
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

// Execute runs the four aggregate queries SEQUENTIALLY and assembles the summary.
//
// They must be sequential, not concurrent: when middleware.TenantDB has pinned a
// single *sql.Conn for the request (so RLS is enforced), all four reads resolve
// to that one connection via dbscope.From — and a *sql.Conn cannot serve queries
// concurrently (a parallel errgroup yields "driver: bad connection"). Four small
// aggregates run serially well within the request budget. Returning on the first
// error also means no other query is left in flight (the original errgroup's F05
// goal), so this is strictly safer.
func (uc *WeeklySummaryUseCase) Execute(ctx context.Context, tenantID uuid.UUID) (Summary, error) {
	replRows, err := uc.repo.ListReplenishCandidates(ctx, tenantID)
	if err != nil {
		return Summary{}, err
	}
	oversellN, err := uc.repo.CountOversell(ctx, tenantID)
	if err != nil {
		return Summary{}, err
	}
	deadStockN, err := uc.repo.CountDeadStock(ctx, tenantID)
	if err != nil {
		return Summary{}, err
	}
	scorecard, err := uc.repo.SuggestionScorecard(ctx, tenantID)
	if err != nil {
		return Summary{}, err
	}

	coverage := decimal.NewFromInt(int64(uc.coverageDays))
	totalAmount := decimal.Zero
	for _, r := range replRows {
		// suggestedQty = avgDailySales × coverageDays − available (floor 0)
		suggested := r.AvgDailySales.Mul(coverage).Sub(r.AvailableQty)
		if suggested.IsNegative() {
			suggested = decimal.Zero
		}
		totalAmount = totalAmount.Add(suggested.Mul(r.UnitCost))
	}

	return Summary{
		ReplenishCount:     len(replRows),
		ReplenishAmountCNY: totalAmount,
		OversellCount:      oversellN,
		DeadStockCount:     deadStockN,
		Suggested:          scorecard.Suggested,
		Adopted:            scorecard.Adopted,
		MissedStockout:     scorecard.MissedStockout,
		GeneratedAt:        time.Now().UTC(),
	}, nil
}
