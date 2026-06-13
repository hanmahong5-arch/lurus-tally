// Package importing implements the Gin HTTP handlers for platform-order CSV import.
// Routes:
//
//	POST /api/v1/imports/orders  — multipart upload (field "file") + form field "platform"
//	                               Query param ?preview=true for dry-run oversell check.
//	GET  /api/v1/imports/mappings — list SKU mappings for the tenant.
package importing

import (
	"context"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
)

// maxUploadBytes caps the multipart body to 10 MB.
const maxUploadBytes = 10 * 1024 * 1024

// ImportUseCase is the narrow interface the handler requires from ImportOrdersUseCase.
type ImportUseCase interface {
	Execute(ctx context.Context, req appimporting.ImportRequest) (*appimporting.ImportResult, error)
	ListMappings(ctx context.Context, tenantID uuid.UUID, platform string) ([]appimporting.SKUMapping, error)
}

// Handler groups the import Gin handlers.
type Handler struct {
	uc          ImportUseCase
	warehouseID uuid.UUID // default warehouse for stock deduction; zero → per-request
}

// New constructs a Handler.
// defaultWarehouseID may be uuid.Nil; in that case the caller must pass warehouse_id
// as a form field (not yet implemented in V1 — the handler will 400 on nil).
func New(uc ImportUseCase, defaultWarehouseID uuid.UUID) *Handler {
	return &Handler{uc: uc, warehouseID: defaultWarehouseID}
}

// RegisterRoutes mounts the import routes onto rg.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/imports/orders", h.ImportOrders)
	rg.GET("/imports/mappings", h.ListMappings)
}

// ImportOrders handles POST /api/v1/imports/orders.
//
// Request: multipart/form-data
//   - field "file"      — CSV file (required)
//   - field "platform"  — "amazon" | "shopify" (required)
//   - field "warehouse" — UUID of the destination warehouse (required when no default)
//   - field "hints"     — JSON array of {platform_sku, product_id} objects (optional)
//
// Query: ?preview=true  — dry-run; returns oversell report without creating bills.
func (h *Handler) ImportOrders(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	creatorID := actorID(c)

	// Bound the multipart body.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadBytes)
	if err := c.Request.ParseMultipartForm(maxUploadBytes); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "multipart parse: " + err.Error()})
		return
	}

	// Read form fields.
	platform := appimporting.Platform(c.PostForm("platform"))
	if err := platform.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": err.Error()})
		return
	}

	warehouseID := h.warehouseID
	if wStr := c.PostForm("warehouse"); wStr != "" {
		wid, err := uuid.Parse(wStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "invalid warehouse UUID"})
			return
		}
		warehouseID = wid
	}
	if warehouseID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "warehouse is required"})
		return
	}

	// Read the uploaded file.
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "missing form field 'file'"})
		return
	}
	f, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "open upload: " + err.Error()})
		return
	}
	defer func() { _ = f.Close() }()

	csvData, err := io.ReadAll(io.LimitReader(f, maxUploadBytes))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "detail": err.Error()})
		return
	}

	preview := c.Query("preview") == "true"

	result, err := h.uc.Execute(c.Request.Context(), appimporting.ImportRequest{
		TenantID:    tenantID,
		CreatorID:   creatorID,
		WarehouseID: warehouseID,
		Platform:    platform,
		CSVData:     csvData,
		DryRun:      preview,
	})
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "import_failed", "detail": err.Error()})
		return
	}

	status := http.StatusOK
	if !preview && len(result.Imported) > 0 {
		status = http.StatusCreated
	}
	c.JSON(status, toResultDTO(result))
}

// ListMappings handles GET /api/v1/imports/mappings.
// Optional query param: ?platform=amazon|shopify
func (h *Handler) ListMappings(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	platform := c.Query("platform")
	mappings, err := h.uc.ListMappings(c.Request.Context(), tenantID, platform)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "detail": err.Error()})
		return
	}

	type mappingDTO struct {
		ID          string `json:"id"`
		Platform    string `json:"platform"`
		PlatformSKU string `json:"platform_sku"`
		ProductID   string `json:"product_id"`
		UpdatedAt   string `json:"updated_at"`
	}

	dtos := make([]mappingDTO, 0, len(mappings))
	for _, m := range mappings {
		dtos = append(dtos, mappingDTO{
			ID:          m.ID.String(),
			Platform:    m.Platform,
			PlatformSKU: m.PlatformSKU,
			ProductID:   m.ProductID.String(),
			UpdatedAt:   m.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": dtos, "total": len(dtos)})
}

// ----- DTOs -----------------------------------------------------------------

type importResultDTO struct {
	Imported    []importedOrderDTO `json:"imported"`
	Skipped     []skippedOrderDTO  `json:"skipped"`
	Oversells   []oversellDTO      `json:"oversells,omitempty"`
	UnknownSKUs []unknownSKUDTO    `json:"unknown_skus,omitempty"`
	Summary     summaryDTO         `json:"summary"`
}

type importedOrderDTO struct {
	PlatformOrderNo string `json:"platform_order_no"`
	BillID          string `json:"bill_id,omitempty"`
	BillNo          string `json:"bill_no,omitempty"`
}

type skippedOrderDTO struct {
	PlatformOrderNo string `json:"platform_order_no"`
	Reason          string `json:"reason"`
}

type oversellDTO struct {
	PlatformOrderNo string `json:"platform_order_no"`
	PlatformSKU     string `json:"platform_sku"`
	ProductID       string `json:"product_id"`
	Requested       string `json:"requested_qty"`
	Available       string `json:"available_qty"`
}

type unknownSKUDTO struct {
	Platform    string `json:"platform"`
	PlatformSKU string `json:"platform_sku"`
}

type summaryDTO struct {
	TotalParsed  int `json:"total_parsed"`
	Imported     int `json:"imported"`
	Skipped      int `json:"skipped"`
	OversellRows int `json:"oversell_rows"`
	UnknownSKUs  int `json:"unknown_skus"`
}

func toResultDTO(r *appimporting.ImportResult) importResultDTO {
	imported := make([]importedOrderDTO, 0, len(r.Imported))
	for _, o := range r.Imported {
		dto := importedOrderDTO{PlatformOrderNo: o.PlatformOrderNo, BillNo: o.BillNo}
		if o.BillID != uuid.Nil {
			dto.BillID = o.BillID.String()
		}
		imported = append(imported, dto)
	}

	skipped := make([]skippedOrderDTO, 0, len(r.Skipped))
	for _, s := range r.Skipped {
		skipped = append(skipped, skippedOrderDTO{PlatformOrderNo: s.PlatformOrderNo, Reason: s.Reason})
	}

	oversells := make([]oversellDTO, 0, len(r.Oversells))
	for _, o := range r.Oversells {
		oversells = append(oversells, oversellDTO{
			PlatformOrderNo: o.PlatformOrderNo,
			PlatformSKU:     o.PlatformSKU, // F06 fix: surface SKU for UI display
			ProductID:       o.ProductID.String(),
			Requested:       o.Requested.String(),
			Available:       o.Available.String(),
		})
	}

	unknownSKUs := make([]unknownSKUDTO, 0, len(r.UnknownSKUs))
	for _, u := range r.UnknownSKUs {
		unknownSKUs = append(unknownSKUs, unknownSKUDTO{Platform: u.Platform, PlatformSKU: u.PlatformSKU})
	}

	return importResultDTO{
		Imported:    imported,
		Skipped:     skipped,
		Oversells:   oversells,
		UnknownSKUs: unknownSKUs,
		Summary: summaryDTO{
			TotalParsed:  len(imported) + len(skipped),
			Imported:     len(imported),
			Skipped:      len(skipped),
			OversellRows: len(oversells),
			UnknownSKUs:  len(unknownSKUs),
		},
	}
}

// actorID reads the Zitadel subject from the request context. Returns uuid.Nil
// when the subject is absent or not a UUID — the use case will apply a zero
// creator_id in that case.
func actorID(c *gin.Context) uuid.UUID {
	sub := middleware.GetZitadelSub(c)
	if sub == "" {
		return uuid.Nil
	}
	id, err := uuid.Parse(sub)
	if err != nil {
		return uuid.Nil
	}
	return id
}
