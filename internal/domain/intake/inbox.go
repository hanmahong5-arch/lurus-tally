package intake

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const (
	CodeDuplicate = "duplicate_document"
	CodeExists    = "intake_already_exists"
	CodeLocked    = "intake_locked"
	CodeMoney     = "money_mismatch"
)

var (
	ErrValidation = errors.New("intake validation")
	ErrNotFound   = errors.New("intake not found")
	ErrDuplicate  = errors.New("intake duplicate")
	ErrLocked     = errors.New("intake locked")
	ErrMoney      = errors.New("intake money")
	ErrConflict   = errors.New("intake conflict")
)

// Duplicate points at the inbox row or invoice that already covers this paper.
type Duplicate struct {
	ExistingID uuid.UUID
	Kind       string
	Code       string
	IssueDate  string
	InvoiceNo  string
	Message    string
}

func (d *Duplicate) Error() string {
	if d == nil || d.Message == "" {
		return "这张票已经收过"
	}
	return d.Message
}

func (d *Duplicate) Is(target error) bool { return target == ErrDuplicate }

// Hit is an existing inbox row or invoice that blocks a second capture.
type Hit struct {
	DocumentID uuid.UUID
	InvoiceID  uuid.UUID
	Status     string
	IssueDate  string
	InvoiceNo  string
	Booked     bool
}

// BillLink is one approved bill a confirmed invoice may cover.
type BillLink struct {
	BillID uuid.UUID
	Amount decimal.Decimal
}

// DraftLine is one purchase-draft row. Prices come from the person confirming.
type DraftLine struct {
	ProductID uuid.UUID
	Qty       decimal.Decimal
	UnitPrice decimal.Decimal
}

// CaptureInput is one upload. QRPayload is parsed again on the server.
type CaptureInput struct {
	TenantID    uuid.UUID
	Actor       uuid.UUID
	Source      string
	Direction   string
	ClientRef   string
	ContentType string
	Body        []byte
	QRPayload   string
	Transcript  string
}

// AcceptInput is what the reviewer confirmed.
type AcceptInput struct {
	TenantID    uuid.UUID
	Actor       uuid.UUID
	ID          uuid.UUID
	Direction   string
	InvoiceNo   string
	InvoiceCode string
	InvoiceType string
	PartnerID   uuid.UUID
	IssueDate   time.Time
	Excl        decimal.Decimal
	Tax         decimal.Decimal
	Total       decimal.Decimal
	LinkBillIDs []uuid.UUID
	CreateDraft bool
	Lines       []DraftLine
	MatchBillID *uuid.UUID
}

// AcceptResult is the inbox row after confirm, plus the draft number when one was written.
type AcceptResult struct {
	Document    *Document
	DraftBillNo string
	Warning     string
}
