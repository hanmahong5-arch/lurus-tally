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
	// LowStock marks this SKU as below safety threshold (qty ≤ low_safe_qty).
	LowStock bool
}

// DemoDeleter is the narrow interface the clear use case needs to remove demo rows.
// Satisfied by *repoonboarding.Repo.
type DemoDeleter interface {
	DeleteDemoProducts(ctx context.Context, tenantID uuid.UUID) error
}

// SeedDemoUseCase seeds a small, persona-specific demo catalogue and initial stock.
// Every created product has remark="DEMO" so ClearDemoUseCase can clean it up.
type SeedDemoUseCase struct {
	products ProductCreator
	stock    StockInitializer
}

// NewSeedDemoUseCase wires the use case. Both deps are required.
func NewSeedDemoUseCase(products ProductCreator, stock StockInitializer) *SeedDemoUseCase {
	return &SeedDemoUseCase{products: products, stock: stock}
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
	lowStock  bool // if true, qty ≤ low_safe_qty threshold
	attrs     map[string]string
}

func demoCatalogue(p Persona) []demoSKU {
	switch p {
	case PersonaCrossBorder:
		return []demoSKU{
			{
				code: "DEMO-CB-001", name: "无线蓝牙耳机 ANC", brand: "SoundMax",
				qtyOnHand: decimal.NewFromInt(120), unitCost: decimal.NewFromInt(85),
				lowStock: false,
				attrs:    map[string]string{"hs_code": "8518300090", "origin": "CN"},
			},
			{
				code: "DEMO-CB-002", name: "便携式充电宝 20000mAh", brand: "PowerGo",
				qtyOnHand: decimal.NewFromInt(80), unitCost: decimal.NewFromInt(42),
				lowStock: false,
				attrs:    map[string]string{"hs_code": "8507600000", "origin": "CN"},
			},
			{
				code: "DEMO-CB-003", name: "USB-C 编织数据线 1m", brand: "CablePro",
				qtyOnHand: decimal.NewFromInt(8), unitCost: decimal.NewFromInt(6),
				lowStock: true, // triggers replenishment suggestion
				attrs:    map[string]string{"hs_code": "8544429900", "origin": "CN"},
			},
		}
	case PersonaRetail:
		return []demoSKU{
			{
				code: "DEMO-RT-001", name: "保温杯 500ml 不锈钢", brand: "BrewKeep",
				qtyOnHand: decimal.NewFromInt(60), unitCost: decimal.NewFromInt(28),
				lowStock: false,
			},
			{
				code: "DEMO-RT-002", name: "帆布手提袋 大号", brand: "EcoBag",
				qtyOnHand: decimal.NewFromInt(45), unitCost: decimal.NewFromInt(12),
				lowStock: false,
			},
			{
				code: "DEMO-RT-003", name: "折叠雨伞 自动款", brand: "RainGuard",
				qtyOnHand: decimal.NewFromInt(5), unitCost: decimal.NewFromInt(18),
				lowStock: true, // triggers replenishment suggestion
			},
		}
	default: // horticulture and unknown personas — generic set
		return []demoSKU{
			{
				code: "DEMO-HO-001", name: "红枫 Acer palmatum 3年苗", brand: "",
				qtyOnHand: decimal.NewFromInt(30), unitCost: decimal.NewFromInt(25),
				lowStock: false,
			},
			{
				code: "DEMO-HO-002", name: "桂花 Osmanthus fragrans 2年苗", brand: "",
				qtyOnHand: decimal.NewFromInt(20), unitCost: decimal.NewFromInt(18),
				lowStock: false,
			},
			{
				code: "DEMO-HO-003", name: "紫荆 Cercis chinensis 1年苗", brand: "",
				qtyOnHand: decimal.NewFromInt(4), unitCost: decimal.NewFromInt(8),
				lowStock: true,
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

		// Record initial stock for the created product.
		_, serr := uc.stock.Execute(ctx, StockInitRequest{
			TenantID:    in.TenantID,
			ProductID:   p.ID,
			WarehouseID: in.WarehouseID,
			Qty:         sku.qtyOnHand,
			UnitCost:    sku.unitCost,
			LowStock:    sku.lowStock,
		})
		if serr != nil {
			return nil, fmt.Errorf("seed demo: record stock for %s: %w", sku.code, serr)
		}
	}

	return &SeedResult{ProductsCreated: created}, nil
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
