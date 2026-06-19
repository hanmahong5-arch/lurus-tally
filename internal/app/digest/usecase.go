// Package digest provides the weekly summary ("Monday card") use case.
// It aggregates three signals across a tenant's inventory:
//   - Replenishment candidates (available below the learned reorder point)
//   - Oversell risk (available_qty < 0 or on_hand < reserved)
//   - Dead stock (on_hand > 0, no movement in 90 days)
//
// The replenishment count + amount come from the SAME replenish engine the
// dashboard low-stock alert and the /replenish suggestions page use, so the
// Monday card stays coherent with them (count == low-stock count; amount ==
// Σ EstAmountCNY). The remaining counting logic lives here (testable with a
// stub repo); SQL queries are kept in the adapter/repo/digest layer.
package digest

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
)

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
	// ReplenishCount is the number of SKUs below their reorder point — identical
	// to the dashboard low-stock alert count (same ROP definition).
	ReplenishCount int
	// ReplenishAmountCNY is Σ EstAmountCNY over those SKUs (estimated purchase cost).
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

// DigestRepo is the read-only port the repo layer must satisfy for the counts
// that are not derived from the replenish engine.
type DigestRepo interface {
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
	repo     DigestRepo
	replRepo replenish.SuggestionRepo
}

// NewWeeklySummaryUseCase wires the digest repo (oversell / dead-stock /
// scorecard) and the replenish suggestion repo (replenishment count + amount).
func NewWeeklySummaryUseCase(repo DigestRepo, replRepo replenish.SuggestionRepo) *WeeklySummaryUseCase {
	return &WeeklySummaryUseCase{repo: repo, replRepo: replRepo}
}

// Execute runs the aggregate reads SEQUENTIALLY and assembles the summary.
//
// They must be sequential, not concurrent: when middleware.TenantDB has pinned a
// single *sql.Conn for the request (so RLS is enforced), all reads resolve to
// that one connection via dbscope.From — and a *sql.Conn cannot serve queries
// concurrently (a parallel errgroup yields "driver: bad connection"). A few
// small aggregates run serially well within the request budget. Returning on the
// first error also means no other query is left in flight.
func (uc *WeeklySummaryUseCase) Execute(ctx context.Context, tenantID uuid.UUID) (Summary, error) {
	raws, err := uc.replRepo.ListSuggestions(ctx, tenantID)
	if err != nil {
		return Summary{}, err
	}

	// Replenishment = SKUs below the learned reorder point, off the SAME
	// Forecast + ReorderPoint the low-stock alert uses, so the count equals the
	// dashboard low-stock count by construction. The amount sums EstAmountCNY
	// (estimated NEW purchase spend): normally positive, but legitimately 0 when
	// the shortfall is already covered by in-transit orders or the SKU has no
	// cost. The count — not the amount — is what lights the Monday card.
	replenishCount := 0
	replenishAmount := decimal.Zero
	for _, raw := range raws {
		f := replenish.Forecast(raw, replenish.DefaultWeeks)
		threshold := replenish.ReorderPoint(f)
		if threshold.IsZero() || f.AvailableQty.GreaterThanOrEqual(threshold) {
			continue
		}
		replenishCount++
		replenishAmount = replenishAmount.Add(f.EstAmountCNY)
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

	return Summary{
		ReplenishCount:     replenishCount,
		ReplenishAmountCNY: replenishAmount,
		OversellCount:      oversellN,
		DeadStockCount:     deadStockN,
		Suggested:          scorecard.Suggested,
		Adopted:            scorecard.Adopted,
		MissedStockout:     scorecard.MissedStockout,
		GeneratedAt:        time.Now().UTC(),
	}, nil
}
