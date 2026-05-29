package llm

import (
	"strings"
	"testing"
)

// TestRedact_Email replaces email addresses with <email>.
func TestRedact_Email(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"contact: user@example.com today", "contact: <email> today"},
		{"no email here", "no email here"},
		{"two: a@b.cn and c@d.io end", "two: <email> and <email> end"},
	}
	for _, tc := range cases {
		got := Redact(tc.in)
		if got != tc.want {
			t.Errorf("Redact(%q)\n  got  %q\n  want %q", tc.in, got, tc.want)
		}
	}
}

// TestRedact_PhoneCN replaces mainland mobile numbers with <phone-cn>.
func TestRedact_PhoneCN(t *testing.T) {
	cases := []struct {
		in      string
		contain string
		absent  string
	}{
		{
			in:      "联系我 13812345678 或发邮件",
			contain: "<phone-cn>",
			absent:  "13812345678",
		},
		{
			// 11-digit number NOT starting with 1[3-9] must not be redacted.
			in:      "单号20240101001",
			contain: "20240101001",
			absent:  "<phone-cn>",
		},
		{
			in:      "mobile: +8613912345678",
			contain: "<phone-cn>",
			absent:  "13912345678",
		},
	}
	for _, tc := range cases {
		got := Redact(tc.in)
		if !strings.Contains(got, tc.contain) {
			t.Errorf("Redact(%q) = %q; expected to contain %q", tc.in, got, tc.contain)
		}
		if tc.absent != "" && strings.Contains(got, tc.absent) {
			t.Errorf("Redact(%q) = %q; expected NOT to contain %q", tc.in, got, tc.absent)
		}
	}
}

// TestRedact_IDCN replaces 18-character Chinese ID numbers with <id-cn>.
func TestRedact_IDCN(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"身份证: 110101199003071234", "身份证: <id-cn>"},
		{"末位X: 11010119900307123X", "末位X: <id-cn>"},
		{"short 12345678 ok", "short 12345678 ok"},
	}
	for _, tc := range cases {
		got := Redact(tc.in)
		if got != tc.want {
			t.Errorf("Redact(%q)\n  got  %q\n  want %q", tc.in, got, tc.want)
		}
	}
}

// TestRedact_Card replaces 13-19 digit sequences with <card>.
func TestRedact_Card(t *testing.T) {
	in := "card: 4111111111111111 thanks"
	got := Redact(in)
	if strings.Contains(got, "4111111111111111") {
		t.Errorf("Redact(%q) = %q; card number should be redacted", in, got)
	}
	if !strings.Contains(got, "<card>") {
		t.Errorf("Redact(%q) = %q; expected <card> placeholder", in, got)
	}
}

// TestRedact_Token replaces long mixed-case alphanumeric strings with <token>.
func TestRedact_Token(t *testing.T) {
	cases := []struct {
		in      string
		contain string
		absent  string
	}{
		{
			// 32-char string with upper, lower, digit — looks like an API key.
			in:      "key=AbCdEfGhIjKlMnOpQrStUvWxYz1234",
			contain: "<token>",
			absent:  "AbCdEfGhIjKlMnOpQrStUvWxYz1234",
		},
		{
			// Short string should not be redacted.
			in:      "abc123",
			contain: "abc123",
			absent:  "<token>",
		},
		{
			// All-lowercase 32-char: no uppercase → not a token.
			in:      "abcdefghijklmnopqrstuvwxyz123456",
			contain: "abcdefghijklmnopqrstuvwxyz123456",
			absent:  "<token>",
		},
	}
	for _, tc := range cases {
		got := Redact(tc.in)
		if !strings.Contains(got, tc.contain) {
			t.Errorf("Redact(%q) = %q; expected to contain %q", tc.in, got, tc.contain)
		}
		if tc.absent != "" && strings.Contains(got, tc.absent) {
			t.Errorf("Redact(%q) = %q; expected NOT to contain %q", tc.in, got, tc.absent)
		}
	}
}

// TestRedactJSON_SecretFields replaces values of known secret keys.
func TestRedactJSON_SecretFields(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		contain string
		absent  string
	}{
		{
			name:    "password field",
			in:      `{"username":"bob","password":"s3cr3t!"}`,
			contain: `"password":"<redacted>"`,
			absent:  "s3cr3t!",
		},
		{
			name:    "api_key field",
			in:      `{"api_key":"sk-abc123xyz789","model":"gpt"}`,
			contain: `"<redacted>"`,
			absent:  "sk-abc123xyz789",
		},
		{
			name:    "multiple secret fields",
			in:      `{"secret":"topsecret","access_token":"tok-99","data":"visible"}`,
			contain: "<redacted>",
			absent:  "topsecret",
		},
		{
			name:    "non-secret field preserved",
			in:      `{"name":"Alice","role":"admin"}`,
			contain: `"name":"Alice"`,
			absent:  "<redacted>",
		},
		{
			name:    "case-insensitive key",
			in:      `{"Password":"hunter2"}`,
			contain: `"<redacted>"`,
			absent:  "hunter2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactJSON(tc.in)
			if !strings.Contains(got, tc.contain) {
				t.Errorf("RedactJSON(%q)\n  got  %q\n  want to contain %q", tc.in, got, tc.contain)
			}
			if tc.absent != "" && strings.Contains(got, tc.absent) {
				t.Errorf("RedactJSON(%q)\n  got  %q\n  expected NOT to contain %q", tc.in, got, tc.absent)
			}
		})
	}
}
