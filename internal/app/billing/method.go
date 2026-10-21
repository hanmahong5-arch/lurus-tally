package billing

import "strings"

// ResolveMethod turns the provider family the buyer picked into the concrete
// platform payment method that suits their device. Platform registers only
// concrete scene ids for WeChat (wechat_native / wechat_h5 / ...), so sending
// the bare family "wechat" is refused as method_not_available; Alipay's bare
// "alipay" is desktop page pay, which a phone browser handles poorly.
//
// The mapping mirrors platform's own SceneMethod (internal/adapter/payment/
// scene.go) for the browser case. It lives here because only tally sees the
// buyer's User-Agent — platform sees tally's server-to-server request.
//
//	alipay + desktop          -> alipay        (page pay, redirect)
//	alipay + mobile           -> alipay_wap    (redirect into the Alipay app)
//	wechat + desktop          -> wechat_native (QR code rendered by tally)
//	wechat + mobile browser   -> wechat_h5     (redirect into WeChat)
//	wechat + WeChat's browser -> wechat_native (H5 pay is refused in-app)
//
// Anything else ("wallet", concrete ids) passes through unchanged.
func ResolveMethod(method, userAgent string) string {
	ua := strings.ToLower(userAgent)
	switch method {
	case "alipay":
		if isMobileUA(ua) {
			return "alipay_wap"
		}
		return "alipay"
	case "wechat":
		if isMobileUA(ua) && !strings.Contains(ua, "micromessenger") {
			return "wechat_h5"
		}
		return "wechat_native"
	}
	return method
}

// IsQRMethod reports whether the method's "pay URL" is really a QR payload the
// buyer scans with their phone, not a page to redirect to.
func IsQRMethod(method string) bool {
	return method == "wechat_native" || method == "alipay_qr"
}

func isMobileUA(ua string) bool {
	for _, m := range []string{"iphone", "ipad", "ipod", "android", "mobile", "harmonyos", "openharmony"} {
		if strings.Contains(ua, m) {
			return true
		}
	}
	return false
}
