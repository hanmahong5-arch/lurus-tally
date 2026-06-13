package lifecycle

import (
	"net/http"
	"time"
)

// HTTP server resource bounds. A bare &http.Server{} has every timeout set to
// zero, i.e. no limit, which lets a slow or stuck client hold a connection (and
// the goroutine + memory behind it) open forever — a trivial resource-exhaustion
// vector. These bound the request side without touching the response side.
//
// WriteTimeout is intentionally left at zero: POST /api/v1/ai/chat streams
// Server-Sent Events for the full duration of an LLM turn (tens of seconds),
// and a WriteTimeout would sever that stream mid-response. Per-request
// processing limits for the non-streaming routes live in the Timeout
// middleware instead, which excludes the streaming routes.
const (
	// serverReadHeaderTimeout bounds how long we wait for request headers —
	// the primary slowloris defence.
	serverReadHeaderTimeout = 10 * time.Second
	// serverReadTimeout bounds reading the whole request (headers + body).
	// Generous enough for CSV/import uploads, short enough to reap a stalled
	// upload.
	serverReadTimeout = 60 * time.Second
	// serverIdleTimeout bounds how long an idle keep-alive connection lingers.
	serverIdleTimeout = 120 * time.Second
	// serverMaxHeaderBytes caps total header size (1 MiB) against header-bomb
	// requests; the default 1 MB is fine but we set it explicitly.
	serverMaxHeaderBytes = 1 << 20
)

// newServer builds the HTTP server with explicit resource bounds. Extracted
// into a factory so the timeout configuration is unit-testable without binding
// a TCP listener.
func newServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		IdleTimeout:       serverIdleTimeout,
		MaxHeaderBytes:    serverMaxHeaderBytes,
	}
}
