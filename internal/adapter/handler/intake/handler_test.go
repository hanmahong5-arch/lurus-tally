package intake

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

func TestAccept_ClerkForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	api := e.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, uuid.New())
		c.Set(middleware.CtxKeyShopRole, team.RoleClerk)
		c.Next()
	}, middleware.EnforceRoutePerms())
	New(nil).RegisterRoutes(api)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/intake/"+uuid.NewString()+"/accept", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}
