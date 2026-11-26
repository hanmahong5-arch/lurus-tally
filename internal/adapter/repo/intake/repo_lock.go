package intake

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/intake"
)

func (r *Repo) Lock(ctx context.Context, tenant, id, actor uuid.UUID, until time.Time) error {
	res, err := r.q(ctx).ExecContext(ctx, `
		UPDATE tally.intake_document
		SET lock_by=$3, lock_until=$4, updated_at=now()
		WHERE tenant_id=$1 AND id=$2
		  AND status IN ('received','needs_review')
		  AND (lock_until IS NULL OR lock_until < now() OR lock_by=$3)`,
		tenant, id, actor, until)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	doc, err := r.Get(ctx, tenant, id)
	if err != nil {
		return err
	}
	if doc.Status == domain.StatusAccepted || doc.Status == domain.StatusRejected {
		return domain.ErrConflict
	}
	return domain.ErrLocked
}

func (r *Repo) Reject(ctx context.Context, tenant, id uuid.UUID, reason string) (*domain.Document, error) {
	if _, err := r.q(ctx).ExecContext(ctx, `
		DELETE FROM tally.intake_fingerprint WHERE tenant_id=$1 AND document_id=$2`, tenant, id); err != nil {
		return nil, err
	}
	res, err := r.q(ctx).ExecContext(ctx, `
		UPDATE tally.intake_document
		SET status='rejected', reject_reason=$3, lock_by=NULL, lock_until=NULL, updated_at=now()
		WHERE tenant_id=$1 AND id=$2 AND status IN ('received','needs_review')`, tenant, id, reason)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, domain.ErrConflict
	}
	return r.Get(ctx, tenant, id)
}

func (r *Repo) MarkAccepted(ctx context.Context, doc *domain.Document) error {
	raw, err := json.Marshal(doc.Parsed)
	if err != nil {
		return err
	}
	var invoice, draft any
	if doc.InvoiceID != nil {
		invoice = *doc.InvoiceID
	}
	if doc.DraftBillID != nil {
		draft = *doc.DraftBillID
	}
	res, err := r.q(ctx).ExecContext(ctx, `
		UPDATE tally.intake_document
		SET status='accepted', direction=$3, invoice_no=$4, invoice_id=$5, draft_bill_id=$6,
		    parsed=$7, lock_by=NULL, lock_until=NULL, updated_at=now()
		WHERE tenant_id=$1 AND id=$2 AND status IN ('received','needs_review')`,
		doc.TenantID, doc.ID, doc.Direction, doc.InvoiceNo, invoice, draft, raw)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		got, gerr := r.Get(ctx, doc.TenantID, doc.ID)
		if gerr != nil {
			return gerr
		}
		if got.Status == domain.StatusAccepted {
			return nil
		}
		return domain.ErrConflict
	}
	return nil
}
