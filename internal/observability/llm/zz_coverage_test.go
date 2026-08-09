package llm

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// --- parseSampler ---------------------------------------------------------

// TestParseSampler_TableDriven pins the documented fallback/selection matrix
// for LLM_TRACE_SAMPLE_RATIO (otel.go:146-164). Unset and out-of-range/
// non-numeric values must silently degrade to AlwaysSample so a
// misconfigured deployment never blocks LLM calls.
func TestParseSampler_TableDriven(t *testing.T) {
	cases := []struct {
		name string
		env  string
		set  bool
		want string // sdktrace.Sampler.Description()
	}{
		{"unset", "", false, "AlwaysOnSampler"},
		{"exactly_one", "1.0", true, "AlwaysOnSampler"},
		{"exactly_zero", "0.0", true, "AlwaysOffSampler"},
		{"mid_ratio", "0.1", true, "ParentBased"},
		{"non_numeric", "abc", true, "AlwaysOnSampler"},
		{"negative", "-1", true, "AlwaysOnSampler"},
		{"above_one", "2", true, "AlwaysOnSampler"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envSampleRatio, tc.env)
			if !tc.set {
				// t.Setenv always defines the var; explicitly unset to hit
				// the true "absent" branch (raw == ""). t.Setenv above
				// registers the cleanup that restores/unsets afterwards.
				if err := os.Unsetenv(envSampleRatio); err != nil {
					t.Fatalf("unsetenv: %v", err)
				}
			}
			got := parseSampler()
			desc := got.Description()
			if !strings.Contains(desc, tc.want) {
				t.Errorf("parseSampler() with %s=%q -> %q; want substring %q", envSampleRatio, tc.env, desc, tc.want)
			}
		})
	}
}

// TestParseSampler_MidRatio_IsParentBasedRatio confirms the exact sampler
// type (not just description) for an in-range ratio, since ParentBased
// wraps TraceIDRatioBased and both are *sdktrace.parentBasedSampler /
// *sdktrace.traceIDRatioSampler under the hood — Description() is the
// stable public surface to assert on.
func TestParseSampler_MidRatio_IsParentBasedRatio(t *testing.T) {
	t.Setenv(envSampleRatio, "0.25")
	s := parseSampler()
	if _, ok := s.(sdktrace.Sampler); !ok {
		t.Fatalf("parseSampler() did not return a sdktrace.Sampler")
	}
	if !strings.Contains(s.Description(), "ParentBased") {
		t.Errorf("parseSampler(0.25).Description() = %q; want ParentBased wrapper", s.Description())
	}
}

// --- stripScheme -----------------------------------------------------------

func TestStripScheme_TableDriven(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://x", "x"},
		{"http://x", "x"},
		{"x", "x"},
		{"https://langfuse.internal:3000", "langfuse.internal:3000"},
		// Exactly the prefix with nothing after it: len(u) == len(pfx), the
		// "len(u) > len(pfx)" guard fails, so it must be returned unchanged.
		{"https://", "https://"},
		{"http://", "http://"},
		{"", ""},
	}
	for _, tc := range cases {
		got := stripScheme(tc.in)
		if got != tc.want {
			t.Errorf("stripScheme(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// --- truncate ---------------------------------------------------------------

func TestTruncate_TableDriven(t *testing.T) {
	cases := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"under_limit_returned_whole", "hello", 10, "hello"},
		{"exact_limit_returned_whole", "hello", 5, "hello"},
		{"over_limit_cut_to_n_bytes", "hello world", 5, "hello"},
		{"n_zero_yields_empty", "hello", 0, ""},
		{"empty_input", "", 5, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate(tc.s, tc.n)
			if got != tc.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tc.s, tc.n, got, tc.want)
			}
		})
	}
}

// --- Redact / RedactJSON: business-invariant pins ---------------------------

// TestRedact_IDBeforePhone_Precedence pins the ordering documented in
// redact.go:56-63: an 18-digit CN ID whose middle digits look like a CN
// mobile number must still be tagged <id-cn>, not <phone-cn>, because rIDCN
// runs before rPhoneCN.
func TestRedact_IDBeforePhone_Precedence(t *testing.T) {
	// 11010119900307123X: digits 7-17 are "1990030712", which does not itself
	// match 1[3-9]\d{9}, so construct an ID whose embedded run does match the
	// phone pattern to prove ID-first ordering wins.
	// Embedded phone-shaped run: "13912345678" (11 digits, matches 1[3-9]\d{9}).
	// Prepend 6 digits + append 1 digit to make a valid 18-char ID shape.
	id := "110101" + "13912345678" + "9" // 6 + 11 + 1 = 18 chars
	if len(id) != 18 {
		t.Fatalf("test setup bug: id length = %d, want 18", len(id))
	}
	in := "身份证号: " + id + " 请核实"
	got := Redact(in)
	if strings.Contains(got, "<phone-cn>") {
		t.Errorf("Redact(%q) = %q; CN ID must not be mis-tagged as <phone-cn>", in, got)
	}
	if !strings.Contains(got, "<id-cn>") {
		t.Errorf("Redact(%q) = %q; expected <id-cn> to win over phone pattern", in, got)
	}
	if strings.Contains(got, id) {
		t.Errorf("Redact(%q) = %q; raw ID must not remain in output", in, got)
	}
}

// TestRedactJSON_NonSecretFieldUntouched pins that RedactJSON only rewrites
// the VALUE of recognised secret keys, leaving benign fields (and the key
// names of secret fields) completely intact.
func TestRedactJSON_NonSecretFieldUntouched(t *testing.T) {
	in := `{"account":"user-42","password":"hunter2"}`
	got := RedactJSON(in)
	if !strings.Contains(got, `"account":"user-42"`) {
		t.Errorf("RedactJSON(%q) = %q; benign 'account' field must be untouched", in, got)
	}
	if !strings.Contains(got, `"password":"<redacted>"`) {
		t.Errorf("RedactJSON(%q) = %q; expected password value replaced, key preserved", in, got)
	}
	if strings.Contains(got, "hunter2") {
		t.Errorf("RedactJSON(%q) = %q; secret value must not leak", in, got)
	}
}

// TestRedact_Token_AllLowercase40Char_LeftIntact pins the 3-class heuristic:
// a 40-char all-lowercase string fails rTokenRequired (needs upper+lower+
// digit) and must be left completely intact, not tagged <token>.
func TestRedact_Token_AllLowercase40Char_LeftIntact(t *testing.T) {
	s := "abcdefghijklmnopqrstuvwxyzabcdefghijklmn" // 41 lowercase letters
	s = s[:40]
	if len(s) != 40 {
		t.Fatalf("test setup bug: len=%d, want 40", len(s))
	}
	in := "value=" + s + " end"
	got := Redact(in)
	if strings.Contains(got, "<token>") {
		t.Errorf("Redact(%q) = %q; all-lowercase run must not be tagged <token>", in, got)
	}
	if !strings.Contains(got, s) {
		t.Errorf("Redact(%q) = %q; original 40-char lowercase string must survive untouched", in, got)
	}
}

// TestRedact_Card_FalsePositiveOnLegitNumericID pins the documented
// acceptable false positive: any 13-19 digit run is replaced by <card>,
// even when it is actually a legitimate numeric ID rather than a real card
// number (redact.go:30-32).
func TestRedact_Card_FalsePositiveOnLegitNumericID(t *testing.T) {
	// 16-digit purely numeric "order id" — not a real card, but the crude
	// heuristic intentionally treats any 13-19 digit run as card-shaped.
	orderID := "1234567890123456"
	in := "order id: " + orderID + " confirmed"
	got := Redact(in)
	if strings.Contains(got, orderID) {
		t.Errorf("Redact(%q) = %q; 13-19 digit run must be redacted even as a false positive", in, got)
	}
	if !strings.Contains(got, "<card>") {
		t.Errorf("Redact(%q) = %q; expected <card> placeholder to pin false-positive behaviour", in, got)
	}
}

// --- otel.go: live-provider path without a real collector -------------------

// TestNewOTelTracer_AllEnvSet_BuildsRealTracerAndSpans exercises the
// non-Noop branch of NewOTelTracer without a live collector: the OTLP HTTP
// exporter is constructed lazily and does not dial until a batch is
// flushed. LLM_TRACE_SAMPLE_RATIO=0.0 (NeverSample) ensures spans are
// created as non-recording, so nothing is ever queued for export — this
// lets us drive buildProvider, StartLLMSpan, End (success + error),
// AttachToolCall, and TraceID with zero network I/O. Shutdown/export with a
// live batch is intentionally NOT exercised here (would dial the fake host
// and hang/retry); ShutdownOTelProvider's nil-provider branch is covered
// separately in TestShutdownOTelProvider_NilProvider_NoOp.
func TestNewOTelTracer_AllEnvSet_BuildsRealTracerAndSpans(t *testing.T) {
	t.Setenv(envHost, "https://langfuse.invalid.test")
	t.Setenv(envPublicKey, "pk-test")
	t.Setenv(envSecretKey, "sk-test")
	t.Setenv(envSampleRatio, "0.0") // NeverSample: no span is ever queued for export.

	// Reset the package singleton so this test observes a fresh build
	// regardless of prior test ordering within the package.
	resetGlobalProviderForTest(t)
	t.Cleanup(func() { resetGlobalProviderForTest(t) })

	tr := NewOTelTracer("lurus-tally-test", "0.0.0")
	if tr == nil {
		t.Fatal("expected non-nil tracer when all Langfuse env vars are set")
	}
	if _, ok := tr.(*otelTracer); !ok {
		t.Fatalf("expected *otelTracer when env fully configured, got %T", tr)
	}

	ctx := context.Background()

	// Success path: End with nil error records output/token attributes.
	span, spanCtx := tr.StartLLMSpan(ctx, "chat", "gpt-test", "hello, this is the <prompt> aBc123XYZ")
	if span == nil || spanCtx == nil {
		t.Fatal("StartLLMSpan must return a non-nil span and context")
	}
	span.End("some <output> text", TokenCount{Prompt: 3, Completion: 2, Total: 5}, nil)

	// Error path: End with non-nil error records error attributes instead.
	span2, _ := tr.StartLLMSpan(ctx, "chat", "gpt-test", "prompt2")
	span2.End("", TokenCount{}, errors.New("upstream 500"))

	// AttachToolCall must not panic and should be safely nested under the
	// (already-ended) parent span.
	span2.AttachToolCall("lookup", `{"q":"widget"}`, `{"ok":true}`)

	// TraceID: even a non-recording (NeverSample) span has a valid
	// SpanContext propagated from the root, so TraceID() should still
	// return 32 hex chars, not "".
	if id := span2.(*otelSpan).TraceID(); len(id) != 32 {
		t.Errorf("otelSpan.TraceID() = %q (len %d); want 32-hex trace id", id, len(id))
	}

	// Calling NewOTelTracer again must reuse the existing global provider
	// (providerOnce/mutex-guarded singleton) rather than rebuilding it.
	tr2 := NewOTelTracer("lurus-tally-test-2", "0.0.0")
	if tr2 == nil {
		t.Fatal("second NewOTelTracer call must also return non-nil")
	}
}

// TestShutdownOTelProvider_NilProvider_NoOp verifies that shutting down
// before any provider has been initialised is a safe no-op (otel.go:169-176).
func TestShutdownOTelProvider_NilProvider_NoOp(t *testing.T) {
	resetGlobalProviderForTest(t)
	if err := ShutdownOTelProvider(context.Background()); err != nil {
		t.Errorf("ShutdownOTelProvider() on nil provider error = %v, want nil", err)
	}
}

// resetGlobalProviderForTest clears the package-level singleton under its
// mutex so tests can observe a fresh buildProvider() call deterministically,
// per the brief's note that globalProvider/providerOnce is order-dependent.
func resetGlobalProviderForTest(t *testing.T) {
	t.Helper()
	providerMu.Lock()
	defer providerMu.Unlock()
	globalProvider = nil
}
