// Package usage counts a tenant's plan-limited objects (members, SKUs, warehouses).
package usage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

// 用量口径：成员 = user_identity_mapping 该租户行数；商品 = 未删除且启用；仓库 = 未删除。
const (
	usersSQL      = `SELECT count(*) FROM tally.user_identity_mapping WHERE tenant_id = $1`
	skusSQL       = `SELECT count(*) FROM tally.product WHERE tenant_id = $1 AND deleted_at IS NULL AND enabled = true`
	warehousesSQL = `SELECT count(*) FROM tally.warehouse WHERE tenant_id = $1 AND deleted_at IS NULL`
)

// Repo implements entitlement.UsageCounter.
type Repo struct{ db *sql.DB }

// New creates a Repo.
func New(db *sql.DB) *Repo { return &Repo{db: db} }

// count 在固定了 app.tenant_id 的独立只读事务里统计（业务表是 strict RLS，
// 且 Join 时调用方所在租户并非被统计的租户，不能复用请求连接）。
func (r *Repo) count(ctx context.Context, q string, tenantID uuid.UUID) (int64, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, fmt.Errorf("usage: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID.String()); err != nil {
		return 0, fmt.Errorf("usage: set rls context: %w", err)
	}
	var n int64
	if err := tx.QueryRowContext(ctx, q, tenantID).Scan(&n); err != nil {
		return 0, fmt.Errorf("usage: count: %w", err)
	}
	return n, nil
}

// CountUsers counts the tenant's member rows.
func (r *Repo) CountUsers(ctx context.Context, t uuid.UUID) (int64, error) {
	return r.count(ctx, usersSQL, t)
}

// CountSKUs counts the tenant's live, enabled products.
func (r *Repo) CountSKUs(ctx context.Context, t uuid.UUID) (int64, error) {
	return r.count(ctx, skusSQL, t)
}

// CountWarehouses counts the tenant's live warehouses.
func (r *Repo) CountWarehouses(ctx context.Context, t uuid.UUID) (int64, error) {
	return r.count(ctx, warehousesSQL, t)
}
