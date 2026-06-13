package middleware

import (
	"bytes"
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// timeoutBody is the canonical 504 envelope (matches the error/message/action
// shape the handlers use). Static so we never allocate on the timeout path.
var timeoutBody = []byte(`{"error":"request_timeout","message":"the request exceeded the server time limit","action":"retry the request"}`)

// Timeout bounds total per-request processing time to d. When a handler runs
// past d the client gets a clean 504 (never a half-written body) and the
// request context is cancelled so downstream DB / HTTP calls abort.
//
// skip lets the caller exclude routes this middleware must NOT wrap — namely
// streaming responses (SSE, CSV). The implementation buffers the response to
// guarantee an all-or-nothing body, which is incompatible with incremental
// flushing; the router supplies the streaming-route predicate.
//
// Recovery ordering: the handler runs on a child goroutine, so a panic there is
// re-raised on the request goroutine where the outer gin.Recovery middleware
// (registered before this one) can log it and emit a 500.
func Timeout(d time.Duration, skip func(*gin.Context) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if skip != nil && skip(c) {
			c.Next()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)

		tw := &timeoutWriter{ResponseWriter: c.Writer, header: make(http.Header)}
		c.Writer = tw

		done := make(chan struct{})
		panicChan := make(chan any, 1)
		go func() {
			defer func() {
				if p := recover(); p != nil {
					panicChan <- p
				}
			}()
			c.Next()
			close(done)
		}()

		select {
		case p := <-panicChan:
			// The child goroutine has finished unwinding; restoring the real
			// writer here is race-free. Re-panic so Recovery handles it.
			c.Writer = tw.ResponseWriter
			panic(p)
		case <-done:
			// The child goroutine has returned (it closed done last), so
			// restoring the real writer here is race-free. We must restore it:
			// gin writes some responses AFTER the handler chain unwinds (e.g.
			// serveError writes the 404 body for an unmatched route), and those
			// writes have to land on the real socket, not the dead buffer.
			tw.flush()
			c.Writer = tw.ResponseWriter
		case <-ctx.Done():
			// Do NOT touch c.Writer here: the handler goroutine may still be
			// running and reading it. markTimedOut makes its late writes no-ops;
			// we write the 504 straight to the underlying writer.
			tw.markTimedOut()
			writeTimeoutResponse(tw.ResponseWriter)
			c.Abort()
		}
	}
}

func writeTimeoutResponse(w gin.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusGatewayTimeout)
	_, _ = w.Write(timeoutBody)
}

// timeoutWriter buffers everything a handler writes so the parent goroutine can
// decide, after the handler returns OR the deadline fires, whether to flush the
// buffered response or replace it with a 504. All exported surface is mutex
// guarded because the handler goroutine and the request goroutine touch it
// concurrently on the timeout path.
type timeoutWriter struct {
	gin.ResponseWriter

	// header is a PRIVATE header map owned by the handler goroutine. The
	// embedded gin.ResponseWriter's own header map is the real socket's; if we
	// let the handler write straight to it (the default, since we don't buffer
	// Header()), a late handler write races the timeout path writing the 504 to
	// that same map. Isolating headers here means the handler and the timeout
	// path touch different maps — no race — and flush() copies these onto the
	// real writer only on the success path.
	header http.Header

	mu       sync.Mutex
	body     bytes.Buffer
	code     int
	written  bool
	timedOut bool
}

// Header returns the private, handler-owned header map. It is touched only by
// the handler goroutine while the request runs, then read by flush() after the
// handler has returned (close(done) establishes the happens-before), so it
// needs no lock and never races the timeout goroutine's writes to the real
// ResponseWriter.
func (w *timeoutWriter) Header() http.Header {
	return w.header
}

func (w *timeoutWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timedOut {
		// Deadline already won the race; swallow the late write so the handler
		// goroutine cannot corrupt the 504 already on the wire.
		return len(b), nil
	}
	if !w.written {
		w.code = http.StatusOK
		w.written = true
	}
	return w.body.Write(b)
}

func (w *timeoutWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *timeoutWriter) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timedOut || w.written {
		return
	}
	w.code = code
	w.written = true
}

// WriteHeaderNow is buffered: gin calls it to commit the status, but we defer
// the real write until flush() so headers never reach the wire mid-handler.
func (w *timeoutWriter) WriteHeaderNow() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timedOut || w.written {
		return
	}
	w.code = http.StatusOK
	w.written = true
}

func (w *timeoutWriter) Status() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.code == 0 {
		return http.StatusOK
	}
	return w.code
}

func (w *timeoutWriter) Size() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.written {
		return -1
	}
	return w.body.Len()
}

func (w *timeoutWriter) Written() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.written
}

// markTimedOut records 504 for downstream metrics and blocks further buffered
// writes from the handler goroutine.
func (w *timeoutWriter) markTimedOut() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.timedOut = true
	w.code = http.StatusGatewayTimeout
	w.written = true
}

// flush copies the buffered response to the real writer. Called only on the
// success path, when the handler returned before the deadline.
//
// When the inner chain wrote nothing (e.g. an unmatched route headed for gin's
// NoRoute/404 machinery, or a handler that defers the default 200 to the
// engine), flush is a no-op: writing a status here would pre-empt that outer
// logic and turn a 404 into a 200.
func (w *timeoutWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timedOut || !w.written {
		return
	}
	// Copy the handler's buffered headers onto the real writer before the status
	// line. Safe here: the handler goroutine has returned, so nothing else
	// touches either map.
	dst := w.ResponseWriter.Header()
	for k, vv := range w.header {
		dst[k] = vv
	}
	w.ResponseWriter.WriteHeader(w.code)
	if w.body.Len() > 0 {
		_, _ = w.ResponseWriter.Write(w.body.Bytes())
	}
}
