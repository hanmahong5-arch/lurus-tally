package memorusclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
)

// fakeMemorus builds a test server that simulates the memorus API.
func fakeMemorus(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// TestClient_New_EmptyAPIKey_ReturnsNil verifies that an empty APIKey produces
// a nil client with no error (degraded/disabled mode).
func TestClient_New_EmptyAPIKey_ReturnsNil(t *testing.T) {
	c, err := memorusclient.New(memorusclient.Config{
		BaseURL: "http://memorus.example.svc:8880",
		APIKey:  "",
	})
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if c != nil {
		t.Fatalf("expected nil client when APIKey is empty, got non-nil")
	}
}

// TestClient_New_EmptyBaseURL_ReturnsNil verifies that an empty BaseURL produces
// a nil client with no error.
func TestClient_New_EmptyBaseURL_ReturnsNil(t *testing.T) {
	c, err := memorusclient.New(memorusclient.Config{
		BaseURL: "",
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if c != nil {
		t.Fatalf("expected nil client when BaseURL is empty, got non-nil")
	}
}

// TestClient_Add_HappyPath verifies that Add stores a memory and returns it.
func TestClient_Add_HappyPath(t *testing.T) {
	srv := fakeMemorus(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/memories" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("missing or wrong X-API-Key header")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"results": map[string]interface{}{"status": "ok"},
		})
	})

	c, err := memorusclient.New(memorusclient.Config{
		BaseURL: srv.URL,
		APIKey:  "test-key",
		Timeout: 5 * time.Second,
	})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	mem, err := c.Add(context.Background(), "user-123", "user prefers early morning reports", map[string]any{"source": "tally"})
	if err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if mem == nil {
		t.Fatal("Add returned nil memory")
	}
	if mem.Content != "user prefers early morning reports" {
		t.Errorf("unexpected content: %s", mem.Content)
	}
	if mem.UserID != "user-123" {
		t.Errorf("unexpected user_id: %s", mem.UserID)
	}
}

// TestClient_Search_ParsesResults verifies that Search returns correctly parsed memories.
func TestClient_Search_ParsesResults(t *testing.T) {
	srv := fakeMemorus(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/memories/search" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("query") != "low stock" {
			t.Errorf("missing query param: %s", r.URL.Query().Get("query"))
		}
		if r.URL.Query().Get("user_id") != "user-42" {
			t.Errorf("missing user_id param")
		}
		w.Header().Set("Content-Type", "application/json")
		results := map[string]interface{}{
			"results": []map[string]interface{}{
				{
					"id":         "mem-1",
					"memory":     "user asked about low stock last week",
					"user_id":    "user-42",
					"score":      0.92,
					"created_at": "2026-05-01T08:00:00Z",
					"metadata":   map[string]interface{}{"type": "conversation"},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(results)
	})

	c, err := memorusclient.New(memorusclient.Config{BaseURL: srv.URL, APIKey: "key"})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	mems, err := c.Search(context.Background(), "user-42", "low stock", 5)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected 1 result, got %d", len(mems))
	}
	if mems[0].ID != "mem-1" {
		t.Errorf("unexpected ID: %s", mems[0].ID)
	}
	if mems[0].Content != "user asked about low stock last week" {
		t.Errorf("unexpected content: %s", mems[0].Content)
	}
	if mems[0].Score != 0.92 {
		t.Errorf("unexpected score: %f", mems[0].Score)
	}
}

// TestClient_Search_NetworkError_ReturnsErrUnavailable verifies that a network
// failure is wrapped as ErrUnavailable.
func TestClient_Search_NetworkError_ReturnsErrUnavailable(t *testing.T) {
	// Use a URL that will fail immediately.
	c, err := memorusclient.New(memorusclient.Config{
		BaseURL: "http://127.0.0.1:1", // nothing listening here
		APIKey:  "key",
		Timeout: 500 * time.Millisecond,
	})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = c.Search(context.Background(), "u", "q", 5)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !memorusclient.IsUnavailable(err) {
		t.Errorf("expected ErrUnavailable wrapping, got: %v", err)
	}
}

// TestClient_Add_4xx_ReturnsErrUnauthorized verifies that a 401 response
// is mapped to ErrUnauthorized.
func TestClient_Add_4xx_ReturnsErrUnauthorized(t *testing.T) {
	srv := fakeMemorus(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"detail": "invalid api key"})
	})

	c, err := memorusclient.New(memorusclient.Config{BaseURL: srv.URL, APIKey: "bad-key"})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = c.Add(context.Background(), "u", "content", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != memorusclient.ErrUnauthorized {
		t.Errorf("expected ErrUnauthorized, got: %v", err)
	}
}

// TestClient_Delete_HappyPath verifies that Delete calls DELETE /memories/{id}.
func TestClient_Delete_HappyPath(t *testing.T) {
	const targetID = "mem-xyz"
	var gotMethod, gotPath string
	srv := fakeMemorus(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "deleted"})
	})

	c, err := memorusclient.New(memorusclient.Config{BaseURL: srv.URL, APIKey: "key"})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	err = c.Delete(context.Background(), targetID)
	if err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("expected DELETE, got %s", gotMethod)
	}
	if gotPath != "/memories/"+targetID {
		t.Errorf("unexpected path: %s", gotPath)
	}
}

// TestClient_Search_TimeoutHandling verifies that a hung server causes
// a context deadline / timeout error wrapped as ErrUnavailable.
func TestClient_Search_TimeoutHandling(t *testing.T) {
	srv := fakeMemorus(t, func(w http.ResponseWriter, r *http.Request) {
		// Block forever — simulates memorus being unresponsive.
		<-r.Context().Done()
	})

	c, err := memorusclient.New(memorusclient.Config{
		BaseURL: srv.URL,
		APIKey:  "key",
		Timeout: 50 * time.Millisecond,
	})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = c.Search(context.Background(), "u", "q", 5)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !memorusclient.IsUnavailable(err) {
		t.Errorf("expected ErrUnavailable for timeout, got: %v", err)
	}
}

// TestClient_Add_NetworkError_ReturnsErrUnavailable verifies network failures
// on Add are also wrapped as ErrUnavailable.
func TestClient_Add_NetworkError_ReturnsErrUnavailable(t *testing.T) {
	c, err := memorusclient.New(memorusclient.Config{
		BaseURL: "http://127.0.0.1:1",
		APIKey:  "key",
		Timeout: 100 * time.Millisecond,
	})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = c.Add(context.Background(), "u", "content", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !memorusclient.IsUnavailable(err) {
		t.Errorf("expected ErrUnavailable, got: %v", err)
	}
}
