package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
)

func runKeyGate(t *testing.T, expectedKey, authHeader string) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequireInternalKey(expectedKey))
	r.POST("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	r.ServeHTTP(w, req)
	return w.Code
}

func TestRequireInternalKey_EmptyKey_FailClosed503(t *testing.T) {
	if got := runKeyGate(t, "", "Bearer anything"); got != http.StatusServiceUnavailable {
		t.Fatalf("empty key: status = %d, want 503 (fail-closed)", got)
	}
}

func TestRequireInternalKey_MissingHeader_401(t *testing.T) {
	if got := runKeyGate(t, "secret", ""); got != http.StatusUnauthorized {
		t.Fatalf("missing header: status = %d, want 401", got)
	}
}

func TestRequireInternalKey_WrongKey_401(t *testing.T) {
	if got := runKeyGate(t, "secret", "Bearer nope"); got != http.StatusUnauthorized {
		t.Fatalf("wrong key: status = %d, want 401", got)
	}
}

func TestRequireInternalKey_CorrectKey_PassesThrough(t *testing.T) {
	if got := runKeyGate(t, "secret", "Bearer secret"); got != http.StatusOK {
		t.Fatalf("correct key: status = %d, want 200", got)
	}
}
