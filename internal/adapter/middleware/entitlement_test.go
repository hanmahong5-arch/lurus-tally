package middleware_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/entitlement"
)

type stubChecker struct {
	allowed bool
	err     error
}

func (s stubChecker) Has(_ context.Context, _ uuid.UUID, _ string) (bool, error) {
	return s.allowed, s.err
}

// runGate wires RequireEntitlement behind a tiny middleware that injects the
// given tenant into context (mimicking AuthMiddleware) and returns the status
// code the gated route produced.
func runGate(checker middleware.EntitlementChecker, tenant uuid.UUID) int {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if tenant != uuid.Nil {
			c.Set(middleware.CtxKeyTenantID, tenant)
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
	if code := runGate(stubChecker{allowed: true}, uuid.New()); code != http.StatusOK {
		t.Fatalf("granted -> 200, got %d", code)
	}
}

func TestRequireEntitlement_Absent_403(t *testing.T) {
	if code := runGate(stubChecker{allowed: false}, uuid.New()); code != http.StatusForbidden {
		t.Fatalf("absent -> 403, got %d", code)
	}
}

func TestRequireEntitlement_Unauthenticated_401(t *testing.T) {
	// No tenant in context → the checker returns ErrUnauthenticated → 401.
	if code := runGate(stubChecker{err: entitlement.ErrUnauthenticated}, uuid.Nil); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated -> 401, got %d", code)
	}
}

func TestRequireEntitlementCode_Absent_StableCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, uuid.New())
		c.Next()
	})
	r.GET("/x", middleware.RequireEntitlementCode(stubChecker{allowed: false}, "ai_assistant", "ai_not_entitled"), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/x", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("absent -> 403, got %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "ai_not_entitled" {
		t.Fatalf("error code: got %v want ai_not_entitled", body["error"])
	}
	if body["entitlement"] != "ai_assistant" {
		t.Fatalf("entitlement: got %v", body["entitlement"])
	}
	if msg, _ := body["message"].(string); msg != "当前套餐不包含此功能，请升级套餐" {
		t.Fatalf("message must be the Chinese upgrade hint, got %q", msg)
	}
}

func TestRequireEntitlement_DefaultCodeUnchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, uuid.New())
		c.Next()
	})
	r.GET("/x", middleware.RequireEntitlement(stubChecker{allowed: false}, "k"), func(c *gin.Context) {})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/x", nil)
	r.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), `"feature_not_in_plan"`) {
		t.Fatalf("default code changed: %s", w.Body.String())
	}
}
