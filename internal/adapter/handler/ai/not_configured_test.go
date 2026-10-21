package ai_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	handlerai "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/ai"
)

func TestNotConfigured_StableCode(t *testing.T) {
	r := gin.New()
	r.POST("/ai/chat", handlerai.NotConfigured)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/ai/chat", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if body["error"] != "ai_not_configured" {
		t.Errorf("error = %q, want ai_not_configured", body["error"])
	}
	if body["message"] != "AI 助手尚未开通" {
		t.Errorf("message = %q", body["message"])
	}
}
