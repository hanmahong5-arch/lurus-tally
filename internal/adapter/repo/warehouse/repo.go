// Package warehouse implements the warehouse repository using PostgreSQL.
// All queries operate within the tally schema and rely on RLS being active.
// tally.warehouse was created in 000006; migration 000033 adds updated_at, code, manager.
package warehouse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/app/warehouse"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/warehouse"
)

const pgUniqueViolation = "23505"

// DB abstracts the minimal database/sql surface needed by this repo.
type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Repo implements the warehouse repository.
type Repo struct {
	db DB
}

// New creates a Repo backed by db.
func New(db DB) *Repo {
	return &Repo{db: db}
}

// Ensure Repo satisfies the interface at compile time.
var _ appwarehouse.Repository = (*Repo)(nil)

// Create inserts a new warehouse row.
func (r *Repo) Create(ctx context.Context, w *domain.Warehouse) error {
	const q = `
		INSERT INTO tally.warehouse
			(id, tenant_id, code, name, address, manager, is_default, remark, created_at, updated_at)
		VALUES
			($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`

	_, err := r.db.ExecContext(ctx, q,
		w.ID, w.TenantID,
		nullableString(w.Code), w.Name,
		nullableString(w.Address), nullableString(w.Manager),
		w.IsDefault, nullableString(w.Remark),
		w.CreatedAt, w.UpdatedAt,
	)
	if err != nil {
		if isPgUniqueViolation(err) {
			return appwarehouse.ErrDuplicateName
		}
		return fmt.Errorf("warehouse repo create: %w", err)
	}
	return nil
}

// GetByID retrieves one warehouse visible to tenantID.
func (r *Repo) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*domain.Warehouse, error) {
	const q = `
		SELECT id, tenant_id, code, name, address, manager, is_default, remark, created_at, updated_at, deleted_at
		FROM tally.warehouse
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL`

	row := r.db.QueryRowContext(ctx, q, id, tenantID)
	w, err := scanWarehouse(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appwarehouse.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("warehouse repo get: %w", err)
	}
	return w, nil
}

// List returns a paginated, filtered slice of warehouses visible to the tenant.
func (r *Repo) List(ctx context.Context, f domain.ListFilter) ([]*domain.Warehouse, int, error) {
	var where []string
	var args []any
	idx := 1

	where = append(where, fmt.Sprintf("tenant_id = $%d AND deleted_at IS NULL", idx))
	args = append(args, f.TenantID)
	idx++

	if f.Query != "" {
		where = append(where, fmt.Sprintf("(name ILIKE $%d OR code ILIKE $%d)", idx, idx))
		args = append(args, "%"+f.Query+"%")
		idx++
	}

	base := "FROM tally.warehouse WHERE " + strings.Join(where, " AND ")

	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) "+base, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("warehouse repo list count: %w", err)
	}

	selectSQL := `SELECT id, tenant_id, code, name, address, manager, is_default, remark, created_at, updated_at, deleted_at ` +
		base + fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, f.Limit, f.Offset)

	rows, err := r.db.QueryContext(ctx, selectSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("warehouse repo list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []*domain.Warehouse
	for rows.Next() {
		w, err := scanWarehouseRow(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("warehouse repo list scan: %w", err)
		}
		items = append(items, w)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("warehouse repo list rows: %w", err)
	}
	return items, total, nil
}

// Update persists changes to an existing warehouse.
func (r *Repo) Update(ctx context.Context, w *domain.Warehouse) error {
	const q = `
		UPDATE tally.warehouse SET
			code=$1, name=$2, address=$3, manager=$4, is_default=$5, remark=$6, updated_at=$7
		WHERE id=$8 AND tenant_id=$9 AND deleted_at IS NULL`

	res, err := r.db.ExecContext(ctx, q,
		nullableString(w.Code), w.Name,
		nullableString(w.Address), nullableString(w.Manager),
		w.IsDefault, nullableString(w.Remark),
		w.UpdatedAt,
		w.ID, w.TenantID,
	)
	if err != nil {
		return fmt.Errorf("warehouse repo update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("warehouse repo update rows affected: %w", err)
	}
	if n == 0 {
		return appwarehouse.ErrNotFound
	}
	return nil
}

// Delete soft-deletes a warehouse.
func (r *Repo) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	const q = `UPDATE tally.warehouse SET deleted_at = $1 WHERE id = $2 AND tenant_id = $3 AND deleted_at IS NULL`
	res, err := r.db.ExecContext(ctx, q, time.Now().UTC(), id, tenantID)
	if err != nil {
		return fmt.Errorf("warehouse repo delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("warehouse repo delete rows affected: %w", err)
	}
	if n == 0 {
		return appwarehouse.ErrNotFound
	}
	return nil
}

// Restore clears deleted_at on a soft-deleted warehouse and returns the restored entry.
func (r *Repo) Restore(ctx context.Context, tenantID, id uuid.UUID) (*domain.Warehouse, error) {
	now := time.Now().UTC()
	const updateQ = `
		UPDATE tally.warehouse
		SET deleted_at = NULL, updated_at = $1
		WHERE id = $2 AND tenant_id = $3 AND deleted_at IS NOT NULL`

	res, err := r.db.ExecContext(ctx, updateQ, now, id, tenantID)
	if err != nil {
		return nil, fmt.Errorf("warehouse repo restore: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("warehouse repo restore rows affected: %w", err)
	}
	if n == 0 {
		return nil, appwarehouse.ErrNotFound
	}
	return r.GetByID(ctx, tenantID, id)
}

// DefaultWarehouseID returns the tenant's default warehouse, falling back to the
// earliest-created warehouse when none is flagged default. Returns ErrNotFound
// when the tenant has no warehouse at all.
func (r *Repo) DefaultWarehouseID(ctx context.Context, tenantID uuid.UUID) (uuid.UUID, error) {
	const q = `
		SELECT id
		FROM tally.warehouse
		WHERE tenant_id = $1 AND deleted_at IS NULL
		ORDER BY is_default DESC, created_at ASC
		LIMIT 1`

	var id uuid.UUID
	err := r.db.QueryRowContext(ctx, q, tenantID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, appwarehouse.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("warehouse repo default: %w", err)
	}
	return id, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanWarehouse(s rowScanner) (*domain.Warehouse, error) {
	return scanWarehouseCommon(s)
}

func scanWarehouseRow(s rowScanner) (*domain.Warehouse, error) {
	return scanWarehouseCommon(s)
}

func scanWarehouseCommon(s rowScanner) (*domain.Warehouse, error) {
	var w domain.Warehouse
	var (
		code      sql.NullString
		address   sql.NullString
		manager   sql.NullString
		remark    sql.NullString
		deletedAt *time.Time
	)

	err := s.Scan(
		&w.ID, &w.TenantID,
		&code, &w.Name,
		&address, &manager,
		&w.IsDefault, &remark,
		&w.CreatedAt, &w.UpdatedAt, &deletedAt,
	)
	if err != nil {
		return nil, err
	}

	w.Code = code.String
	w.Address = address.String
	w.Manager = manager.String
	w.Remark = remark.String
	w.DeletedAt = deletedAt

	return &w, nil
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func isPgUniqueViolation(err error) bool {
	type pgErr interface {
		SQLState() string
	}
	var pe pgErr
	if errors.As(err, &pe) {
		return pe.SQLState() == pgUniqueViolation
	}
	return strings.Contains(err.Error(), pgUniqueViolation) ||
		strings.Contains(err.Error(), "unique constraint") ||
		strings.Contains(err.Error(), "duplicate key")
}
