// Package project implements the project repository using PostgreSQL.
// All queries operate within the tally schema and rely on RLS being active.
package project

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appproject "github.com/hanmahong5-arch/lurus-tally/internal/app/project"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/project"
)

// pgUniqueViolation is the PostgreSQL error code for unique constraint violations.
const pgUniqueViolation = "23505"

// DB abstracts the minimal database/sql surface needed by this repo.
// Both *sql.DB and *sql.Tx satisfy this interface.
type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Repo implements the project repository.
type Repo struct {
	db DB
}

// New creates a Repo backed by db.
func New(db DB) *Repo {
	return &Repo{db: db}
}

// Ensure Repo satisfies the interface at compile time.
var _ appproject.Repository = (*Repo)(nil)

// Create inserts a new project row.
// Returns appproject.ErrDuplicateCode on unique constraint violation.
func (r *Repo) Create(ctx context.Context, p *domain.Project) error {
	const q = `
		INSERT INTO tally.project
			(id, tenant_id, code, name, customer_id, contract_amount,
			 start_date, end_date, status, address, manager, remark,
			 created_at, updated_at)
		VALUES
			($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`

	_, err := r.db.ExecContext(ctx, q,
		p.ID, p.TenantID, p.Code, p.Name,
		p.CustomerID, p.ContractAmount,
		p.StartDate, p.EndDate,
		string(p.Status),
		nullableString(p.Address), nullableString(p.Manager), nullableString(p.Remark),
		p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		if isPgUniqueViolation(err) {
			return appproject.ErrDuplicateCode
		}
		return fmt.Errorf("project repo create: %w", err)
	}
	return nil
}

// GetByID retrieves one project visible to tenantID.
// Returns appproject.ErrNotFound if not found or soft-deleted.
func (r *Repo) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*domain.Project, error) {
	const q = `
		SELECT id, tenant_id, code, name, customer_id, contract_amount,
		       start_date, end_date, status, address, manager, remark,
		       created_at, updated_at, deleted_at
		FROM tally.project
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL`

	row := r.db.QueryRowContext(ctx, q, id, tenantID)
	p, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appproject.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("project repo get: %w", err)
	}
	return p, nil
}

// List returns a paginated, filtered slice of projects visible to the tenant.
func (r *Repo) List(ctx context.Context, f domain.ListFilter) ([]*domain.Project, int, error) {
	var where []string
	var args []any
	idx := 1

	where = append(where,
		fmt.Sprintf("tenant_id = $%d AND deleted_at IS NULL", idx))
	args = append(args, f.TenantID)
	idx++

	if f.Query != "" {
		where = append(where, fmt.Sprintf("(name ILIKE $%d OR code ILIKE $%d)", idx, idx))
		args = append(args, "%"+f.Query+"%")
		idx++
	}
	if f.Status != nil {
		where = append(where, fmt.Sprintf("status = $%d", idx))
		args = append(args, string(*f.Status))
		idx++
	}
	if f.CustomerID != nil {
		where = append(where, fmt.Sprintf("customer_id = $%d", idx))
		args = append(args, *f.CustomerID)
		idx++
	}

	base := "FROM tally.project WHERE " + strings.Join(where, " AND ")

	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) "+base, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("project repo list count: %w", err)
	}

	selectSQL := `SELECT id, tenant_id, code, name, customer_id, contract_amount,
		       start_date, end_date, status, address, manager, remark,
		       created_at, updated_at, deleted_at ` + base +
		fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, f.Limit, f.Offset)

	rows, err := r.db.QueryContext(ctx, selectSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("project repo list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []*domain.Project
	for rows.Next() {
		p, err := scanProjectRow(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("project repo list scan: %w", err)
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("project repo list rows: %w", err)
	}
	return items, total, nil
}

// Update persists changes to an existing project.
// Returns appproject.ErrNotFound if 0 rows were affected (not found or deleted).
func (r *Repo) Update(ctx context.Context, p *domain.Project) error {
	const q = `
		UPDATE tally.project SET
			code=$1, name=$2, customer_id=$3, contract_amount=$4,
			start_date=$5, end_date=$6, status=$7,
			address=$8, manager=$9, remark=$10, updated_at=$11
		WHERE id=$12 AND tenant_id=$13 AND deleted_at IS NULL`

	res, err := r.db.ExecContext(ctx, q,
		p.Code, p.Name, p.CustomerID, p.ContractAmount,
		p.StartDate, p.EndDate, string(p.Status),
		nullableString(p.Address), nullableString(p.Manager), nullableString(p.Remark),
		p.UpdatedAt,
		p.ID, p.TenantID,
	)
	if err != nil {
		return fmt.Errorf("project repo update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("project repo update rows affected: %w", err)
	}
	if n == 0 {
		return appproject.ErrNotFound
	}
	return nil
}

// Delete soft-deletes a project by setting deleted_at = now().
// Returns appproject.ErrNotFound if 0 rows were affected.
func (r *Repo) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	const q = `UPDATE tally.project SET deleted_at = $1 WHERE id = $2 AND tenant_id = $3 AND deleted_at IS NULL`
	res, err := r.db.ExecContext(ctx, q, time.Now().UTC(), id, tenantID)
	if err != nil {
		return fmt.Errorf("project repo delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("project repo delete rows affected: %w", err)
	}
	if n == 0 {
		return appproject.ErrNotFound
	}
	return nil
}

// Restore clears deleted_at on a soft-deleted project and returns the restored entry.
func (r *Repo) Restore(ctx context.Context, tenantID, id uuid.UUID) (*domain.Project, error) {
	now := time.Now().UTC()
	const updateQ = `
		UPDATE tally.project
		SET deleted_at = NULL, updated_at = $1
		WHERE id = $2 AND tenant_id = $3 AND deleted_at IS NOT NULL`

	res, err := r.db.ExecContext(ctx, updateQ, now, id, tenantID)
	if err != nil {
		return nil, fmt.Errorf("project repo restore: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("project repo restore rows affected: %w", err)
	}
	if n == 0 {
		return nil, appproject.ErrNotFound
	}
	return r.GetByID(ctx, tenantID, id)
}

// rowScanner abstracts *sql.Row and *sql.Rows for scanProject/scanProjectRow.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanProject(s rowScanner) (*domain.Project, error) {
	return scanProjectCommon(s)
}

func scanProjectRow(s rowScanner) (*domain.Project, error) {
	return scanProjectCommon(s)
}

func scanProjectCommon(s rowScanner) (*domain.Project, error) {
	var p domain.Project
	var (
		contractAmount sql.NullString
		startDate      *time.Time
		endDate        *time.Time
		statusStr      string
		address        sql.NullString
		manager        sql.NullString
		remark         sql.NullString
		deletedAt      *time.Time
	)

	err := s.Scan(
		&p.ID, &p.TenantID, &p.Code, &p.Name,
		&p.CustomerID, &contractAmount,
		&startDate, &endDate,
		&statusStr,
		&address, &manager, &remark,
		&p.CreatedAt, &p.UpdatedAt, &deletedAt,
	)
	if err != nil {
		return nil, err
	}

	if contractAmount.Valid {
		p.ContractAmount = &contractAmount.String
	}
	p.StartDate = startDate
	p.EndDate = endDate
	p.Status = domain.ProjectStatus(statusStr)
	p.Address = address.String
	p.Manager = manager.String
	p.Remark = remark.String
	p.DeletedAt = deletedAt

	return &p, nil
}

// nullableString returns nil *string for empty strings, pointer otherwise.
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// isPgUniqueViolation reports whether the error is a PostgreSQL unique_violation (23505).
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
