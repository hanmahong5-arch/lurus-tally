package lifecycle

import (
	"net/http"
	"testing"
)

// TestNewServer_SetsResourceBounds proves the server factory bounds the request
// side (header/read/idle timeouts + header cap) while deliberately leaving
// WriteTimeout at zero so the SSE chat endpoint can stream for a full LLM turn.
func TestNewServer_SetsResourceBounds(t *testing.T) {
	h := http.NewServeMux()
	srv := newServer(":18200", h)

	if srv.Addr != ":18200" {
		t.Errorf("Addr: got %q, want :18200", srv.Addr)
	}
	if srv.Handler == nil {
		t.Error("Handler must be set")
	}
	if srv.ReadHeaderTimeout <= 0 {
		t.Errorf("ReadHeaderTimeout must be > 0, got %v", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout <= 0 {
		t.Errorf("ReadTimeout must be > 0, got %v", srv.ReadTimeout)
	}
	if srv.IdleTimeout <= 0 {
		t.Errorf("IdleTimeout must be > 0, got %v", srv.IdleTimeout)
	}
	if srv.MaxHeaderBytes <= 0 {
		t.Errorf("MaxHeaderBytes must be > 0, got %d", srv.MaxHeaderBytes)
	}
	// WriteTimeout intentionally unset: a non-zero value would sever the SSE
	// stream served by POST /api/v1/ai/chat.
	if srv.WriteTimeout != 0 {
		t.Errorf("WriteTimeout must be 0 to allow SSE streaming, got %v", srv.WriteTimeout)
	}
}
