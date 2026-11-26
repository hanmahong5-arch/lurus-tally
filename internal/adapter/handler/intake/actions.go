package intake

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/intake"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/permission"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

func (h *Handler) Lock(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.api.Lock(c.Request.Context(), middleware.GetTenantID(c), id, middleware.ActorUUID(c)); err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type acceptBody struct {
	Direction     string   `json:"direction"`
	InvoiceNo     string   `json:"invoice_no"`
	InvoiceCode   string   `json:"invoice_code"`
	InvoiceType   string   `json:"invoice_type"`
	PartnerID     string   `json:"partner_id"`
	IssueDate     string   `json:"issue_date"`
	AmountExclTax string   `json:"amount_excl_tax"`
	TaxAmount     string   `json:"tax_amount"`
	TotalAmount   string   `json:"total_amount"`
	LinkBillIDs   []string `json:"link_bill_ids"`
	CreateDraft   bool     `json:"create_draft"`
	MatchBillID   string   `json:"match_bill_id"`
	Lines         []struct {
		ProductID string `json:"product_id"`
		Qty       string `json:"qty"`
		UnitPrice string `json:"unit_price"`
	} `json:"lines"`
}

func (h *Handler) Accept(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	var body acceptBody
	if err := c.ShouldBindJSON(&body); err != nil {
		httperr.Write(c, httperr.BadRequest("validation_error", "请按票面填写后再确认", ""))
		return
	}
	in, err := body.toInput(middleware.GetTenantID(c), middleware.ActorUUID(c), id)
	if err != nil {
		httperr.Write(c, httperr.BadRequest("validation_error", err.Error(), ""))
		return
	}
	res, err := h.api.Accept(c.Request.Context(), in)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"document":      toDTO(res.Document, middleware.HasPerm(c, permission.CostView), res.DraftBillNo),
		"draft_bill_no": res.DraftBillNo,
		"warning":       res.Warning,
	})
}

func (h *Handler) Reject(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)
	doc, err := h.api.Reject(c.Request.Context(), middleware.GetTenantID(c), id, middleware.ActorUUID(c), strings.TrimSpace(body.Reason))
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, toDTO(doc, middleware.HasPerm(c, permission.CostView), ""))
}

func (b acceptBody) toInput(tenant, actor, id uuid.UUID) (domain.AcceptInput, error) {
	in := domain.AcceptInput{
		TenantID: tenant, Actor: actor, ID: id, Direction: b.Direction,
		InvoiceNo: strings.TrimSpace(b.InvoiceNo), InvoiceCode: strings.TrimSpace(b.InvoiceCode),
		InvoiceType: strings.TrimSpace(b.InvoiceType), CreateDraft: b.CreateDraft,
	}
	if b.PartnerID != "" {
		pid, err := uuid.Parse(b.PartnerID)
		if err != nil {
			return in, err
		}
		in.PartnerID = pid
	}
	if b.IssueDate != "" {
		day, err := time.Parse("2006-01-02", b.IssueDate)
		if err != nil {
			return in, err
		}
		in.IssueDate = day
	}
	var err error
	if b.AmountExclTax != "" {
		if in.Excl, err = decimal.NewFromString(b.AmountExclTax); err != nil {
			return in, err
		}
	}
	if b.TaxAmount != "" {
		if in.Tax, err = decimal.NewFromString(b.TaxAmount); err != nil {
			return in, err
		}
	}
	if b.TotalAmount != "" {
		if in.Total, err = decimal.NewFromString(b.TotalAmount); err != nil {
			return in, err
		}
	}
	for _, id := range b.LinkBillIDs {
		u, err := uuid.Parse(id)
		if err != nil {
			return in, err
		}
		in.LinkBillIDs = append(in.LinkBillIDs, u)
	}
	if b.MatchBillID != "" {
		u, err := uuid.Parse(b.MatchBillID)
		if err != nil {
			return in, err
		}
		in.MatchBillID = &u
	}
	for _, ln := range b.Lines {
		pid, err := uuid.Parse(ln.ProductID)
		if err != nil {
			return in, err
		}
		qty, err := decimal.NewFromString(ln.Qty)
		if err != nil {
			return in, err
		}
		price, err := decimal.NewFromString(ln.UnitPrice)
		if err != nil {
			return in, err
		}
		in.Lines = append(in.Lines, domain.DraftLine{ProductID: pid, Qty: qty, UnitPrice: price})
	}
	return in, nil
}

type docDTO struct {
	ID           string        `json:"id"`
	Source       string        `json:"source"`
	Direction    string        `json:"direction"`
	Status       string        `json:"status"`
	InvoiceNo    string        `json:"invoice_no,omitempty"`
	IssueDate    string        `json:"issue_date,omitempty"`
	Parsed       domain.Parsed `json:"parsed"`
	InvoiceID    string        `json:"invoice_id,omitempty"`
	DraftBillID  string        `json:"draft_bill_id,omitempty"`
	DraftBillNo  string        `json:"draft_bill_no,omitempty"`
	RejectReason string        `json:"reject_reason,omitempty"`
	CreatedBy    string        `json:"created_by,omitempty"`
	OpenBills    []openBillDTO `json:"open_bills,omitempty"`
	HasFile      bool          `json:"has_file"`
	ContentType  string        `json:"content_type,omitempty"`
}

type openBillDTO struct {
	ID        string `json:"id"`
	BillNo    string `json:"bill_no"`
	BillDate  string `json:"bill_date,omitempty"`
	Status    int16  `json:"status"`
	Total     string `json:"total,omitempty"`
	Remaining string `json:"remaining,omitempty"`
}

func toDTO(doc *domain.Document, cost bool, draftNo string) docDTO {
	if doc == nil {
		return docDTO{}
	}
	dto := docDTO{
		ID: doc.ID.String(), Source: doc.Source, Direction: doc.Direction, Status: doc.Status,
		InvoiceNo: doc.InvoiceNo, Parsed: doc.Parsed, RejectReason: doc.RejectReason,
		CreatedBy: doc.CreatedBy.String(), DraftBillNo: draftNo, HasFile: doc.ObjectKey != "",
		ContentType: doc.ContentType,
	}
	if doc.IssueDate != nil {
		dto.IssueDate = doc.IssueDate.Format("2006-01-02")
	}
	if doc.InvoiceID != nil {
		dto.InvoiceID = doc.InvoiceID.String()
	}
	if doc.DraftBillID != nil {
		dto.DraftBillID = doc.DraftBillID.String()
	}
	for _, b := range doc.OpenBills {
		row := openBillDTO{ID: b.ID.String(), BillNo: b.BillNo, Status: b.Status, BillDate: b.BillDate}
		if cost {
			row.Total = b.Total.StringFixed(2)
			row.Remaining = b.Remaining.StringFixed(2)
		}
		dto.OpenBills = append(dto.OpenBills, row)
	}
	if !cost {
		dto.Parsed.AmountExclTax, dto.Parsed.TaxAmount, dto.Parsed.TotalAmount = "", "", ""
		dto.Parsed.MoneyOK = false
		for i := range dto.Parsed.Lines {
			dto.Parsed.Lines[i].UnitPrice = ""
		}
		for i := range dto.Parsed.Matches {
			dto.Parsed.Matches[i].Amount = ""
		}
	}
	return dto
}

func writeErr(c *gin.Context, err error) {
	var dup *domain.Duplicate
	switch {
	case errors.As(err, &dup):
		c.JSON(http.StatusConflict, gin.H{
			"error": dup.Code, "message": dup.Message,
			"existing_id": dup.ExistingID.String(), "existing_kind": dup.Kind,
			"issue_date": dup.IssueDate, "invoice_no": dup.InvoiceNo,
		})
	case errors.Is(err, domain.ErrLocked):
		httperr.Write(c, httperr.Conflict(domain.CodeLocked, "这张收件正在被其他人核对"))
	case errors.Is(err, domain.ErrNotFound):
		httperr.Write(c, httperr.NotFound("intake_not_found", "找不到这张收件"))
	case errors.Is(err, domain.ErrMoney):
		httperr.Write(c, httperr.New(http.StatusUnprocessableEntity, domain.CodeMoney, clientText(err), ""))
	case errors.Is(err, domain.ErrValidation):
		httperr.Write(c, httperr.BadRequest("validation_error", clientText(err), ""))
	case errors.Is(err, domain.ErrConflict):
		httperr.Write(c, httperr.Conflict("intake_conflict", clientText(err)))
	case errors.Is(err, domain.ErrDuplicate):
		httperr.Write(c, httperr.Conflict(domain.CodeDuplicate, "这张票已经收过"))
	default:
		httperr.WriteInternal(c, err)
	}
}

func clientText(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		return s[i+2:]
	}
	return s
}
