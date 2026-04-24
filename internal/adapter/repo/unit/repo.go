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
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/unit"
)

// ErrNotFound is returned when a unit_def row is not found.
var ErrNotFound = errors.New("unit not found")

// ErrSystemUnit is returned when a caller attempts to delete a system unit.
var ErrSystemUnit = errors.New("system unit cannot be deleted")

// DB abstracts the minimal database/sql surface needed by this repo.
type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Repo implements the unit repository.
type Repo struct {
	db DB
}

// New creates a Repo backed by db.
func New(db DB) *Repo {
	return &Repo{db: db}
}

// Create inserts a new tenant-custom unit_def.
func (r *Repo) Create(ctx context.Context, u *domain.UnitDef) error {
	const q = `
		INSERT INTO tally.unit_def (id, tenant_id, code, name, unit_type, is_system, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err := r.db.ExecContext(ctx, q,
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

	rows, err := r.db.QueryContext(ctx, q, args...)
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
	err := r.db.QueryRowContext(ctx, q, id, tenantID).
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

// Delete removes a tenant-custom unit_def.
// The caller (use case) is responsible for checking is_system before calling this.
func (r *Repo) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	const q = `DELETE FROM tally.unit_def WHERE id = $1 AND tenant_id = $2 AND is_system = false`
	res, err := r.db.ExecContext(ctx, q, id, tenantID)
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
