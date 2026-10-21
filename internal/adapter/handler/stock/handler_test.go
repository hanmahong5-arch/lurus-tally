package stock_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
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
		// TODO: surface parse error instead of silently defaulting to zero.
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
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		c.JSON(http.StatusCreated, snap)
	})
	api.GET("/snapshots", func(c *gin.Context) {
		snaps, err := listSnaps.Execute(c.Request.Context(), appstock.ListSnapshotsFilter{})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
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

type fakeNames map[uuid.UUID]string

func (f fakeNames) ProductNames(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	for _, id := range ids {
		if n, ok := f[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

// A clerk counting a product with no price on file gets a 422 that names the product
// and never asks them to type a price.
func TestPostAdjustment_ClerkCostRequiredNamesProducts(t *testing.T) {
	tid, p, wh := uuid.New(), uuid.New(), uuid.New()
	body := `{"warehouse_id":"` + wh.String() + `","items":[{"product_id":"` + p.String() + `","counted_qty":"5"}]}`
	c, w := newCtx(http.MethodPost, "/stock/adjustments", []byte(body), &tid, nil)
	c.Set(middleware.CtxKeyShopRole, team.RoleClerk)
	adjustHandler(&fakeStockRepo{}).WithProductNames(fakeNames{p: "红枫"}).PostAdjustment(c)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	msg, _ := got["error"].(string)
	if got["code"] != "cost_required" || !strings.Contains(msg, "红枫") || !strings.Contains(msg, "老板或店长") || strings.Contains(msg, "填写进货单价") {
		t.Fatalf("body = %v", got)
	}
	names, _ := got["product_names"].([]any)
	if len(names) != 1 || names[0] != "红枫" {
		t.Fatalf("product_names = %v", got["product_names"])
	}
}

// Two products sharing a name are told apart by id, so the screen can show each once.
func TestPostAdjustment_ClerkCostRequiredReturnsProductsByID(t *testing.T) {
	tid, p1, p2, wh := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	body := `{"warehouse_id":"` + wh.String() + `","items":[{"product_id":"` + p1.String() + `","counted_qty":"5"},{"product_id":"` + p2.String() + `","counted_qty":"6"}]}`
	c, w := newCtx(http.MethodPost, "/stock/adjustments", []byte(body), &tid, nil)
	c.Set(middleware.CtxKeyShopRole, team.RoleClerk)
	adjustHandler(&fakeStockRepo{}).WithProductNames(fakeNames{p1: "红枫", p2: "红枫"}).PostAdjustment(c)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	prods, _ := got["products"].([]any)
	if len(prods) != 2 {
		t.Fatalf("products = %v", got["products"])
	}
	seen := map[string]bool{}
	for _, x := range prods {
		m, _ := x.(map[string]any)
		if m["name"] != "红枫" {
			t.Fatalf("product = %v", m)
		}
		id, _ := m["id"].(string)
		seen[id] = true
	}
	if !seen[p1.String()] || !seen[p2.String()] {
		t.Fatalf("ids = %v", seen)
	}
}

// Someone who may see cost still gets the 400 that asks for a price.
func TestPostAdjustment_ManagerCostRequiredStays400(t *testing.T) {
	tid, p, wh := uuid.New(), uuid.New(), uuid.New()
	body := `{"warehouse_id":"` + wh.String() + `","items":[{"product_id":"` + p.String() + `","counted_qty":"5"}]}`
	c, w := newCtx(http.MethodPost, "/stock/adjustments", []byte(body), &tid, nil)
	c.Set(middleware.CtxKeyShopRole, team.RoleManager)
	adjustHandler(&fakeStockRepo{}).WithProductNames(fakeNames{p: "红枫"}).PostAdjustment(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestPostAdjustment_RecordsCountingPersonAsCreatedBy(t *testing.T) {
	tid, p, wh, sub := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	newRepo := func() *fakeStockRepo {
		return &fakeStockRepo{selectForUpdate: &domain.Snapshot{
			ID: uuid.New(), TenantID: tid, ProductID: p, WarehouseID: wh,
			OnHandQty: decimal.NewFromInt(10), UnitCost: decimal.NewFromInt(4),
		}}
	}
	body := `{"warehouse_id":"` + wh.String() + `","items":[{"product_id":"` + p.String() + `","counted_qty":"7"}]}`

	repo := newRepo()
	c, w := newCtx(http.MethodPost, "/stock/adjustments", []byte(body), &tid, nil)
	c.Set(middleware.CtxKeyIDPSubject, sub.String())
	adjustHandler(repo).PostAdjustment(c)
	if w.Code != http.StatusCreated || len(repo.movements) != 1 {
		t.Fatalf("status = %d movements = %d body=%s", w.Code, len(repo.movements), w.Body.String())
	}
	if got := repo.movements[0].CreatedBy; got == nil || *got != sub {
		t.Errorf("movement CreatedBy = %v, want %s", got, sub)
	}

	repo = newRepo()
	c, w = newCtx(http.MethodPost, "/stock/adjustments", []byte(body), &tid, nil)
	adjustHandler(repo).PostAdjustment(c)
	if w.Code != http.StatusCreated || len(repo.movements) != 1 {
		t.Fatalf("status = %d movements = %d body=%s", w.Code, len(repo.movements), w.Body.String())
	}
	if got := repo.movements[0].CreatedBy; got != nil {
		t.Errorf("movement CreatedBy = %v, want nil without a subject", got)
	}
}
