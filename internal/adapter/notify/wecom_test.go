package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewWeComNotifier_EmptyURLReturnsNil(t *testing.T) {
	if n := NewWeComNotifier(""); n != nil {
		t.Fatalf("expected nil notifier for empty webhook URL, got %+v", n)
	}
}

func TestWeComNotifier_Send_RequestBody(t *testing.T) {
	var gotBody []byte
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = buf
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	n := NewWeComNotifier(srv.URL)
	if n == nil {
		t.Fatal("expected non-nil notifier")
	}

	if err := n.Send(context.Background(), "Alert", "stock is low"); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}

	var payload wecomTextMessage
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("failed to unmarshal request body %q: %v", gotBody, err)
	}
	if payload.MsgType != "text" {
		t.Errorf("msgtype = %q, want text", payload.MsgType)
	}
	if !strings.Contains(payload.Text.Content, "Alert") || !strings.Contains(payload.Text.Content, "stock is low") {
		t.Errorf("content = %q, want it to contain title and body", payload.Text.Content)
	}
}

func TestWeComNotifier_Send_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook url"}`))
	}))
	defer srv.Close()

	n := NewWeComNotifier(srv.URL)
	err := n.Send(context.Background(), "Alert", "body")
	if err == nil {
		t.Fatal("expected error for HTTP 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want it to mention HTTP 500", err)
	}
}

func TestWeComNotifier_Send_EmptyBodyOmitsSeparator(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = buf
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewWeComNotifier(srv.URL)
	if err := n.Send(context.Background(), "TitleOnly", ""); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	var payload wecomTextMessage
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if payload.Text.Content != "TitleOnly" {
		t.Errorf("content = %q, want exactly %q (no trailing separator)", payload.Text.Content, "TitleOnly")
	}
}
