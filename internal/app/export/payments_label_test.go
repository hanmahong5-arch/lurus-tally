package export

import "testing"

func TestPayTypeLabel(t *testing.T) {
	cases := map[string]string{
		"cash":     "现金",
		"wechat":   "微信",
		"alipay":   "支付宝",
		"card":     "银行卡",
		"credit":   "赊账",
		"transfer": "转账",
		"wire":     "wire", // unknown values are kept as-is rather than hidden
		"":         "",
	}
	for in, want := range cases {
		if got := payTypeLabel(in); got != want {
			t.Errorf("payTypeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
