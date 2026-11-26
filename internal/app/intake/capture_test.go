package intake

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/intake"
)

type mem struct {
	docs        map[uuid.UUID]*domain.Document
	fps         map[string]uuid.UUID
	refs        map[string]uuid.UUID
	invoice     *Hit
	bills       []domain.OpenBill
	seenPartner uuid.UUID
	seenDir     string
}

func newMem() *mem {
	return &mem{docs: map[uuid.UUID]*domain.Document{}, fps: map[string]uuid.UUID{}, refs: map[string]uuid.UUID{}}
}

func (m *mem) FindFingerprint(_ context.Context, _ uuid.UUID, kind, hash string) (*Hit, error) {
	id, ok := m.fps[kind+"/"+hash]
	if !ok {
		return nil, nil
	}
	doc := m.docs[id]
	return &Hit{DocumentID: id, Status: doc.Status, IssueDate: dayOf(doc), Booked: doc.Status == domain.StatusAccepted}, nil
}

func (m *mem) FindClientRef(_ context.Context, _ uuid.UUID, ref string) (*domain.Document, error) {
	id, ok := m.refs[ref]
	if !ok {
		return nil, nil
	}
	return m.docs[id], nil
}

func (m *mem) FindNear(_ context.Context, _ uuid.UUID, tax string, day time.Time, total decimal.Decimal) (*Hit, error) {
	for _, doc := range m.docs {
		if doc.Status == domain.StatusRejected || doc.SellerTaxNo != tax || doc.IssueDate == nil || doc.Total == nil {
			continue
		}
		if doc.IssueDate.Format("2006-01-02") == day.Format("2006-01-02") && doc.Total.Sub(total).Abs().LessThanOrEqual(decimal.RequireFromString("0.01")) {
			return &Hit{DocumentID: doc.ID, Status: doc.Status, IssueDate: dayOf(doc), Booked: doc.Status == domain.StatusAccepted}, nil
		}
	}
	return nil, nil
}

func (m *mem) FindInvoice(context.Context, uuid.UUID, string, string) (*Hit, error) {
	return m.invoice, nil
}
func (m *mem) SuggestPartner(context.Context, uuid.UUID, string) (uuid.UUID, string, error) {
	return uuid.Nil, "", nil
}
func (m *mem) ProductNames(context.Context, uuid.UUID) (map[string]uuid.UUID, error) {
	return map[string]uuid.UUID{"红叶石楠": uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")}, nil
}
func (m *mem) Insert(_ context.Context, doc *domain.Document, fps []domain.Fingerprint) error {
	m.docs[doc.ID] = doc
	if doc.ClientRef != "" {
		m.refs[doc.ClientRef] = doc.ID
	}
	for _, fp := range fps {
		m.fps[fp.Kind+"/"+fp.Hash] = doc.ID
	}
	return nil
}
func (m *mem) Get(_ context.Context, _, id uuid.UUID) (*domain.Document, error) {
	doc, ok := m.docs[id]
	if !ok {
		return nil, ErrNotFound
	}
	return doc, nil
}
func (m *mem) List(context.Context, uuid.UUID, *uuid.UUID, int, int) ([]*domain.Document, int, error) {
	return nil, 0, nil
}
func (m *mem) CountOpen(context.Context, uuid.UUID, *uuid.UUID) (int, error) { return 0, nil }
func (m *mem) Lock(_ context.Context, _, id, actor uuid.UUID, until time.Time) error {
	doc, ok := m.docs[id]
	if !ok {
		return ErrNotFound
	}
	if doc.LockBy != nil && *doc.LockBy != actor && doc.LockUntil != nil && doc.LockUntil.After(time.Now()) {
		return ErrLocked
	}
	doc.LockBy = &actor
	doc.LockUntil = &until
	return nil
}
func (m *mem) Reject(_ context.Context, _, id uuid.UUID, reason string) (*domain.Document, error) {
	doc := m.docs[id]
	doc.Status = domain.StatusRejected
	doc.RejectReason = reason
	for k, v := range m.fps {
		if v == id {
			delete(m.fps, k)
		}
	}
	return doc, nil
}
func (m *mem) MarkAccepted(_ context.Context, doc *domain.Document) error {
	m.docs[doc.ID] = doc
	return nil
}
func (m *mem) OpenBills(_ context.Context, _, partner uuid.UUID, direction string) ([]domain.OpenBill, error) {
	m.seenPartner, m.seenDir = partner, direction
	return m.bills, nil
}
func (m *mem) ApprovedLinks(context.Context, uuid.UUID, uuid.UUID, []uuid.UUID, decimal.Decimal) ([]BillLink, error) {
	return nil, nil
}
func (m *mem) MatchReceipts(context.Context, uuid.UUID, *uuid.UUID, decimal.Decimal) ([]domain.Match, error) {
	return []domain.Match{{BillID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", BillNo: "SL-1", Amount: "113.00"}}, nil
}

type reg struct{ n int }

func (r *reg) Register(context.Context, RegisterInput) (uuid.UUID, error) {
	r.n++
	return uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc"), nil
}

type drafts struct{ n int }

func (d *drafts) CreatePurchaseDraft(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, []DraftLine) (uuid.UUID, string, error) {
	d.n++
	return uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd"), "PO-1", nil
}

func TestCapture_SameQRAndFileOnce(t *testing.T) {
	repo := newMem()
	svc := &Service{Repo: repo, Blobs: memBlob{}, Now: func() time.Time { return time.Unix(0, 0).UTC() }}
	tenant, actor := uuid.New(), uuid.New()
	qr := "01,32,,24317000000012345678,113.00,20260315,ABCD,EE"
	first, err := svc.Capture(context.Background(), CaptureInput{
		TenantID: tenant, Actor: actor, Source: domain.SourceCamera, QRPayload: qr, Body: []byte("img-a"), ContentType: "image/jpeg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Parsed.MoneyOK || first.Parsed.TotalAmount != "113.00" || first.Status != domain.StatusNeedsReview {
		t.Fatalf("%+v", first.Parsed)
	}
	_, err = svc.Capture(context.Background(), CaptureInput{
		TenantID: tenant, Actor: actor, Source: domain.SourceFile, QRPayload: qr, Body: []byte("img-b"),
	})
	dup, ok := err.(*Duplicate)
	if !ok || dup.ExistingID != first.ID || dup.Code != CodeDuplicate {
		t.Fatalf("%v", err)
	}
	_, err = svc.Capture(context.Background(), CaptureInput{
		TenantID: tenant, Actor: actor, Source: domain.SourceFile, Body: []byte("img-a"), ClientRef: "dev-1",
	})
	if err == nil {
		t.Fatal("same bytes must not create a second row")
	}
}

func TestCapture_ClientRefReplay(t *testing.T) {
	repo := newMem()
	svc := &Service{Repo: repo, Blobs: memBlob{}}
	in := CaptureInput{TenantID: uuid.New(), Actor: uuid.New(), Source: domain.SourceFile, QRPayload: "01,32,,1,10.00,20260101,A,B", ClientRef: "abcd1234-1"}
	first, err := svc.Capture(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Capture(context.Background(), in)
	dup, ok := err.(*Duplicate)
	if !ok || dup.Code != CodeExists || dup.ExistingID != first.ID {
		t.Fatalf("%v", err)
	}
}

func TestAccept_MoneyAndSecondConfirm(t *testing.T) {
	repo := newMem()
	invoices := &reg{}
	drafts := &drafts{}
	svc := &Service{Repo: repo, Blobs: memBlob{}, Invoices: invoices, Drafts: drafts}
	doc, err := svc.Capture(context.Background(), CaptureInput{
		TenantID: uuid.New(), Actor: uuid.New(), Source: domain.SourceCamera,
		QRPayload: "01,32,,24317000000012345678,113.00,20260315,ABCD,EE",
	})
	if err != nil {
		t.Fatal(err)
	}
	reviewer := uuid.New()
	_, err = svc.Accept(context.Background(), AcceptInput{
		TenantID: doc.TenantID, Actor: reviewer, ID: doc.ID, Direction: "in",
		InvoiceNo: doc.Parsed.InvoiceNo, InvoiceType: "数电普票", PartnerID: uuid.New(),
		IssueDate: time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
		Excl:      decimal.RequireFromString("100"), Tax: decimal.RequireFromString("13"), Total: decimal.RequireFromString("112"),
	})
	if !errorIs(err, ErrMoney) {
		t.Fatalf("want money error, got %v", err)
	}
	if invoices.n != 0 {
		t.Fatal("mismatch must not register")
	}
	partner := uuid.New()
	res, err := svc.Accept(context.Background(), AcceptInput{
		TenantID: doc.TenantID, Actor: reviewer, ID: doc.ID, Direction: "in",
		InvoiceNo: "24317000000012345678", InvoiceType: "数电普票", PartnerID: partner,
		IssueDate: time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
		Excl:      decimal.RequireFromString("100"), Tax: decimal.RequireFromString("13"), Total: decimal.RequireFromString("113"),
		CreateDraft: true, Lines: []DraftLine{{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.RequireFromString("100")}},
	})
	if err != nil || invoices.n != 1 || drafts.n != 1 || res.DraftBillNo != "PO-1" {
		t.Fatalf("err=%v invoices=%d drafts=%d res=%+v", err, invoices.n, drafts.n, res)
	}
	again, err := svc.Accept(context.Background(), AcceptInput{TenantID: doc.TenantID, Actor: reviewer, ID: doc.ID})
	if err != nil || invoices.n != 1 || again.Document.Status != domain.StatusAccepted {
		t.Fatalf("second confirm err=%v n=%d", err, invoices.n)
	}
}

func TestAccept_ExistingInvoiceDoesNotRegisterAgain(t *testing.T) {
	repo := newMem()
	existing := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	invoices := &reg{}
	svc := &Service{Repo: repo, Invoices: invoices}
	doc, err := svc.Capture(context.Background(), CaptureInput{
		TenantID: uuid.New(), Actor: uuid.New(), Source: domain.SourceFile,
		QRPayload: "01,32,,24317000000012345678,113.00,20260315,A,B",
	})
	if err != nil {
		t.Fatal(err)
	}
	repo.invoice = &Hit{InvoiceID: existing, Status: "issued", IssueDate: "2026-03-15", Booked: true}
	res, err := svc.Accept(context.Background(), AcceptInput{
		TenantID: doc.TenantID, Actor: uuid.New(), ID: doc.ID, Direction: "in",
		InvoiceNo: "24317000000012345678", InvoiceType: "数电普票", PartnerID: uuid.New(),
		IssueDate: time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
		Excl:      decimal.RequireFromString("100"), Tax: decimal.RequireFromString("13"), Total: decimal.RequireFromString("113"),
	})
	if err != nil || invoices.n != 0 || res.Document.InvoiceID == nil || *res.Document.InvoiceID != existing {
		t.Fatalf("err=%v n=%d res=%+v", err, invoices.n, res)
	}
}

func TestCapture_DuplicateInvoiceCarriesItsNumber(t *testing.T) {
	repo := newMem()
	svc := &Service{Repo: repo}
	tenant := uuid.New()
	_, err := svc.Capture(context.Background(), CaptureInput{
		TenantID: tenant, Actor: uuid.New(), Source: domain.SourceFile,
		QRPayload: "01,32,,24317000000012345678,113.00,20260315,AAAA,BB",
	})
	if err != nil {
		t.Fatal(err)
	}
	repo.invoice = &Hit{InvoiceID: uuid.New(), Status: "issued", IssueDate: "2026-03-15", InvoiceNo: "24317000000012345678", Booked: true}
	_, err = svc.Capture(context.Background(), CaptureInput{
		TenantID: tenant, Actor: uuid.New(), Source: domain.SourceFile,
		QRPayload: "01,32,,24317000000012345678,113.00,20260315,CCCC,DD",
	})
	var dup *domain.Duplicate
	if !errors.As(err, &dup) || dup.Kind != "invoice" || dup.InvoiceNo != "24317000000012345678" {
		t.Fatalf("%v", err)
	}
}

func TestAccept_RejectsUnknownInvoiceType(t *testing.T) {
	repo := newMem()
	invoices := &reg{}
	svc := &Service{Repo: repo, Invoices: invoices}
	doc, err := svc.Capture(context.Background(), CaptureInput{
		TenantID: uuid.New(), Actor: uuid.New(), Source: domain.SourceFile,
		QRPayload: "01,32,,24317000000012345678,113.00,20260315,A,B",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Accept(context.Background(), AcceptInput{
		TenantID: doc.TenantID, Actor: uuid.New(), ID: doc.ID, Direction: "in",
		InvoiceNo: "24317000000012345678", InvoiceType: "增值税发票", PartnerID: uuid.New(),
		IssueDate: time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
		Excl:      decimal.RequireFromString("100"), Tax: decimal.RequireFromString("13"), Total: decimal.RequireFromString("113"),
	})
	if !errorIs(err, ErrValidation) || invoices.n != 0 {
		t.Fatalf("err=%v registered=%d", err, invoices.n)
	}
}

func TestAccept_LockConflict(t *testing.T) {
	repo := newMem()
	svc := &Service{Repo: repo, Blobs: memBlob{}}
	doc, err := svc.Capture(context.Background(), CaptureInput{
		TenantID: uuid.New(), Actor: uuid.New(), Source: domain.SourceFile, QRPayload: "01,04,1,2,10.00,20260101,A,B",
	})
	if err != nil {
		t.Fatal(err)
	}
	holder := uuid.New()
	until := time.Now().Add(time.Minute)
	doc.LockBy = &holder
	doc.LockUntil = &until
	err = svc.Lock(context.Background(), doc.TenantID, doc.ID, uuid.New())
	if !errorIs(err, ErrLocked) {
		t.Fatalf("%v", err)
	}
}

func TestReceipt_DoesNotDraftOrPay(t *testing.T) {
	repo := newMem()
	drafts := &drafts{}
	invoices := &reg{}
	svc := &Service{Repo: repo, Blobs: memBlob{}, Drafts: drafts, Invoices: invoices}
	doc, err := svc.Capture(context.Background(), CaptureInput{
		TenantID: uuid.New(), Actor: uuid.New(), Source: domain.SourceReceipt,
		Body: []byte("slip"), ContentType: "image/jpeg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Parsed.Hints) == 0 {
		t.Fatal("receipt must say it will not pay")
	}
	match := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	res, err := svc.Accept(context.Background(), AcceptInput{
		TenantID: doc.TenantID, Actor: uuid.New(), ID: doc.ID, MatchBillID: &match,
	})
	if err != nil || invoices.n != 0 || drafts.n != 0 || res.Warning == "" {
		t.Fatalf("err=%v inv=%d draft=%d %+v", err, invoices.n, drafts.n, res)
	}
}

func TestCapture_PaperWithoutVisionStaysBlank(t *testing.T) {
	repo := newMem()
	svc := &Service{Repo: repo, Blobs: memBlob{}}
	doc, err := svc.Capture(context.Background(), CaptureInput{
		TenantID: uuid.New(), Actor: uuid.New(), Source: domain.SourceFile, Body: []byte("paper"), ContentType: "image/jpeg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Parsed.TotalAmount != "" || doc.Parsed.MoneyOK || doc.Status != domain.StatusReceived || len(doc.Parsed.Hints) == 0 {
		t.Fatalf("%s %+v", doc.Status, doc.Parsed)
	}
}

func TestVoice_FillsNamesNotPrices(t *testing.T) {
	repo := newMem()
	svc := &Service{Repo: repo, Blobs: memBlob{}, Speech: script("红叶石楠 2")}
	doc, err := svc.Capture(context.Background(), CaptureInput{
		TenantID: uuid.New(), Actor: uuid.New(), Source: domain.SourceVoice, Body: []byte("audio"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Parsed.Transcript != "红叶石楠 2" || len(doc.Parsed.Lines) != 1 || doc.Parsed.Lines[0].UnitPrice != "" {
		t.Fatalf("%+v", doc.Parsed)
	}
	if doc.Parsed.TotalAmount != "" {
		t.Fatal("speech must not fill money")
	}
}

type script string

func (s script) Transcribe(context.Context, string, []byte) (string, error) { return string(s), nil }

type memBlob struct{}

func (memBlob) Put(context.Context, string, []byte, string) error { return nil }
func (memBlob) Open(context.Context, string) (io.ReadCloser, string, error) {
	return io.NopCloser(bytes.NewReader(nil)), "", nil
}

func errorIs(err, target error) bool { return errors.Is(err, target) }

func TestCapture_SniffsQRInsidePDF(t *testing.T) {
	repo := newMem()
	svc := &Service{Repo: repo, Blobs: memBlob{}}
	body := []byte("%PDF 01,32,,24317000000012345678,113.00,20260315,ABCD trailer")
	doc, err := svc.Capture(context.Background(), CaptureInput{
		TenantID: uuid.New(), Actor: uuid.New(), Source: domain.SourceFile, ContentType: "application/pdf", Body: body,
	})
	if err != nil {
		t.Fatal(err)
	}
	if doc.InvoiceNo != "24317000000012345678" || doc.Parsed.InvoiceType != "数电普票" || doc.Parsed.MoneyOK {
		t.Fatalf("%s %+v", doc.InvoiceNo, doc.Parsed)
	}
	_, err = svc.Capture(context.Background(), CaptureInput{
		TenantID: doc.TenantID, Actor: uuid.New(), Source: domain.SourceFile, ContentType: "application/pdf",
		Body: []byte("different bytes 01,32,,24317000000012345678,113.00,20260315,ABCD end"),
	})
	if !errorIs(err, ErrDuplicate) {
		t.Fatalf("same QR inside another file should be one row: %v", err)
	}
}

func TestGet_UsesThePartnerTheReviewerPicked(t *testing.T) {
	repo := newMem()
	svc := &Service{Repo: repo, Blobs: memBlob{}}
	tenant := uuid.New()
	doc, err := svc.Capture(context.Background(), CaptureInput{
		TenantID: tenant, Actor: uuid.New(), Source: domain.SourceFile, ContentType: "application/pdf",
		Body: []byte("%PDF 01,32,,24317000000012345678,113.00,20260315,ABCD"),
	})
	if err != nil {
		t.Fatal(err)
	}
	partner := uuid.New()
	repo.bills = []domain.OpenBill{{ID: uuid.New(), BillNo: "PO-9", Status: 2, Total: decimal.RequireFromString("100")}}
	got, err := svc.Get(context.Background(), tenant, doc.ID, nil, &partner, domain.DirectionOut, true)
	if err != nil {
		t.Fatal(err)
	}
	if repo.seenPartner != partner || repo.seenDir != domain.DirectionOut || len(got.OpenBills) != 1 || got.OpenBills[0].BillNo != "PO-9" {
		t.Fatalf("partner %s dir %s bills %+v", repo.seenPartner, repo.seenDir, got.OpenBills)
	}
	cleared, err := svc.Get(context.Background(), tenant, doc.ID, nil, nil, domain.DirectionIn, true)
	if err != nil || len(cleared.OpenBills) != 0 {
		t.Fatalf("cleared partner should not keep old bills: %v %+v", err, cleared)
	}
	rc, ct, err := svc.OpenFile(context.Background(), tenant, doc.ID, nil)
	if err != nil || ct != "application/pdf" {
		t.Fatalf("%v %s", err, ct)
	}
	rc.Close()
}
