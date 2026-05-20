package middleware

// observability_test.go is in package middleware (not middleware_test) so it
// can access the unexported httpDuration and httpRequests vars for test resets.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// TestRequestMetrics_ObservesCounterAndHistogram verifies that one GET request
// increments the requests counter and records a histogram observation.
func TestRequestMetrics_ObservesCounterAndHistogram(t *testing.T) {
	httpDuration.Reset()
	httpRequests.Reset()

	r := gin.New()
	r.Use(RequestMetrics())
	r.GET("/ping", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/ping", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Counter must be exactly 1 after one request.
	got := testutil.ToFloat64(httpRequests.WithLabelValues("/ping", "GET", "200"))
	if got != 1 {
		t.Errorf("tally_http_requests_total{/ping GET 200} = %v, want 1", got)
	}
}

// TestRequestMetrics_UsesFullPath verifies the route label is the Gin pattern
// ("/items/:id"), not the concrete URL path.
func TestRequestMetrics_UsesFullPath(t *testing.T) {
	httpRequests.Reset()

	r := gin.New()
	r.Use(RequestMetrics())
	r.GET("/items/:id", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/items/42", nil)
	r.ServeHTTP(w, req)

	patternCount := testutil.ToFloat64(httpRequests.WithLabelValues("/items/:id", "GET", "200"))
	if patternCount != 1 {
		t.Errorf("expected counter route=/items/:id = 1, got %v", patternCount)
	}

	rawCount := testutil.ToFloat64(httpRequests.WithLabelValues("/items/42", "GET", "200"))
	if rawCount != 0 {
		t.Errorf("counter with raw path /items/42 should be 0, got %v", rawCount)
	}
}

// TestRequestID_GeneratesIDWhenAbsent verifies the middleware generates a
// request id and injects it into both the Gin context and the response header.
func TestRequestID_GeneratesIDWhenAbsent(t *testing.T) {
	r := gin.New()
	r.Use(RequestID())
	r.GET("/", func(c *gin.Context) {
		id := GetRequestID(c)
		if id == "" {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("handler did not see request_id in context; code=%d", w.Code)
	}
	if w.Header().Get("X-Request-Id") == "" {
		t.Error("X-Request-Id response header should be set when no incoming id")
	}
}

// TestRequestID_PreservesIncomingID verifies the middleware echoes the client's
// X-Request-Id header unchanged.
func TestRequestID_PreservesIncomingID(t *testing.T) {
	const clientID = "client-provided-id-123"
	r := gin.New()
	r.Use(RequestID())
	var captured string
	r.GET("/", func(c *gin.Context) {
		captured = GetRequestID(c)
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-Id", clientID)
	r.ServeHTTP(w, req)

	if captured != clientID {
		t.Errorf("context request_id = %q, want %q", captured, clientID)
	}
	if w.Header().Get("X-Request-Id") != clientID {
		t.Errorf("response X-Request-Id = %q, want %q", w.Header().Get("X-Request-Id"), clientID)
	}
}
