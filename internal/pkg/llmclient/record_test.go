//go:build record

package llmclient

// Re-records testdata/recorded/ against the real gateway (a handful of short
// requests on deepseek-v4-flash):
//
//	NEWAPI_BASE_URL=https://newapi.lurus.cn/v1 NEWAPI_API_KEY=... \
//	  go test -tags=record -run TestRecordGateway ./internal/pkg/llmclient/
//
// Model output varies between recordings; the scenario assertions (in
// recorded_test.go) run live here too, so a recording that would not pass is
// never written.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

var balanceRe = regexp.MustCompile(`"balance_remaining":[0-9.eE+-]+`)

type recorder struct {
	ex []exchange
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	req.Body = io.NopCloser(bytes.NewReader(body))
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	respBody, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(respBody))
	r.ex = append(r.ex, exchange{
		Path:        req.URL.Path,
		Request:     json.RawMessage(body),
		Status:      resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		// The gateway reports the account balance in usage.x_lurus; keep the
		// field, not the number.
		Body: balanceRe.ReplaceAllString(string(respBody), `"balance_remaining":0`),
	})
	return resp, nil
}

func TestRecordGateway(t *testing.T) {
	base, key := os.Getenv("NEWAPI_BASE_URL"), os.Getenv("NEWAPI_API_KEY")
	if base == "" || key == "" {
		t.Fatal("NEWAPI_BASE_URL and NEWAPI_API_KEY are required to record")
	}
	for _, sc := range recordedScenarios() {
		t.Run(sc.name, func(t *testing.T) {
			k := key
			if sc.badKey {
				k = "sk-invalid-recorded"
			}
			c, err := New(Config{BaseURL: base, APIKey: k, DefaultModel: recordedModel})
			if err != nil {
				t.Fatal(err)
			}
			rec := &recorder{}
			c.http.Transport = rec
			sc.run(t, c)
			if t.Failed() {
				return
			}
			out, _ := json.MarshalIndent(rec.ex, "", "  ")
			p := recordingPath(sc.name)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, append(out, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
		})
	}
}
