package warehouse_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	handlerwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/warehouse"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/entitlement"
	appwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/app/warehouse"
)

type fullGuard struct{}

func (fullGuard) Check(_ context.Context, _ uuid.UUID, key string, _ int) error {
	return &entitlement.ErrPlanLimitReached{Key: key, Used: 2, Limit: 2}
}

func TestWarehouseHandler_Create_PlanLimit403(t *testing.T) {
	repo := &fakeRepo{}
	h := handlerwarehouse.New(
		appwarehouse.NewCreateUseCase(repo).WithPlanGuard(fullGuard{}),
		appwarehouse.NewGetByIDUseCase(repo), appwarehouse.NewListUseCase(repo),
		appwarehouse.NewUpdateUseCase(repo), appwarehouse.NewDeleteUseCase(repo), appwarehouse.NewRestoreUseCase(repo),
	)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("tenant_id", uuid.New()); c.Next() })
	h.RegisterRoutes(r.Group("/api/v1"))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/warehouses", bytes.NewReader([]byte(`{"name":"二仓"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `"code":"plan_limit_reached"`) ||
		!strings.Contains(w.Body.String(), "当前套餐最多 2 个仓库，已用 2，请升级套餐") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
