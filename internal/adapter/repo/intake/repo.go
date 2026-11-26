package intake

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/intake"
)

// Repo is the Postgres inbox.
type Repo struct{ db *sql.DB }

// New constructs a Repo.
func New(db *sql.DB) *Repo { return &Repo{db: db} }

const docCols = `id, tenant_id, source, direction, status, COALESCE(client_ref,''), COALESCE(object_key,''), COALESCE(content_type,''), COALESCE(qr_payload,''), COALESCE(seller_tax_no,''), COALESCE(invoice_no,''), issue_date, total_amount::text, parsed::text, invoice_id::text, draft_bill_id::text, COALESCE(reject_reason,''), COALESCE(created_by::text,''), lock_by::text, lock_until, created_at, updated_at`

func (r *Repo) Insert(ctx context.Context, doc *domain.Document, fps []domain.Fingerprint) error {
	raw, err := json.Marshal(doc.Parsed)
	if err != nil {
		return err
	}
	return dbscope.InTx(ctx, r.db, func(ctx context.Context) error {
		tx := dbscope.TxFrom(ctx)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO tally.intake_document (
				id, tenant_id, source, direction, status, client_ref, object_key, content_type, qr_payload,
				seller_tax_no, invoice_no, issue_date, total_amount, parsed, created_by, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),
				NULLIF($10,''),NULLIF($11,''),$12,$13,$14,$15,$16,$16)`,
			doc.ID, doc.TenantID, doc.Source, doc.Direction, doc.Status, doc.ClientRef, doc.ObjectKey, doc.ContentType, doc.QRPayload,
			doc.SellerTaxNo, doc.InvoiceNo, nullDay(doc.IssueDate), nullMoney(doc.Total), raw, doc.CreatedBy, doc.CreatedAt)
		if err != nil {
			return mapUnique(err)
		}
		for _, fp := range fps {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO tally.intake_fingerprint (tenant_id, kind, fingerprint, document_id)
				VALUES ($1,$2,$3,$4)`, doc.TenantID, fp.Kind, fp.Hash, doc.ID); err != nil {
				return mapUnique(err)
			}
		}
		return nil
	})
}

func (r *Repo) q(ctx context.Context) dbscope.Querier { return dbscope.From(ctx, r.db) }

func (r *Repo) FindClientRef(ctx context.Context, tenant uuid.UUID, ref string) (*domain.Document, error) {
	row := r.q(ctx).QueryRowContext(ctx, `SELECT `+docCols+` FROM tally.intake_document WHERE tenant_id=$1 AND client_ref=$2`, tenant, ref)
	doc, err := scanDoc(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return doc, err
}

func (r *Repo) FindFingerprint(ctx context.Context, tenant uuid.UUID, kind, hash string) (*domain.Hit, error) {
	var id uuid.UUID
	var status, day string
	var invoice sql.NullString
	err := r.q(ctx).QueryRowContext(ctx, `
		SELECT d.id, d.status, COALESCE(to_char(d.issue_date,'YYYY-MM-DD'),''), d.invoice_id::text
		FROM tally.intake_fingerprint f
		JOIN tally.intake_document d ON d.id = f.document_id AND d.tenant_id = f.tenant_id
		WHERE f.tenant_id=$1 AND f.kind=$2 AND f.fingerprint=$3`, tenant, kind, hash).
		Scan(&id, &status, &day, &invoice)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	hit := &domain.Hit{DocumentID: id, Status: status, IssueDate: day, Booked: status == domain.StatusAccepted}
	if invoice.Valid {
		if iid, err := uuid.Parse(invoice.String); err == nil {
			hit.InvoiceID = iid
		}
	}
	return hit, nil
}

func (r *Repo) Get(ctx context.Context, tenant, id uuid.UUID) (*domain.Document, error) {
	doc, err := scanDoc(r.q(ctx).QueryRowContext(ctx, `SELECT `+docCols+` FROM tally.intake_document WHERE tenant_id=$1 AND id=$2`, tenant, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return doc, err
}

func (r *Repo) List(ctx context.Context, tenant uuid.UUID, own *uuid.UUID, limit, offset int) ([]*domain.Document, int, error) {
	ownID := any(nil)
	if own != nil {
		ownID = *own
	}
	var total int
	if err := r.q(ctx).QueryRowContext(ctx, `
		SELECT COUNT(*) FROM tally.intake_document
		WHERE tenant_id=$1 AND ($2::uuid IS NULL OR created_by=$2)`, tenant, ownID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.q(ctx).QueryContext(ctx, `
		SELECT `+docCols+` FROM tally.intake_document
		WHERE tenant_id=$1 AND ($2::uuid IS NULL OR created_by=$2)
		ORDER BY created_at DESC LIMIT $3 OFFSET $4`, tenant, ownID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*domain.Document
	for rows.Next() {
		doc, err := scanDoc(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, doc)
	}
	return out, total, rows.Err()
}

func (r *Repo) CountOpen(ctx context.Context, tenant uuid.UUID, own *uuid.UUID) (int, error) {
	ownID := any(nil)
	if own != nil {
		ownID = *own
	}
	var n int
	err := r.q(ctx).QueryRowContext(ctx, `
		SELECT COUNT(*) FROM tally.intake_document
		WHERE tenant_id=$1 AND status IN ('received','needs_review')
		AND ($2::uuid IS NULL OR created_by=$2)`, tenant, ownID).Scan(&n)
	return n, err
}

func nullDay(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC()
}

func nullMoney(d *decimal.Decimal) any {
	if d == nil {
		return nil
	}
	return d.String()
}

func mapUnique(err error) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23505" {
		return err
	}
	return fmt.Errorf("%w: 这张收件已经上传过了", domain.ErrDuplicate)
}

type scanner interface{ Scan(dest ...any) error }

func scanDoc(row scanner) (*domain.Document, error) {
	var doc domain.Document
	var issue sql.NullTime
	var total, parsed, invoiceID, draftID, lockBy sql.NullString
	var lockUntil sql.NullTime
	var createdBy sql.NullString
	err := row.Scan(
		&doc.ID, &doc.TenantID, &doc.Source, &doc.Direction, &doc.Status, &doc.ClientRef, &doc.ObjectKey, &doc.ContentType, &doc.QRPayload,
		&doc.SellerTaxNo, &doc.InvoiceNo, &issue, &total, &parsed, &invoiceID, &draftID, &doc.RejectReason, &createdBy, &lockBy, &lockUntil, &doc.CreatedAt, &doc.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if issue.Valid {
		day := issue.Time
		doc.IssueDate = &day
	}
	if total.Valid && total.String != "" {
		if d, err := decimal.NewFromString(total.String); err == nil {
			doc.Total = &d
		}
	}
	if parsed.Valid && parsed.String != "" {
		_ = json.Unmarshal([]byte(parsed.String), &doc.Parsed)
	}
	doc.InvoiceID = parseUUID(invoiceID.String)
	doc.DraftBillID = parseUUID(draftID.String)
	doc.LockBy = parseUUID(lockBy.String)
	if createdBy.Valid {
		if id, err := uuid.Parse(createdBy.String); err == nil {
			doc.CreatedBy = id
		}
	}
	if lockUntil.Valid {
		t := lockUntil.Time
		doc.LockUntil = &t
	}
	return &doc, nil
}

func parseUUID(s string) *uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil || id == uuid.Nil {
		return nil
	}
	return &id
}
