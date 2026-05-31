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
	"golang.org/x/sync/errgroup"
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
// Uses errgroup.WithContext so the first failure cancels the other in-flight
// queries — without this, a slow DB on one query keeps holding a connection
// even when the caller has already received a failure from another (UAT-3 F05).
func (uc *WeeklySummaryUseCase) Execute(ctx context.Context, tenantID uuid.UUID) (Summary, error) {
	g, gctx := errgroup.WithContext(ctx)

	var (
		replRows   []ReplenishRow
		oversellN  int
		deadStockN int
	)

	g.Go(func() error {
		rows, err := uc.repo.ListReplenishCandidates(gctx, tenantID)
		if err != nil {
			return err
		}
		replRows = rows
		return nil
	})
	g.Go(func() error {
		n, err := uc.repo.CountOversell(gctx, tenantID)
		if err != nil {
			return err
		}
		oversellN = n
		return nil
	})
	g.Go(func() error {
		n, err := uc.repo.CountDeadStock(gctx, tenantID)
		if err != nil {
			return err
		}
		deadStockN = n
		return nil
	})

	if err := g.Wait(); err != nil {
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
		GeneratedAt:        time.Now().UTC(),
	}, nil
}
