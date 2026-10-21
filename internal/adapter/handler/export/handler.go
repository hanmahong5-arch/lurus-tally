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
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	appexport "github.com/hanmahong5-arch/lurus-tally/internal/app/export"
)

// utf8BOM is prepended to every CSV so Excel opens Chinese headers without
// mojibake on Windows.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Exporter is the interface that each CSV use case must satisfy.
// Execute writes CSV rows (no BOM) to w and returns the row count written.
type Exporter interface {
	Execute(ctx context.Context, tenantID uuid.UUID, w io.Writer) (int, error)
}

// BillsExporter writes the bills CSV narrowed by a filter. The zero filter
// exports every bill.
type BillsExporter interface {
	ExecuteFiltered(ctx context.Context, tenantID uuid.UUID, f appexport.BillsFilter, w io.Writer) (int, error)
}

// Handler groups the three CSV export Gin handlers.
type Handler struct {
	bills    BillsExporter
	stock    Exporter
	payments Exporter
	all      AllExporter
	log      *slog.Logger
}

// New creates a Handler.
func New(bills BillsExporter, stock, payments Exporter, log *slog.Logger) *Handler {
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
	exports.GET("/all.zip", h.All)
}

// ----- handlers -----

// Bills handles GET /api/v1/exports/bills.csv
func (h *Handler) Bills(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "message": "请先登录"})
		return
	}
	f, rng, msg := parseBillsFilter(c)
	if msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "message": msg})
		return
	}
	filename := fmt.Sprintf("tally-bills-%s.csv", repobill.BillNoDay(time.Now()))
	if rng != "" {
		filename = fmt.Sprintf("tally-bills-%s.csv", rng)
	}
	h.streamCSV(c, filename, func(w io.Writer) error {
		_, err := h.bills.ExecuteFiltered(c.Request.Context(), tenantID, f, w)
		return err
	})
}

// billsTZ is the business time zone dates are interpreted in (shared with the
// use case so parsing and rendering agree).
var billsTZ = appexport.BusinessTZ

// parseBillsFilter reads the list-style query parameters. On invalid input it
// returns a Chinese message for the 400 body. rng is the file-name suffix built
// from the date range ("" when no date was given).
func parseBillsFilter(c *gin.Context) (f appexport.BillsFilter, rng, msg string) {
	switch t := c.Query("type"); t {
	case "", "purchase", "sale":
		f.Type = t
	default:
		return f, "", "type 只能是 purchase 或 sale"
	}
	if s := c.Query("status"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || (n != 0 && n != 2 && n != 9) {
			return f, "", "状态不正确，可选：0 草稿、2 已审核、9 已取消"
		}
		st := int16(n)
		f.Status = &st
	}
	if s := c.Query("partner_id"); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			return f, "", "往来单位不正确"
		}
		f.PartnerID = &id
	}
	var fromRaw, toRaw string
	if fromRaw = c.Query("date_from"); fromRaw != "" {
		d, err := time.ParseInLocation("2006-01-02", fromRaw, billsTZ)
		if err != nil {
			return f, "", "开始日期格式不正确，请用 年-月-日，例如 2026-10-01"
		}
		f.DateFrom = &d
	}
	if toRaw = c.Query("date_to"); toRaw != "" {
		d, err := time.ParseInLocation("2006-01-02", toRaw, billsTZ)
		if err != nil {
			return f, "", "结束日期格式不正确，请用 年-月-日，例如 2026-10-31"
		}
		d = d.AddDate(0, 0, 1) // the end day itself is included
		f.DateTo = &d
	}
	if f.DateFrom != nil && f.DateTo != nil && !f.DateFrom.Before(*f.DateTo) {
		return f, "", "开始日期不能晚于结束日期"
	}
	switch ps := c.Query("payment_status"); ps {
	case "", "paid", "partial", "unpaid", "open":
		f.PaymentStatus = ps
	default:
		return f, "", "收付款状态只能是 open、paid、partial 或 unpaid"
	}
	if f.PaymentStatus != "" && f.Status != nil && *f.Status != 2 {
		return f, "", "按收付款状态筛选时只能查看已审核的单据"
	}
	switch is := c.Query("invoice_status"); is {
	case "", "none", "partial", "full":
		f.InvoiceStatus = is
	default:
		return f, "", "开票状态只能是 none、partial 或 full"
	}
	if f.InvoiceStatus != "" && f.Status != nil && *f.Status != 2 {
		return f, "", "按开票状态筛选时只能查看已审核的单据"
	}
	f.Q = strings.TrimSpace(c.Query("q"))
	if len([]rune(f.Q)) > 64 {
		return f, "", "搜索关键字太长，最多 64 个字"
	}
	switch m := c.Query("mine"); m {
	case "", "0":
	case "1":
		id := middleware.ActorUUID(c)
		if id == uuid.Nil {
			return f, "", "无法识别当前用户，不能只看我的单据"
		}
		f.CreatorID = &id
	default:
		return f, "", "“只看我的”参数不正确"
	}
	if fromRaw != "" || toRaw != "" {
		rng = strings.ReplaceAll(fromRaw, "-", "") + "-" + strings.ReplaceAll(toRaw, "-", "")
		rng = strings.Trim(rng, "-")
	}
	return f, rng, ""
}

// Stock handles GET /api/v1/exports/stock.csv
func (h *Handler) Stock(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "message": "请先登录"})
		return
	}
	filename := fmt.Sprintf("tally-stock-%s.csv", repobill.BillNoDay(time.Now()))
	showCost := middleware.CanViewCost(c)
	h.streamCSV(c, filename, func(w io.Writer) error {
		if showCost {
			_, err := h.stock.Execute(c.Request.Context(), tenantID, w)
			return err
		}
		// No cost.view: the unit cost and stock value columns never leave the server.
		fw := newCSVColumnFilter(w, stockCostColumns...)
		_, err := h.stock.Execute(c.Request.Context(), tenantID, fw)
		if cerr := fw.Close(); err == nil {
			err = cerr
		}
		return err
	})
}

// Payments handles GET /api/v1/exports/payments.csv
func (h *Handler) Payments(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "message": "请先登录"})
		return
	}
	from, to, msg := parsePaymentsRange(c)
	if msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "message": msg})
		return
	}
	filter := appexport.PaymentsFilter{From: from, To: to, CountedOnly: c.Query("counted_only") == "1"}
	switch d := c.Query("direction"); d {
	case "", "in", "out":
		filter.Direction = d
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "message": "收付方向只能是 收款 或 付款"})
		return
	}
	filename := fmt.Sprintf("tally-payments-%s.csv", repobill.BillNoDay(time.Now()))
	h.streamCSV(c, filename, func(w io.Writer) error {
		if fe, ok := h.payments.(FilteredExporter); ok && (filter.Direction != "" || filter.CountedOnly) {
			_, err := fe.ExecuteFiltered(c.Request.Context(), tenantID, filter, w)
			return err
		}
		if re, ok := h.payments.(RangeExporter); ok && (from != nil || to != nil) {
			_, err := re.ExecuteRange(c.Request.Context(), tenantID, from, to, w)
			return err
		}
		_, err := h.payments.Execute(c.Request.Context(), tenantID, w)
		return err
	})
}

// FilteredExporter is an Exporter that can also narrow rows by direction and by whether they
// count as money actually moved.
type FilteredExporter interface {
	ExecuteFiltered(ctx context.Context, tenantID uuid.UUID, f appexport.PaymentsFilter, w io.Writer) (int, error)
}

// RangeExporter is an Exporter that can also narrow its rows to [from, to).
type RangeExporter interface {
	ExecuteRange(ctx context.Context, tenantID uuid.UUID, from, to *time.Time, w io.Writer) (int, error)
}

// parsePaymentsRange reads date_from / date_to (inclusive days, Beijing time) as the
// half-open window [from, to) the use case takes. msg is a Chinese 400 message.
func parsePaymentsRange(c *gin.Context) (from, to *time.Time, msg string) {
	if raw := c.Query("date_from"); raw != "" {
		d, err := time.ParseInLocation("2006-01-02", raw, billsTZ)
		if err != nil {
			return nil, nil, "开始日期格式不正确，请用 年-月-日，例如 2026-10-01"
		}
		from = &d
	}
	if raw := c.Query("date_to"); raw != "" {
		d, err := time.ParseInLocation("2006-01-02", raw, billsTZ)
		if err != nil {
			return nil, nil, "结束日期格式不正确，请用 年-月-日，例如 2026-10-31"
		}
		d = d.AddDate(0, 0, 1)
		to = &d
	}
	if from != nil && to != nil && !from.Before(*to) {
		return nil, nil, "开始日期不能晚于结束日期"
	}
	return from, to, ""
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
	// The producer goroutine runs the export query through dbscope.From, i.e. on
	// the request-pinned *sql.Conn. middleware.TenantDB RESETs and Closes that
	// conn the instant this handler returns, so we must NOT return while the
	// producer is still reading rows on it (use-after-close / data race on a
	// single conn). done is closed when the producer exits; the defer closes the
	// read end first so a producer parked in pw.Write observes ErrClosedPipe and
	// exits promptly (no stranded goroutine + DB cursor on flaky links), then
	// joins it before releasing the pinned conn back to the middleware.
	done := make(chan struct{})
	defer func() {
		_ = pr.Close()
		<-done
	}()

	// Use case writes into pw; Gin reads from pr.
	go func() {
		defer close(done)
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
