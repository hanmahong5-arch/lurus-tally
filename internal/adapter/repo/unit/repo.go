// Package unit implements the unit.Repository interface using PostgreSQL via pgx/v5.
// Both system units (is_system=true) and tenant-custom units are accessible here.
// RLS policy on unit_def allows rows where tenant_id = app.tenant_id OR is_system = true.
package unit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/unit"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
	"github.com/shopspring/decimal"
)

// ErrNotFound is returned when a unit_def row is not found.
var ErrNotFound = errors.New("unit not found")

// ErrSystemUnit is returned when a caller attempts to delete a system unit.
var ErrSystemUnit = errors.New("system unit cannot be deleted")

// Repo implements the unit repository. It retains the shared *sql.DB pool as the
// fallback handle; per-request queries prefer the tenant-pinned connection
// carried in context (see dbscope).
type Repo struct {
	db *sql.DB
}

// New creates a Repo backed by the shared connection pool.
func New(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// Create inserts a new tenant-custom unit_def.
func (r *Repo) Create(ctx context.Context, u *domain.UnitDef) error {
	const q = `
		INSERT INTO tally.unit_def (id, tenant_id, code, name, unit_type, is_system, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	dbh := dbscope.From(ctx, r.db)
	_, err := dbh.ExecContext(ctx, q,
		u.ID, u.TenantID, u.Code, u.Name, string(u.UnitType), u.IsSystem,
		u.CreatedAt, u.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("unit repo create: %w", err)
	}
	return nil
}

// List returns unit_defs visible to the tenant (system + tenant-custom).
// The RLS policy on unit_def handles the OR condition natively.
func (r *Repo) List(ctx context.Context, f domain.ListFilter) ([]*domain.UnitDef, error) {
	q := `SELECT id, tenant_id, code, name, unit_type, is_system, created_at, updated_at
		  FROM tally.unit_def`
	var args []any
	var where []string

	// When RLS is active, the policy already filters by tenant; we still pass tenant_id
	// explicitly so queries work under superuser sessions (e.g. during integration tests).
	where = append(where, "is_system = true OR tenant_id = $1")
	args = append(args, f.TenantID)

	if f.UnitType != "" {
		where = append(where, fmt.Sprintf("unit_type = $%d", len(args)+1))
		args = append(args, string(f.UnitType))
	}

	q += " WHERE " + joinAnd(where) + " ORDER BY is_system DESC, code ASC"

	dbh := dbscope.From(ctx, r.db)
	rows, err := dbh.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("unit repo list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var units []*domain.UnitDef
	for rows.Next() {
		u := &domain.UnitDef{}
		var unitType string
		if err := rows.Scan(&u.ID, &u.TenantID, &u.Code, &u.Name, &unitType, &u.IsSystem,
			&u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("unit repo list scan: %w", err)
		}
		u.UnitType = domain.UnitType(unitType)
		units = append(units, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("unit repo list rows: %w", err)
	}
	return units, nil
}

// GetByID retrieves a unit_def by primary key.
// Returns ErrNotFound if the row does not exist or is invisible under RLS.
func (r *Repo) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*domain.UnitDef, error) {
	const q = `SELECT id, tenant_id, code, name, unit_type, is_system, created_at, updated_at
		FROM tally.unit_def WHERE id = $1 AND (is_system = true OR tenant_id = $2)`

	u := &domain.UnitDef{}
	var unitType string
	dbh := dbscope.From(ctx, r.db)
	err := dbh.QueryRowContext(ctx, q, id, tenantID).
		Scan(&u.ID, &u.TenantID, &u.Code, &u.Name, &unitType, &u.IsSystem, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("unit repo get: %w", err)
	}
	u.UnitType = domain.UnitType(unitType)
	return u, nil
}

// GetConversionFactor returns the conversion_factor for the (product, unit) pair from product_unit.
// A product's base unit (product.default_unit_id / product.unit_id) needs no product_unit
// row and converts at 1. Any other unit without a row returns appbill.ErrInvalidUnitForProduct.
// This is a surgical extension for Story 6.1 (ApprovePurchaseUseCase dependency).
func (r *Repo) GetConversionFactor(ctx context.Context, productID, unitID uuid.UUID) (decimal.Decimal, error) {
	const q = `SELECT conversion_factor FROM tally.product_unit WHERE product_id = $1 AND unit_id = $2`

	var factor string
	dbh := dbscope.From(ctx, r.db)
	err := dbh.QueryRowContext(ctx, q, productID, unitID).Scan(&factor)
	if errors.Is(err, sql.ErrNoRows) {
		const baseQ = `SELECT EXISTS (
			SELECT 1 FROM tally.product
			WHERE id = $1 AND (default_unit_id = $2 OR unit_id = $2))`
		var isBase bool
		if err := dbh.QueryRowContext(ctx, baseQ, productID, unitID).Scan(&isBase); err != nil {
			return decimal.Zero, fmt.Errorf("unit repo: check base unit: %w", err)
		}
		if isBase {
			return decimal.NewFromInt(1), nil
		}
		return decimal.Zero, appbill.ErrInvalidUnitForProduct
	}
	if err != nil {
		return decimal.Zero, fmt.Errorf("unit repo: get conversion factor: %w", err)
	}
	// TODO: originally ignored parse error; behaviour preserved.
	f, _ := decimalutil.Parse(factor, "conversion_factor")
	return f, nil
}

// Delete removes a tenant-custom unit_def.
// The caller (use case) is responsible for checking is_system before calling this.
func (r *Repo) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	const q = `DELETE FROM tally.unit_def WHERE id = $1 AND tenant_id = $2 AND is_system = false`
	dbh := dbscope.From(ctx, r.db)
	res, err := dbh.ExecContext(ctx, q, id, tenantID)
	if err != nil {
		return fmt.Errorf("unit repo delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("unit repo delete rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func joinAnd(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += " AND "
		}
		result += "(" + p + ")"
	}
	return result
}

// ProductUnit is one unit a product can be billed in. Factor converts one of this unit
// into the product's base unit (the base unit itself is 1).
type ProductUnit struct {
	ID     uuid.UUID
	Code   string
	Name   string
	Factor decimal.Decimal
	IsBase bool
}

// ProductUnits returns the product's base unit first, then every unit with a configured
// conversion factor. Units without a product_unit row (and that are not the base unit)
// are left out because the bill approval would reject them. Returns ErrNotFound when the
// product does not exist for the tenant.
func (r *Repo) ProductUnits(ctx context.Context, tenantID, productID uuid.UUID) ([]ProductUnit, error) {
	dbh := dbscope.From(ctx, r.db)

	var baseA, baseB uuid.NullUUID
	err := dbh.QueryRowContext(ctx,
		`SELECT default_unit_id, unit_id FROM tally.product
		 WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL`, productID, tenantID).Scan(&baseA, &baseB)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("unit repo: product units: %w", err)
	}

	rows, err := dbh.QueryContext(ctx, `
		SELECT u.id, u.code, u.name, pu.conversion_factor::text, pu.is_base
		FROM tally.product_unit pu
		JOIN tally.unit_def u ON u.id = pu.unit_id
		WHERE pu.product_id = $1 AND pu.tenant_id = $2`, productID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("unit repo: product units: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var conv []ProductUnit
	for rows.Next() {
		var u ProductUnit
		var factor string
		if err := rows.Scan(&u.ID, &u.Code, &u.Name, &factor, &u.IsBase); err != nil {
			return nil, fmt.Errorf("unit repo: product units scan: %w", err)
		}
		if u.Factor, err = decimalutil.Parse(factor, "conversion_factor"); err != nil {
			return nil, fmt.Errorf("unit repo: product units scan: %w", err)
		}
		conv = append(conv, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("unit repo: product units rows: %w", err)
	}

	// Base unit: the product's own pointer wins, else the unit flagged is_base.
	baseID := uuid.Nil
	switch {
	case baseA.Valid:
		baseID = baseA.UUID
	case baseB.Valid:
		baseID = baseB.UUID
	default:
		for _, u := range conv {
			if u.IsBase {
				baseID = u.ID
				break
			}
		}
	}
	out := make([]ProductUnit, 0, len(conv)+1)
	if baseID != uuid.Nil {
		base := ProductUnit{ID: baseID, Factor: decimal.NewFromInt(1), IsBase: true}
		err := dbh.QueryRowContext(ctx,
			`SELECT code, name FROM tally.unit_def WHERE id = $1 AND (is_system = true OR tenant_id = $2)`,
			baseID, tenantID).Scan(&base.Code, &base.Name)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("unit repo: product units base: %w", err)
		}
		if err == nil {
			out = append(out, base)
		}
	}
	for _, u := range conv {
		if u.ID == baseID {
			continue
		}
		u.IsBase = false
		out = append(out, u)
	}
	return out, nil
}
