package stock

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// BatchInsufficientStockError collects every product that would go negative in a
// multi-line bill approval. It is returned by ApprovePurchaseUseCase and
// ApproveSaleUseCase when one or more line items lack sufficient stock.
// The HTTP layer serialises it as {"error":"insufficient_stock","details":[...]}
// so the UI can highlight every short row at once.
type BatchInsufficientStockError struct {
	// Shortages is the ordered list of per-line shortage entries.
	// It contains at least one element.
	Shortages []InsufficientStockError
}

func (e *BatchInsufficientStockError) Error() string {
	msgs := make([]string, len(e.Shortages))
	for i, s := range e.Shortages {
		msgs[i] = fmt.Sprintf("product=%s available=%s requested=%s",
			s.ProductID, s.Available, s.Requested)
	}
	return "stock: insufficient stock for " + strings.Join(msgs, "; ")
}

// IsBatchInsufficientStock reports whether err is a *BatchInsufficientStockError.
func IsBatchInsufficientStock(err error) bool {
	_, ok := err.(*BatchInsufficientStockError)
	return ok
}

// InsufficientStockDetail is the wire representation used by HTTP handlers.
type InsufficientStockDetail struct {
	ProductID    uuid.UUID       `json:"-"`
	ProductIDStr string          `json:"product_id"`
	Available    decimal.Decimal `json:"-"`
	AvailableStr string          `json:"available_qty"`
	Requested    decimal.Decimal `json:"-"`
	RequestedStr string          `json:"requested_qty"`
}

// BatchDetails converts the shortages list into the handler-ready detail slice.
func (e *BatchInsufficientStockError) BatchDetails() []InsufficientStockDetail {
	out := make([]InsufficientStockDetail, len(e.Shortages))
	for i, s := range e.Shortages {
		out[i] = InsufficientStockDetail{
			ProductID:    s.ProductID,
			ProductIDStr: s.ProductID.String(),
			Available:    s.Available,
			AvailableStr: s.Available.String(),
			Requested:    s.Requested,
			RequestedStr: s.Requested.String(),
		}
	}
	return out
}
