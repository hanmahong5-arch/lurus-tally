// Package customer implements the customer repository using PostgreSQL.
// Customers live in tally.partner (partner_type='customer'); there is no
// separate customer table. All queries operate within the tally schema and rely
// on RLS being active.
package customer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	appcust "github.com/hanmahong5-arch/lurus-tally/internal/app/customer"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/customer"
)

// partnerTypesIn is the SQL IN-list of partner kinds a customer read surfaces.
// Built once from the domain constants (never from input), so it is safe to
// splice into the query text.
var partnerTypesIn = "('" + strings.Join(domain.ListPartnerTypes, "','") + "')"

// Repo implements the customer repository. It retains the shared *sql.DB pool as
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
var _ appcust.Repository = (*Repo)(nil)

// Create inserts a new tally.partner row with partner_type='customer'.
//
// The per-tenant code uniqueness index (idx_partner_code) spans every partner
// kind, so a code already used by another customer OR a supplier mirror is a
// unique violation. The driver error is wrapped with %w (not translated) so the
// handler's httperr classification renders it as 409.
func (r *Repo) Create(ctx context.Context, c *domain.Customer) error {
	const q = `
		INSERT INTO tally.partner
			(id, tenant_id, partner_type, name, code, contact_name, mobile, phone, address, remark, created_at, updated_at)
		VALUES
			($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`

	db := dbscope.From(ctx, r.db)
	_, err := db.ExecContext(ctx, q,
		c.ID, c.TenantID, domain.PartnerType,
		c.Name, nullableString(c.Code),
		nullableString(c.ContactName), nullableString(c.Mobile), nullableString(c.Phone),
		nullableString(c.Address), nullableString(c.Remark),
		c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("customer repo create: %w", err)
	}
	return nil
}

// GetByID retrieves one customer visible to tenantID. Supplier-only partner rows
// (e.g. supplier mirrors) are not customers and read as not found.
func (r *Repo) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*domain.Customer, error) {
	q := `
		SELECT id, tenant_id, code, name, contact_name, mobile, phone, address, remark, created_at, updated_at, deleted_at
		FROM tally.partner
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		  AND partner_type IN ` + partnerTypesIn

	db := dbscope.From(ctx, r.db)
	row := db.QueryRowContext(ctx, q, id, tenantID)
	c, err := scanCustomer(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appcust.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("customer repo get: %w", err)
	}
	return c, nil
}

// List returns a paginated, filtered slice of customers visible to the tenant,
// most recently updated first.
func (r *Repo) List(ctx context.Context, f domain.ListFilter) ([]*domain.Customer, int, error) {
	var where []string
	var args []any
	idx := 1

	where = append(where, fmt.Sprintf("tenant_id = $%d AND deleted_at IS NULL AND partner_type IN %s", idx, partnerTypesIn))
	args = append(args, f.TenantID)
	idx++

	if f.Query != "" {
		where = append(where, fmt.Sprintf("(name ILIKE $%d OR code ILIKE $%d OR mobile ILIKE $%d OR phone ILIKE $%d)", idx, idx, idx, idx))
		args = append(args, "%"+f.Query+"%")
		idx++
	}

	base := "FROM tally.partner WHERE " + strings.Join(where, " AND ")

	db := dbscope.From(ctx, r.db)

	var total int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) "+base, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("customer repo list count: %w", err)
	}

	// id breaks updated_at ties so offset pagination is stable.
	selectSQL := `SELECT id, tenant_id, code, name, contact_name, mobile, phone, address, remark, created_at, updated_at, deleted_at ` +
		base + fmt.Sprintf(" ORDER BY updated_at DESC, id DESC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, f.Limit, f.Offset)

	rows, err := db.QueryContext(ctx, selectSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("customer repo list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []*domain.Customer
	for rows.Next() {
		c, err := scanCustomer(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("customer repo list scan: %w", err)
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("customer repo list rows: %w", err)
	}
	return items, total, nil
}

// FindActiveByName matches on lower(trimmed, whitespace-collapsed name); the
// caller passes the name already normalised the same way.
func (r *Repo) FindActiveByName(ctx context.Context, tenantID uuid.UUID, name string) (*domain.Customer, error) {
	return r.findOne(ctx, `lower(btrim(regexp_replace(name, '[[:space:]]+', ' ', 'g'))) = lower($2)`, tenantID, name)
}

// FindActiveByMobile matches the trimmed mobile column exactly (legacy rows may carry stray spaces).
func (r *Repo) FindActiveByMobile(ctx context.Context, tenantID uuid.UUID, mobile string) (*domain.Customer, error) {
	return r.findOne(ctx, "btrim(mobile) = $2", tenantID, mobile)
}

func (r *Repo) findOne(ctx context.Context, cond string, tenantID uuid.UUID, arg string) (*domain.Customer, error) {
	q := `
		SELECT id, tenant_id, code, name, contact_name, mobile, phone, address, remark, created_at, updated_at, deleted_at
		FROM tally.partner
		WHERE tenant_id = $1 AND deleted_at IS NULL AND partner_type IN ` + partnerTypesIn + `
		  AND ` + cond + `
		ORDER BY created_at, id LIMIT 1`

	db := dbscope.From(ctx, r.db)
	c, err := scanCustomer(db.QueryRowContext(ctx, q, tenantID, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appcust.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("customer repo find: %w", err)
	}
	return c, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCustomer(s rowScanner) (*domain.Customer, error) {
	var c domain.Customer
	var (
		code        sql.NullString
		contactName sql.NullString
		mobile      sql.NullString
		phone       sql.NullString
		address     sql.NullString
		remark      sql.NullString
		deletedAt   *time.Time
	)

	err := s.Scan(
		&c.ID, &c.TenantID,
		&code, &c.Name,
		&contactName, &mobile, &phone, &address, &remark,
		&c.CreatedAt, &c.UpdatedAt, &deletedAt,
	)
	if err != nil {
		return nil, err
	}

	c.Code = code.String
	c.ContactName = contactName.String
	c.Mobile = mobile.String
	c.Phone = phone.String
	c.Address = address.String
	c.Remark = remark.String
	c.DeletedAt = deletedAt

	return &c, nil
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
