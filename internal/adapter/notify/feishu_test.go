package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewFeishuNotifier_EmptyURLReturnsNil(t *testing.T) {
	if n := NewFeishuNotifier(""); n != nil {
		t.Fatalf("expected nil notifier for empty webhook URL, got %+v", n)
	}
}

func TestFeishuNotifier_Send_RequestBody(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = buf
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	defer srv.Close()

	n := NewFeishuNotifier(srv.URL)
	if n == nil {
		t.Fatal("expected non-nil notifier")
	}

	if err := n.Send(context.Background(), "Alert", "replenishment needed"); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	var payload feishuTextMessage
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("failed to unmarshal request body %q: %v", gotBody, err)
	}
	if payload.MsgType != "text" {
		t.Errorf("msg_type = %q, want text", payload.MsgType)
	}
	if !strings.Contains(payload.Content.Text, "Alert") || !strings.Contains(payload.Content.Text, "replenishment needed") {
		t.Errorf("content.text = %q, want it to contain title and body", payload.Content.Text)
	}
}

func TestFeishuNotifier_Send_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":19021,"msg":"bot forbidden"}`))
	}))
	defer srv.Close()

	n := NewFeishuNotifier(srv.URL)
	err := n.Send(context.Background(), "Alert", "body")
	if err == nil {
		t.Fatal("expected error for HTTP 403 response, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %v, want it to mention HTTP 403", err)
	}
}
