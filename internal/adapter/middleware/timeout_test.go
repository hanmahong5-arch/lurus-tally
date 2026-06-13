package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

// newReq routes a GET at path through eng and returns the recorder.
func serve(eng *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	eng.ServeHTTP(w, req)
	return w
}

func TestRequestTimeout_AttachesDeadlineToContext(t *testing.T) {
	eng := gin.New()
	eng.Use(RequestTimeout(50*time.Millisecond, nil))

	var hadDeadline bool
	eng.GET("/x", func(c *gin.Context) {
		_, hadDeadline = c.Request.Context().Deadline()
		c.Status(http.StatusOK)
	})

	if w := serve(eng, "/x"); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !hadDeadline {
		t.Fatal("handler context must carry a deadline")
	}
}

func TestRequestTimeout_SkipPredicateLeavesContextUnbounded(t *testing.T) {
	eng := gin.New()
	skip := func(c *gin.Context) bool { return c.FullPath() == "/stream" }
	eng.Use(RequestTimeout(50*time.Millisecond, skip))

	var streamHadDeadline, plainHadDeadline bool
	eng.GET("/stream", func(c *gin.Context) {
		_, streamHadDeadline = c.Request.Context().Deadline()
		c.Status(http.StatusOK)
	})
	eng.GET("/plain", func(c *gin.Context) {
		_, plainHadDeadline = c.Request.Context().Deadline()
		c.Status(http.StatusOK)
	})

	serve(eng, "/stream")
	serve(eng, "/plain")

	if streamHadDeadline {
		t.Error("skipped (streaming) route must NOT carry a deadline")
	}
	if !plainHadDeadline {
		t.Error("non-skipped route must carry a deadline")
	}
}

func TestRequestTimeout_CancelsHandlerThatHonoursContext(t *testing.T) {
	eng := gin.New()
	eng.Use(RequestTimeout(20*time.Millisecond, nil))

	var cancelled bool
	eng.GET("/slow", func(c *gin.Context) {
		select {
		case <-c.Request.Context().Done():
			cancelled = true
			c.Status(http.StatusGatewayTimeout)
		case <-time.After(2 * time.Second):
			c.Status(http.StatusOK)
		}
	})

	start := time.Now()
	w := serve(eng, "/slow")
	elapsed := time.Since(start)

	if !cancelled {
		t.Fatal("handler must observe context cancellation at the deadline")
	}
	if elapsed > time.Second {
		t.Fatalf("deadline did not fire promptly: %v", elapsed)
	}
	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", w.Code)
	}
}

// TestRequestTimeout_DeadlineDerivedFromIncomingContext verifies the middleware
// chains onto (does not discard) a deadline already on the request context: a
// shorter inbound deadline must still win over the middleware's longer one.
func TestRequestTimeout_DeadlineDerivedFromIncomingContext(t *testing.T) {
	eng := gin.New()
	eng.Use(RequestTimeout(time.Hour, nil)) // long; the inbound one must win

	var deadline time.Time
	var ok bool
	eng.GET("/y", func(c *gin.Context) {
		deadline, ok = c.Request.Context().Deadline()
		c.Status(http.StatusOK)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/y", nil).WithContext(ctx)
	eng.ServeHTTP(w, req)

	if !ok {
		t.Fatal("expected a deadline on the handler context")
	}
	if time.Until(deadline) > time.Minute {
		t.Fatalf("inbound short deadline should win, got %v out", time.Until(deadline))
	}
}
