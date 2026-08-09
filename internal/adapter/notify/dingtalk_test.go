package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNewDingTalkNotifier_EmptyURLReturnsNil(t *testing.T) {
	if n := NewDingTalkNotifier("", "secret"); n != nil {
		t.Fatalf("expected nil notifier for empty webhook URL, got %+v", n)
	}
}

func TestDingTalkNotifier_Send_RequestBody(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = buf
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	n := NewDingTalkNotifier(srv.URL, "") // no secret: unsigned request
	if n == nil {
		t.Fatal("expected non-nil notifier")
	}

	if err := n.Send(context.Background(), "Alert", "低库存预警"); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	var payload dingTalkTextMessage
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("failed to unmarshal request body %q: %v", gotBody, err)
	}
	if payload.MsgType != "text" {
		t.Errorf("msgtype = %q, want text", payload.MsgType)
	}
	if !strings.Contains(payload.Text.Content, "Alert") || !strings.Contains(payload.Text.Content, "低库存预警") {
		t.Errorf("content = %q, want it to contain title and body", payload.Text.Content)
	}
}

// TestDingTalkNotifier_Signature verifies the HMAC-SHA256 signature against
// DingTalk's documented algorithm, computed independently in the test
// (not by calling production code for the expected value) so a bug in
// signedURL cannot also be baked into the assertion.
func TestDingTalkNotifier_Signature(t *testing.T) {
	const secret = "SECxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
	fixedTime := time.UnixMilli(1_700_000_000_000)

	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewDingTalkNotifier(srv.URL, secret)
	n.now = func() time.Time { return fixedTime }

	if err := n.Send(context.Background(), "t", "b"); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	wantTimestamp := strconv.FormatInt(fixedTime.UnixMilli(), 10)
	if gotQuery.Get("timestamp") != wantTimestamp {
		t.Fatalf("timestamp = %q, want %q", gotQuery.Get("timestamp"), wantTimestamp)
	}

	// Independently reproduce DingTalk's documented signing algorithm:
	// sign = base64(hmac_sha256(secret, "{timestamp}\n{secret}"))
	stringToSign := wantTimestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stringToSign))
	wantSign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if gotQuery.Get("sign") != wantSign {
		t.Fatalf("sign = %q, want %q", gotQuery.Get("sign"), wantSign)
	}
}

func TestDingTalkNotifier_Send_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errcode":310000,"errmsg":"sign not match"}`))
	}))
	defer srv.Close()

	n := NewDingTalkNotifier(srv.URL, "wrong-secret")
	err := n.Send(context.Background(), "Alert", "body")
	if err == nil {
		t.Fatal("expected error for HTTP 400 response, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error = %v, want it to mention HTTP 400", err)
	}
}

func TestDingTalkNotifier_SignedURL_PreservesExistingQueryParams(t *testing.T) {
	n := NewDingTalkNotifier("https://oapi.dingtalk.com/robot/send?access_token=abc123", "sekret")
	n.now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }

	signed, err := n.signedURL()
	if err != nil {
		t.Fatalf("signedURL returned error: %v", err)
	}

	parsed, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("failed to parse signed URL %q: %v", signed, err)
	}
	q := parsed.Query()
	if q.Get("access_token") != "abc123" {
		t.Errorf("access_token = %q, want abc123 (must survive signing)", q.Get("access_token"))
	}
	if q.Get("timestamp") == "" || q.Get("sign") == "" {
		t.Errorf("expected both timestamp and sign to be set, got query=%v", q)
	}
}
