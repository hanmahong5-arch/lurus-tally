// Package intake is the capture inbox: a document waits for a person before it
// becomes an invoice or a draft bill.
package intake

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const (
	SourceCamera  = "camera"
	SourceFile    = "file"
	SourceVoice   = "voice"
	SourceReceipt = "receipt"

	DirectionIn  = "in"
	DirectionOut = "out"

	StatusReceived    = "received"
	StatusNeedsReview = "needs_review"
	StatusAccepted    = "accepted"
	StatusRejected    = "rejected"

	KindQR    = "qr"
	KindImage = "image"
	KindText  = "text"
)

// Parsed is the machine reading. Empty money fields mean a person must fill them.
type Parsed struct {
	InvoiceNo     string       `json:"invoice_no,omitempty"`
	InvoiceCode   string       `json:"invoice_code,omitempty"`
	InvoiceType   string       `json:"invoice_type,omitempty"`
	SellerTaxNo   string       `json:"seller_tax_no,omitempty"`
	SellerName    string       `json:"seller_name,omitempty"`
	PartnerID     string       `json:"partner_id,omitempty"`
	PartnerName   string       `json:"partner_name,omitempty"`
	IssueDate     string       `json:"issue_date,omitempty"`
	AmountExclTax string       `json:"amount_excl_tax,omitempty"`
	TaxAmount     string       `json:"tax_amount,omitempty"`
	TotalAmount   string       `json:"total_amount,omitempty"`
	Direction     string       `json:"direction,omitempty"`
	Transcript    string       `json:"transcript,omitempty"`
	MoneyOK       bool         `json:"money_ok"`
	Lines         []ParsedLine `json:"lines,omitempty"`
	Matches       []Match      `json:"matches,omitempty"`
	Hints         []string     `json:"hints,omitempty"`
}

// ParsedLine is one suggested row. ProductID is set only when the name is already in the catalog.
type ParsedLine struct {
	Name      string `json:"name,omitempty"`
	ProductID string `json:"product_id,omitempty"`
	Qty       string `json:"qty,omitempty"`
	UnitPrice string `json:"unit_price,omitempty"`
}

// Match is an existing bill a receipt might belong to. Accepting never pays it.
type Match struct {
	BillID string `json:"bill_id"`
	BillNo string `json:"bill_no"`
	Amount string `json:"amount,omitempty"`
	Status string `json:"status,omitempty"`
}

// OpenBill is an approved bill that can still take part of an invoice.
// Remaining is the tax-inclusive amount not yet covered by a live invoice.
type OpenBill struct {
	ID        uuid.UUID
	BillNo    string
	Status    int16
	BillDate  string
	Total     decimal.Decimal
	Remaining decimal.Decimal
}

// Document is one inbox row.
type Document struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	Source       string
	Direction    string
	Status       string
	ClientRef    string
	ObjectKey    string
	ContentType  string
	QRPayload    string
	SellerTaxNo  string
	InvoiceNo    string
	IssueDate    *time.Time
	Total        *decimal.Decimal
	Parsed       Parsed
	InvoiceID    *uuid.UUID
	DraftBillID  *uuid.UUID
	RejectReason string
	CreatedBy    uuid.UUID
	LockBy       *uuid.UUID
	LockUntil    *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
	OpenBills    []OpenBill
}

// Fingerprint is one dedup key (qr text, file bytes, or transcript).
type Fingerprint struct {
	Kind string
	Hash string
}

// StatusFor chooses the inbox status from what could be read.
func StatusFor(p Parsed) string {
	if p.InvoiceNo != "" || p.Transcript != "" || p.TotalAmount != "" || len(p.Lines) > 0 || len(p.Matches) > 0 {
		return StatusNeedsReview
	}
	return StatusReceived
}

// ValidSource reports whether s is a capture channel.
func ValidSource(s string) bool {
	switch s {
	case SourceCamera, SourceFile, SourceVoice, SourceReceipt:
		return true
	default:
		return false
	}
}

// ValidDirection reports whether d is in or out. Empty is allowed and means in.
func ValidDirection(d string) bool {
	return d == "" || d == DirectionIn || d == DirectionOut
}
