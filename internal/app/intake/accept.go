package intake

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/intake"
	domaininv "github.com/hanmahong5-arch/lurus-tally/internal/domain/invoice"
)

// Accept registers an invoice and, when asked, a purchase draft. It does not approve or pay.
func (s *Service) Accept(ctx context.Context, in AcceptInput) (*AcceptResult, error) {
	doc, err := s.Repo.Get(ctx, in.TenantID, in.ID)
	if err != nil {
		return nil, err
	}
	if doc.Status == domain.StatusAccepted {
		return &AcceptResult{Document: doc}, nil
	}
	if doc.Status == domain.StatusRejected {
		return nil, fmt.Errorf("%w: 这张收件已经退回", ErrConflict)
	}
	if err := s.Repo.Lock(ctx, in.TenantID, in.ID, in.Actor, s.now().Add(lockFor)); err != nil {
		return nil, err
	}
	if in.Direction == "" {
		in.Direction = doc.Direction
	}
	if !domain.ValidDirection(in.Direction) || in.Direction == "" {
		return nil, fmt.Errorf("%w: 发票方向不正确", ErrValidation)
	}
	receiptOnly := doc.Source == domain.SourceReceipt && !domain.MoneyReconciles(in.Excl, in.Tax, in.Total)
	if receiptOnly {
		return s.acceptReceipt(ctx, doc, in)
	}
	if !domain.MoneyReconciles(in.Excl, in.Tax, in.Total) {
		return nil, fmt.Errorf("%w: 不含税金额、税额、价税合计对不齐，请改到三者相符", ErrMoney)
	}
	if in.PartnerID == uuid.Nil || in.InvoiceNo == "" || in.IssueDate.IsZero() {
		return nil, fmt.Errorf("%w: 请补全发票号码、往来单位和开票日期", ErrValidation)
	}
	if !knownInvoiceType(in.InvoiceType) {
		return nil, fmt.Errorf("%w: 发票类型请选数电普票、数电专票、纸质普票或纸质专票", ErrValidation)
	}
	if hit, err := s.Repo.FindInvoice(ctx, in.TenantID, in.Direction, in.InvoiceNo); err != nil || hit != nil {
		if err != nil || hit.InvoiceID == uuid.Nil || hit.Status == "void" || hit.Status == "red" {
			if err != nil {
				return nil, err
			}
			return nil, errOr(nil, hit)
		}
		doc.InvoiceID = &hit.InvoiceID
		doc.Status = domain.StatusAccepted
		doc.Direction = in.Direction
		doc.InvoiceNo = in.InvoiceNo
		if err := s.Repo.MarkAccepted(ctx, doc); err != nil {
			return nil, err
		}
		return &AcceptResult{Document: doc, Warning: "这张发票已经登记过，收件已核对"}, nil
	}
	links, err := s.Repo.ApprovedLinks(ctx, in.TenantID, in.PartnerID, in.LinkBillIDs, in.Total)
	if err != nil {
		return nil, err
	}
	invID, err := s.Invoices.Register(ctx, RegisterInput{
		TenantID: in.TenantID, PartnerID: in.PartnerID, Actor: in.Actor,
		Direction: in.Direction, InvoiceNo: in.InvoiceNo, InvoiceCode: in.InvoiceCode,
		InvoiceType: in.InvoiceType, IssueDate: in.IssueDate, Excl: in.Excl, Tax: in.Tax, Links: links,
	})
	if err != nil {
		return nil, err
	}
	doc.InvoiceID = &invID
	doc.Status = domain.StatusAccepted
	doc.Direction = in.Direction
	doc.InvoiceNo = in.InvoiceNo
	warning := domain.CoverWarning(in.Total, links)
	var billNo string
	if in.CreateDraft && doc.Source != domain.SourceReceipt && s.Drafts != nil && len(in.Lines) > 0 {
		draftID, no, derr := s.Drafts.CreatePurchaseDraft(ctx, in.TenantID, in.Actor, in.PartnerID, in.Lines)
		if derr != nil {
			warning = joinWarning("发票已登记，采购草稿没有生成", warning)
		} else {
			doc.DraftBillID = &draftID
			billNo = no
		}
	}
	if err := s.Repo.MarkAccepted(ctx, doc); err != nil {
		return nil, err
	}
	return &AcceptResult{Document: doc, DraftBillNo: billNo, Warning: warning}, nil
}

func (s *Service) acceptReceipt(ctx context.Context, doc *domain.Document, in AcceptInput) (*AcceptResult, error) {
	if in.MatchBillID == nil || *in.MatchBillID == uuid.Nil {
		return nil, fmt.Errorf("%w: 请选择要对照的单据，或补全发票金额后再确认", ErrValidation)
	}
	doc.Status = domain.StatusAccepted
	doc.Parsed.Matches = []domain.Match{{BillID: in.MatchBillID.String()}}
	doc.Parsed.Hints = append(doc.Parsed.Hints, "已核对，未自动收款")
	if err := s.Repo.MarkAccepted(ctx, doc); err != nil {
		return nil, err
	}
	return &AcceptResult{Document: doc, Warning: "已核对，未自动收款"}, nil
}

// Reject drops the fingerprints so the same photo can be sent again, and keeps the row.
func (s *Service) Reject(ctx context.Context, tenant, id, actor uuid.UUID, reason string) (*domain.Document, error) {
	if reason == "" {
		return nil, fmt.Errorf("%w: 请填写退回原因", ErrValidation)
	}
	if err := s.Repo.Lock(ctx, tenant, id, actor, s.now().Add(lockFor)); err != nil {
		return nil, err
	}
	return s.Repo.Reject(ctx, tenant, id, reason)
}

// Lock holds the row for two minutes so two people cannot confirm it together.
func (s *Service) Lock(ctx context.Context, tenant, id, actor uuid.UUID) error {
	return s.Repo.Lock(ctx, tenant, id, actor, s.now().Add(lockFor))
}

func joinWarning(a, b string) string {
	if b == "" {
		return a
	}
	return a + "。" + b
}

func knownInvoiceType(s string) bool {
	for _, t := range domaininv.Types {
		if s == t {
			return true
		}
	}
	return false
}
