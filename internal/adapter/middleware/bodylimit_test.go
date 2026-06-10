package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
)

func newBodyLimitEngine(max int64) *gin.Engine {
	r := gin.New()
	r.Use(middleware.BodyLimit(max))
	r.POST("/echo", func(c *gin.Context) {
		_, _ = c.GetRawData() // forces a body read so MaxBytesReader engages
		c.Status(http.StatusOK)
	})
	return r
}

// TestBodyLimit_OverContentLength_Returns413 proves a request whose declared
// Content-Length exceeds the cap is rejected up front with 413, without the
// handler running.
func TestBodyLimit_OverContentLength_Returns413(t *testing.T) {
	r := newBodyLimitEngine(1024)

	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader("small"))
	req.ContentLength = 2048 // claim a body larger than the 1KiB cap

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status: got %d, want 413", w.Code)
	}
	if !strings.Contains(w.Body.String(), "payload_too_large") {
		t.Errorf("body: got %q, want payload_too_large envelope", w.Body.String())
	}
}

// TestBodyLimit_UnderLimit_PassesThrough proves a request under the cap reaches
// the handler normally.
func TestBodyLimit_UnderLimit_PassesThrough(t *testing.T) {
	r := newBodyLimitEngine(1024)

	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader("hello"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
}

// TestBodyLimit_ChunkedOverLimit_ReadFails proves that a body with no declared
// Content-Length that overflows the cap is cut off by MaxBytesReader (the
// handler's read returns an error rather than slurping unbounded bytes).
func TestBodyLimit_ChunkedOverLimit_ReadFails(t *testing.T) {
	r := gin.New()
	r.Use(middleware.BodyLimit(8))
	var readErr error
	r.POST("/read", func(c *gin.Context) {
		_, readErr = c.GetRawData()
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/read", strings.NewReader("0123456789ABCDEF"))
	req.ContentLength = -1 // unknown length: the up-front check cannot reject it
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if readErr == nil {
		t.Fatal("expected MaxBytesReader to fail the oversized read, got nil error")
	}
}
