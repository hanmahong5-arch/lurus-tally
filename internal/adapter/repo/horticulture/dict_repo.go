// Package horticulture implements the nursery dictionary repository using PostgreSQL.
// All queries operate within the tally schema and rely on RLS being active.
package horticulture

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	apphort "github.com/hanmahong5-arch/lurus-tally/internal/app/horticulture"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/horticulture"
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

// Repo implements the nursery dictionary repository.
type Repo struct {
	db DB
}

// New creates a Repo backed by db.
func New(db DB) *Repo {
	return &Repo{db: db}
}

// Create inserts a new nursery_dict row.
// Returns apphort.ErrDuplicateName on unique constraint violation.
func (r *Repo) Create(ctx context.Context, d *domain.NurseryDict) error {
	const q = `
		INSERT INTO tally.nursery_dict
			(id, tenant_id, name, latin_name, family, genus, type, is_evergreen,
			 climate_zones, best_season, spec_template, default_unit_id,
			 photo_url, remark, created_at, updated_at)
		VALUES
			($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`

	_, err := r.db.ExecContext(ctx, q,
		d.ID, d.TenantID, d.Name, d.LatinName, d.Family, d.Genus,
		string(d.Type), d.IsEvergreen,
		sliceToArray(d.ClimateZones), intSliceToArray(d.BestSeason[:]),
		string(d.SpecTemplate), d.DefaultUnitID,
		nullString(d.PhotoURL), nullString(d.Remark),
		d.CreatedAt, d.UpdatedAt,
	)
	if err != nil {
		if isPgUniqueViolation(err) {
			return apphort.ErrDuplicateName
		}
		return fmt.Errorf("nursery dict repo create: %w", err)
	}
	return nil
}

// GetByID retrieves one entry visible to tenantID (own rows + seed rows).
func (r *Repo) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*domain.NurseryDict, error) {
	const q = `
		SELECT id, tenant_id, name, latin_name, family, genus, type, is_evergreen,
		       climate_zones, best_season, spec_template, default_unit_id,
		       photo_url, remark, created_at, updated_at, deleted_at
		FROM tally.nursery_dict
		WHERE id = $1
		  AND (tenant_id = $2 OR tenant_id = '00000000-0000-0000-0000-000000000000'::uuid)
		  AND deleted_at IS NULL`

	row := r.db.QueryRowContext(ctx, q, id, tenantID)
	d, err := scanDict(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apphort.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("nursery dict repo get: %w", err)
	}
	return d, nil
}

// List returns a paginated, filtered slice of entries visible to the tenant.
func (r *Repo) List(ctx context.Context, f domain.ListFilter) ([]*domain.NurseryDict, int, error) {
	var where []string
	var args []any
	idx := 1

	where = append(where,
		fmt.Sprintf("(tenant_id = $%d OR tenant_id = '00000000-0000-0000-0000-000000000000'::uuid) AND deleted_at IS NULL", idx))
	args = append(args, f.TenantID)
	idx++

	if f.Query != "" {
		where = append(where, fmt.Sprintf("name ILIKE $%d", idx))
		args = append(args, "%"+f.Query+"%")
		idx++
	}
	if f.Type != nil {
		where = append(where, fmt.Sprintf("type = $%d", idx))
		args = append(args, string(*f.Type))
		idx++
	}
	if f.IsEvergreen != nil {
		where = append(where, fmt.Sprintf("is_evergreen = $%d", idx))
		args = append(args, *f.IsEvergreen)
		idx++
	}

	base := "FROM tally.nursery_dict WHERE " + strings.Join(where, " AND ")

	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) "+base, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("nursery dict repo list count: %w", err)
	}

	selectSQL := `SELECT id, tenant_id, name, latin_name, family, genus, type, is_evergreen,
		       climate_zones, best_season, spec_template, default_unit_id,
		       photo_url, remark, created_at, updated_at, deleted_at ` + base +
		fmt.Sprintf(" ORDER BY name ASC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, f.Limit, f.Offset)

	rows, err := r.db.QueryContext(ctx, selectSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("nursery dict repo list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []*domain.NurseryDict
	for rows.Next() {
		d, err := scanDictRow(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("nursery dict repo list scan: %w", err)
		}
		items = append(items, d)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("nursery dict repo list rows: %w", err)
	}
	return items, total, nil
}

// Update persists changes to an existing entry.
func (r *Repo) Update(ctx context.Context, d *domain.NurseryDict) error {
	const q = `
		UPDATE tally.nursery_dict SET
			name=$1, latin_name=$2, family=$3, genus=$4, type=$5, is_evergreen=$6,
			climate_zones=$7, best_season=$8, spec_template=$9, default_unit_id=$10,
			photo_url=$11, remark=$12, updated_at=$13
		WHERE id=$14 AND tenant_id=$15 AND deleted_at IS NULL`

	res, err := r.db.ExecContext(ctx, q,
		d.Name, d.LatinName, d.Family, d.Genus, string(d.Type), d.IsEvergreen,
		sliceToArray(d.ClimateZones), intSliceToArray(d.BestSeason[:]),
		string(d.SpecTemplate), d.DefaultUnitID,
		nullString(d.PhotoURL), nullString(d.Remark),
		d.UpdatedAt,
		d.ID, d.TenantID,
	)
	if err != nil {
		return fmt.Errorf("nursery dict repo update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("nursery dict repo update rows affected: %w", err)
	}
	if n == 0 {
		return apphort.ErrNotFound
	}
	return nil
}

// Delete soft-deletes an entry by setting deleted_at = now().
func (r *Repo) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	const q = `UPDATE tally.nursery_dict SET deleted_at = $1 WHERE id = $2 AND tenant_id = $3 AND deleted_at IS NULL`
	res, err := r.db.ExecContext(ctx, q, time.Now().UTC(), id, tenantID)
	if err != nil {
		return fmt.Errorf("nursery dict repo delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("nursery dict repo delete rows affected: %w", err)
	}
	if n == 0 {
		return apphort.ErrNotFound
	}
	return nil
}

// Restore clears deleted_at on a soft-deleted entry.
func (r *Repo) Restore(ctx context.Context, tenantID, id uuid.UUID) (*domain.NurseryDict, error) {
	now := time.Now().UTC()
	const updateQ = `
		UPDATE tally.nursery_dict
		SET deleted_at = NULL, updated_at = $1
		WHERE id = $2 AND tenant_id = $3 AND deleted_at IS NOT NULL`

	res, err := r.db.ExecContext(ctx, updateQ, now, id, tenantID)
	if err != nil {
		return nil, fmt.Errorf("nursery dict repo restore: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("nursery dict repo restore rows affected: %w", err)
	}
	if n == 0 {
		return nil, apphort.ErrNotFound
	}
	return r.GetByID(ctx, tenantID, id)
}

// rowScanner abstracts *sql.Row and *sql.Rows for scanDict/scanDictRow.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanDict(s rowScanner) (*domain.NurseryDict, error) {
	return scanDictCommon(s)
}

func scanDictRow(s rowScanner) (*domain.NurseryDict, error) {
	return scanDictCommon(s)
}

func scanDictCommon(s rowScanner) (*domain.NurseryDict, error) {
	var d domain.NurseryDict
	var (
		latinName     sql.NullString
		family        sql.NullString
		genus         sql.NullString
		nurseryType   string
		climateRaw    *string
		bestSeasonRaw *string
		specRaw       string
		defaultUnitID *uuid.UUID
		photoURL      sql.NullString
		remark        sql.NullString
		deletedAt     *time.Time
	)

	err := s.Scan(
		&d.ID, &d.TenantID, &d.Name, &latinName, &family, &genus,
		&nurseryType, &d.IsEvergreen,
		&climateRaw, &bestSeasonRaw, &specRaw, &defaultUnitID,
		&photoURL, &remark,
		&d.CreatedAt, &d.UpdatedAt, &deletedAt,
	)
	if err != nil {
		return nil, err
	}

	d.LatinName = latinName.String
	d.Family = family.String
	d.Genus = genus.String
	d.Type = domain.NurseryType(nurseryType)
	d.DefaultUnitID = defaultUnitID
	d.PhotoURL = photoURL.String
	d.Remark = remark.String
	d.DeletedAt = deletedAt
	d.SpecTemplate = json.RawMessage(specRaw)

	// Parse PostgreSQL TEXT[] like "{华东,华北}" into []string.
	if climateRaw != nil {
		raw := strings.Trim(*climateRaw, "{}")
		if raw != "" {
			d.ClimateZones = strings.Split(raw, ",")
		}
	}
	if d.ClimateZones == nil {
		d.ClimateZones = []string{}
	}

	// Parse PostgreSQL INT[] like "{3,5}" into [2]int.
	if bestSeasonRaw != nil {
		raw := strings.Trim(*bestSeasonRaw, "{}")
		if raw != "" {
			parts := strings.SplitN(raw, ",", 2)
			if len(parts) == 2 {
				var start, end int
				_, _ = fmt.Sscanf(parts[0], "%d", &start)
				_, _ = fmt.Sscanf(parts[1], "%d", &end)
				d.BestSeason = [2]int{start, end}
			}
		}
	}

	return &d, nil
}

// sliceToArray converts a Go string slice to a PostgreSQL TEXT[] literal.
func sliceToArray(s []string) *string {
	if len(s) == 0 {
		empty := "{}"
		return &empty
	}
	out := "{" + strings.Join(s, ",") + "}"
	return &out
}

// intSliceToArray converts a Go int slice to a PostgreSQL INT[] literal.
// [0, 0] (unset sentinel) is stored as empty array '{}' per spec.
func intSliceToArray(s []int) *string {
	empty := "{}"
	if len(s) == 0 {
		return &empty
	}
	// Check if all zeros (unset)
	allZero := true
	for _, v := range s {
		if v != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return &empty
	}
	parts := make([]string, len(s))
	for i, v := range s {
		parts[i] = fmt.Sprintf("%d", v)
	}
	out := "{" + strings.Join(parts, ",") + "}"
	return &out
}

// nullString returns nil if s is empty, otherwise returns a pointer to s.
func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// isPgUniqueViolation reports whether the error is a PostgreSQL unique_violation (23505).
func isPgUniqueViolation(err error) bool {
	// pgx v5 wraps errors; check by message substring as a fallback.
	// The lib/pq driver exposes Code field.
	type pgErr interface {
		SQLState() string
	}
	var pe pgErr
	if errors.As(err, &pe) {
		return pe.SQLState() == pgUniqueViolation
	}
	// Check error string as last resort.
	return strings.Contains(err.Error(), pgUniqueViolation) ||
		strings.Contains(err.Error(), "unique constraint") ||
		strings.Contains(err.Error(), "duplicate key")
}
