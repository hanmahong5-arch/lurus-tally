package memorusclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSearchAsOf_SendsTheMomentAndTheFilter(t *testing.T) {
	var got map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"id":"m1","content":"张三只收现金","score":0.9,"created_at":"2026-03-01T00:00:00Z"}]}`))
	}))
	defer srv.Close()
	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 3, 31, 23, 59, 59, 0, time.FixedZone("CST", 8*3600))
	hits, err := c.SearchAsOf(context.Background(), "t1", "张三", 10, map[string]string{"subject_id": "partner:x"}, at)
	if err != nil {
		t.Fatal(err)
	}
	if got["as_of"][0] != "2026-03-31T15:59:59Z" {
		t.Errorf("as_of = %q (must be UTC RFC3339)", got["as_of"])
	}
	if got["metadata.subject_id"][0] != "partner:x" || got["user_id"][0] != "t1" {
		t.Errorf("filter params: %v", got)
	}
	if len(hits) != 1 || hits[0].Content != "张三只收现金" {
		t.Errorf("hits: %+v", hits)
	}
}
