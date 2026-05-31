// Package webhook implements public (unauthenticated) webhook receivers.
// Routes are registered on *gin.Engine directly so they do not inherit the
// /api/v1 auth middleware.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
)

// maxBodyBytes caps raw body reads to 1 MB.
const maxBodyBytes = 1 * 1024 * 1024

// Shopify topic constants.  Shopify delivers the topic in the X-Shopify-Topic
// header; all values are lower-case with a slash separator.
const (
	topicOrdersCreate    = "orders/create"
	topicOrdersCancelled = "orders/cancelled"
	topicRefundsCreate   = "refunds/create"
)

// ShopResolver resolves a Shopify shop domain to Tally tenant identifiers.
// Returns nil, nil when the domain has no registered mapping.
type ShopResolver interface {
	GetByDomain(ctx context.Context, domain string) (*ShopMapping, error)
}

// ShopMapping carries the tenant context resolved from a shop domain.
type ShopMapping struct {
	ShopDomain  string
	TenantID    uuid.UUID
	WarehouseID uuid.UUID
	CreatorID   uuid.UUID
}

// IngestUseCase is the narrow interface the handler requires from
// ImportOrdersUseCase to ingest webhook-delivered events.
type IngestUseCase interface {
	IngestSingleOrder(ctx context.Context, req appimporting.SingleOrderRequest) (appimporting.ImportedOrder, *appimporting.SkippedOrder, error)
	IngestCancelOrder(ctx context.Context, req appimporting.CancelRequest) (*appimporting.CancelResult, error)
	IngestRefund(ctx context.Context, req appimporting.RefundRequest) (*appimporting.RefundResult, error)
}

// Handler holds the dependencies for all webhook routes.
type Handler struct {
	secret   string // SHOPIFY_WEBHOOK_SECRET; read once from env at build time
	resolver ShopResolver
	importUC IngestUseCase
	log      *slog.Logger
}

// New constructs a Handler.
// secret must be the raw (un-hashed) webhook secret from Shopify; it is read
// from env at startup and never logged.
func New(secret string, resolver ShopResolver, importUC IngestUseCase, log *slog.Logger) *Handler {
	return &Handler{
		secret:   secret,
		resolver: resolver,
		importUC: importUC,
		log:      log,
	}
}

// RegisterRoutes mounts all webhook routes onto the root engine.
// Must receive *gin.Engine (not a RouterGroup) so the path does not inherit
// the /api/v1 auth middleware.
//
// A single endpoint handles all order-level topics; topic routing is performed
// via the X-Shopify-Topic header rather than separate URL paths.  This matches
// the Shopify recommended pattern for a single webhook subscription target.
//
//	POST /webhooks/shopify/orders  — orders/create | orders/cancelled
//	POST /webhooks/shopify/refunds — refunds/create
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.POST("/webhooks/shopify/orders", h.handleOrders)
	r.POST("/webhooks/shopify/refunds", h.handleRefunds)
}

// handleOrders dispatches POST /webhooks/shopify/orders by topic header.
// Accepted topics: orders/create | orders/cancelled
func (h *Handler) handleOrders(c *gin.Context) {
	raw, mapping, ok := h.readAndVerify(c)
	if !ok {
		return
	}

	topic := c.GetHeader("X-Shopify-Topic")
	switch topic {
	case topicOrdersCreate:
		h.handleOrdersCreate(c, raw, mapping)
	case topicOrdersCancelled:
		h.handleOrderCancelled(c, raw, mapping)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_topic", "topic": topic})
	}
}

// handleRefunds dispatches POST /webhooks/shopify/refunds by topic header.
// Accepted topics: refunds/create
func (h *Handler) handleRefunds(c *gin.Context) {
	raw, mapping, ok := h.readAndVerify(c)
	if !ok {
		return
	}

	topic := c.GetHeader("X-Shopify-Topic")
	switch topic {
	case topicRefundsCreate:
		h.handleRefundCreate(c, raw, mapping)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_topic", "topic": topic})
	}
}

// readAndVerify performs the shared pre-dispatch steps:
//  1. Read body (bounded to maxBodyBytes).
//  2. HMAC-SHA256 verification.
//  3. Shop domain → tenant resolution.
//
// Returns (raw body, mapping, true) on success; writes the response and
// returns (nil, nil, false) on any failure so callers can return immediately.
func (h *Handler) readAndVerify(c *gin.Context) ([]byte, *ShopMapping, bool) {
	// 1. Read body (bounded).
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.log.Warn("shopify webhook: read body failed", slog.String("error", err.Error()))
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot_read_body"})
		return nil, nil, false
	}

	// 2. HMAC verification.
	if !h.verifySignature(raw, c.GetHeader("X-Shopify-Hmac-Sha256")) {
		h.log.Warn("shopify webhook: signature mismatch",
			slog.String("shop", c.GetHeader("X-Shopify-Shop-Domain")))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_signature"})
		return nil, nil, false
	}

	// 3. Resolve shop → tenant.
	shopDomain := c.GetHeader("X-Shopify-Shop-Domain")
	mapping, err := h.resolver.GetByDomain(c.Request.Context(), shopDomain)
	if err != nil {
		h.log.Error("shopify webhook: shop resolver error",
			slog.String("shop", shopDomain),
			slog.String("error", err.Error()))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "resolver_error"})
		return nil, nil, false
	}
	if mapping == nil {
		h.log.Warn("shopify webhook: unknown shop domain", slog.String("shop", shopDomain))
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown_shop", "shop": shopDomain})
		return nil, nil, false
	}

	return raw, mapping, true
}

// handleOrdersCreate processes a verified orders/create payload.
func (h *Handler) handleOrdersCreate(c *gin.Context, raw []byte, mapping *ShopMapping) {
	shopDomain := c.GetHeader("X-Shopify-Shop-Domain")

	req, err := parseShopifyOrder(raw, mapping)
	if err != nil {
		h.log.Warn("shopify webhook: order parse failed",
			slog.String("shop", shopDomain),
			slog.String("error", err.Error()))
		// Return 200 so Shopify does not retry a malformed payload.
		c.JSON(http.StatusOK, gin.H{"error": "parse_failed", "detail": err.Error()})
		return
	}

	imported, skipped, err := h.importUC.IngestSingleOrder(c.Request.Context(), req)
	if err != nil {
		h.log.Error("shopify webhook: ingest failed",
			slog.String("shop", shopDomain),
			slog.String("platform_order_no", req.PlatformOrderNo),
			slog.String("error", err.Error()))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ingest_error"})
		return
	}

	if skipped != nil {
		h.log.Info("shopify webhook: order skipped",
			slog.String("platform_order_no", req.PlatformOrderNo),
			slog.String("reason", skipped.Reason))
		c.JSON(http.StatusOK, gin.H{"status": "skipped", "reason": skipped.Reason})
		return
	}

	h.log.Info("shopify webhook: order ingested",
		slog.String("platform_order_no", imported.PlatformOrderNo),
		slog.String("bill_id", imported.BillID.String()),
		slog.String("bill_no", imported.BillNo))
	c.JSON(http.StatusOK, gin.H{
		"status":            "imported",
		"platform_order_no": imported.PlatformOrderNo,
		"bill_id":           imported.BillID.String(),
		"bill_no":           imported.BillNo,
	})
}

// handleOrderCancelled processes a verified orders/cancelled payload.
// Shopify delivers the same order JSON shape for cancellations.
func (h *Handler) handleOrderCancelled(c *gin.Context, raw []byte, mapping *ShopMapping) {
	shopDomain := c.GetHeader("X-Shopify-Shop-Domain")

	// Parse just enough to extract the order number.
	var o shopifyOrder
	if err := json.Unmarshal(raw, &o); err != nil {
		h.log.Warn("shopify webhook: cancel parse failed",
			slog.String("shop", shopDomain),
			slog.String("error", err.Error()))
		c.JSON(http.StatusOK, gin.H{"error": "parse_failed", "detail": err.Error()})
		return
	}
	orderNo := strings.TrimSpace(o.Name)
	if orderNo == "" {
		orderNo = o.ID.String()
	}
	if orderNo == "" {
		c.JSON(http.StatusOK, gin.H{"error": "parse_failed", "detail": "order has no id or name"})
		return
	}

	result, err := h.importUC.IngestCancelOrder(c.Request.Context(), appimporting.CancelRequest{
		TenantID:        mapping.TenantID,
		CreatorID:       mapping.CreatorID,
		Platform:        appimporting.PlatformShopify,
		PlatformOrderNo: orderNo,
	})
	if err != nil {
		h.log.Error("shopify webhook: cancel ingest failed",
			slog.String("shop", shopDomain),
			slog.String("platform_order_no", orderNo),
			slog.String("error", err.Error()))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cancel_error"})
		return
	}

	h.log.Info("shopify webhook: order cancelled",
		slog.String("platform_order_no", result.PlatformOrderNo),
		slog.String("reversal_bill_id", result.ReversalBillID.String()))
	c.JSON(http.StatusOK, gin.H{
		"status":            "cancelled",
		"platform_order_no": result.PlatformOrderNo,
		"original_bill_id":  result.OriginalBillID.String(),
		"reversal_bill_id":  result.ReversalBillID.String(),
		"reversal_bill_no":  result.ReversalBillNo,
	})
}

// handleRefundCreate processes a verified refunds/create payload.
func (h *Handler) handleRefundCreate(c *gin.Context, raw []byte, mapping *ShopMapping) {
	shopDomain := c.GetHeader("X-Shopify-Shop-Domain")

	req, err := parseShopifyRefund(raw, mapping)
	if err != nil {
		h.log.Warn("shopify webhook: refund parse failed",
			slog.String("shop", shopDomain),
			slog.String("error", err.Error()))
		c.JSON(http.StatusOK, gin.H{"error": "parse_failed", "detail": err.Error()})
		return
	}

	result, err := h.importUC.IngestRefund(c.Request.Context(), req)
	if err != nil {
		h.log.Error("shopify webhook: refund ingest failed",
			slog.String("shop", shopDomain),
			slog.String("platform_refund_id", req.PlatformRefundID),
			slog.String("error", err.Error()))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "refund_error"})
		return
	}

	h.log.Info("shopify webhook: refund ingested",
		slog.String("platform_refund_id", result.PlatformRefundID),
		slog.String("bill_id", result.BillID.String()))
	c.JSON(http.StatusOK, gin.H{
		"status":             "refunded",
		"platform_order_no":  result.PlatformOrderNo,
		"platform_refund_id": result.PlatformRefundID,
		"bill_id":            result.BillID.String(),
		"bill_no":            result.BillNo,
	})
}

// verifySignature returns true when the base64-encoded HMAC-SHA256 of body
// computed with h.secret matches the provided signature header value.
// Constant-time comparison prevents timing attacks.
func (h *Handler) verifySignature(body []byte, sigHeader string) bool {
	if sigHeader == "" || h.secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write(body)
	expected := mac.Sum(nil)

	got, err := base64.StdEncoding.DecodeString(sigHeader)
	if err != nil {
		return false
	}
	return hmac.Equal(expected, got)
}

// ----- Shopify JSON schemas (minimal fields used by ingestion) ---------------

type shopifyOrder struct {
	ID        json.Number   `json:"id"`
	Name      string        `json:"name"`
	Currency  string        `json:"currency"`
	CreatedAt time.Time     `json:"created_at"`
	LineItems []shopifyLine `json:"line_items"`
}

type shopifyLine struct {
	SKU      string      `json:"sku"`
	Quantity int64       `json:"quantity"`
	Price    json.Number `json:"price"`
}

// shopifyRefund is the minimal Shopify refund JSON shape.
// https://shopify.dev/docs/api/admin-rest/latest/resources/refund
type shopifyRefund struct {
	ID      json.Number `json:"id"`
	OrderID json.Number `json:"order_id"`
	// Note: "created_at" in refund payload.
	CreatedAt time.Time `json:"created_at"`
	// Currency of the parent order.
	Currency        string              `json:"currency"`
	RefundLineItems []shopifyRefundLine `json:"refund_line_items"`
}

type shopifyRefundLine struct {
	// LineItem is embedded; we only need the SKU and quantity.
	LineItem struct {
		SKU      string      `json:"sku"`
		Quantity int64       `json:"quantity"`
		Price    json.Number `json:"price"`
	} `json:"line_item"`
	Quantity int64 `json:"quantity"` // refunded quantity (may differ from original)
}

// parseShopifyOrder decodes raw JSON into a SingleOrderRequest.
// Returns an error when the payload is malformed or has no usable line items.
func parseShopifyOrder(raw []byte, m *ShopMapping) (appimporting.SingleOrderRequest, error) {
	var o shopifyOrder
	if err := json.Unmarshal(raw, &o); err != nil {
		return appimporting.SingleOrderRequest{}, fmt.Errorf("json decode: %w", err)
	}

	// Platform order number: prefer the human-readable "name" (e.g. "#1001"),
	// fall back to numeric id string.
	orderNo := strings.TrimSpace(o.Name)
	if orderNo == "" {
		orderNo = o.ID.String()
	}
	if orderNo == "" {
		return appimporting.SingleOrderRequest{}, fmt.Errorf("order has no id or name")
	}

	currency := strings.ToUpper(strings.TrimSpace(o.Currency))
	if currency == "" {
		currency = "USD"
	}

	var lines []appimporting.OrderRow
	for _, li := range o.LineItems {
		sku := strings.TrimSpace(li.SKU)
		if sku == "" {
			// Skip shipping or fee lines that have no SKU.
			continue
		}
		if li.Quantity <= 0 {
			continue
		}
		price, err := decimal.NewFromString(li.Price.String())
		if err != nil || price.IsNegative() {
			return appimporting.SingleOrderRequest{}, fmt.Errorf("line sku %q: invalid price %q", sku, li.Price)
		}
		lines = append(lines, appimporting.OrderRow{
			PlatformOrderNo: orderNo,
			PlatformSKU:     sku,
			Qty:             decimal.NewFromInt(li.Quantity),
			UnitPrice:       price,
			Currency:        currency,
			OrderDate:       o.CreatedAt.UTC(),
		})
	}
	if len(lines) == 0 {
		return appimporting.SingleOrderRequest{}, fmt.Errorf("order %q has no line items with a SKU", orderNo)
	}

	return appimporting.SingleOrderRequest{
		TenantID:        m.TenantID,
		CreatorID:       m.CreatorID,
		WarehouseID:     m.WarehouseID,
		Platform:        appimporting.PlatformShopify,
		PlatformOrderNo: orderNo,
		Lines:           lines,
	}, nil
}

// parseShopifyRefund decodes a refunds/create payload into a RefundRequest.
func parseShopifyRefund(raw []byte, m *ShopMapping) (appimporting.RefundRequest, error) {
	var r shopifyRefund
	if err := json.Unmarshal(raw, &r); err != nil {
		return appimporting.RefundRequest{}, fmt.Errorf("json decode: %w", err)
	}

	refundID := strings.TrimSpace(r.ID.String())
	if refundID == "" || refundID == "0" {
		return appimporting.RefundRequest{}, fmt.Errorf("refund has no id")
	}

	// Shopify uses the parent order id in the refund body; we surface order name
	// via the order_id field (numeric).  Webhook handlers that need the human-
	// readable order name should look it up; here we use the numeric order_id
	// as the platform_order_no since order name is not included in refund payloads.
	orderNo := r.OrderID.String()
	if orderNo == "" || orderNo == "0" {
		return appimporting.RefundRequest{}, fmt.Errorf("refund %s has no order_id", refundID)
	}

	currency := strings.ToUpper(strings.TrimSpace(r.Currency))
	if currency == "" {
		currency = "USD"
	}

	var lines []appimporting.RefundLine
	for _, rli := range r.RefundLineItems {
		sku := strings.TrimSpace(rli.LineItem.SKU)
		if sku == "" {
			continue
		}
		qty := rli.Quantity
		if qty <= 0 {
			qty = rli.LineItem.Quantity
		}
		if qty <= 0 {
			continue
		}
		price, err := decimal.NewFromString(rli.LineItem.Price.String())
		if err != nil || price.IsNegative() {
			return appimporting.RefundRequest{}, fmt.Errorf("refund line sku %q: invalid price %q", sku, rli.LineItem.Price)
		}
		lines = append(lines, appimporting.RefundLine{
			PlatformSKU:  sku,
			Qty:          decimal.NewFromInt(qty),
			RefundAmount: price,
		})
	}
	if len(lines) == 0 {
		return appimporting.RefundRequest{}, fmt.Errorf("refund %s has no refundable line items with a SKU", refundID)
	}

	refundDate := r.CreatedAt.UTC()
	if refundDate.IsZero() {
		refundDate = time.Now().UTC()
	}

	return appimporting.RefundRequest{
		TenantID:         m.TenantID,
		CreatorID:        m.CreatorID,
		WarehouseID:      m.WarehouseID,
		Platform:         appimporting.PlatformShopify,
		PlatformOrderNo:  orderNo,
		PlatformRefundID: refundID,
		Currency:         currency,
		RefundDate:       refundDate,
		Lines:            lines,
	}, nil
}
