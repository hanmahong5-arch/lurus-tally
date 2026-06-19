// Package supplier implements the supplier repository using PostgreSQL.
// All queries operate within the tally schema and rely on RLS being active.
package supplier

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	appsupp "github.com/hanmahong5-arch/lurus-tally/internal/app/supplier"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/supplier"
)

const pgUniqueViolation = "23505"

// Repo implements the supplier repository. It retains the shared *sql.DB pool as
// the fallback handle; per-request queries prefer the tenant-pinned connection
// carried in context (see dbscope).
type Repo struct {
	db *sql.DB
}

// New creates a Repo backed by the shared connection pool.
func New(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// Ensure Repo satisfies the interface at compile time.
var _ appsupp.Repository = (*Repo)(nil)

// Create inserts a new supplier row AND co-creates a mirrored tally.partner
// row (partner_type='supplier') that shares the supplier's id. This makes a
// freshly created supplier immediately referenceable as a bill partner —
// bill_head.partner_id has a FK to tally.partner(id), so without the mirror a
// bill that points partner_id at a supplier would fail the FK (23503 → 409).
// Both inserts run in ONE statement via a CTE so they commit atomically: there
// is never a supplier without its partner mirror, and a uniqueness violation on
// either side rolls back the whole create.
func (r *Repo) Create(ctx context.Context, s *domain.Supplier) error {
	const q = `
		WITH ins_supplier AS (
			INSERT INTO tally.supplier
				(id, tenant_id, code, name, contact, phone, email, address, remark, created_at, updated_at)
			VALUES
				($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			RETURNING id, tenant_id, code, name, created_at, updated_at
		)
		INSERT INTO tally.partner
			(id, tenant_id, partner_type, name, code, created_at, updated_at)
		SELECT id, tenant_id, 'supplier', name, code, created_at, updated_at FROM ins_supplier`

	db := dbscope.From(ctx, r.db)
	_, err := db.ExecContext(ctx, q,
		s.ID, s.TenantID,
		nullableString(s.Code), s.Name,
		nullableString(s.Contact), nullableString(s.Phone), nullableString(s.Email),
		nullableString(s.Address), nullableString(s.Remark),
		s.CreatedAt, s.UpdatedAt,
	)
	if err != nil {
		if isPgUniqueViolation(err) {
			return appsupp.ErrDuplicateName
		}
		return fmt.Errorf("supplier repo create: %w", err)
	}
	return nil
}

// GetByID retrieves one supplier visible to tenantID.
func (r *Repo) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*domain.Supplier, error) {
	const q = `
		SELECT id, tenant_id, code, name, contact, phone, email, address, remark, created_at, updated_at, deleted_at
		FROM tally.supplier
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL`

	db := dbscope.From(ctx, r.db)
	row := db.QueryRowContext(ctx, q, id, tenantID)
	s, err := scanSupplier(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appsupp.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("supplier repo get: %w", err)
	}
	return s, nil
}

// List returns a paginated, filtered slice of suppliers visible to the tenant.
func (r *Repo) List(ctx context.Context, f domain.ListFilter) ([]*domain.Supplier, int, error) {
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

	base := "FROM tally.supplier WHERE " + strings.Join(where, " AND ")

	db := dbscope.From(ctx, r.db)

	var total int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) "+base, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("supplier repo list count: %w", err)
	}

	selectSQL := `SELECT id, tenant_id, code, name, contact, phone, email, address, remark, created_at, updated_at, deleted_at ` +
		base + fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, f.Limit, f.Offset)

	rows, err := db.QueryContext(ctx, selectSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("supplier repo list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []*domain.Supplier
	for rows.Next() {
		s, err := scanSupplierRow(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("supplier repo list scan: %w", err)
		}
		items = append(items, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("supplier repo list rows: %w", err)
	}
	return items, total, nil
}

// Update persists changes to an existing supplier.
func (r *Repo) Update(ctx context.Context, s *domain.Supplier) error {
	const q = `
		UPDATE tally.supplier SET
			code=$1, name=$2, contact=$3, phone=$4, email=$5, address=$6, remark=$7, updated_at=$8
		WHERE id=$9 AND tenant_id=$10 AND deleted_at IS NULL`

	db := dbscope.From(ctx, r.db)
	res, err := db.ExecContext(ctx, q,
		nullableString(s.Code), s.Name,
		nullableString(s.Contact), nullableString(s.Phone), nullableString(s.Email),
		nullableString(s.Address), nullableString(s.Remark),
		s.UpdatedAt,
		s.ID, s.TenantID,
	)
	if err != nil {
		return fmt.Errorf("supplier repo update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("supplier repo update rows affected: %w", err)
	}
	if n == 0 {
		return appsupp.ErrNotFound
	}
	return nil
}

// Delete soft-deletes a supplier.
func (r *Repo) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	const q = `UPDATE tally.supplier SET deleted_at = $1 WHERE id = $2 AND tenant_id = $3 AND deleted_at IS NULL`
	db := dbscope.From(ctx, r.db)
	res, err := db.ExecContext(ctx, q, time.Now().UTC(), id, tenantID)
	if err != nil {
		return fmt.Errorf("supplier repo delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("supplier repo delete rows affected: %w", err)
	}
	if n == 0 {
		return appsupp.ErrNotFound
	}
	return nil
}

// Restore clears deleted_at on a soft-deleted supplier and returns the restored entry.
func (r *Repo) Restore(ctx context.Context, tenantID, id uuid.UUID) (*domain.Supplier, error) {
	now := time.Now().UTC()
	const updateQ = `
		UPDATE tally.supplier
		SET deleted_at = NULL, updated_at = $1
		WHERE id = $2 AND tenant_id = $3 AND deleted_at IS NOT NULL`

	db := dbscope.From(ctx, r.db)
	res, err := db.ExecContext(ctx, updateQ, now, id, tenantID)
	if err != nil {
		return nil, fmt.Errorf("supplier repo restore: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("supplier repo restore rows affected: %w", err)
	}
	if n == 0 {
		return nil, appsupp.ErrNotFound
	}
	return r.GetByID(ctx, tenantID, id)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSupplier(s rowScanner) (*domain.Supplier, error) {
	return scanSupplierCommon(s)
}

func scanSupplierRow(s rowScanner) (*domain.Supplier, error) {
	return scanSupplierCommon(s)
}

func scanSupplierCommon(s rowScanner) (*domain.Supplier, error) {
	var sup domain.Supplier
	var (
		code      sql.NullString
		contact   sql.NullString
		phone     sql.NullString
		email     sql.NullString
		address   sql.NullString
		remark    sql.NullString
		deletedAt *time.Time
	)

	err := s.Scan(
		&sup.ID, &sup.TenantID,
		&code, &sup.Name,
		&contact, &phone, &email, &address, &remark,
		&sup.CreatedAt, &sup.UpdatedAt, &deletedAt,
	)
	if err != nil {
		return nil, err
	}

	sup.Code = code.String
	sup.Contact = contact.String
	sup.Phone = phone.String
	sup.Email = email.String
	sup.Address = address.String
	sup.Remark = remark.String
	sup.DeletedAt = deletedAt

	return &sup, nil
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
