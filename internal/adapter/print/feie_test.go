package print

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *FeieClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := New(Config{User: "u1", UKey: "k1", APIURL: srv.URL})
	if err != nil || c == nil {
		t.Fatalf("New: %v %v", c, err)
	}
	c.now = func() time.Time { return time.Unix(1700000000, 0) }
	return c
}

func TestNewDegradesWithoutCredentials(t *testing.T) {
	for _, cfg := range []Config{{}, {User: "u"}, {UKey: "k"}} {
		if c, err := New(cfg); c != nil || err != nil {
			t.Errorf("New(%+v) = %v, %v; want nil, nil", cfg, c, err)
		}
		if p, _ := NewPrinter(cfg); p != nil {
			t.Errorf("NewPrinter(%+v) 应返回 nil 接口", cfg)
		}
	}
}

func TestSignIsSha1OfUserUkeyStime(t *testing.T) {
	// sha1("u1k11700000000") 的独立计算值
	if got := sign("u1", "k1", "1700000000"); got != "97caf6c03a2ff0c91ba6a9876ab3550cf7a38445" {
		t.Fatalf("sig = %q", got)
	}
	if sign("u1", "k1", "1") == sign("u1", "k1", "2") {
		t.Error("stime 必须参与签名")
	}
}

func TestPrintSendsDocumentedFields(t *testing.T) {
	var got url.Values
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			t.Errorf("method/content-type: %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		_ = r.ParseForm()
		got = r.PostForm
		_, _ = w.Write([]byte(`{"msg":"ok","ret":0,"data":"816500499_20160919_1","serverExecutedTime":9}`))
	})
	id, err := c.Print(context.Background(), "SN123", "<CB>店</CB>", 2)
	if err != nil || id != "816500499_20160919_1" {
		t.Fatalf("Print = %q, %v", id, err)
	}
	for k, want := range map[string]string{
		"user": "u1", "stime": "1700000000", "apiname": "Open_printMsg",
		"sn": "SN123", "content": "<CB>店</CB>", "times": "2",
		"sig": sign("u1", "k1", "1700000000"),
	} {
		if got.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, got.Get(k), want)
		}
	}
}

func TestPrintVendorError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"msg":"验证失败","ret":-2,"data":null}`))
	})
	if _, err := c.Print(context.Background(), "S", "x", 1); err == nil || !strings.Contains(err.Error(), "ret=-2") {
		t.Fatalf("err = %v", err)
	}
}

func TestStatusParsesSentence(t *testing.T) {
	cases := map[string]PrinterStatus{
		"离线。":         {Online: false, Normal: false, Text: "离线。"},
		"在线，工作状态正常。":  {Online: true, Normal: true, Text: "在线，工作状态正常。"},
		"在线，工作状态不正常。": {Online: true, Normal: false, Text: "在线，工作状态不正常。"},
	}
	for text, want := range cases {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			if r.PostForm.Get("apiname") != "Open_queryPrinterStatus" || r.PostForm.Get("sn") != "S1" {
				t.Errorf("form = %v", r.PostForm)
			}
			_, _ = w.Write([]byte(`{"msg":"ok","ret":0,"data":"` + text + `"}`))
		})
		got, err := c.Status(context.Background(), "S1")
		if err != nil || got != want {
			t.Errorf("Status(%q) = %+v, %v; want %+v", text, got, err, want)
		}
	}
}

func TestBindFormatAndAlreadyBound(t *testing.T) {
	var content string
	reply := `{"msg":"ok","ret":0,"data":{"ok":["S1#K1#柜台"],"no":[]}}`
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		content = r.PostForm.Get("printerContent")
		_, _ = w.Write([]byte(reply))
	})
	if err := c.Bind(context.Background(), "S1", "K1", "柜台"); err != nil {
		t.Fatal(err)
	}
	if content != "S1#K1#柜台" {
		t.Errorf("printerContent = %q", content)
	}
	reply = `{"msg":"ok","ret":0,"data":{"ok":[],"no":["S1#K1#柜台 已被添加"]}}`
	if err := c.Bind(context.Background(), "S1", "K1", "柜台"); err != nil {
		t.Errorf("已绑定应视为成功: %v", err)
	}
	reply = `{"msg":"ok","ret":0,"data":{"ok":[],"no":["S1#K1 编号和KEY不正确"]}}`
	if err := c.Bind(context.Background(), "S1", "K1", ""); err == nil {
		t.Error("KEY 不正确应报错")
	}
}
