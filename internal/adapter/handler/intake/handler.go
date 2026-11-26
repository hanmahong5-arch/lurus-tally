// Package intake is the HTTP inbox: capture, review, confirm.
package intake

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/intake"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/permission"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// API is the inbox service the handler calls.
type API interface {
	Capture(ctx context.Context, in domain.CaptureInput) (*domain.Document, error)
	List(ctx context.Context, tenant uuid.UUID, own *uuid.UUID, limit, offset int) ([]*domain.Document, int, error)
	CountOpen(ctx context.Context, tenant uuid.UUID, own *uuid.UUID) (int, error)
	Get(ctx context.Context, tenant, id uuid.UUID, own, partner *uuid.UUID, direction string, choosePartner bool) (*domain.Document, error)
	OpenFile(ctx context.Context, tenant, id uuid.UUID, own *uuid.UUID) (io.ReadCloser, string, error)
	Lock(ctx context.Context, tenant, id, actor uuid.UUID) error
	Accept(ctx context.Context, in domain.AcceptInput) (*domain.AcceptResult, error)
	Reject(ctx context.Context, tenant, id, actor uuid.UUID, reason string) (*domain.Document, error)
}

// Handler serves /api/v1/intake.
type Handler struct{ api API }

// New constructs a handler. A nil API answers 501 so the route table can still be probed.
func New(api API) *Handler { return &Handler{api: api} }

// RegisterRoutes mounts the inbox on the /api/v1 group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/intake", h.Capture)
	rg.GET("/intake", h.List)
	rg.GET("/intake/count", h.Count)
	rg.GET("/intake/:id", h.Get)
	rg.GET("/intake/:id/file", h.File)
	rg.POST("/intake/:id/lock", h.Lock)
	rg.POST("/intake/:id/accept", h.Accept)
	rg.POST("/intake/:id/reject", h.Reject)
}

func (h *Handler) ready(c *gin.Context) bool {
	if h != nil && h.api != nil {
		return true
	}
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not_implemented", "message": "收件还没启用"})
	return false
}

func (h *Handler) Capture(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	tenant, actor := middleware.GetTenantID(c), middleware.ActorUUID(c)
	in, err := readCapture(c, tenant, actor)
	if err != nil {
		httperr.Write(c, httperr.BadRequest("validation_error", err.Error(), ""))
		return
	}
	doc, err := h.api.Capture(c.Request.Context(), in)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, toDTO(doc, middleware.HasPerm(c, permission.CostView), ""))
}

type captureBody struct {
	Source      string `json:"source"`
	Direction   string `json:"direction"`
	ClientRef   string `json:"client_ref"`
	QRPayload   string `json:"qr_payload"`
	Transcript  string `json:"transcript"`
	ContentType string `json:"content_type"`
	FileBase64  string `json:"file_base64"`
}

func readCapture(c *gin.Context, tenant, actor uuid.UUID) (domain.CaptureInput, error) {
	in := domain.CaptureInput{TenantID: tenant, Actor: actor, Source: domain.SourceFile, Direction: domain.DirectionIn}
	ct := c.ContentType()
	if strings.Contains(ct, "multipart/form-data") {
		if err := c.Request.ParseMultipartForm(8 << 20); err != nil {
			return in, err
		}
		in.Source = formOr(c, "source", in.Source)
		in.Direction = formOr(c, "direction", in.Direction)
		in.ClientRef = c.PostForm("client_ref")
		in.QRPayload = c.PostForm("qr_payload")
		in.Transcript = c.PostForm("transcript")
		if f, hdr, err := c.Request.FormFile("file"); err == nil {
			defer f.Close()
			body, err := io.ReadAll(io.LimitReader(f, 8<<20))
			if err != nil {
				return in, err
			}
			in.Body = body
			in.ContentType = hdr.Header.Get("Content-Type")
		}
		return in, nil
	}
	var body captureBody
	if err := c.ShouldBindJSON(&body); err != nil {
		return in, err
	}
	if body.Source != "" {
		in.Source = body.Source
	}
	if body.Direction != "" {
		in.Direction = body.Direction
	}
	in.ClientRef, in.QRPayload, in.Transcript, in.ContentType = body.ClientRef, body.QRPayload, body.Transcript, body.ContentType
	if body.FileBase64 != "" {
		raw, err := base64.StdEncoding.DecodeString(body.FileBase64)
		if err != nil {
			return in, err
		}
		in.Body = raw
	}
	return in, nil
}

func formOr(c *gin.Context, key, fallback string) string {
	if v := c.PostForm(key); v != "" {
		return v
	}
	return fallback
}

func (h *Handler) List(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	tenant := middleware.GetTenantID(c)
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	items, total, err := h.api.List(c.Request.Context(), tenant, ownFilter(c), limit, offset)
	if err != nil {
		writeErr(c, err)
		return
	}
	cost := middleware.HasPerm(c, permission.CostView)
	out := make([]docDTO, 0, len(items))
	for _, it := range items {
		out = append(out, toDTO(it, cost, ""))
	}
	c.JSON(http.StatusOK, gin.H{"items": out, "total": total})
}

func (h *Handler) Count(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	n, err := h.api.CountOpen(c.Request.Context(), middleware.GetTenantID(c), ownFilter(c))
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"count": n})
}

func (h *Handler) Get(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	partner, direction, choose, err := reviewQuery(c)
	if err != nil {
		httperr.Write(c, httperr.BadRequest("validation_error", "往来单位编号不正确", ""))
		return
	}
	doc, err := h.api.Get(c.Request.Context(), middleware.GetTenantID(c), id, ownFilter(c), partner, direction, choose)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, toDTO(doc, middleware.HasPerm(c, permission.CostView), ""))
}

func (h *Handler) File(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	rc, ct, err := h.api.OpenFile(c.Request.Context(), middleware.GetTenantID(c), id, ownFilter(c))
	if err != nil {
		writeErr(c, err)
		return
	}
	defer rc.Close()
	body, err := io.ReadAll(io.LimitReader(rc, 8<<20))
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	if ct == "" {
		ct = "application/octet-stream"
	}
	c.Data(http.StatusOK, ct, body)
}

func ownFilter(c *gin.Context) *uuid.UUID {
	if middleware.HasPerm(c, permission.IntakeAccept) {
		return nil
	}
	id := middleware.ActorUUID(c)
	return &id
}

func reviewQuery(c *gin.Context) (*uuid.UUID, string, bool, error) {
	direction := c.Query("direction")
	raw, ok := c.GetQuery("partner_id")
	if !ok {
		return nil, direction, false, nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, direction, true, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, "", false, err
	}
	return &id, direction, true, nil
}

func pathID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httperr.Write(c, httperr.BadRequest("validation_error", "收件编号不正确", ""))
		return uuid.Nil, false
	}
	return id, true
}
