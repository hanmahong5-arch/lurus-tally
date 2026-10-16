package billing_test

import (
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/billing"
)

func TestResolveMethod(t *testing.T) {
	const (
		desktop = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/126.0"
		iphone  = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) Mobile/15E148 Safari/604.1"
		wechat  = "Mozilla/5.0 (Linux; Android 14) Mobile Safari/537.36 MicroMessenger/8.0.47"
	)
	cases := []struct{ method, ua, want string }{
		{"alipay", desktop, "alipay"},
		{"alipay", iphone, "alipay_wap"},
		{"wechat", desktop, "wechat_native"},
		{"wechat", iphone, "wechat_h5"},
		{"wechat", wechat, "wechat_native"},
		{"wallet", iphone, "wallet"},
		{"wechat_h5", desktop, "wechat_h5"},
	}
	for _, c := range cases {
		if got := billing.ResolveMethod(c.method, c.ua); got != c.want {
			t.Errorf("ResolveMethod(%q, %q) = %q, want %q", c.method, c.ua, got, c.want)
		}
	}
}
