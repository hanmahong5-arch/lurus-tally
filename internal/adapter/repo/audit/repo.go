// Package audit persists the shop operation log in tally.audit_log.
package audit

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	appaudit "github.com/hanmahong5-arch/lurus-tally/internal/app/audit"
)

// Repo is the PostgreSQL implementation of appaudit.Repo.
type Repo struct{ db *sql.DB }

// New wires the repo to db.
func New(db *sql.DB) *Repo { return &Repo{db: db} }

var _ appaudit.Repo = (*Repo)(nil)

// Append inserts one row.
func (r *Repo) Append(ctx context.Context, e appaudit.Entry) error {
	var uid, rid any
	if e.UserID != uuid.Nil {
		uid = e.UserID
	}
	if e.ResourceID != uuid.Nil {
		rid = e.ResourceID
	}
	var changes any
	if len(e.Changes) > 0 {
		changes = string(e.Changes)
	}
	_, err := dbscope.From(ctx, r.db).ExecContext(ctx, `
		INSERT INTO tally.audit_log (tenant_id, user_id, action, resource, resource_id, changes, client_ip)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)`,
		e.TenantID, uid, e.Action, e.Resource, rid, changes, e.ClientIP)
	if err != nil {
		return fmt.Errorf("audit repo: append: %w", err)
	}
	return nil
}

// List returns one page (newest first) and the total matching count. Every
// query is scoped to the tenant; the document number is joined from bill_head.
func (r *Repo) List(ctx context.Context, tenantID uuid.UUID, f appaudit.Filter) ([]appaudit.Row, int, error) {
	where := []string{"a.tenant_id = $1"}
	args := []any{tenantID}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.Replace(cond, "?", "$"+strconv.Itoa(len(args)), 1))
	}
	if f.Resource != "" {
		add("a.resource = ?", f.Resource)
	}
	if f.ResourceID != uuid.Nil {
		add("a.resource_id = ?", f.ResourceID)
	}
	if f.From != nil {
		add("a.created_at >= ?", *f.From)
	}
	if f.To != nil {
		add("a.created_at < ?", *f.To)
	}
	cond := strings.Join(where, " AND ")
	db := dbscope.From(ctx, r.db)

	var total int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tally.audit_log a WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("audit repo: count: %w", err)
	}

	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	args = append(args, limit, offset)
	rows, err := db.QueryContext(ctx, `
		SELECT a.id, a.user_id, a.action, a.resource, a.resource_id, COALESCE(b.bill_no, ''), a.changes, a.created_at
		FROM tally.audit_log a
		LEFT JOIN tally.bill_head b ON b.id = a.resource_id AND b.tenant_id = a.tenant_id
		WHERE `+cond+`
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("audit repo: list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []appaudit.Row
	for rows.Next() {
		var row appaudit.Row
		var uid, rid uuid.NullUUID
		var changes []byte
		if err := rows.Scan(&row.ID, &uid, &row.Action, &row.Resource, &rid, &row.ResourceNo, &changes, &row.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("audit repo: scan: %w", err)
		}
		row.UserID, row.ResourceID, row.Changes = uid.UUID, rid.UUID, changes
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("audit repo: rows: %w", err)
	}
	return out, total, nil
}
