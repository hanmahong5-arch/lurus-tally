package memorusclient_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
)

// zzFakeServer builds an httptest.Server driven by handler and registers cleanup.
func zzFakeServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// --- classifyStatus is unexported, so we exercise it indirectly through Delete,
// which returns the classifyStatus result directly with no other transformation.

func TestZZClassifyStatus_Table(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		wantErr    error // nil means expect nil error
		wantWrap   bool  // when wantErr is nil-sentinel but we expect wrapped ErrUnavailable
	}{
		{name: "200 OK -> nil", statusCode: http.StatusOK, wantErr: nil},
		{name: "204 NoContent -> nil", statusCode: http.StatusNoContent, wantErr: nil},
		{name: "299 boundary still 2xx -> nil", statusCode: 299, wantErr: nil},
		{name: "300 boundary no longer 2xx -> ErrUnavailable", statusCode: 300, wantWrap: true},
		{name: "401 -> ErrUnauthorized", statusCode: http.StatusUnauthorized, wantErr: memorusclient.ErrUnauthorized},
		{name: "404 -> ErrNotFound", statusCode: http.StatusNotFound, wantErr: memorusclient.ErrNotFound},
		{name: "500 -> ErrUnavailable", statusCode: http.StatusInternalServerError, wantWrap: true},
		{name: "429 -> ErrUnavailable", statusCode: http.StatusTooManyRequests, wantWrap: true},
		{name: "418 teapot -> ErrUnavailable", statusCode: 418, wantWrap: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := zzFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
			})

			c, err := memorusclient.New(memorusclient.Config{BaseURL: srv.URL, APIKey: "k"})
			if err != nil || c == nil {
				t.Fatalf("New failed: %v", err)
			}

			gotErr := c.Delete(context.Background(), "some-id")

			switch {
			case tc.wantErr == nil && !tc.wantWrap:
				if gotErr != nil {
					t.Fatalf("expected nil error for status %d, got: %v", tc.statusCode, gotErr)
				}
			case tc.wantWrap:
				if gotErr == nil {
					t.Fatalf("expected ErrUnavailable for status %d, got nil", tc.statusCode)
				}
				if !errors.Is(gotErr, memorusclient.ErrUnavailable) {
					t.Fatalf("expected ErrUnavailable wrap for status %d, got: %v", tc.statusCode, gotErr)
				}
			default:
				if !errors.Is(gotErr, tc.wantErr) {
					t.Fatalf("expected errors.Is(%v, %v) for status %d", gotErr, tc.wantErr, tc.statusCode)
				}
			}
		})
	}
}

func TestZZIsUnavailable_WrappedFmtErrorf(t *testing.T) {
	wrapped := fmt.Errorf("%w: HTTP 500", memorusclient.ErrUnavailable)
	if !memorusclient.IsUnavailable(wrapped) {
		t.Fatalf("expected IsUnavailable(true) for fmt.Errorf-wrapped ErrUnavailable")
	}
	if memorusclient.IsUnavailable(errors.New("plain error")) {
		t.Fatalf("expected IsUnavailable(false) for an unrelated plain error")
	}
	if memorusclient.IsUnavailable(nil) {
		t.Fatalf("expected IsUnavailable(false) for nil error")
	}
}

// --- New: both empty branches, and the case where only one field is empty.

func TestZZNew_DegradedMode_BothEmpty(t *testing.T) {
	c, err := memorusclient.New(memorusclient.Config{})
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if c != nil {
		t.Fatalf("expected nil client when both BaseURL and APIKey are empty")
	}
}

func TestZZNew_DefaultTimeoutApplied(t *testing.T) {
	// Timeout: 0 should fall back to the package default without erroring.
	srv := zzFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	c, err := memorusclient.New(memorusclient.Config{BaseURL: srv.URL, APIKey: "k", Timeout: 0})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}
	if err := c.Delete(context.Background(), "id"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
}

// --- Add: request creation error, marshal error, 404 mapping, 500 mapping.

func TestZZAdd_RequestCreationError(t *testing.T) {
	// A control character in the base URL makes http.NewRequestWithContext fail
	// deterministically (net/url: invalid control character in URL) without ever
	// reaching the network.
	c, err := memorusclient.New(memorusclient.Config{BaseURL: "http://\x7f", APIKey: "k"})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	mem, err := c.Add(context.Background(), "u", "content", nil)
	if err == nil {
		t.Fatal("expected error from malformed base URL, got nil")
	}
	if mem != nil {
		t.Fatalf("expected nil Memory on error, got: %+v", mem)
	}
	if !errors.Is(err, memorusclient.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable wrap for request-creation failure, got: %v", err)
	}
}

func TestZZAdd_MarshalError(t *testing.T) {
	c, err := memorusclient.New(memorusclient.Config{BaseURL: "http://example.invalid", APIKey: "k"})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	// channels are not JSON-marshalable, forcing json.Marshal to fail before any
	// network I/O happens.
	badMeta := map[string]any{"bad": make(chan int)}
	mem, err := c.Add(context.Background(), "u", "content", badMeta)
	if err == nil {
		t.Fatal("expected marshal error, got nil")
	}
	if mem != nil {
		t.Fatalf("expected nil Memory on marshal error, got: %+v", mem)
	}
}

func TestZZAdd_404_ReturnsErrNotFound(t *testing.T) {
	srv := zzFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	c, err := memorusclient.New(memorusclient.Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	mem, err := c.Add(context.Background(), "u", "content", nil)
	if !errors.Is(err, memorusclient.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
	if mem != nil {
		t.Fatalf("expected nil Memory on non-2xx, got: %+v", mem)
	}
}

func TestZZAdd_500_ReturnsErrUnavailable(t *testing.T) {
	srv := zzFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	c, err := memorusclient.New(memorusclient.Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	mem, err := c.Add(context.Background(), "u", "content", nil)
	if !errors.Is(err, memorusclient.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got: %v", err)
	}
	if mem != nil {
		t.Fatalf("expected nil Memory on non-2xx, got: %+v", mem)
	}
}

func TestZZAdd_DoesNotTrustServerBody(t *testing.T) {
	// The server returns a body claiming a *different* user/content than what was
	// requested. Add must synthesise the Memory purely from its own inputs and
	// must NOT echo back whatever the server sent.
	srv := zzFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"id":"server-side-id","user_id":"someone-else","content":"not what we sent"}}`))
	})
	c, err := memorusclient.New(memorusclient.Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	before := time.Now().UTC()
	mem, err := c.Add(context.Background(), "our-user", "our-content", map[string]any{"k": "v"})
	after := time.Now().UTC()
	if err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if mem.UserID != "our-user" {
		t.Fatalf("expected UserID echoed from request (our-user), got: %s", mem.UserID)
	}
	if mem.Content != "our-content" {
		t.Fatalf("expected Content echoed from request (our-content), got: %s", mem.Content)
	}
	if mem.Metadata["k"] != "v" {
		t.Fatalf("expected Metadata echoed from request, got: %+v", mem.Metadata)
	}
	if mem.ID != "" {
		t.Fatalf("expected ID to stay zero-value (never trust server body), got: %s", mem.ID)
	}
	if mem.CreatedAt.Before(before) || mem.CreatedAt.After(after) {
		t.Fatalf("expected CreatedAt to be set to now(), got: %v (window [%v, %v])", mem.CreatedAt, before, after)
	}
}

// --- Search: query building branches, decode error, empty results, request
// creation error.

func TestZZSearch_QueryBuilding_NoUserIDNoLimit(t *testing.T) {
	var gotQuery, gotUserID, gotLimit string
	srv := zzFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		gotUserID = r.URL.Query().Get("user_id")
		gotLimit = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	})
	c, err := memorusclient.New(memorusclient.Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	// userID == "" and limit <= 0 must skip setting those query params entirely.
	mems, err := c.Search(context.Background(), "", "q", 0)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if gotQuery != "q" {
		t.Fatalf("expected query=q, got %q", gotQuery)
	}
	if gotUserID != "" {
		t.Fatalf("expected empty user_id param when userID is empty, got %q", gotUserID)
	}
	if gotLimit != "" {
		t.Fatalf("expected empty limit param when limit<=0, got %q", gotLimit)
	}
	if mems == nil {
		t.Fatal("expected non-nil empty slice for empty results")
	}
	if len(mems) != 0 {
		t.Fatalf("expected 0 results, got %d", len(mems))
	}
}

func TestZZSearch_NegativeLimit_OmitsParam(t *testing.T) {
	var gotLimit string
	var sawLimit bool
	srv := zzFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, sawLimit = r.URL.Query()["limit"]
		if sawLimit {
			gotLimit = r.URL.Query().Get("limit")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	})
	c, err := memorusclient.New(memorusclient.Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	if _, err := c.Search(context.Background(), "u", "q", -5); err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if sawLimit {
		t.Fatalf("expected limit param to be omitted for negative limit, got %q", gotLimit)
	}
}

func TestZZSearch_MalformedJSON_ReturnsErrUnavailable(t *testing.T) {
	srv := zzFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not valid json`))
	})
	c, err := memorusclient.New(memorusclient.Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	mems, err := c.Search(context.Background(), "u", "q", 5)
	if !errors.Is(err, memorusclient.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable for malformed JSON body, got: %v", err)
	}
	if mems != nil {
		t.Fatalf("expected nil slice on decode error, got: %+v", mems)
	}
}

func TestZZSearch_MissingCreatedAt_LeavesZeroTime(t *testing.T) {
	srv := zzFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"id":"m1","memory":"hi","user_id":"u","created_at":""}]}`))
	})
	c, err := memorusclient.New(memorusclient.Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	mems, err := c.Search(context.Background(), "u", "q", 5)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected 1 result, got %d", len(mems))
	}
	if !mems[0].CreatedAt.IsZero() {
		t.Fatalf("expected zero-value CreatedAt when created_at is empty, got: %v", mems[0].CreatedAt)
	}
}

func TestZZSearch_401_ReturnsErrUnauthorized(t *testing.T) {
	srv := zzFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	c, err := memorusclient.New(memorusclient.Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	mems, err := c.Search(context.Background(), "u", "q", 5)
	if !errors.Is(err, memorusclient.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got: %v", err)
	}
	if mems != nil {
		t.Fatalf("expected nil slice on non-2xx, got: %+v", mems)
	}
}

func TestZZSearch_RequestCreationError(t *testing.T) {
	c, err := memorusclient.New(memorusclient.Config{BaseURL: "http://\x7f", APIKey: "k"})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	mems, err := c.Search(context.Background(), "u", "q", 5)
	if !errors.Is(err, memorusclient.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable for malformed base URL, got: %v", err)
	}
	if mems != nil {
		t.Fatalf("expected nil slice on request-creation error, got: %+v", mems)
	}
}

func TestZZSearch_HeaderSet(t *testing.T) {
	var gotKey string
	srv := zzFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	})
	c, err := memorusclient.New(memorusclient.Config{BaseURL: srv.URL, APIKey: "search-secret"})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}
	if _, err := c.Search(context.Background(), "u", "q", 5); err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if gotKey != "search-secret" {
		t.Fatalf("expected X-API-Key header to be forwarded, got: %q", gotKey)
	}
}

// --- Delete: request creation error, network error, header + path assertions.

func TestZZDelete_RequestCreationError(t *testing.T) {
	c, err := memorusclient.New(memorusclient.Config{BaseURL: "http://\x7f", APIKey: "k"})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	err = c.Delete(context.Background(), "id-1")
	if !errors.Is(err, memorusclient.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable for malformed base URL, got: %v", err)
	}
}

func TestZZDelete_NetworkError_ReturnsErrUnavailable(t *testing.T) {
	c, err := memorusclient.New(memorusclient.Config{
		BaseURL: "http://127.0.0.1:1", // nothing listening
		APIKey:  "k",
		Timeout: 300 * time.Millisecond,
	})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	err = c.Delete(context.Background(), "id-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !memorusclient.IsUnavailable(err) {
		t.Fatalf("expected ErrUnavailable wrap for network failure, got: %v", err)
	}
}

func TestZZDelete_HeaderAndPathEncoding(t *testing.T) {
	var gotKey, gotPath string
	srv := zzFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})
	c, err := memorusclient.New(memorusclient.Config{BaseURL: srv.URL, APIKey: "delete-secret"})
	if err != nil || c == nil {
		t.Fatalf("New failed: %v", err)
	}

	if err := c.Delete(context.Background(), "mem-42"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if gotKey != "delete-secret" {
		t.Fatalf("expected X-API-Key header to be forwarded, got: %q", gotKey)
	}
	if gotPath != "/memories/mem-42" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
}
