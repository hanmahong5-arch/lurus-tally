// Package product implements the product.Repository interface using PostgreSQL via pgx/v5.
// All queries operate within the tally schema and rely on RLS being active
// (app.tenant_id must be set in the connection/transaction before calling these methods).
package product

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
)

// ErrNotFound is returned when a product does not exist or is invisible to the current tenant.
var ErrNotFound = errors.New("product not found")

// DB abstracts the minimal database/sql surface needed by this repo.
// Both *sql.DB and *sql.Tx satisfy this interface.
type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Repo implements the product repository.
type Repo struct {
	db DB
}

// New creates a Repo backed by db.
func New(db DB) *Repo {
	return &Repo{db: db}
}

// Create inserts a new product row.
func (r *Repo) Create(ctx context.Context, p *domain.Product) error {
	const q = `
		INSERT INTO tally.product
			(id, tenant_id, category_id, code, name, manufacturer, model, spec, brand,
			 mnemonic, color, expiry_days, weight_kg, enabled, enable_serial_no, enable_lot_no,
			 shelf_position, img_urls, remark, measurement_strategy, default_unit_id, attributes,
			 created_at, updated_at)
		VALUES
			($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)`

	_, err := r.db.ExecContext(ctx, q,
		p.ID, p.TenantID, p.CategoryID, p.Code, p.Name,
		p.Manufacturer, p.Model, p.Spec, p.Brand,
		p.Mnemonic, p.Color, p.ExpiryDays, p.WeightKg,
		p.Enabled, p.EnableSerialNo, p.EnableLotNo,
		p.ShelfPosition, sliceToArray(p.ImgURLs), p.Remark,
		string(p.MeasurementStrategy), p.DefaultUnitID,
		string(p.Attributes),
		p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("product repo create: %w", err)
	}
	return nil
}

// GetByID retrieves one product by primary key within the tenant scope.
func (r *Repo) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*domain.Product, error) {
	const q = `
		SELECT id, tenant_id, category_id, code, name, manufacturer, model, spec, brand,
		       mnemonic, color, expiry_days, weight_kg, enabled, enable_serial_no, enable_lot_no,
		       shelf_position, img_urls, remark, measurement_strategy, default_unit_id, attributes,
		       created_at, updated_at
		FROM tally.product
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL`

	row := r.db.QueryRowContext(ctx, q, id, tenantID)
	p, err := scanProduct(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("product repo get: %w", err)
	}
	return p, nil
}

// List returns a paginated, filtered slice of products.
func (r *Repo) List(ctx context.Context, f domain.ListFilter) ([]*domain.Product, int, error) {
	var where []string
	var args []any
	idx := 1

	where = append(where, fmt.Sprintf("tenant_id = $%d AND deleted_at IS NULL", idx))
	args = append(args, f.TenantID)
	idx++

	if f.Query != "" {
		where = append(where, fmt.Sprintf(
			"(name ILIKE $%d OR code ILIKE $%d OR mnemonic ILIKE $%d)",
			idx, idx, idx,
		))
		args = append(args, "%"+f.Query+"%")
		idx++
	}

	if f.Enabled != nil {
		where = append(where, fmt.Sprintf("enabled = $%d", idx))
		args = append(args, *f.Enabled)
		idx++
	}

	if len(f.AttributesFilter) > 0 && string(f.AttributesFilter) != "{}" {
		where = append(where, fmt.Sprintf("attributes @> $%d::jsonb", idx))
		args = append(args, string(f.AttributesFilter))
		idx++
	}

	base := "FROM tally.product WHERE " + strings.Join(where, " AND ")

	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) "+base, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("product repo list count: %w", err)
	}

	selectSQL := `SELECT id, tenant_id, category_id, code, name, manufacturer, model, spec, brand,
		       mnemonic, color, expiry_days, weight_kg, enabled, enable_serial_no, enable_lot_no,
		       shelf_position, img_urls, remark, measurement_strategy, default_unit_id, attributes,
		       created_at, updated_at ` + base +
		fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, f.Limit, f.Offset)

	rows, err := r.db.QueryContext(ctx, selectSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("product repo list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var products []*domain.Product
	for rows.Next() {
		p, err := scanProductRow(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("product repo list scan: %w", err)
		}
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("product repo list rows: %w", err)
	}
	return products, total, nil
}

// Update persists changes to an existing product row.
func (r *Repo) Update(ctx context.Context, p *domain.Product) error {
	const q = `
		UPDATE tally.product SET
			category_id=$1, name=$2, manufacturer=$3, model=$4, spec=$5, brand=$6,
			mnemonic=$7, color=$8, expiry_days=$9, weight_kg=$10, enabled=$11,
			enable_serial_no=$12, enable_lot_no=$13, shelf_position=$14, img_urls=$15,
			remark=$16, measurement_strategy=$17, default_unit_id=$18, attributes=$19,
			updated_at=$20
		WHERE id=$21 AND tenant_id=$22 AND deleted_at IS NULL`

	res, err := r.db.ExecContext(ctx, q,
		p.CategoryID, p.Name, p.Manufacturer, p.Model, p.Spec, p.Brand,
		p.Mnemonic, p.Color, p.ExpiryDays, p.WeightKg, p.Enabled,
		p.EnableSerialNo, p.EnableLotNo, p.ShelfPosition, sliceToArray(p.ImgURLs),
		p.Remark, string(p.MeasurementStrategy), p.DefaultUnitID, string(p.Attributes),
		p.UpdatedAt,
		p.ID, p.TenantID,
	)
	if err != nil {
		return fmt.Errorf("product repo update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("product repo update rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete soft-deletes a product by setting deleted_at = now().
func (r *Repo) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	const q = `UPDATE tally.product SET deleted_at = $1 WHERE id = $2 AND tenant_id = $3 AND deleted_at IS NULL`
	res, err := r.db.ExecContext(ctx, q, time.Now().UTC(), id, tenantID)
	if err != nil {
		return fmt.Errorf("product repo delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("product repo delete rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// rowScanner abstracts *sql.Row and *sql.Rows for reuse in scanProduct/scanProductRow.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanProduct(s rowScanner) (*domain.Product, error) {
	return scanProductCommon(s)
}

func scanProductRow(s rowScanner) (*domain.Product, error) {
	return scanProductCommon(s)
}

func scanProductCommon(s rowScanner) (*domain.Product, error) {
	var p domain.Product
	var (
		categoryID    *uuid.UUID
		weightKg      *string
		expiryDays    *int
		imgURLsRaw    *string
		measureStrat  string
		defaultUnitID *uuid.UUID
		attrsRaw      string
	)

	err := s.Scan(
		&p.ID, &p.TenantID, &categoryID, &p.Code, &p.Name,
		&p.Manufacturer, &p.Model, &p.Spec, &p.Brand,
		&p.Mnemonic, &p.Color, &expiryDays, &weightKg,
		&p.Enabled, &p.EnableSerialNo, &p.EnableLotNo,
		&p.ShelfPosition, &imgURLsRaw, &p.Remark,
		&measureStrat, &defaultUnitID, &attrsRaw,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	p.CategoryID = categoryID
	p.WeightKg = weightKg
	p.ExpiryDays = expiryDays
	p.DefaultUnitID = defaultUnitID
	p.MeasurementStrategy = domain.MeasurementStrategy(measureStrat)
	p.Attributes = json.RawMessage(attrsRaw)

	if imgURLsRaw != nil {
		// PostgreSQL TEXT[] comes back as a string like "{url1,url2}".
		raw := strings.Trim(*imgURLsRaw, "{}")
		if raw != "" {
			p.ImgURLs = strings.Split(raw, ",")
		}
	}
	return &p, nil
}

// sliceToArray converts a Go string slice to a PostgreSQL TEXT[] literal.
func sliceToArray(s []string) *string {
	if len(s) == 0 {
		return nil
	}
	out := "{" + strings.Join(s, ",") + "}"
	return &out
}
