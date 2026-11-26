package intake

import (
	"context"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/intake"
)

type (
	Hit          = domain.Hit
	BillLink     = domain.BillLink
	DraftLine    = domain.DraftLine
	CaptureInput = domain.CaptureInput
	AcceptInput  = domain.AcceptInput
	AcceptResult = domain.AcceptResult
)

// RegisterInput is the invoice the inbox is allowed to create. It never approves.
type RegisterInput struct {
	TenantID    uuid.UUID
	PartnerID   uuid.UUID
	Actor       uuid.UUID
	Direction   string
	InvoiceNo   string
	InvoiceCode string
	InvoiceType string
	IssueDate   time.Time
	Excl        decimal.Decimal
	Tax         decimal.Decimal
	Links       []BillLink
}

// InvoiceRegistrar is the existing invoice registry. Defined here so this
// package does not import the invoice use-case package.
type InvoiceRegistrar interface {
	Register(ctx context.Context, in RegisterInput) (uuid.UUID, error)
}

// DraftCreator writes a purchase draft. It does not approve it.
type DraftCreator interface {
	CreatePurchaseDraft(ctx context.Context, tenant, actor, partner uuid.UUID, lines []DraftLine) (uuid.UUID, string, error)
}

// VisionParser reads a page. A nil parser means the gateway is off.
type VisionParser interface {
	Parse(ctx context.Context, contentType string, body []byte) (domain.Parsed, error)
}

// SpeechParser turns audio into text. A nil parser leaves the audio for a person.
type SpeechParser interface {
	Transcribe(ctx context.Context, contentType string, body []byte) (string, error)
}

// BlobStore keeps the original bytes. The key includes the tenant id.
type BlobStore interface {
	Put(ctx context.Context, key string, body []byte, contentType string) error
	Open(ctx context.Context, key string) (io.ReadCloser, string, error)
}

// Repository is the inbox store.
type Repository interface {
	FindFingerprint(ctx context.Context, tenant uuid.UUID, kind, hash string) (*Hit, error)
	FindClientRef(ctx context.Context, tenant uuid.UUID, ref string) (*domain.Document, error)
	FindNear(ctx context.Context, tenant uuid.UUID, taxNo string, day time.Time, total decimal.Decimal) (*Hit, error)
	FindInvoice(ctx context.Context, tenant uuid.UUID, direction, number string) (*Hit, error)
	SuggestPartner(ctx context.Context, tenant uuid.UUID, taxNo string) (uuid.UUID, string, error)
	ProductNames(ctx context.Context, tenant uuid.UUID) (map[string]uuid.UUID, error)
	Insert(ctx context.Context, doc *domain.Document, fps []domain.Fingerprint) error
	Get(ctx context.Context, tenant, id uuid.UUID) (*domain.Document, error)
	List(ctx context.Context, tenant uuid.UUID, own *uuid.UUID, limit, offset int) ([]*domain.Document, int, error)
	CountOpen(ctx context.Context, tenant uuid.UUID, own *uuid.UUID) (int, error)
	Lock(ctx context.Context, tenant, id, actor uuid.UUID, until time.Time) error
	Reject(ctx context.Context, tenant, id uuid.UUID, reason string) (*domain.Document, error)
	MarkAccepted(ctx context.Context, doc *domain.Document) error
	OpenBills(ctx context.Context, tenant, partner uuid.UUID, direction string) ([]domain.OpenBill, error)
	ApprovedLinks(ctx context.Context, tenant, partner uuid.UUID, ids []uuid.UUID, amount decimal.Decimal) ([]BillLink, error)
	MatchReceipts(ctx context.Context, tenant uuid.UUID, partner *uuid.UUID, total decimal.Decimal) ([]domain.Match, error)
}
