package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
)

// newTimeoutEngine builds an engine with Recovery + Timeout, mirroring the
// production middleware order (Recovery outermost so it catches handler panics
// re-raised by Timeout).
func newTimeoutEngine(d time.Duration, skip func(*gin.Context) bool) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Timeout(d, skip))
	return r
}

// TestTimeout_SlowHandler_Returns504 proves a handler that runs past the
// deadline yields a clean 504 with the timeout envelope, and that the request
// returns BEFORE the handler finishes (it does not hang until the handler is
// done).
func TestTimeout_SlowHandler_Returns504(t *testing.T) {
	handlerDone := make(chan struct{})
	r := newTimeoutEngine(40*time.Millisecond, nil)
	r.GET("/slow", func(c *gin.Context) {
		defer close(handlerDone)
		time.Sleep(300 * time.Millisecond)
		c.JSON(http.StatusOK, gin.H{"ok": true}) // swallowed: deadline already won
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/slow", nil)

	start := time.Now()
	r.ServeHTTP(w, req)
	elapsed := time.Since(start)

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("status: got %d, want 504", w.Code)
	}
	if !strings.Contains(w.Body.String(), "request_timeout") {
		t.Errorf("body: got %q, want timeout envelope", w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"ok"`) {
		t.Errorf("body leaked the handler's late write: %q", w.Body.String())
	}
	// Returned promptly — well under the handler's 300ms sleep.
	if elapsed > 200*time.Millisecond {
		t.Errorf("ServeHTTP hung %v, expected it to return near the 40ms deadline", elapsed)
	}
	<-handlerDone // join the detached handler goroutine before the test exits
}

// TestTimeout_FastHandler_PassesThrough proves a handler that finishes within
// the deadline has its real response flushed through untouched.
func TestTimeout_FastHandler_PassesThrough(t *testing.T) {
	r := newTimeoutEngine(500*time.Millisecond, nil)
	r.GET("/fast", func(c *gin.Context) {
		c.JSON(http.StatusCreated, gin.H{"value": "ok"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/fast", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"value":"ok"`) {
		t.Errorf("body: got %q, want the handler's JSON", w.Body.String())
	}
}

// TestTimeout_SkipsExcludedRoute proves the skip predicate bypasses the
// middleware entirely, so a slow streaming route is NOT wrapped (and would not
// be capped at the deadline).
func TestTimeout_SkipsExcludedRoute(t *testing.T) {
	skip := func(c *gin.Context) bool { return c.FullPath() == "/stream" }
	r := newTimeoutEngine(20*time.Millisecond, skip)
	r.GET("/stream", func(c *gin.Context) {
		time.Sleep(60 * time.Millisecond) // exceeds the deadline, but skipped
		c.String(http.StatusOK, "streamed")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (route should be skipped)", w.Code)
	}
	if w.Body.String() != "streamed" {
		t.Errorf("body: got %q, want %q", w.Body.String(), "streamed")
	}
}

// TestTimeout_HandlerPanic_PropagatesToRecovery proves a panic in the handler
// goroutine is re-raised on the request goroutine and handled by the outer
// Recovery middleware (500), rather than crashing the process.
func TestTimeout_HandlerPanic_PropagatesToRecovery(t *testing.T) {
	r := newTimeoutEngine(500*time.Millisecond, nil)
	r.GET("/boom", func(c *gin.Context) {
		panic("handler exploded")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	// Must not panic out of ServeHTTP.
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500 from Recovery", w.Code)
	}
}
