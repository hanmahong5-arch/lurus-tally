// Package middleware holds cross-cutting Gin middleware.
package middleware

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestTimeout bounds per-request processing for non-streaming routes by
// attaching a deadline to the request context. Every downstream call that
// honours ctx — GORM queries (WithContext), the httpx LLM/platform clients,
// go-redis, NATS — observes the deadline and aborts, so a stuck dependency can
// no longer pin a request goroutine indefinitely.
//
// Why a context deadline and NOT a goroutine-driven hard 504 (the previous,
// removed design): enforcing a wall-clock cap on a handler that *ignores* its
// context requires running the handler chain on a child goroutine while the
// parent watches the clock. But *gin.Context is not concurrency-safe — its
// unexported index/handlers fields are mutated by c.Next() — so parent and
// child racing on it is a genuine data race with no fix through the exported
// API. Critically, that old design did not actually stop a runaway handler
// either: it returned 504 to the client and left the orphaned goroutine
// running (leaked). This design is therefore no weaker for the only case the
// goroutine bought us (a context-ignoring handler — none exist here, all
// blocking work is cancellable I/O) and strictly better otherwise: no race, no
// goroutine per request, no buffering the whole response in memory.
//
// A connection-level write deadline as a hard backstop was considered and
// rejected: with the server's WriteTimeout intentionally 0 (for SSE), net/http
// does not reset a manually-set write deadline between keep-alive requests, so
// it would leak onto a later request on the same connection — including a
// streaming one it must not bound.
//
// skip excludes routes whose response is streamed incrementally (the SSE chat
// endpoint and CSV exports): a deadline would abort a legitimately long LLM
// turn or a large export mid-stream. The router supplies the predicate.
func RequestTimeout(d time.Duration, skip func(*gin.Context) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if skip != nil && skip(c) {
			c.Next()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
