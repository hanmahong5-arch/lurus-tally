package memorusclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchWithFilter_SendsMetadataEqualityParams(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("metadata.subject_id")
		if r.URL.Query().Get("user_id") != "u1" || r.URL.Query().Get("query") != "q" {
			t.Errorf("query params: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"id":"m1","memory":"张三喜欢少糖","score":0.9,"metadata":{"metadata":{"subject_id":"partner:1"}}}]}`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL + "/api/v1", APIKey: "k"})
	if err != nil || c == nil {
		t.Fatalf("New: %v", err)
	}
	hits, err := c.SearchWithFilter(context.Background(), "u1", "q", 5, map[string]string{"subject_id": "partner:1"})
	if err != nil {
		t.Fatalf("SearchWithFilter: %v", err)
	}
	if got != "partner:1" {
		t.Errorf("metadata.subject_id = %q, want partner:1", got)
	}
	if len(hits) != 1 || hits[0].Content != "张三喜欢少糖" {
		t.Errorf("hits = %+v", hits)
	}
}
