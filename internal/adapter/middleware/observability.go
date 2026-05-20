package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	httpDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "tally_http_request_duration_seconds",
			Help:    "HTTP request latency histogram, by route, method, and status.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"route", "method", "status"},
	)

	httpRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tally_http_requests_total",
			Help: "Total HTTP requests served, by route, method, and status.",
		},
		[]string{"route", "method", "status"},
	)
)

func init() {
	prometheus.MustRegister(httpDuration, httpRequests)
}

// RequestMetrics returns a gin.HandlerFunc that records request latency and
// request counts. Routes are identified via c.FullPath() to avoid cardinality
// explosion from raw URL paths with embedded IDs.
// Mount this after gin.Recovery() so panics are caught before timing stops.
func RequestMetrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		route := c.FullPath()
		if route == "" {
			// FullPath is empty for 404/405. Use a stable synthetic label.
			route = "unmatched"
		}
		status := strconv.Itoa(c.Writer.Status())
		method := c.Request.Method
		elapsed := time.Since(start).Seconds()

		httpDuration.WithLabelValues(route, method, status).Observe(elapsed)
		httpRequests.WithLabelValues(route, method, status).Inc()
	}
}

// RequestID returns a gin.HandlerFunc that reads the X-Request-Id header and,
// when absent, generates a new UUID. The id is:
//   - stored in the Gin context under the key "request_id"
//   - echoed back in the X-Request-Id response header
//
// Mount this before RequestMetrics so the id is available to downstream
// middleware and handlers on the same request.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-Id")
		if id == "" {
			id = uuid.New().String()
		}
		c.Set(CtxKeyRequestID, id)
		c.Header("X-Request-Id", id)
		c.Next()
	}
}

// CtxKeyRequestID is the Gin context key where RequestID injects the request id.
const CtxKeyRequestID = "request_id"

// GetRequestID returns the request id injected by the RequestID middleware, or "" if absent.
func GetRequestID(c *gin.Context) string {
	v, _ := c.Get(CtxKeyRequestID)
	s, _ := v.(string)
	return s
}
