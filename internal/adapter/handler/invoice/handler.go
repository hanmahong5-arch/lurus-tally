// Package invoice implements the Gin HTTP handlers for the invoice registry REST API.
package invoice

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appinv "github.com/hanmahong5-arch/lurus-tally/internal/app/invoice"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/invoice"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// listLocation interprets date-only filters as Asia/Shanghai wall-clock days (same
// as the bill list). A fixed zone avoids LoadLocation, which scratch images lack.
var listLocation = time.FixedZone("CST", 8*3600)

const (
	dateLayout      = "2006-01-02"
	maxKeywordRunes = 64
)

// Handler groups all invoice Gin handlers.
type Handler struct {
	create *appinv.CreateUseCase
	get    *appinv.GetUseCase
	list   *appinv.ListUseCase
	void   *appinv.VoidUseCase
	red    *appinv.RedUseCase
}

// New constructs a Handler wired to the provided use cases.
func New(
	create *appinv.CreateUseCase,
	get *appinv.GetUseCase,
	list *appinv.ListUseCase,
	void *appinv.VoidUseCase,
	red *appinv.RedUseCase,
) *Handler {
	return &Handler{create: create, get: get, list: list, void: void, red: red}
}

// RegisterRoutes registers all invoice routes on the provided router group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/invoices", h.List)
	rg.POST("/invoices", h.Create)
	rg.GET("/invoices/:id", h.GetByID)
	rg.POST("/invoices/:id/void", h.Void)
	rg.POST("/invoices/:id/red", h.Red)
}

// LinkDTO is the wire form of an invoice-bill link.
type LinkDTO struct {
	BillID string `json:"bill_id"`
	BillNo string `json:"bill_no"`
	Amount string `json:"amount"`
}

// InvoiceDTO is the wire representation of an invoice. Amounts are decimal strings.
type InvoiceDTO struct {
	ID            string    `json:"id"`
	Direction     string    `json:"direction"`
	InvoiceNo     string    `json:"invoice_no"`
	InvoiceCode   string    `json:"invoice_code"`
	InvoiceType   string    `json:"invoice_type"`
	PartnerID     string    `json:"partner_id"`
	PartnerName   string    `json:"partner_name"`
	IssueDate     string    `json:"issue_date"`
	AmountExclTax string    `json:"amount_excl_tax"`
	TaxAmount     string    `json:"tax_amount"`
	TotalAmount   string    `json:"total_amount"`
	Status        string    `json:"status"`
	RedOfID       *string   `json:"red_of_id"`
	Remark        string    `json:"remark"`
	CreatedAt     string    `json:"created_at"`
	UpdatedAt     string    `json:"updated_at"`
	Links         []LinkDTO `json:"links"`
}

type linkRequest struct {
	BillID uuid.UUID       `json:"bill_id"`
	Amount decimal.Decimal `json:"amount"`
}

type createRequest struct {
	Direction     string          `json:"direction"`
	InvoiceNo     string          `json:"invoice_no"`
	InvoiceCode   string          `json:"invoice_code"`
	InvoiceType   string          `json:"invoice_type"`
	PartnerID     uuid.UUID       `json:"partner_id"`
	IssueDate     string          `json:"issue_date"`
	AmountExclTax decimal.Decimal `json:"amount_excl_tax"`
	TaxAmount     decimal.Decimal `json:"tax_amount"`
	Remark        string          `json:"remark"`
	Links         []linkRequest   `json:"links"`
}

type redRequest struct {
	InvoiceNo   string `json:"invoice_no"`
	InvoiceCode string `json:"invoice_code"`
	IssueDate   string `json:"issue_date"`
	Remark      string `json:"remark"`
}

func badRequest(c *gin.Context, msg string) {
	httperr.Write(c, httperr.BadRequest("validation_error", msg, ""))
}

func unauthorized(c *gin.Context) {
	c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "message": "tenant_id required"})
}

// writeErr maps use-case failures to the HTTP contract: 400 invalid input, 404 not
// found, 409 duplicate number / state conflict, 422 business-rule rejection; the
// rest is a leak-free 5xx.
func writeErr(c *gin.Context, err error) {
	msg, _ := appinv.MessageOf(err)
	switch {
	case errors.Is(err, appinv.ErrInvalidInput):
		httperr.Write(c, httperr.BadRequest("validation_error", msg, ""))
	case errors.Is(err, appinv.ErrNotFound):
		httperr.Write(c, httperr.NotFound("invoice_not_found", "找不到这张发票"))
	case errors.Is(err, appinv.ErrDuplicateNo):
		httperr.Write(c, httperr.Conflict("duplicate_invoice_no", "这个发票号码已经登记过了"))
	case errors.Is(err, appinv.ErrConflict):
		httperr.Write(c, httperr.Conflict("invoice_state_conflict", msg))
	case errors.Is(err, appinv.ErrRule):
		httperr.Write(c, httperr.New(http.StatusUnprocessableEntity, "invoice_rule_violation", msg, ""))
	default:
		httperr.WriteInternal(c, err)
	}
}

func parseDate(s string) (time.Time, error) {
	t, err := time.ParseInLocation(dateLayout, s, listLocation)
	if err != nil {
		return time.Time{}, err
	}
	// Keep the calendar day, stored as midnight UTC so the DATE round-trips unchanged.
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
}

func resolveCreatedBy(c *gin.Context) *uuid.UUID {
	id := middleware.ActorUUID(c)
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// Create handles POST /api/v1/invoices
func (h *Handler) Create(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		unauthorized(c)
		return
	}
	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "提交的内容格式不正确，请检查金额、日期和单据后重试")
		return
	}
	issue, err := parseDate(req.IssueDate)
	if err != nil {
		badRequest(c, "开票日期格式不正确，应为 年-月-日")
		return
	}
	links := make([]domain.Link, len(req.Links))
	for i, l := range req.Links {
		links[i] = domain.Link{BillID: l.BillID, Amount: l.Amount}
	}
	inv, err := h.create.Execute(c.Request.Context(), domain.CreateInput{
		TenantID:      tenantID,
		Direction:     domain.Direction(req.Direction),
		InvoiceNo:     req.InvoiceNo,
		InvoiceCode:   req.InvoiceCode,
		InvoiceType:   req.InvoiceType,
		PartnerID:     req.PartnerID,
		IssueDate:     issue,
		AmountExclTax: req.AmountExclTax,
		TaxAmount:     req.TaxAmount,
		Remark:        req.Remark,
		CreatedBy:     resolveCreatedBy(c),
		Links:         links,
	})
	if err != nil {
		writeErr(c, err)
		return
	}
	c.Header("Location", "/api/v1/invoices/"+inv.ID.String())
	c.JSON(http.StatusCreated, toDTO(inv))
}

// parseListFilter fills f from query; any invalid value yields a user-facing Chinese message.
func parseListFilter(c *gin.Context, f *domain.ListFilter) error {
	q := c.Request.URL.Query()
	if s := q.Get("direction"); s != "" {
		if !domain.ValidDirection(domain.Direction(s)) {
			return errors.New("发票方向不正确，应为销项或进项")
		}
		f.Direction = domain.Direction(s)
	}
	if s := q.Get("status"); s != "" {
		if !domain.ValidStatus(domain.Status(s)) {
			return errors.New("发票状态不正确")
		}
		f.Status = domain.Status(s)
	}
	if s := q.Get("partner_id"); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			return errors.New("往来单位不正确")
		}
		f.PartnerID = &id
	}
	if s := q.Get("bill_id"); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			return errors.New("单据不正确")
		}
		f.BillID = &id
	}
	var from, to *time.Time
	if s := q.Get("date_from"); s != "" {
		t, err := time.ParseInLocation(dateLayout, s, listLocation)
		if err != nil {
			return errors.New("开始日期格式不正确，应为 年-月-日")
		}
		from = &t
	}
	if s := q.Get("date_to"); s != "" {
		t, err := time.ParseInLocation(dateLayout, s, listLocation)
		if err != nil {
			return errors.New("结束日期格式不正确，应为 年-月-日")
		}
		next := t.AddDate(0, 0, 1) // inclusive last day -> exclusive next day
		to = &next
	}
	if from != nil && to != nil && !from.Before(*to) {
		return errors.New("开始日期不能晚于结束日期")
	}
	f.DateFrom, f.DateTo = from, to
	if s := strings.TrimSpace(q.Get("q")); s != "" {
		if utf8.RuneCountInString(s) > maxKeywordRunes {
			return errors.New("搜索关键字太长，最多 64 个字")
		}
		f.Keyword = s
	}
	if s := q.Get("offset"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return errors.New("翻页参数不正确")
		}
		f.Offset = n
	}
	return nil
}

// List handles GET /api/v1/invoices
func (h *Handler) List(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		unauthorized(c)
		return
	}
	f := domain.ListFilter{
		TenantID: tenantID,
		Limit:    middleware.ParseLimitQuery(c, "limit", 20, middleware.DefaultMaxPageLimit),
	}
	if err := parseListFilter(c, &f); err != nil {
		badRequest(c, err.Error())
		return
	}
	out, err := h.list.Execute(c.Request.Context(), f)
	if err != nil {
		writeErr(c, err)
		return
	}
	items := make([]*InvoiceDTO, 0, len(out.Items))
	for _, inv := range out.Items {
		items = append(items, toDTO(inv))
	}
	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"total": out.Total,
		"summary": gin.H{
			"count":           out.Summary.Count,
			"amount_excl_tax": out.Summary.AmountExclTax.String(),
			"tax_amount":      out.Summary.TaxAmount.String(),
			"total_amount":    out.Summary.TotalAmount.String(),
		},
	})
}

func parseID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, "发票编号不正确")
		return uuid.Nil, false
	}
	return id, true
}

// GetByID handles GET /api/v1/invoices/:id
func (h *Handler) GetByID(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		unauthorized(c)
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	inv, err := h.get.Execute(c.Request.Context(), tenantID, id)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, toDTO(inv))
}

// Void handles POST /api/v1/invoices/:id/void
func (h *Handler) Void(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		unauthorized(c)
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	inv, err := h.void.Execute(c.Request.Context(), tenantID, id)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, toDTO(inv))
}

// Red handles POST /api/v1/invoices/:id/red: registers the red-letter invoice and
// marks the original red. The response is the new red-letter invoice.
func (h *Handler) Red(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		unauthorized(c)
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req redRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "提交的内容格式不正确，请检查后重试")
		return
	}
	issue := time.Now().In(listLocation)
	issue = time.Date(issue.Year(), issue.Month(), issue.Day(), 0, 0, 0, 0, time.UTC)
	if req.IssueDate != "" {
		var err error
		if issue, err = parseDate(req.IssueDate); err != nil {
			badRequest(c, "开票日期格式不正确，应为 年-月-日")
			return
		}
	}
	red, err := h.red.Execute(c.Request.Context(), appinv.RedInput{
		TenantID:    tenantID,
		ID:          id,
		InvoiceNo:   req.InvoiceNo,
		InvoiceCode: req.InvoiceCode,
		IssueDate:   issue,
		Remark:      req.Remark,
		CreatedBy:   resolveCreatedBy(c),
	})
	if err != nil {
		writeErr(c, err)
		return
	}
	c.Header("Location", "/api/v1/invoices/"+red.ID.String())
	c.JSON(http.StatusCreated, toDTO(red))
}

func toDTO(inv *domain.Invoice) *InvoiceDTO {
	dto := &InvoiceDTO{
		ID:            inv.ID.String(),
		Direction:     string(inv.Direction),
		InvoiceNo:     inv.InvoiceNo,
		InvoiceCode:   inv.InvoiceCode,
		InvoiceType:   inv.InvoiceType,
		PartnerID:     inv.PartnerID.String(),
		PartnerName:   inv.PartnerName,
		IssueDate:     inv.IssueDate.Format(dateLayout),
		AmountExclTax: inv.AmountExclTax.String(),
		TaxAmount:     inv.TaxAmount.String(),
		TotalAmount:   inv.TotalAmount.String(),
		Status:        string(inv.Status),
		Remark:        inv.Remark,
		CreatedAt:     inv.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     inv.UpdatedAt.Format(time.RFC3339),
		Links:         make([]LinkDTO, 0, len(inv.Links)),
	}
	if inv.RedOfID != nil {
		s := inv.RedOfID.String()
		dto.RedOfID = &s
	}
	for _, l := range inv.Links {
		dto.Links = append(dto.Links, LinkDTO{BillID: l.BillID.String(), BillNo: l.BillNo, Amount: l.Amount.String()})
	}
	return dto
}
