package replenish

import (
	"context"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/notify"
)

// NotifyLines maps low-stock rows to the notify package's IM-alert shape. It is
// a straight field copy (both sides carry decimal quantities as strings), kept
// here rather than in package notify so notify stays free of any app-layer
// import. This is the seam that connects the real low-stock use case output to
// the WeCom/DingTalk/Feishu dispatcher.
func NotifyLines(rows []LowStockRow) []notify.LowStockLine {
	lines := make([]notify.LowStockLine, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, notify.LowStockLine{
			ProductName:  r.ProductName,
			ProductCode:  r.ProductCode,
			AvailableQty: r.AvailableQty,
			ReorderPoint: r.ReorderPoint,
			DaysOfSupply: r.DaysOfSupply,
		})
	}
	return lines
}

// AlertLowStock runs the low-stock use case for a tenant and dispatches the
// result to whatever IM notifiers the dispatcher was built with (a no-op when
// none are configured). It is fire-and-log by delegation: the dispatcher never
// returns an error, so a notify failure can never fail this call. The only
// error path is the underlying stock query; scope names the tenant in the IM
// message (e.g. the company name).
//
// This is the wiring entrypoint a scheduler or the bill-approval path calls to
// turn a real low-stock condition into an IM alert; it deliberately takes the
// dispatcher as a parameter so the hot lifecycle wiring file is not a
// dependency of this package.
func AlertLowStock(ctx context.Context, uc *ListLowStockUseCase, d *notify.Dispatcher, tenantID uuid.UUID, scope string, limit int) error {
	if d == nil || !d.Configured() {
		return nil
	}
	rows, err := uc.Execute(ctx, tenantID, limit)
	if err != nil {
		return err
	}
	d.DispatchLowStock(ctx, scope, NotifyLines(rows))
	return nil
}
