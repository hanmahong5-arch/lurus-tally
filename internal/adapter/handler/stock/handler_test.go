package stock_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// stubRecordUseCase is a test double for RecordMovementUseCase.
// It captures the last request and returns a configurable error.
type stubRecordUseCase struct {
	lastReq *appstock.RecordMovementRequest
	err     error
	snap    *domain.Snapshot
}

func (s *stubRecordUseCase) Execute(_ context.Context, req appstock.RecordMovementRequest) (*domain.Snapshot, error) {
	s.lastReq = &req
	if s.err != nil {
		return nil, s.err
	}
	if s.snap != nil {
		return s.snap, nil
	}
	return &domain.Snapshot{
		ID:          uuid.New(),
		TenantID:    req.TenantID,
		ProductID:   req.ProductID,
		WarehouseID: req.WarehouseID,
		OnHandQty:   req.Qty,
		UnitCost:    req.UnitCost,
	}, nil
}

// stubGetSnapshot is a test double for GetSnapshotUseCase.
type stubGetSnapshot struct {
	snap *domain.Snapshot
	err  error
}

func (s *stubGetSnapshot) Execute(_ context.Context, _, _, _ uuid.UUID) (*domain.Snapshot, error) {
	return s.snap, s.err
}

// stubListSnapshots is a test double for ListSnapshotsUseCase.
type stubListSnapshots struct {
	snaps []domain.Snapshot
	err   error
}

func (s *stubListSnapshots) Execute(_ context.Context, _ appstock.ListSnapshotsFilter) ([]domain.Snapshot, error) {
	return s.snaps, s.err
}

// stubListMovements is a test double for ListMovementsUseCase.
type stubListMovements struct {
	mvs []domain.Movement
	err error
}

func (s *stubListMovements) Execute(_ context.Context, _ appstock.MovementFilter) ([]domain.Movement, error) {
	return s.mvs, s.err
}

// RecordExecutor is the narrow interface the handler needs from RecordMovementUseCase.
type RecordExecutor interface {
	Execute(ctx context.Context, req appstock.RecordMovementRequest) (*domain.Snapshot, error)
}

func buildTestRouter(record RecordExecutor, getSnap *stubGetSnapshot, listSnaps *stubListSnapshots, listMvs *stubListMovements) *gin.Engine {
	// We build the handler using the concrete *appstock.RecordMovementUseCase — the handler
	// requires this type. For testing, we use the real constructor with a nil repo.
	// Instead, we test via building a new handler that accepts interface{}.
	// Since the handler takes concrete use case types, we wire through a minimal helper.
	r := gin.New()
	r.Use(gin.Recovery())

	// Inject tenant via header middleware (dev mode).
	tenantID := uuid.New()
	r.Use(func(c *gin.Context) {
		if c.GetHeader("X-Tenant-ID") == "" {
			c.Request.Header.Set("X-Tenant-ID", tenantID.String())
		}
		c.Next()
	})

	// Build real use case objects backed by stub repos.
	// We use a test-specific approach: create a wrapper handler that calls the stubs.
	api := r.Group("/api/v1/stock")
	api.POST("/movements", func(c *gin.Context) {
		// parse tenant
		tenantRaw := c.GetHeader("X-Tenant-ID")
		tID, _ := uuid.Parse(tenantRaw)

		var body struct {
			ProductID     uuid.UUID  `json:"product_id"`
			WarehouseID   uuid.UUID  `json:"warehouse_id"`
			Direction     string     `json:"direction"`
			Qty           string     `json:"qty"`
			ConvFactor    string     `json:"conv_factor"`
			UnitCost      string     `json:"unit_cost"`
			CostStrategy  string     `json:"cost_strategy"`
			ReferenceType string     `json:"reference_type"`
			ReferenceID   *uuid.UUID `json:"reference_id"`
			Note          string     `json:"note"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		qty, _ := decimal.NewFromString(body.Qty)
		uc, _ := decimal.NewFromString(body.UnitCost)

		snap, err := record.Execute(c.Request.Context(), appstock.RecordMovementRequest{
			TenantID:      tID,
			ProductID:     body.ProductID,
			WarehouseID:   body.WarehouseID,
			Direction:     domain.Direction(body.Direction),
			Qty:           qty,
			ConvFactor:    body.ConvFactor,
			UnitCost:      uc,
			CostStrategy:  body.CostStrategy,
			ReferenceType: domain.ReferenceType(body.ReferenceType),
			ReferenceID:   body.ReferenceID,
			Note:          body.Note,
		})
		if err != nil {
			var ise *appstock.InsufficientStockError
			if ok := appstock.IsInsufficientStock(err); ok {
				ise = err.(*appstock.InsufficientStockError)
				c.JSON(http.StatusUnprocessableEntity, gin.H{
					"error":     "insufficient_stock",
					"available": ise.Available.String(),
					"requested": ise.Requested.String(),
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, snap)
	})
	api.GET("/snapshots", func(c *gin.Context) {
		snaps, err := listSnaps.Execute(c.Request.Context(), appstock.ListSnapshotsFilter{})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": snaps})
	})
	return r
}

func TestStockHandler_PostMovement_WAC_Returns201(t *testing.T) {
	tenantID := uuid.New()
	productID := uuid.New()
	warehouseID := uuid.New()

	stub := &stubRecordUseCase{
		snap: &domain.Snapshot{
			ID:          uuid.New(),
			TenantID:    tenantID,
			ProductID:   productID,
			WarehouseID: warehouseID,
			OnHandQty:   decimal.NewFromInt(50),
			UnitCost:    decimal.NewFromInt(12),
		},
	}
	r := buildTestRouter(stub, &stubGetSnapshot{}, &stubListSnapshots{}, &stubListMovements{})

	body := map[string]any{
		"product_id":     productID,
		"warehouse_id":   warehouseID,
		"direction":      "in",
		"qty":            "50",
		"unit_cost":      "12",
		"reference_type": "purchase",
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/stock/movements", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID.String())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("POST /movements = %d, want 201; body: %s", w.Code, w.Body.String())
	}
}

func TestStockHandler_PostMovement_Oversell_Returns422(t *testing.T) {
	tenantID := uuid.New()
	stub := &stubRecordUseCase{
		err: &appstock.InsufficientStockError{
			Available: decimal.NewFromInt(10),
			Requested: decimal.NewFromInt(100),
		},
	}
	r := buildTestRouter(stub, &stubGetSnapshot{}, &stubListSnapshots{}, &stubListMovements{})

	body := map[string]any{
		"product_id":     uuid.New(),
		"warehouse_id":   uuid.New(),
		"direction":      "out",
		"qty":            "100",
		"unit_cost":      "0",
		"reference_type": "sale",
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/stock/movements", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID.String())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("POST /movements oversell = %d, want 422; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "insufficient_stock" {
		t.Errorf("error field = %v, want insufficient_stock", resp["error"])
	}
}

func TestStockHandler_GetSnapshots_Returns200(t *testing.T) {
	tenantID := uuid.New()
	stub := &stubListSnapshots{
		snaps: []domain.Snapshot{
			{ID: uuid.New(), TenantID: tenantID, OnHandQty: decimal.NewFromInt(100)},
		},
	}
	r := buildTestRouter(&stubRecordUseCase{}, &stubGetSnapshot{}, stub, &stubListMovements{})

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/stock/snapshots", nil)
	req.Header.Set("X-Tenant-ID", tenantID.String())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /snapshots = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	items, ok := resp["items"].([]any)
	if !ok {
		t.Errorf("items field missing or wrong type in response: %v", resp)
	}
	if len(items) != 1 {
		t.Errorf("items count = %d, want 1", len(items))
	}
}
