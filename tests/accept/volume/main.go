// Package main seeds tally_accept with a large single-tenant demo dataset.
//
// Run (WSL):
//
//	DATABASE_URL='host=/var/run/postgresql user=postgres dbname=tally_accept' \
//	  go run ./tests/accept/volume
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/hanmahong5-arch/lurus-tally/internal/lifecycle"
)

type counts struct {
	products, partners, suppliers, customers, warehouses, stockSnap, stockMov, categories, units int
}

func main() {
	ctx := context.Background()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "host=/var/run/postgresql user=postgres dbname=tally_accept"
	}
	if err := lifecycle.RunMigrations(ctx, dsn, nil); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()
	st, err := runSeed(ctx, db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed: %v\n", err)
		os.Exit(1)
	}
	c, err := queryCounts(ctx, db, st.tenantID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "count: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("tenant_id=%s\n", st.tenantID)
	fmt.Printf("products=%d suppliers=%d customers=%d warehouses=%d stock_snapshot=%d stock_movement=%d product_category=%d unit_def_custom=%d partners=%d\n",
		c.products, c.suppliers, c.customers, c.warehouses, c.stockSnap, c.stockMov, c.categories, c.units, c.partners)
}

func queryCounts(ctx context.Context, db *sql.DB, tenant uuid.UUID) (*counts, error) {
	c := &counts{}
	t := tenant.String()
	scan := func(q string, dst *int) error {
		return db.QueryRowContext(ctx, q, t).Scan(dst)
	}
	if err := scan(`SELECT COUNT(*) FROM tally.product WHERE tenant_id = $1 AND deleted_at IS NULL`, &c.products); err != nil {
		return nil, err
	}
	if err := scan(`SELECT COUNT(*) FROM tally.supplier WHERE tenant_id = $1 AND deleted_at IS NULL`, &c.suppliers); err != nil {
		return nil, err
	}
	if err := scan(`SELECT COUNT(*) FROM tally.partner WHERE tenant_id = $1 AND partner_type = 'customer' AND deleted_at IS NULL`, &c.customers); err != nil {
		return nil, err
	}
	if err := scan(`SELECT COUNT(*) FROM tally.partner WHERE tenant_id = $1 AND deleted_at IS NULL`, &c.partners); err != nil {
		return nil, err
	}
	if err := scan(`SELECT COUNT(*) FROM tally.warehouse WHERE tenant_id = $1 AND deleted_at IS NULL`, &c.warehouses); err != nil {
		return nil, err
	}
	if err := scan(`SELECT COUNT(*) FROM tally.stock_snapshot WHERE tenant_id = $1`, &c.stockSnap); err != nil {
		return nil, err
	}
	if err := scan(`SELECT COUNT(*) FROM tally.stock_movement WHERE tenant_id = $1`, &c.stockMov); err != nil {
		return nil, err
	}
	if err := scan(`SELECT COUNT(*) FROM tally.product_category WHERE tenant_id = $1 AND deleted_at IS NULL`, &c.categories); err != nil {
		return nil, err
	}
	if err := scan(`SELECT COUNT(*) FROM tally.unit_def WHERE tenant_id = $1 AND is_system = false`, &c.units); err != nil {
		return nil, err
	}
	return c, nil
}