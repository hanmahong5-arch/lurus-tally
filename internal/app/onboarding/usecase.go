// Package onboarding implements the guided first-run seeding flow.
// It seeds demo data per persona so new tenants reach the replenishment
// "aha moment" in under ten minutes. All demo rows are marked with
// remark="DEMO" so ClearDemoUseCase can delete exactly those rows.
//
// The use case depends only on narrow interfaces; the supervisor wires
// the concrete implementations (product + stock use cases) at startup.
package onboarding

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domainproduct "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// Persona is the profile type chosen during onboarding.
type Persona string

const (
	PersonaCrossBorder  Persona = "cross_border"
	PersonaRetail       Persona = "retail"
	PersonaHorticulture Persona = "horticulture"
)

// demoRemark is written into every seeded product's remark field.
// ClearDemoUseCase targets exactly this marker.
const demoRemark = "DEMO"

// ProductCreator is the narrow interface the onboarding use case needs
// to create a product. Satisfied by *appproduct.CreateUseCase.
type ProductCreator interface {
	Execute(ctx context.Context, in domainproduct.CreateInput) (*domainproduct.Product, error)
}

// StockInitializer is the narrow interface the onboarding use case needs
// to record an initial stock movement. Satisfied by *appstock.RecordMovementUseCase.
type StockInitializer interface {
	Execute(ctx context.Context, req StockInitRequest) (*domainstock.Snapshot, error)
}

// StockInitRequest is a simplified movement request used by this package only.
// The handler-layer adapter translates this to the real appstock.RecordMovementRequest.
type StockInitRequest struct {
	TenantID    uuid.UUID
	ProductID   uuid.UUID
	WarehouseID uuid.UUID
	Qty         decimal.Decimal
	UnitCost    decimal.Decimal
	// OccurredAt backdates the opening receipt so the seeded ledger reads
	// receipt-then-sales. Zero defaults to now() downstream.
	OccurredAt time.Time
}

// SalesRecorder is the narrow interface the onboarding use case needs to record
// a backdated demo sale (an 'out' movement). Satisfied by the handler-layer
// adapter wrapping *appstock.RecordMovementUseCase.
type SalesRecorder interface {
	RecordSale(ctx context.Context, req DemoSaleRequest) error
}

// DemoSaleRequest is one backdated demo sale. The adapter translates it to a
// RefSale 'out' movement with a synthetic reference_id (reference_id carries no
// FK, so a fresh uuid simply marks the row as a demo sale).
type DemoSaleRequest struct {
	TenantID    uuid.UUID
	ProductID   uuid.UUID
	WarehouseID uuid.UUID
	Qty         decimal.Decimal
	OccurredAt  time.Time
}

// DemoDeleter is the narrow interface the clear use case needs to remove demo rows.
// Satisfied by *repoonboarding.Repo.
type DemoDeleter interface {
	DeleteDemoProducts(ctx context.Context, tenantID uuid.UUID) error
}

// SeedDemoUseCase seeds a small, persona-specific demo catalogue, initial stock,
// and ~30 days of backdated sales so the replenishment intelligence (low-stock
// alert, suggestions, Monday digest, reports) is alive on first run.
// Every created product has remark="DEMO" so ClearDemoUseCase can clean it up.
type SeedDemoUseCase struct {
	products ProductCreator
	stock    StockInitializer
	sales    SalesRecorder
}

// NewSeedDemoUseCase wires the use case. All three deps are required.
func NewSeedDemoUseCase(products ProductCreator, stock StockInitializer, sales SalesRecorder) *SeedDemoUseCase {
	return &SeedDemoUseCase{products: products, stock: stock, sales: sales}
}

// SeedInput is the caller-facing request.
type SeedInput struct {
	TenantID    uuid.UUID
	WarehouseID uuid.UUID // demo warehouse UUID; caller must resolve a real one
	Persona     Persona
}

// SeedResult reports what was created.
type SeedResult struct {
	ProductsCreated int `json:"products_created"`
}

type demoSKU struct {
	code      string
	name      string
	brand     string
	qtyOnHand decimal.Decimal
	unitCost  decimal.Decimal
	// monthlySales is the total units sold over the seeded ~30-day window. It
	// drives the learned velocity: a SKU alerts iff its learned ROP exceeds
	// qtyOnHand (≈ monthlySales > 3.61 × qtyOnHand at leadTime 7). Calibrated so
	// lowStock SKUs sit below ROP (urgent) and the rest stay healthy.
	monthlySales decimal.Decimal
	// lowStock is the calibration intent: true SKUs are tuned to alert. It no
	// longer flows to low_safe_qty — the alert is purely velocity-driven now —
	// but is kept so the calibration invariant test can assert intent == outcome.
	lowStock bool
	attrs    map[string]string
}

func demoCatalogue(p Persona) []demoSKU {
	switch p {
	case PersonaCrossBorder:
		return []demoSKU{
			{
				code: "DEMO-CB-001", name: "无线蓝牙耳机 ANC", brand: "SoundMax",
				qtyOnHand: decimal.NewFromInt(120), unitCost: decimal.NewFromInt(85),
				monthlySales: decimal.NewFromInt(150), lowStock: false,
				attrs: map[string]string{"hs_code": "8518300090", "origin": "CN"},
			},
			{
				code: "DEMO-CB-002", name: "便携式充电宝 20000mAh", brand: "PowerGo",
				qtyOnHand: decimal.NewFromInt(80), unitCost: decimal.NewFromInt(42),
				monthlySales: decimal.NewFromInt(96), lowStock: false,
				attrs: map[string]string{"hs_code": "8507600000", "origin": "CN"},
			},
			{
				code: "DEMO-CB-003", name: "USB-C 编织数据线 1m", brand: "CablePro",
				qtyOnHand: decimal.NewFromInt(8), unitCost: decimal.NewFromInt(6),
				monthlySales: decimal.NewFromInt(90), lowStock: true, // high velocity vs stock → triggers alert
				attrs: map[string]string{"hs_code": "8544429900", "origin": "CN"},
			},
		}
	case PersonaRetail:
		return []demoSKU{
			{
				code: "DEMO-RT-001", name: "保温杯 500ml 不锈钢", brand: "BrewKeep",
				qtyOnHand: decimal.NewFromInt(60), unitCost: decimal.NewFromInt(28),
				monthlySales: decimal.NewFromInt(72), lowStock: false,
			},
			{
				code: "DEMO-RT-002", name: "帆布手提袋 大号", brand: "EcoBag",
				qtyOnHand: decimal.NewFromInt(45), unitCost: decimal.NewFromInt(12),
				monthlySales: decimal.NewFromInt(54), lowStock: false,
			},
			{
				code: "DEMO-RT-003", name: "折叠雨伞 自动款", brand: "RainGuard",
				qtyOnHand: decimal.NewFromInt(5), unitCost: decimal.NewFromInt(18),
				monthlySales: decimal.NewFromInt(66), lowStock: true, // high velocity vs stock → triggers alert
			},
		}
	default: // horticulture and unknown personas — generic set
		return []demoSKU{
			{
				code: "DEMO-HO-001", name: "红枫 Acer palmatum 3年苗", brand: "",
				qtyOnHand: decimal.NewFromInt(30), unitCost: decimal.NewFromInt(25),
				monthlySales: decimal.NewFromInt(36), lowStock: false,
			},
			{
				code: "DEMO-HO-002", name: "桂花 Osmanthus fragrans 2年苗", brand: "",
				qtyOnHand: decimal.NewFromInt(20), unitCost: decimal.NewFromInt(18),
				monthlySales: decimal.NewFromInt(24), lowStock: false,
			},
			{
				code: "DEMO-HO-003", name: "紫荆 Cercis chinensis 1年苗", brand: "",
				qtyOnHand: decimal.NewFromInt(4), unitCost: decimal.NewFromInt(8),
				monthlySales: decimal.NewFromInt(36), lowStock: true, // high velocity vs stock → triggers alert
			},
		}
	}
}

// Execute seeds demo products and stock for the given tenant + persona.
// It is idempotent at the product-code level: the repo's UNIQUE index on
// (tenant_id, code) prevents duplicate rows on retry; the use case proceeds
// to the next SKU when a duplicate-code error is returned.
func (uc *SeedDemoUseCase) Execute(ctx context.Context, in SeedInput) (*SeedResult, error) {
	if in.TenantID == uuid.Nil {
		return nil, fmt.Errorf("seed demo: tenant_id required")
	}
	if in.WarehouseID == uuid.Nil {
		return nil, fmt.Errorf("seed demo: warehouse_id required — resolve a real warehouse first")
	}

	skus := demoCatalogue(in.Persona)
	created := 0

	// One clock read for the whole seed: the opening receipt is backdated to
	// −openingOffset and sales are spread across the trailing window relative to it.
	now := time.Now().UTC()
	openingAt := now.Add(-demoOpeningOffsetDays * 24 * time.Hour)

	for _, sku := range skus {
		attrsRaw, merr := marshalAttrs(sku.attrs)
		if merr != nil {
			return nil, fmt.Errorf("seed demo: marshal attrs for %s: %w", sku.code, merr)
		}

		p, perr := uc.products.Execute(ctx, domainproduct.CreateInput{
			TenantID:   in.TenantID,
			Code:       sku.code,
			Name:       sku.name,
			Brand:      sku.brand,
			Remark:     demoRemark,
			Attributes: attrsRaw,
		})
		if perr != nil {
			// Duplicate code means this SKU was already seeded — skip silently.
			// Other errors abort the whole seed operation.
			if isDuplicateCode(perr) {
				continue
			}
			return nil, fmt.Errorf("seed demo: create product %s: %w", sku.code, perr)
		}
		created++

		// Over-receive at −openingOffset: receive qtyOnHand + monthlySales so the
		// trailing month of sales can be drawn down against real on-hand (the WAC
		// engine has no negative-stock bypass). Recording the 'in' first keeps the
		// running snapshot non-negative regardless of occurred_at.
		if _, serr := uc.stock.Execute(ctx, StockInitRequest{
			TenantID:    in.TenantID,
			ProductID:   p.ID,
			WarehouseID: in.WarehouseID,
			Qty:         sku.qtyOnHand.Add(sku.monthlySales),
			UnitCost:    sku.unitCost,
			OccurredAt:  openingAt,
		}); serr != nil {
			return nil, fmt.Errorf("seed demo: record opening stock for %s: %w", sku.code, serr)
		}

		// Sell monthlySales back down across the trailing window so end-state
		// on-hand == qtyOnHand and the learned velocity drives the alert. Skip any
		// non-positive part (only possible if monthlySales < demoSalesParts or is
		// non-positive — no current SKU, but guarded) so we never record a
		// meaningless 0-qty or invalid negative 'out' movement.
		for _, sale := range salesSchedule(sku.monthlySales, now) {
			if !sale.qty.IsPositive() {
				continue
			}
			if serr := uc.sales.RecordSale(ctx, DemoSaleRequest{
				TenantID:    in.TenantID,
				ProductID:   p.ID,
				WarehouseID: in.WarehouseID,
				Qty:         sale.qty,
				OccurredAt:  sale.occurredAt,
			}); serr != nil {
				return nil, fmt.Errorf("seed demo: record sale for %s: %w", sku.code, serr)
			}
		}
	}

	return &SeedResult{ProductsCreated: created}, nil
}

// demoOpeningOffsetDays backdates the opening receipt. It sits before the
// earliest sale (demoSalesStartDays) so the seeded ledger reads receipt-then-sales.
const demoOpeningOffsetDays = 30

// Sales-window bounds (days before now). The window stays strictly inside the
// 30-day velocity lookback so every seeded sale counts toward avg_daily_sales.
const (
	demoSalesStartDays = 28 // earliest sale
	demoSalesEndDays   = 2  // latest sale
	demoSalesParts     = 8  // K — number of sale movements per SKU
)

// demoSale is one scheduled backdated sale: an integer quantity at a point in time.
type demoSale struct {
	qty        decimal.Decimal
	occurredAt time.Time
}

// salesSchedule splits total into demoSalesParts quantities that sum to EXACTLY
// total, each stamped at an instant evenly spread across [now−28d, now−2d].
// Pure: no clock read, no I/O. For a whole-number total (every demo SKU) the
// parts are whole numbers: the integer remainder (total mod K) is spread one
// unit at a time across the earliest parts. Any sub-unit remainder (only if a
// caller ever passes a fractional total) lands on the last part so the sum is
// exact for any non-negative total. A negative total yields negative parts,
// which the seed loop skips (RecordSale guards on a positive quantity).
func salesSchedule(total decimal.Decimal, now time.Time) []demoSale {
	parts := int64(demoSalesParts)
	base := total.Div(decimal.NewFromInt(parts)).Floor()
	remainder := total.Sub(base.Mul(decimal.NewFromInt(parts))) // total − base×K (≥0 when total ≥ 0)
	extra := remainder.IntPart()                                // whole units to spread (+1 each)
	frac := remainder.Sub(decimal.NewFromInt(extra))            // sub-unit leftover (0 for integer totals)

	startOffset := time.Duration(demoSalesStartDays) * 24 * time.Hour
	endOffset := time.Duration(demoSalesEndDays) * 24 * time.Hour
	span := startOffset - endOffset // 26 days

	out := make([]demoSale, 0, demoSalesParts)
	for i := 0; i < demoSalesParts; i++ {
		qty := base
		if int64(i) < extra {
			qty = qty.Add(decimal.NewFromInt(1))
		}
		if i == demoSalesParts-1 {
			qty = qty.Add(frac) // last part absorbs any sub-unit remainder → Σ == total exactly
		}
		// i=0 → now−28d; i=K-1 → now−2d (linear spread).
		step := time.Duration(int64(span) * int64(i) / int64(demoSalesParts-1))
		out = append(out, demoSale{qty: qty, occurredAt: now.Add(-startOffset + step)})
	}
	return out
}

// ClearDemoUseCase removes all demo-marked rows for a tenant.
type ClearDemoUseCase struct {
	repo DemoDeleter
}

// NewClearDemoUseCase wires the use case.
func NewClearDemoUseCase(repo DemoDeleter) *ClearDemoUseCase {
	return &ClearDemoUseCase{repo: repo}
}

// Execute deletes all products with remark='DEMO' for the tenant.
// Associated stock_snapshot and stock_movement rows are cascade-deleted by FK.
func (uc *ClearDemoUseCase) Execute(ctx context.Context, tenantID uuid.UUID) error {
	if tenantID == uuid.Nil {
		return fmt.Errorf("clear demo: tenant_id required")
	}
	return uc.repo.DeleteDemoProducts(ctx, tenantID)
}

// marshalAttrs converts a string map to a JSON raw message.
// Returns "{}" when attrs is nil or empty.
func marshalAttrs(attrs map[string]string) (json.RawMessage, error) {
	if len(attrs) == 0 {
		return json.RawMessage("{}"), nil
	}
	b, err := json.Marshal(attrs)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// isDuplicateCode returns true when err is a PostgreSQL unique-violation on the
// product code column. We check the error string rather than importing the pq
// driver here to keep this package infrastructure-free.
func isDuplicateCode(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// pq error code 23505 = unique_violation; GORM wraps it in the message.
	return contains(msg, "23505") || contains(msg, "duplicate key") || contains(msg, "unique constraint")
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && indexString(s, sub) >= 0)
}

func indexString(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
