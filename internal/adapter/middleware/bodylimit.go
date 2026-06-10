package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// BodyLimit caps the size of a request body at maxBytes. It is a backpressure
// backstop: an oversized or never-ending body is rejected before a handler
// reads it into memory.
//
// Two layers:
//   - A declared Content-Length over the limit is rejected up front with 413,
//     so the client learns immediately without streaming the body.
//   - The body is wrapped in http.MaxBytesReader, which also stops chunked /
//     undeclared bodies once they cross the limit (the subsequent read returns
//     an error the handler surfaces as a 4xx).
//
// Routes that legitimately need a tighter bound (e.g. the Shopify webhook) keep
// their own MaxBytesReader; the most restrictive limit applies first.
func BodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"error":   "payload_too_large",
				"message": "request body exceeds the maximum allowed size",
				"action":  "reduce the request size and retry",
			})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}
