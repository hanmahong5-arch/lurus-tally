// Package export implements Gin HTTP handlers for CSV export endpoints.
// Each handler streams the response via io.Pipe so the full dataset is never
// held in memory: the use case writes into the pipe writer while Gin reads
// from the pipe reader and forwards bytes to the client.
package export

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
)

// utf8BOM is prepended to every CSV so Excel opens Chinese headers without
// mojibake on Windows.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Exporter is the interface that each CSV use case must satisfy.
// Execute writes CSV rows (no BOM) to w and returns the row count written.
type Exporter interface {
	Execute(ctx context.Context, tenantID uuid.UUID, w io.Writer) (int, error)
}

// Handler groups the three CSV export Gin handlers.
type Handler struct {
	bills    Exporter
	stock    Exporter
	payments Exporter
	log      *slog.Logger
}

// New creates a Handler.
func New(bills, stock, payments Exporter, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{bills: bills, stock: stock, payments: payments, log: log}
}

// RegisterRoutes mounts the export routes onto rg.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	exports := rg.Group("/exports")
	exports.GET("/bills.csv", h.Bills)
	exports.GET("/stock.csv", h.Stock)
	exports.GET("/payments.csv", h.Payments)
}

// ----- handlers -----

// Bills handles GET /api/v1/exports/bills.csv
func (h *Handler) Bills(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "message": "tenant_id required"})
		return
	}
	filename := fmt.Sprintf("tally-bills-%s.csv", time.Now().Format("20060102"))
	h.streamCSV(c, filename, func(w io.Writer) error {
		_, err := h.bills.Execute(c.Request.Context(), tenantID, w)
		return err
	})
}

// Stock handles GET /api/v1/exports/stock.csv
func (h *Handler) Stock(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "message": "tenant_id required"})
		return
	}
	filename := fmt.Sprintf("tally-stock-%s.csv", time.Now().Format("20060102"))
	h.streamCSV(c, filename, func(w io.Writer) error {
		_, err := h.stock.Execute(c.Request.Context(), tenantID, w)
		return err
	})
}

// Payments handles GET /api/v1/exports/payments.csv
func (h *Handler) Payments(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "message": "tenant_id required"})
		return
	}
	filename := fmt.Sprintf("tally-payments-%s.csv", time.Now().Format("20060102"))
	h.streamCSV(c, filename, func(w io.Writer) error {
		_, err := h.payments.Execute(c.Request.Context(), tenantID, w)
		return err
	})
}

// ----- helpers -----

// streamCSV sets response headers and pipes CSV data from fn into the client.
// fn receives an io.Writer and should write BOM-less CSV rows to it;
// streamCSV prepends the UTF-8 BOM automatically.
// Errors from fn after headers are sent are logged but cannot change the HTTP
// status (headers are already flushed), so the client will receive a truncated
// file on error.
func (h *Handler) streamCSV(c *gin.Context, filename string, fn func(io.Writer) error) {
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Cache-Control", "no-store")

	pr, pw := io.Pipe()
	// If the client disconnects mid-stream, io.Copy below returns and the handler
	// unwinds; closing the read end makes a blocked pw.Write in the producer
	// goroutine return ErrClosedPipe so the goroutine cannot leak (a stranded
	// goroutine + DB cursor per aborted export is a slow DoS on flaky links).
	defer func() { _ = pr.Close() }()

	// Use case writes into pw; Gin reads from pr.
	go func() {
		_, err := pw.Write(utf8BOM)
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if err := fn(pw); err != nil {
			h.log.Error("export: CSV generation error",
				slog.String("file", filename),
				slog.String("error", err.Error()))
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.Close()
	}()

	c.Status(http.StatusOK)
	_, err := io.Copy(c.Writer, pr)
	if err != nil {
		h.log.Warn("export: client connection lost mid-stream",
			slog.String("file", filename),
			slog.String("error", err.Error()))
	}
}
