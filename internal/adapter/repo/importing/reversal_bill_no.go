package importing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
)

// ReversalBillNo returns the bill_no of billID, empty when the bill is gone.
func (r *Repo) ReversalBillNo(ctx context.Context, tenantID, billID uuid.UUID) (string, error) {
	const q = `SELECT bill_no FROM tally.bill_head WHERE id = $1 AND tenant_id = $2`
	var no string
	err := dbscope.From(ctx, r.db).QueryRowContext(ctx, q, billID, tenantID).Scan(&no)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("import repo: reversal bill no: %w", err)
	}
	return no, nil
}
