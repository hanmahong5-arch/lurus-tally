package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	reposku "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/sku"
	repocust "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/customer"
	repoproduct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/product"
	reposupp "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/supplier"
	reposwh "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/warehouse"
	appcust "github.com/hanmahong5-arch/lurus-tally/internal/app/customer"
	appproduct "github.com/hanmahong5-arch/lurus-tally/internal/app/product"
	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
	appsupp "github.com/hanmahong5-arch/lurus-tally/internal/app/supplier"
	appwh "github.com/hanmahong5-arch/lurus-tally/internal/app/warehouse"
	domaincust "github.com/hanmahong5-arch/lurus-tally/internal/domain/customer"
	domainproduct "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
	domainsupp "github.com/hanmahong5-arch/lurus-tally/internal/domain/supplier"
	domainwh "github.com/hanmahong5-arch/lurus-tally/internal/domain/warehouse"
)

type seedState struct {
	tenantID   uuid.UUID
	categories map[string]uuid.UUID
	warehouses []uuid.UUID
	productIDs []uuid.UUID
}

func runSeed(ctx context.Context, db *sql.DB) (*seedState, error) {
	st := &seedState{tenantID: uuid.MustParse("6b2e9a14-7c3d-4f18-9e55-0a8b1c2d3e4f"), categories: map[string]uuid.UUID{}}
	if err := purgeTenant(ctx, db, st.tenantID); err != nil {
		return nil, err
	}
	if err := seedTenant(ctx, db, st.tenantID); err != nil {
		return nil, err
	}
	if err := seedCategories(ctx, db, st); err != nil {
		return nil, err
	}
	if err := seedCustomUnits(ctx, db, st.tenantID); err != nil {
		return nil, err
	}
	whUC := appwh.NewCreateUseCase(reposwh.New(db))
	for i, spec := range []struct {
		code, name, addr, mgr string
		def                   bool
	}{
		{"WH-01", "华东总仓", "上海市嘉定区物流大道88号", "周建华", true},
		{"WH-02", "华南分仓", "广州市白云区太和镇仓储路66号", "陈玉梅", false},
		{"WH-03", "西南备货仓", "成都市双流区成双大道168号", "李国强", false},
	} {
		w, err := whUC.Execute(ctx, domainwh.CreateInput{
			TenantID: st.tenantID, Code: spec.code, Name: spec.name,
			Address: spec.addr, Manager: spec.mgr, IsDefault: spec.def,
		})
		if err != nil {
			return nil, fmt.Errorf("warehouse %d: %w", i, err)
		}
		st.warehouses = append(st.warehouses, w.ID)
	}
	suppUC := appsupp.NewCreateUseCase(reposupp.New(db))
	for i := 0; i < 80; i++ {
		_, err := suppUC.Execute(ctx, domainsupp.CreateInput{
			TenantID: st.tenantID,
			Code:     fmt.Sprintf("SUP-%03d", i+1),
			Name:     supplierName(i),
			Contact:  fmt.Sprintf("联系人%d", i+1),
			Phone:    fmt.Sprintf("021-%07d", 6000000+i),
		})
		if err != nil {
			return nil, fmt.Errorf("supplier %d: %w", i, err)
		}
	}
	custUC := appcust.NewCreateUseCase(repocust.New(db))
	for i := 0; i < 120; i++ {
		_, err := custUC.Execute(ctx, domaincust.CreateInput{
			TenantID:    st.tenantID,
			Code:        fmt.Sprintf("CUS-%03d", i+1),
			Name:        customerName(i),
			ContactName: custGiven[i%len(custGiven)],
			Mobile:      fmt.Sprintf("138%08d", 10000000+i*97),
			Address:     fmt.Sprintf("%sXX路%d号", cities[i%len(cities)], (i%200)+1),
		})
		if err != nil {
			return nil, fmt.Errorf("customer %d: %w", i, err)
		}
	}
	skuRepo := reposku.New(db)
	prodUC := appproduct.NewCreateUseCase(repoproduct.New(db)).WithPriceWriter(skuRepo).WithTx(skuRepo)
	for _, row := range productRows() {
		catID := st.categories[row.industry]
		attrs, _ := json.Marshal(map[string]string{"industry": row.industry, "sale_unit": row.unit})
		purchase := row.purchase
		retail := row.retail
		p, err := prodUC.ExecuteWithPrices(ctx, domainproduct.CreateInput{
			TenantID: st.tenantID, CategoryID: &catID,
			Code: row.code, Name: row.name, Brand: row.industry,
			Attributes: attrs, MeasurementStrategy: domainproduct.StrategyIndividual,
		}, appsku.PriceInput{Purchase: &purchase, Retail: &retail, Wholesale: &retail})
		if err != nil {
			return nil, fmt.Errorf("product %s: %w", row.code, err)
		}
		st.productIDs = append(st.productIDs, p.ID)
	}
	if err := seedStock(ctx, db, st); err != nil {
		return nil, err
	}
	return st, nil
}

func purgeTenant(ctx context.Context, db *sql.DB, tenantID uuid.UUID) error {
	tables := []string{
		"stock_movement", "stock_snapshot", "stock_initial", "product_sku", "product_unit",
		"project", "product", "product_category", "partner", "supplier", "warehouse",
		"unit_def", "tenant_profile",
	}
	for _, tbl := range tables {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("DELETE FROM tally.%s WHERE tenant_id = $1", tbl), tenantID); err != nil {
			return fmt.Errorf("purge %s: %w", tbl, err)
		}
	}
	_, err := db.ExecContext(ctx, `DELETE FROM tally.tenant WHERE id = $1`, tenantID)
	return err
}

func seedTenant(ctx context.Context, db *sql.DB, tenantID uuid.UUID) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO tally.tenant (id, name, status, plan_type, settings)
		VALUES ($1, $2, 1, 'pro', '{"demo_accept_volume":true}')
		ON CONFLICT (id) DO NOTHING`, tenantID, "验收演示商贸（综合业态）")
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO tally.tenant_profile (tenant_id, profile_type, inventory_method)
		VALUES ($1, 'hybrid', 'wac')
		ON CONFLICT (tenant_id) DO NOTHING`, tenantID)
	return err
}

func seedCategories(ctx context.Context, db *sql.DB, st *seedState) error {
	for _, ind := range industries {
		id := uuid.New()
		_, err := db.ExecContext(ctx, `
			INSERT INTO tally.product_category (id, tenant_id, name, code, sort)
			VALUES ($1, $2, $3, $4, $5)`,
			id, st.tenantID, ind.label+"类目", ind.code, len(st.categories))
		if err != nil {
			return err
		}
		st.categories[ind.label] = id
	}
	return nil
}

func seedCustomUnits(ctx context.Context, db *sql.DB, tenantID uuid.UUID) error {
	units := []struct{ code, name, typ string }{
		{"bundle", "捆", "count"}, {"roll", "卷", "count"}, {"pair", "双", "count"},
		{"sheet", "张", "count"}, {"bottle", "瓶", "count"},
	}
	for _, u := range units {
		_, err := db.ExecContext(ctx, `
			INSERT INTO tally.unit_def (id, tenant_id, code, name, unit_type, is_system)
			VALUES ($1, $2, $3, $4, $5, false)
			ON CONFLICT (tenant_id, code) DO NOTHING`,
			uuid.New(), tenantID, u.code, u.name, u.typ)
		if err != nil {
			return err
		}
	}
	return nil
}

func seedStock(ctx context.Context, db *sql.DB, st *seedState) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	rng := rand.New(rand.NewSource(42))
	now := time.Now().UTC()
	for _, prodID := range st.productIDs {
		for wi, whID := range st.warehouses {
			qty := decimal.NewFromInt(int64(15 + rng.Intn(280) + wi*17))
			cost := decimal.NewFromFloat(8 + rng.Float64()*120).Round(2)
			snapID := uuid.New()
			_, err := tx.ExecContext(ctx, `
				INSERT INTO tally.stock_snapshot
					(id, tenant_id, product_id, warehouse_id, on_hand_qty, available_qty,
					 avg_cost_price, unit_cost, cost_strategy, updated_at)
				VALUES ($1,$2,$3,$4,$5,$5,$6,$6,'wac',$7)
				ON CONFLICT (tenant_id, product_id, warehouse_id) DO UPDATE SET
					on_hand_qty = EXCLUDED.on_hand_qty, available_qty = EXCLUDED.available_qty`,
				snapID, st.tenantID, prodID, whID, qty.String(), cost.String(), now)
			if err != nil {
				return err
			}
			total := qty.Mul(cost).Round(2)
			movID := uuid.New()
			_, err = tx.ExecContext(ctx, `
				INSERT INTO tally.stock_movement
					(id, tenant_id, product_id, warehouse_id, direction, qty_base, unit_cost, total_cost,
					 reference_type, reference_id, occurred_at, created_at)
				VALUES ($1,$2,$3,$4,'in',$5,$6,$7,'init',$1,$8,$8)`,
				movID, st.tenantID, prodID, whID, qty.String(), cost.String(), total.String(), now)
			if err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}