package export

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
)

const stockRowLimit = 50_000

var stockHeader = []string{"商品编码", "商品名", "仓库", "在库", "单位成本"}

// StockExportUseCase streams stock_snapshot rows joined with product info as CSV.
type StockExportUseCase struct {
	db       *sql.DB
	log      *slog.Logger
	rowLimit int
}

// NewStockExportUseCase creates a StockExportUseCase. Pass WithRowLimit to
// override the default cap (stockRowLimit).
func NewStockExportUseCase(db *sql.DB, log *slog.Logger, opts ...Option) *StockExportUseCase {
	if log == nil {
		log = slog.Default()
	}
	o := resolve(stockRowLimit, opts)
	return &StockExportUseCase{db: db, log: log, rowLimit: o.rowLimit}
}

// Execute fetches stock snapshot rows for tenantID and writes CSV to w.
// Returns the number of data rows written (excluding the header).
func (uc *StockExportUseCase) Execute(ctx context.Context, tenantID uuid.UUID, w io.Writer) (int, error) {
	// Join product to get code + name. Left join so orphan snapshots still export.
	const q = `
		SELECT
			COALESCE(p.code, ''),
			COALESCE(p.name, ''),
			ss.warehouse_id::text,
			ss.on_hand_qty,
			ss.unit_cost
		FROM tally.stock_snapshot ss
		LEFT JOIN tally.product p ON p.id = ss.product_id AND p.tenant_id = ss.tenant_id
		WHERE ss.tenant_id = $1
		ORDER BY p.code ASC NULLS LAST
		LIMIT $2`

	rows, err := dbscope.From(ctx, uc.db).QueryContext(ctx, q, tenantID, uc.rowLimit+1)
	if err != nil {
		return 0, fmt.Errorf("export stock: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	cw := csv.NewWriter(w)
	if err := cw.Write(stockHeader); err != nil {
		return 0, fmt.Errorf("export stock: write header: %w", err)
	}

	count := 0
	for rows.Next() {
		var code, name, warehouseID, onHandQty, unitCost string
		if err := rows.Scan(&code, &name, &warehouseID, &onHandQty, &unitCost); err != nil {
			return count, fmt.Errorf("export stock: scan row: %w", err)
		}
		if count == uc.rowLimit {
			uc.log.Warn("export stock: result truncated at row limit",
				slog.String("tenant_id", tenantID.String()),
				slog.Int("limit", uc.rowLimit))
			if err := cw.Write([]string{"[截断]", fmt.Sprintf("数据超过 %d 行限制", uc.rowLimit), "", "", ""}); err != nil {
				return count, fmt.Errorf("export stock: write truncation note: %w", err)
			}
			break
		}
		if err := cw.Write([]string{code, name, warehouseID, onHandQty, unitCost}); err != nil {
			return count, fmt.Errorf("export stock: write row: %w", err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("export stock: rows error: %w", err)
	}
	cw.Flush()
	return count, cw.Error()
}
