package memorusclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const objectBody = `{
  "subject_id": "partner:x", "object_type": "partner", "as_of": "2026-03-31T15:59:59Z",
  "slots": [
    {"slot": "payment_method", "single": true,
     "current": {"id": "m2", "content": "张三改用微信", "slot_value": "微信", "source": "tally-ai",
                 "valid_from": "2026-03-01T00:00:00Z", "effective_from": "2026-03-01T00:00:00Z",
                 "created_at": "2026-05-02T00:00:00Z", "evidence_count": 2, "conflict_with": ["m1"]},
     "values": [{"id": "m2", "content": "张三改用微信", "effective_from": "2026-03-01T00:00:00Z",
                 "created_at": "2026-05-02T00:00:00Z", "evidence_count": 2}]},
    {"slot": "taste", "single": false, "values": [
      {"id": "t1", "content": "张三爱喝冰美式", "slot_value": "冰美式", "effective_from": "2026-01-01T00:00:00Z", "created_at": "2026-01-01T00:00:00Z", "evidence_count": 1},
      {"id": "t2", "content": "张三也喝热拿铁", "slot_value": "热拿铁", "effective_from": "2026-01-02T00:00:00Z", "created_at": "2026-01-02T00:00:00Z", "evidence_count": 1}]}
  ],
  "untyped": [{"id": "u1", "content": "张三人很好", "effective_from": "2026-01-03T00:00:00Z", "created_at": "2026-01-03T00:00:00Z", "evidence_count": 1}],
  "conflicts": [{"slot": "payment_method", "ids": ["m1", "m2"], "reason": "source_disagreement"}],
  "truncated": false
}`

func TestGetObject_ParsesTheViewAndSendsAsOf(t *testing.T) {
	var gotPath, gotAsOf, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAsOf, gotKey = r.URL.Path, r.URL.Query().Get("as_of"), r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(objectBody))
	}))
	defer srv.Close()
	c, err := New(Config{BaseURL: srv.URL + "/api/v1", APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 3, 31, 23, 59, 59, 0, time.FixedZone("CST", 8*3600))
	v, err := c.GetObject(context.Background(), "partner:x", &at)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/objects/partner:x" || gotKey != "k" {
		t.Errorf("request: path=%q key=%q", gotPath, gotKey)
	}
	if gotAsOf != "2026-03-31T15:59:59Z" {
		t.Errorf("as_of = %q (must be UTC RFC3339)", gotAsOf)
	}
	if v.SubjectID != "partner:x" || v.ObjectType != "partner" || v.AsOf == nil || !v.AsOf.Equal(at) {
		t.Errorf("header fields: %+v", v)
	}
	pay := v.Slot("payment_method")
	if pay == nil || !pay.Single || pay.Current == nil || pay.Current.SlotValue != "微信" {
		t.Fatalf("payment slot: %+v", pay)
	}
	if pay.Current.ValidFrom == nil || pay.Current.ValidFrom.Format(time.RFC3339) != "2026-03-01T00:00:00Z" {
		t.Errorf("valid_from: %+v", pay.Current.ValidFrom)
	}
	if !pay.Current.EffectiveFrom.Equal(*pay.Current.ValidFrom) || pay.Current.CreatedAt.Month() != time.May {
		t.Errorf("effective_from/created_at: %+v", pay.Current)
	}
	if len(pay.Current.ConflictWith) != 1 || pay.Current.ConflictWith[0] != "m1" || pay.Current.EvidenceCount != 2 {
		t.Errorf("conflict_with/evidence: %+v", pay.Current)
	}
	taste := v.Slot("taste")
	if taste == nil || taste.Single || taste.Current != nil || len(taste.Values) != 2 || taste.Values[1].SlotValue != "热拿铁" {
		t.Errorf("taste slot: %+v", taste)
	}
	if len(v.Untyped) != 1 || v.Untyped[0].Content != "张三人很好" {
		t.Errorf("untyped: %+v", v.Untyped)
	}
	if len(v.Conflicts) != 1 || v.Conflicts[0].Reason != "source_disagreement" || len(v.Conflicts[0].IDs) != 2 {
		t.Errorf("conflicts: %+v", v.Conflicts)
	}
	if v.Slot("nope") != nil || v.Truncated {
		t.Errorf("unknown slot / truncated: %+v", v)
	}
}

func TestGetObject_404MeansUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer srv.Close()
	c, _ := New(Config{BaseURL: srv.URL + "/api/v1", APIKey: "k"})
	_, err := c.GetObject(context.Background(), "partner:x", nil)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
	// Not the generic not-found: callers must not read "no such subject" into it.
	if errors.Is(err, ErrNotFound) {
		t.Fatal("must not be ErrNotFound")
	}
}

func TestGetObject_EmptyObjectIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"subject_id":"partner:nobody","slots":[],"untyped":[],"conflicts":[],"truncated":false}`))
	}))
	defer srv.Close()
	c, _ := New(Config{BaseURL: srv.URL + "/api/v1", APIKey: "k"})
	v, err := c.GetObject(context.Background(), "partner:nobody", nil)
	if err != nil || len(v.Slots) != 0 || v.AsOf != nil {
		t.Fatalf("v=%+v err=%v", v, err)
	}
}

func TestAddWithOptions_SendsValidFromAndReplacesValueAndReadsTheResponse(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"n1","content":"x","created_at":"2026-05-02T00:00:00.123456Z","event":"ADD","superseded":["o1"],"unmatched_replaces_value":false}`))
	}))
	defer srv.Close()
	c, _ := New(Config{BaseURL: srv.URL + "/api/v1", APIKey: "k"})
	since := time.Date(2026, 3, 1, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	res, err := c.AddWithOptions(context.Background(), "t1", "张三改喝澳白",
		map[string]any{"subject_id": "partner:x", "slot": "taste", "slot_single": false},
		AddOptions{ValidFrom: &since, ReplacesValue: "冰美式"})
	if err != nil {
		t.Fatal(err)
	}
	if got["valid_from"] != "2026-02-28T16:00:00Z" {
		t.Errorf("valid_from = %v (must be UTC RFC3339)", got["valid_from"])
	}
	meta, _ := got["metadata"].(map[string]any)
	if meta["replaces_value"] != "冰美式" || meta["subject_id"] != "partner:x" || meta["slot_single"] != false {
		t.Errorf("metadata = %v", meta)
	}
	if res.ID != "n1" || res.Event != "ADD" || len(res.Superseded) != 1 || res.Superseded[0] != "o1" || res.UnmatchedReplacesValue {
		t.Errorf("result = %+v", res)
	}
	if res.CreatedAt.Format(time.RFC3339Nano) != "2026-05-02T00:00:00.123456Z" {
		t.Errorf("created_at = %s", res.CreatedAt)
	}
}

func TestAddWithOptions_OmitsAbsentExtras(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"n2","created_at":"2026-05-02T00:00:00Z","event":"NOOP","unmatched_replaces_value":true}`))
	}))
	defer srv.Close()
	c, _ := New(Config{BaseURL: srv.URL + "/api/v1", APIKey: "k"})
	res, err := c.AddWithOptions(context.Background(), "t1", "x", nil, AddOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["valid_from"]; ok {
		t.Errorf("valid_from must be absent: %v", got)
	}
	if _, ok := got["metadata"]; ok {
		t.Errorf("metadata must be absent when nothing was given: %v", got)
	}
	if res.Event != "NOOP" || !res.UnmatchedReplacesValue || len(res.Superseded) != 0 {
		t.Errorf("result = %+v", res)
	}
}
