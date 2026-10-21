package stock

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
)

// ProductNames returns the names of the tenant's products, keyed by id.
func (r *Repo) ProductNames(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = id.String()
	}
	rows, err := dbscope.From(ctx, r.db).QueryContext(ctx,
		`SELECT id, name FROM tally.product WHERE tenant_id = $1 AND id = ANY($2::uuid[])`, tenantID, pq.Array(strs))
	if err != nil {
		return nil, fmt.Errorf("stock repo: product names: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("stock repo: scan product name: %w", err)
		}
		out[id] = name
	}
	return out, rows.Err()
}
