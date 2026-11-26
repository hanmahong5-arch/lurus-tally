// Package intakebridge adapts the existing invoice and purchase-draft use cases
// to the inbox ports. The inbox package does not import those use cases.
package intakebridge

import (
	"context"

	"github.com/google/uuid"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appintake "github.com/hanmahong5-arch/lurus-tally/internal/app/intake"
	appinv "github.com/hanmahong5-arch/lurus-tally/internal/app/invoice"
	domaininv "github.com/hanmahong5-arch/lurus-tally/internal/domain/invoice"
)

// Invoices registers through the invoice registry and never approves a bill.
type Invoices struct{ UC *appinv.CreateUseCase }

// Register implements appintake.InvoiceRegistrar.
func (b Invoices) Register(ctx context.Context, in appintake.RegisterInput) (uuid.UUID, error) {
	links := make([]domaininv.Link, len(in.Links))
	for i, l := range in.Links {
		links[i] = domaininv.Link{BillID: l.BillID, Amount: l.Amount}
	}
	actor := in.Actor
	inv, err := b.UC.Execute(ctx, domaininv.CreateInput{
		TenantID: in.TenantID, Direction: domaininv.Direction(in.Direction),
		InvoiceNo: in.InvoiceNo, InvoiceCode: in.InvoiceCode, InvoiceType: in.InvoiceType,
		PartnerID: in.PartnerID, IssueDate: in.IssueDate,
		AmountExclTax: in.Excl, TaxAmount: in.Tax, CreatedBy: &actor, Links: links,
	})
	if err != nil {
		return uuid.Nil, err
	}
	return inv.ID, nil
}

// Drafts creates a purchase draft and leaves it unapproved.
type Drafts struct {
	UC *appbill.CreatePurchaseDraftUseCase
}

// CreatePurchaseDraft implements appintake.DraftCreator.
func (b Drafts) CreatePurchaseDraft(ctx context.Context, tenant, actor, partner uuid.UUID, lines []appintake.DraftLine) (uuid.UUID, string, error) {
	items := make([]appbill.CreatePurchaseItemInput, len(lines))
	for i, ln := range lines {
		items[i] = appbill.CreatePurchaseItemInput{
			ProductID: ln.ProductID, LineNo: i + 1, Qty: ln.Qty, UnitPrice: ln.UnitPrice,
		}
	}
	out, err := b.UC.Execute(ctx, appbill.CreatePurchaseDraftRequest{
		TenantID: tenant, CreatorID: actor, PartnerID: &partner, Items: items, Currency: "CNY",
	})
	if err != nil {
		return uuid.Nil, "", err
	}
	return out.BillID, out.BillNo, nil
}
