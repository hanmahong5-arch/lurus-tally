package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/entitlement"
)

type stubChecker struct {
	allowed bool
	err     error
}

func (s stubChecker) Has(_ context.Context, _, _ string) (bool, error) { return s.allowed, s.err }

// runGate wires RequireEntitlement behind a tiny middleware that injects the
// given sub into context (mimicking AuthMiddleware) and returns the status code
// the gated route produced.
func runGate(checker middleware.EntitlementChecker, sub string) int {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if sub != "" {
			c.Set(middleware.CtxKeyZitadelSub, sub)
		}
		c.Next()
	})
	r.GET("/x", middleware.RequireEntitlement(checker, "ai_assistant"), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/x", nil)
	r.ServeHTTP(w, req)
	return w.Code
}

func TestRequireEntitlement_Granted_PassesThrough(t *testing.T) {
	if code := runGate(stubChecker{allowed: true}, "sub"); code != http.StatusOK {
		t.Fatalf("granted → 200, got %d", code)
	}
}

func TestRequireEntitlement_Absent_403(t *testing.T) {
	if code := runGate(stubChecker{allowed: false}, "sub"); code != http.StatusForbidden {
		t.Fatalf("absent → 403, got %d", code)
	}
}

func TestRequireEntitlement_Unauthenticated_401(t *testing.T) {
	if code := runGate(stubChecker{err: entitlement.ErrUnauthenticated}, ""); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated → 401, got %d", code)
	}
}
