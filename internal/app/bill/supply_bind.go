package bill

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

type saleSourceStore interface {
	PurchaseLine(ctx context.Context, tx *sqlTx, tenantID, itemID uuid.UUID) (PurchaseLine, error)
	SetLinkBill(ctx context.Context, tx *sqlTx, tenantID, billID, linked uuid.UUID) error
	SetLinkItems(ctx context.Context, tx *sqlTx, tenantID, purchaseID uuid.UUID, links map[uuid.UUID]uuid.UUID) error
	ClearLinkBill(ctx context.Context, tx *sqlTx, tenantID, billID uuid.UUID) error
}

// attachSaleSource points the sale at the approved purchase its lines were opened from.
// Lines without a source are ordinary stock sales. Every sourced line must be the same purchase.
func attachSaleSource(ctx context.Context, tx *sqlTx, repo BillRepo, tenant, saleID uuid.UUID, items []*domain.BillItem, src []SaleItem) error {
	store, ok := repo.(saleSourceStore)
	var head uuid.UUID
	links := map[uuid.UUID]uuid.UUID{}
	for i, it := range src {
		if it.SourceItemID == uuid.Nil {
			continue
		}
		if !ok {
			return fmt.Errorf("%w: sale source cannot be stored", ErrValidation)
		}
		line, err := store.PurchaseLine(ctx, tx, tenant, it.SourceItemID)
		if err != nil {
			if errors.Is(err, errNoRows) {
				return fmt.Errorf("%w: sale source line not found", ErrValidation)
			}
			return fmt.Errorf("attach sale source: %w", err)
		}
		if line.Status != domain.StatusApproved || line.SubType != domain.BillSubTypePurchase {
			return fmt.Errorf("%w: sale source is not an approved purchase line", ErrValidation)
		}
		if i >= len(items) || line.ProductID != items[i].ProductID {
			return fmt.Errorf("%w: sale source does not match the purchase line", ErrValidation)
		}
		if head != uuid.Nil && head != line.HeadID {
			return fmt.Errorf("%w: sale source spans two purchases", ErrValidation)
		}
		head = line.HeadID
		links[items[i].ID] = it.SourceItemID
	}
	if !ok {
		return nil
	}
	if len(links) == 0 {
		return store.ClearLinkBill(ctx, tx, tenant, saleID)
	}
	if err := store.SetLinkBill(ctx, tx, tenant, saleID, head); err != nil {
		return err
	}
	return store.SetLinkItems(ctx, tx, tenant, head, links)
}
