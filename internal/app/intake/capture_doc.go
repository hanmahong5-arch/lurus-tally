package intake

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/intake"
)

func (s *Service) newDoc(in CaptureInput, parsed domain.Parsed) (*domain.Document, []domain.Fingerprint) {
	now := s.now()
	doc := &domain.Document{
		ID: inNew(), TenantID: in.TenantID, Source: in.Source,
		Direction: directionOr(parsed.Direction, in.Direction), Status: domain.StatusFor(parsed),
		ClientRef: in.ClientRef, ContentType: in.ContentType, QRPayload: in.QRPayload,
		SellerTaxNo: parsed.SellerTaxNo, InvoiceNo: parsed.InvoiceNo, Parsed: parsed,
		CreatedBy: in.Actor, CreatedAt: now, UpdatedAt: now,
	}
	if parsed.IssueDate != "" {
		if day, err := time.Parse("2006-01-02", parsed.IssueDate); err == nil {
			doc.IssueDate = &day
		}
	}
	if parsed.TotalAmount != "" {
		if total, err := decimal.NewFromString(parsed.TotalAmount); err == nil {
			doc.Total = &total
		}
	}
	var fps []domain.Fingerprint
	if in.QRPayload != "" {
		fps = append(fps, domain.Fingerprint{Kind: domain.KindQR, Hash: domain.Hash([]byte(in.QRPayload))})
	}
	if len(in.Body) > 0 {
		kind := domain.KindImage
		if in.Source == domain.SourceVoice {
			kind = domain.KindText
		}
		fps = append(fps, domain.Fingerprint{Kind: kind, Hash: domain.Hash(in.Body)})
	}
	return doc, fps
}

func inNew() uuid.UUID { return uuid.New() }

func directionOr(a, b string) string {
	if a != "" {
		return a
	}
	if b != "" {
		return b
	}
	return domain.DirectionIn
}

func dayOf(doc *domain.Document) string {
	if doc == nil || doc.IssueDate == nil {
		return ""
	}
	return doc.IssueDate.Format("2006-01-02")
}

func mergeParse(base, extra domain.Parsed) domain.Parsed {
	if base.InvoiceNo == "" {
		base.InvoiceNo = extra.InvoiceNo
	}
	if base.InvoiceCode == "" {
		base.InvoiceCode = extra.InvoiceCode
	}
	if base.InvoiceType == "" {
		base.InvoiceType = extra.InvoiceType
	}
	if base.SellerTaxNo == "" {
		base.SellerTaxNo = extra.SellerTaxNo
	}
	if base.SellerName == "" {
		base.SellerName = extra.SellerName
	}
	if base.IssueDate == "" {
		base.IssueDate = extra.IssueDate
	}
	if extra.MoneyOK {
		base.AmountExclTax = extra.AmountExclTax
		base.TaxAmount = extra.TaxAmount
		base.TotalAmount = extra.TotalAmount
		base.MoneyOK = true
	} else if base.TotalAmount == "" {
		base.TotalAmount = extra.TotalAmount
	}
	base.Lines = append(base.Lines, extra.Lines...)
	base.Hints = append(base.Hints, extra.Hints...)
	if extra.Direction != "" && base.Direction == "" {
		base.Direction = extra.Direction
	}
	return base
}
