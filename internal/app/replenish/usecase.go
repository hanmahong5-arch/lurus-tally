// Package replenish implements the weekly replenishment decision surface.
// It answers "what should we order this week, and how much?" by combining
// current available stock, recent sales velocity, product lead time, and
// in-transit quantities to compute a demand-forecast Re-Order Point (ROP)
// with safety stock.
package replenish

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// SuggestionRow is a single product replenishment suggestion.
type SuggestionRow struct {
	ProductID     uuid.UUID
	ProductName   string
	ProductCode   string
	AvailableQty  decimal.Decimal
	SafetyQty     decimal.Decimal // low_safe_qty from stock_initial; zero if not configured
	AvgDailySales decimal.Decimal
	SuggestedQty  decimal.Decimal // max(0, target + safetyStock − available − inTransit)
	EstAmountCNY  decimal.Decimal // SuggestedQty × unit_cost
	SupplierID    *uuid.UUID      // nil when no supplier is linked
	SupplierName  string          // empty when nil
	// UrgencyScore is days-of-supply at current velocity; lower = more urgent.
	// Zero velocity products are scored MaxFloat64 (not urgent in a sales sense).
	UrgencyScore decimal.Decimal

	// Forecast-driven fields (v2 formula).
	LeadTimeDays int             // per-product lead time (days); default 7
	InTransit    decimal.Decimal // open purchase qty already ordered but not yet received
	ROP          decimal.Decimal // Re-Order Point = avgDaily × leadTime + safetyStock
	SafetyStock  decimal.Decimal // z × σ × √leadTime  (z=1.65, σ≈avgDaily×0.3)
	// Reason is a human-readable explanation of why this qty was suggested.
	// Example: "日均5×提前期7天 + 安全库存10 − 可用30 − 在途5"
	Reason string

	// Learning-driven fields (F1/F2).
	LastPurchasePrice *decimal.Decimal // most recent approved purchase price in CNY; nil = no history
	LeadTimeSource    string           // one of LeadTimeSource{Learned,Configured,Default}
	LeadTimeSamples   int              // approved-bill samples behind a learned lead time; 0 otherwise
}

// LeadTimeSource values for SuggestionRow.LeadTimeSource.
const (
	LeadTimeSourceLearned    = "learned"    // median of recent actual arrivals
	LeadTimeSourceConfigured = "configured" // user set product.lead_time_days
	LeadTimeSourceDefault    = "default"    // untouched schema default
)

// SuggestionRepo is the data access interface required by ListSuggestionsUseCase.
// Only the use case and its SQL adapter need to depend on this type.
type SuggestionRepo interface {
	// ListSuggestions returns all active products for the tenant together with
	// stock and velocity data needed to compute replenishment quantities.
	// The repo is responsible for the SQL join; the use case owns the formula.
	ListSuggestions(ctx context.Context, tenantID uuid.UUID) ([]RawRow, error)
}

// RawRow is what the repo returns — raw DB fields without formula application.
type RawRow struct {
	ProductID     uuid.UUID
	ProductName   string
	ProductCode   string
	AvailableQty  decimal.Decimal
	SafetyQty     decimal.Decimal // zero when not configured
	UnitCost      decimal.Decimal
	AvgDailySales decimal.Decimal
	LeadTimeDays  int             // per-product lead time from tally.product
	InTransit     decimal.Decimal // open purchase qty (status 0 or 1)
	SupplierID    *uuid.UUID
	SupplierName  string

	// Learning-driven fields (F1/F2).
	LearnedLeadDays   float64          // median approved_at−created_at in days; only meaningful when LeadTimeSamples ≥ minLeadTimeSamples
	LeadTimeSamples   int              // number of approved purchase bills behind LearnedLeadDays
	LastPurchasePrice *decimal.Decimal // most recent approved purchase price in CNY; nil = no purchase history
}

// SnapshotRow is one row of today's suggestion snapshot persisted to the
// result ledger (F3). Only actionable rows (SuggestedQty > 0) are recorded —
// "nothing to order" is not a suggestion the user could have adopted, so
// logging it would inflate the scorecard denominator.
type SnapshotRow struct {
	ProductID      uuid.UUID
	SuggestedQty   decimal.Decimal
	AvailableQty   decimal.Decimal
	AvgDailySales  decimal.Decimal
	LeadTimeDays   int
	LeadTimeSource string
	DaysOfSupply   decimal.Decimal
}

// SnapshotLedger persists today's suggestion rows so the scorecard can later
// report adoption and stockout misses. Implemented by the SQL repo as one
// multi-row upsert; adopted rows are immutable at the SQL level.
type SnapshotLedger interface {
	UpsertSnapshots(ctx context.Context, tenantID uuid.UUID, rows []SnapshotRow) error
}

// ListSuggestionsUseCase computes weekly replenishment suggestions.
type ListSuggestionsUseCase struct {
	repo   SuggestionRepo
	ledger SnapshotLedger // optional — nil keeps the read path ledger-free
	log    *slog.Logger
}

// NewListSuggestionsUseCase constructs the use case with its required repo.
func NewListSuggestionsUseCase(repo SuggestionRepo) *ListSuggestionsUseCase {
	return &ListSuggestionsUseCase{repo: repo, log: slog.Default()}
}

// WithLedger enables the best-effort F3 result ledger. A nil logger falls
// back to slog.Default so ledger failures are never silently dropped.
func (uc *ListSuggestionsUseCase) WithLedger(ledger SnapshotLedger, log *slog.Logger) *ListSuggestionsUseCase {
	uc.ledger = ledger
	if log != nil {
		uc.log = log
	}
	return uc
}

const defaultWeeks = 2

// defaultLeadTimeDays mirrors the tally.product.lead_time_days NOT NULL DEFAULT.
// A stored value of 7 is indistinguishable from the untouched default (the
// schema default makes the distinction lossy), so 7 is reported as "default"
// even if the user deliberately typed 7.
const defaultLeadTimeDays = 7

// minLeadTimeSamples is the minimum number of approved purchase bills required
// before a learned median lead time overrides the configured value. Mirrors
// the HAVING threshold in the repo's lead_learned CTE.
const minLeadTimeSamples = 2

// z is the service-level factor for safety stock (z=1.65 → ~95% service level).
const z = 1.65

// sigmaFactor approximates σ (demand standard deviation) as a fraction of avg daily sales.
// Using 0.3 (30%) as a conservative approximation — same as internal/app/ai computeROP.
const sigmaFactor = 0.3

// Execute returns replenishment suggestions sorted by urgency ascending
// (products closest to stockout first).
//
// Formula (v2 — demand-forecast ROP with safety stock):
//
//	safetyStock  = z × (avgDailySales × σFactor) × √leadTimeDays  (z=1.65, σFactor=0.3)
//	ROP          = avgDailySales × leadTimeDays + safetyStock
//	target       = avgDailySales × 7 × weeks  (coverage window)
//	suggestedQty = max(0, target + safetyStock − availableQty − inTransit)
//	urgencyScore = availableQty / avgDailySales  (days-of-supply; 0 velocity → 999999)
func (uc *ListSuggestionsUseCase) Execute(ctx context.Context, tenantID uuid.UUID, weeks int) ([]SuggestionRow, error) {
	if weeks <= 0 {
		weeks = defaultWeeks
	}

	raws, err := uc.repo.ListSuggestions(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	weeksD := decimal.NewFromInt(int64(weeks))
	seven := decimal.NewFromInt(7)
	zD := decimal.NewFromFloat(z)
	sigmaFactorD := decimal.NewFromFloat(sigmaFactor)
	// largeScore represents "not urgent" for zero-velocity products.
	largeScore := decimal.NewFromInt(999999)

	out := make([]SuggestionRow, 0, len(raws))
	for _, r := range raws {
		// Lead-time three-state resolution: a learned median (≥2 real arrival
		// samples) beats whatever is configured; otherwise a non-default
		// configured value wins; 7 collapses to "default" (see defaultLeadTimeDays).
		leadTime := r.LeadTimeDays
		leadTimeSource := LeadTimeSourceDefault
		if leadTime <= 0 {
			leadTime = defaultLeadTimeDays
		} else if leadTime != defaultLeadTimeDays {
			leadTimeSource = LeadTimeSourceConfigured
		}
		if r.LeadTimeSamples >= minLeadTimeSamples {
			learned := int(math.Round(r.LearnedLeadDays))
			if learned < 1 {
				// Sub-day medians round to 1 so the ROP formula keeps a horizon.
				learned = 1
			}
			leadTime = learned
			leadTimeSource = LeadTimeSourceLearned
		}

		// Safety stock: z × σ × √leadTime.
		// σ is approximated as avgDailySales × sigmaFactor.
		var safetyStock decimal.Decimal
		if !r.AvgDailySales.IsZero() {
			sigma := r.AvgDailySales.Mul(sigmaFactorD)
			sqrtLT := decimal.NewFromFloat(math.Sqrt(float64(leadTime)))
			safetyStock = zD.Mul(sigma).Mul(sqrtLT)
		}

		// ROP = avgDailySales × leadTime + safetyStock.
		ltD := decimal.NewFromInt(int64(leadTime))
		rop := r.AvgDailySales.Mul(ltD).Add(safetyStock)

		// Target coverage: avgDailySales × 7 × weeks.
		target := r.AvgDailySales.Mul(seven).Mul(weeksD)

		// Suggested = target + safetyStock − available − inTransit, floored at 0, whole units.
		suggested := target.Add(safetyStock).Sub(r.AvailableQty).Sub(r.InTransit)
		if suggested.IsNegative() {
			suggested = decimal.Zero
		}
		// Round up to the nearest whole unit.
		suggested = suggested.Ceil()

		// Estimated amount prefers the real last purchase price over the
		// weighted-average cost — WAC drifts when old cheap stock lingers.
		// A nil or non-positive last price (no history / free sample) keeps WAC.
		unitPrice := r.UnitCost
		if r.LastPurchasePrice != nil && r.LastPurchasePrice.IsPositive() {
			unitPrice = *r.LastPurchasePrice
		}
		estAmount := suggested.Mul(unitPrice)

		// Urgency: days-of-supply.
		var urgency decimal.Decimal
		if r.AvgDailySales.IsZero() {
			urgency = largeScore
		} else {
			urgency = r.AvailableQty.Div(r.AvgDailySales)
		}

		// Human-readable reason string (why this qty).
		reason := fmt.Sprintf(
			"日均%s×提前期%d天 + 安全库存%s − 可用%s − 在途%s",
			r.AvgDailySales.StringFixed(1),
			leadTime,
			safetyStock.StringFixed(1),
			r.AvailableQty.StringFixed(0),
			r.InTransit.StringFixed(0),
		)
		if leadTimeSource == LeadTimeSourceLearned {
			reason += fmt.Sprintf("；基于最近%d次实际到货,中位交期%d天", r.LeadTimeSamples, leadTime)
		}

		out = append(out, SuggestionRow{
			ProductID:     r.ProductID,
			ProductName:   r.ProductName,
			ProductCode:   r.ProductCode,
			AvailableQty:  r.AvailableQty,
			SafetyQty:     r.SafetyQty,
			AvgDailySales: r.AvgDailySales,
			SuggestedQty:  suggested,
			EstAmountCNY:  estAmount,
			SupplierID:    r.SupplierID,
			SupplierName:  r.SupplierName,
			UrgencyScore:  urgency,
			LeadTimeDays:  leadTime,
			InTransit:     r.InTransit,
			ROP:           rop,
			SafetyStock:   safetyStock,
			Reason:        reason,

			LastPurchasePrice: r.LastPurchasePrice,
			LeadTimeSource:    leadTimeSource,
			LeadTimeSamples:   r.LeadTimeSamples,
		})
	}

	// Sort by urgency ascending — closest to stockout first.
	sort.Slice(out, func(i, j int) bool {
		return out[i].UrgencyScore.LessThan(out[j].UrgencyScore)
	})

	// Best-effort F3 ledger write: record today's actionable rows so the
	// scorecard can later report adoption and misses. Runs SEQUENTIALLY after
	// the read on the same request-pinned connection (dbscope) — never a
	// goroutine — and a failure only logs: the suggestions read must not
	// break because trust bookkeeping did.
	if uc.ledger != nil {
		snaps := make([]SnapshotRow, 0, len(out))
		for _, s := range out {
			if !s.SuggestedQty.IsPositive() {
				continue
			}
			snaps = append(snaps, SnapshotRow{
				ProductID:      s.ProductID,
				SuggestedQty:   s.SuggestedQty,
				AvailableQty:   s.AvailableQty,
				AvgDailySales:  s.AvgDailySales,
				LeadTimeDays:   s.LeadTimeDays,
				LeadTimeSource: s.LeadTimeSource,
				DaysOfSupply:   s.UrgencyScore, // days-of-supply IS the urgency score
			})
		}
		if len(snaps) > 0 {
			if lerr := uc.ledger.UpsertSnapshots(ctx, tenantID, snaps); lerr != nil {
				uc.log.Warn("replenish: suggestion ledger upsert failed",
					slog.String("tenant_id", tenantID.String()),
					slog.String("error", lerr.Error()))
			}
		}
	}

	return out, nil
}
