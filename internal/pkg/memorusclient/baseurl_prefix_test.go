package memorusclient

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// 这条守的是 2026-09-07 抓到的静默 404:客户端做 baseURL+"/memories",
// 而服务端把路由嵌套在 /api/v1 之下(memorus-server/src/rest/mod.rs 的
// Router::new().nest("/api/v1", api))。基址不带路径时每次调用都 404,
// 而且**不报错** —— 召回只是什么都查不到,看起来像「这功能就这样」。
//
// 断言的是「有没有喊出来」,不是「有没有拒绝启动」:memorus 是可降级的可选依赖,
// 运维也可能把它挂在一个自带重写的自定义路径上,所以只对「完全没有路径」这一种
// 不可能工作的形态报错,其余放行。
func captureLogs(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)
	fn()
	return buf.String()
}

func TestNew_PrefixlessBaseURL_ShoutsAtStartup(t *testing.T) {
	out := captureLogs(t, func() {
		if _, err := New(Config{BaseURL: "http://memorus:8880", APIKey: "k"}); err != nil {
			t.Fatalf("New should not fail on a prefixless URL, got %v", err)
		}
	})
	if !strings.Contains(out, "level=ERROR") {
		t.Fatalf("prefixless base URL must log at ERROR; got: %s", out)
	}
	if !strings.Contains(out, "/api/v1") {
		t.Fatalf("the log must name the expected prefix so it is actionable; got: %s", out)
	}
}

func TestNew_PrefixedBaseURL_IsQuiet(t *testing.T) {
	// 正确配置不许产生噪音 —— 否则这条 ERROR 会被训练成背景音。
	out := captureLogs(t, func() {
		if _, err := New(Config{BaseURL: "http://memorus:8880/api/v1", APIKey: "k"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if strings.Contains(out, "level=ERROR") {
		t.Fatalf("a correctly prefixed base URL must not log ERROR; got: %s", out)
	}
}

func TestNew_CustomPathBaseURL_IsQuiet(t *testing.T) {
	// 运维把 memorus 挂在自定义路径后面(反代重写前缀)是合法部署,不该误伤。
	out := captureLogs(t, func() {
		if _, err := New(Config{BaseURL: "https://mem.example.com/memory-api", APIKey: "k"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if strings.Contains(out, "level=ERROR") {
		t.Fatalf("a custom path must not be flagged; got: %s", out)
	}
}

func TestNew_TrailingSlashOnly_IsFlagged(t *testing.T) {
	// "http://host:8880/" 与没有路径是同一回事,不能因为多一个斜杠就放过。
	out := captureLogs(t, func() {
		if _, err := New(Config{BaseURL: "http://memorus:8880/", APIKey: "k"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "level=ERROR") {
		t.Fatalf("a bare trailing slash must still be flagged; got: %s", out)
	}
}

func TestNew_DegradedMode_StaysQuiet(t *testing.T) {
	// 没配 = 有意不启用,不该喊 —— 喊了就是把「没开这个功能」误报成「配错了」。
	out := captureLogs(t, func() {
		c, err := New(Config{BaseURL: "", APIKey: ""})
		if err != nil || c != nil {
			t.Fatalf("empty config must yield (nil, nil); got %v %v", c, err)
		}
	})
	if strings.Contains(out, "level=ERROR") {
		t.Fatalf("degraded mode must be silent; got: %s", out)
	}
}
