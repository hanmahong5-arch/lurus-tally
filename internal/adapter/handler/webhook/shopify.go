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
// ImportOrdersUseCase to ingest a single webhook-delivered order.
type IngestUseCase interface {
	IngestSingleOrder(ctx context.Context, req appimporting.SingleOrderRequest) (appimporting.ImportedOrder, *appimporting.SkippedOrder, error)
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
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.POST("/webhooks/shopify/orders", h.handleOrdersCreate)
}

// handleOrdersCreate handles POST /webhooks/shopify/orders.
//
// Protocol:
//  1. Read body (bounded to maxBodyBytes).
//  2. Verify HMAC-SHA256 signature from X-Shopify-Hmac-Sha256 header.
//  3. Reject topics other than orders/create.
//  4. Resolve shop_domain → tenant via ShopResolver.
//  5. Decode order JSON and call IngestSingleOrder.
//
// Return 200 even on internal errors (with error field) to avoid Shopify
// retry storms on non-transient failures.  5xx is reserved for cases where
// retry makes sense (e.g. DB down), per the design decision in the ticket.
func (h *Handler) handleOrdersCreate(c *gin.Context) {
	// --- 1. Read body (bounded) ---
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.log.Warn("shopify webhook: read body failed", slog.String("error", err.Error()))
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot_read_body"})
		return
	}

	// --- 2. HMAC verification ---
	if !h.verifySignature(raw, c.GetHeader("X-Shopify-Hmac-Sha256")) {
		h.log.Warn("shopify webhook: signature mismatch",
			slog.String("shop", c.GetHeader("X-Shopify-Shop-Domain")))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_signature"})
		return
	}

	// --- 3. Topic guard ---
	topic := c.GetHeader("X-Shopify-Topic")
	if topic != "orders/create" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_topic", "topic": topic})
		return
	}

	// --- 4. Resolve shop → tenant ---
	shopDomain := c.GetHeader("X-Shopify-Shop-Domain")
	mapping, err := h.resolver.GetByDomain(c.Request.Context(), shopDomain)
	if err != nil {
		h.log.Error("shopify webhook: shop resolver error",
			slog.String("shop", shopDomain),
			slog.String("error", err.Error()))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "resolver_error"})
		return
	}
	if mapping == nil {
		h.log.Warn("shopify webhook: unknown shop domain", slog.String("shop", shopDomain))
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown_shop", "shop": shopDomain})
		return
	}

	// --- 5. Decode order JSON ---
	req, err := parseShopifyOrder(raw, mapping)
	if err != nil {
		h.log.Warn("shopify webhook: order parse failed",
			slog.String("shop", shopDomain),
			slog.String("error", err.Error()))
		// Return 200 so Shopify does not retry a malformed payload.
		c.JSON(http.StatusOK, gin.H{"error": "parse_failed", "detail": err.Error()})
		return
	}

	// --- 6. Ingest ---
	imported, skipped, err := h.importUC.IngestSingleOrder(c.Request.Context(), req)
	if err != nil {
		h.log.Error("shopify webhook: ingest failed",
			slog.String("shop", shopDomain),
			slog.String("platform_order_no", req.PlatformOrderNo),
			slog.String("error", err.Error()))
		// 500 → Shopify will retry (appropriate for transient DB failures).
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

// ----- Shopify order JSON schema (minimal fields used by ingestion) ----------

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
