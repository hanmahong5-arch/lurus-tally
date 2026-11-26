package intake

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/intake"
)

const (
	maxBytes     = 6 << 20
	maxClientRef = 64
	lockFor      = 2 * time.Minute
)

// Service is the inbox. Vision and speech may be nil; capture still stores the file.
type Service struct {
	Repo     Repository
	Blobs    BlobStore
	Invoices InvoiceRegistrar
	Drafts   DraftCreator
	Vision   VisionParser
	Speech   SpeechParser
	Now      func() time.Time
}

func (s *Service) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// Capture stores one document or returns *Duplicate when it was already received.
func (s *Service) Capture(ctx context.Context, in CaptureInput) (*domain.Document, error) {
	if err := validateCapture(in); err != nil {
		return nil, err
	}
	if in.Direction == "" {
		in.Direction = domain.DirectionIn
	}
	if in.QRPayload == "" {
		in.QRPayload = domain.SniffQR(in.Body)
	}
	if doc, err := s.replay(ctx, in); doc != nil || err != nil {
		return doc, err
	}
	parsed := domain.Parsed{Direction: in.Direction}
	if q, ok := domain.ParseQR(in.QRPayload); ok {
		parsed = domain.ApplyQR(parsed, q)
	}
	if s.Vision != nil && len(in.Body) > 0 && in.Source != domain.SourceVoice {
		if seen, err := s.Vision.Parse(ctx, in.ContentType, in.Body); err == nil {
			parsed = mergeParse(parsed, seen)
			if q, ok := domain.ParseQR(in.QRPayload); ok {
				parsed = domain.ApplyQR(parsed, q)
			}
		}
	}
	transcript := in.Transcript
	if s.Speech != nil && len(in.Body) > 0 && in.Source == domain.SourceVoice {
		if text, err := s.Speech.Transcribe(ctx, in.ContentType, in.Body); err == nil && text != "" {
			transcript = text
		}
	}
	if transcript != "" {
		parsed.Transcript = transcript
		names, err := s.Repo.ProductNames(ctx, in.TenantID)
		if err != nil {
			return nil, err
		}
		parsed.Lines = append(parsed.Lines, domain.MatchTranscript(transcript, names)...)
	}
	if err := s.suggest(ctx, in.TenantID, &parsed); err != nil {
		return nil, err
	}
	if in.Source == domain.SourceReceipt {
		if err := s.attachReceipts(ctx, in.TenantID, &parsed); err != nil {
			return nil, err
		}
	}
	if hit, err := s.near(ctx, in.TenantID, in.Direction, parsed); hit != nil || err != nil {
		return nil, errOr(err, hit)
	}
	if parsed.InvoiceNo == "" && parsed.TotalAmount == "" && transcript == "" {
		parsed.Hints = append(parsed.Hints, "已存原件，待人工填写")
	}
	doc, fps := s.newDoc(in, parsed)
	if len(in.Body) > 0 {
		if s.Blobs == nil {
			return nil, fmt.Errorf("%w: 文件存储还没准备好", ErrValidation)
		}
		doc.ObjectKey = in.TenantID.String() + "/" + doc.ID.String()
		if err := s.Blobs.Put(ctx, doc.ObjectKey, in.Body, in.ContentType); err != nil {
			return nil, fmt.Errorf("intake store: %w", err)
		}
	}
	if err := s.Repo.Insert(ctx, doc, fps); err != nil {
		return nil, err
	}
	return doc, nil
}

func validateCapture(in CaptureInput) error {
	if in.TenantID == uuid.Nil || in.Actor == uuid.Nil {
		return fmt.Errorf("%w: 缺少店铺或操作人", ErrValidation)
	}
	if !domain.ValidSource(in.Source) {
		return fmt.Errorf("%w: 不认识的收件来源", ErrValidation)
	}
	if !domain.ValidDirection(in.Direction) {
		return fmt.Errorf("%w: 发票方向不正确", ErrValidation)
	}
	if utf8.RuneCountInString(in.ClientRef) > maxClientRef {
		return fmt.Errorf("%w: 客户端编号太长", ErrValidation)
	}
	if len(in.Body) > maxBytes {
		return fmt.Errorf("%w: 文件超过 6MB", ErrValidation)
	}
	if len(in.Body) == 0 && in.QRPayload == "" && in.Transcript == "" {
		return fmt.Errorf("%w: 请拍照、上传文件，或贴上二维码内容", ErrValidation)
	}
	return nil
}

func (s *Service) replay(ctx context.Context, in CaptureInput) (*domain.Document, error) {
	if in.ClientRef != "" {
		doc, err := s.Repo.FindClientRef(ctx, in.TenantID, in.ClientRef)
		if err != nil || doc != nil {
			if doc != nil {
				return nil, dupIntake(doc.ID, dayOf(doc), "这张收件已经上传过了", CodeExists)
			}
			return nil, err
		}
	}
	if in.QRPayload != "" {
		if hit, err := s.Repo.FindFingerprint(ctx, in.TenantID, domain.KindQR, domain.Hash([]byte(in.QRPayload))); err != nil || hit != nil {
			return nil, errOr(err, hit)
		}
	}
	if len(in.Body) > 0 {
		kind := domain.KindImage
		if in.Source == domain.SourceVoice {
			kind = domain.KindText
		}
		if hit, err := s.Repo.FindFingerprint(ctx, in.TenantID, kind, domain.Hash(in.Body)); err != nil || hit != nil {
			return nil, errOr(err, hit)
		}
	}
	return nil, nil
}

func errOr(err error, hit *Hit) error {
	if err != nil {
		return err
	}
	if hit == nil {
		return nil
	}
	id := hit.DocumentID
	kind := "intake"
	if hit.InvoiceID != uuid.Nil && hit.DocumentID == uuid.Nil {
		id = hit.InvoiceID
		kind = "invoice"
	}
	code := CodeDuplicate
	msg := bookedMessage(hit.IssueDate, hit.Booked || hit.Status == domain.StatusAccepted || kind == "invoice")
	if hit.Status == "void" || hit.Status == "red" {
		msg = "这张发票已经作废或红冲，不能再次入账"
	}
	return &Duplicate{ExistingID: id, Kind: kind, Code: code, IssueDate: hit.IssueDate, InvoiceNo: hit.InvoiceNo, Message: msg}
}

func (s *Service) suggest(ctx context.Context, tenant uuid.UUID, p *domain.Parsed) error {
	if p.SellerTaxNo == "" {
		return nil
	}
	id, name, err := s.Repo.SuggestPartner(ctx, tenant, p.SellerTaxNo)
	if err != nil || id == uuid.Nil {
		return err
	}
	p.PartnerID = id.String()
	p.PartnerName = name
	return nil
}

func (s *Service) attachReceipts(ctx context.Context, tenant uuid.UUID, p *domain.Parsed) error {
	var partner *uuid.UUID
	if id, err := uuid.Parse(p.PartnerID); err == nil && id != uuid.Nil {
		partner = &id
	}
	total, err := decimal.NewFromString(p.TotalAmount)
	if p.TotalAmount == "" || err != nil {
		p.Hints = append(p.Hints, "回单已收下，确认时不会自动收款")
		return nil
	}
	matches, err := s.Repo.MatchReceipts(ctx, tenant, partner, total)
	if err != nil {
		return err
	}
	p.Matches = matches
	p.Hints = append(p.Hints, "回单已收下，确认时不会自动收款")
	return nil
}

func (s *Service) near(ctx context.Context, tenant uuid.UUID, direction string, p domain.Parsed) (*Hit, error) {
	if p.InvoiceNo != "" {
		hit, err := s.Repo.FindInvoice(ctx, tenant, directionOr(p.Direction, direction), p.InvoiceNo)
		if err != nil || hit != nil {
			return hit, err
		}
	}
	if p.SellerTaxNo == "" || p.IssueDate == "" || p.TotalAmount == "" {
		return nil, nil
	}
	day, err := time.Parse("2006-01-02", p.IssueDate)
	if err != nil {
		return nil, nil
	}
	total, err := decimal.NewFromString(p.TotalAmount)
	if err != nil {
		return nil, nil
	}
	return s.Repo.FindNear(ctx, tenant, p.SellerTaxNo, day, total)
}
