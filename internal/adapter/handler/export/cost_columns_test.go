package export_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	handlerexport "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/export"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

const stockSheet = "商品编码,商品名,仓库,在库,单位成本,库存金额\nA1,\"螺丝,M6\",主仓,10,1.5,15.00\n[截断],数据超过 1 行限制,,,,\n"

func stockAs(role team.Role) string {
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, uuid.New())
		c.Set(middleware.CtxKeyShopRole, role)
	})
	handlerexport.New(&stubExporter{}, &stubExporter{body: stockSheet}, &stubExporter{}, nil).RegisterRoutes(api)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/exports/stock.csv", nil)
	r.ServeHTTP(w, req)
	return w.Body.String()
}

func TestStockCSV_CostColumnsOnlyForCostViewers(t *testing.T) {
	for _, role := range []team.Role{team.RoleClerk, team.RoleCashier} {
		body := stockAs(role)
		if strings.Contains(body, "单位成本") || strings.Contains(body, "库存金额") || strings.Contains(body, "1.5") || strings.Contains(body, "15.00") {
			t.Errorf("%s got cost columns: %q", role, body)
		}
		if !strings.Contains(body, "商品编码,商品名,仓库,在库") || !strings.Contains(body, `"螺丝,M6"`) {
			t.Errorf("%s lost non-cost columns: %q", role, body)
		}
	}
	for _, role := range []team.Role{team.RoleOwner, team.RoleManager, team.RoleFinance} {
		body := stockAs(role)
		if !strings.Contains(body, "单位成本") || !strings.Contains(body, "15.00") {
			t.Errorf("%s should keep cost columns: %q", role, body)
		}
	}
}
