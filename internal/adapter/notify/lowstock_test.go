package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// sampleLines mirrors what app/replenish.ListLowStockUseCase.Execute produces
// for a tenant with two SKUs under their reorder point (decimal strings,
// urgency-sorted). Kept here so the notify package stays decoupled from
// app/replenish while still exercising the exact wire shape.
func sampleLines() []LowStockLine {
	return []LowStockLine{
		{ProductName: "32# 角钢", ProductCode: "GG-032", AvailableQty: "12", ReorderPoint: "50", DaysOfSupply: "1.5"},
		{ProductName: "M8 六角螺栓", ProductCode: "BOLT-M8", AvailableQty: "200", ReorderPoint: "800", DaysOfSupply: "4"},
	}
}

func TestFormatLowStockAlert_TitleAndBody(t *testing.T) {
	title, body := FormatLowStockAlert("演示五金店", sampleLines())

	if !strings.Contains(title, "演示五金店") || !strings.Contains(title, "2") {
		t.Errorf("title = %q, want it to name the scope and SKU count", title)
	}
	for _, want := range []string{"32# 角钢", "GG-032", "可用 12", "补货点 50", "M8 六角螺栓"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\nbody:\n%s", want, body)
		}
	}
}

func TestFormatLowStockAlert_Empty(t *testing.T) {
	if title, body := FormatLowStockAlert("x", nil); title != "" || body != "" {
		t.Errorf("empty lines: got (%q, %q), want empty pair", title, body)
	}
}

func TestFormatLowStockAlert_OverflowSummarised(t *testing.T) {
	lines := make([]LowStockLine, maxAlertLines+3)
	for i := range lines {
		lines[i] = LowStockLine{ProductName: "SKU", AvailableQty: "0", ReorderPoint: "1", DaysOfSupply: "0"}
	}
	_, body := FormatLowStockAlert("", lines)
	if !strings.Contains(body, "另有 3 个商品待补货") {
		t.Errorf("overflow not summarised; body tail:\n%s", body)
	}
}

// TestDispatcher_DispatchLowStock_DryRunLog is the connector-loop acceptance:
// it drives the real low-stock line shape through the Dispatcher wired to a
// dry-run LogNotifier and asserts a structured "would send IM alert" push log
// is emitted carrying the alert title. This proves the notify seam fires
// without hitting any external IM webhook.
func TestDispatcher_DispatchLowStock_DryRunLog(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	d := NewDispatcher(logger, NewLogNotifier(logger))
	if !d.Configured() {
		t.Fatal("dispatcher should report configured with one dry-run notifier")
	}

	d.DispatchLowStock(context.Background(), "演示五金店", sampleLines())

	// Two info lines expected: the dry-run notifier's "would send" record and
	// the dispatcher's per-notifier "alert sent" record.
	var sawDryRun, sawSent bool
	for _, ln := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if ln == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(ln), &rec); err != nil {
			t.Fatalf("log line not JSON: %q: %v", ln, err)
		}
		switch rec["msg"] {
		case "notify/dry-run: would send IM alert":
			sawDryRun = true
			if title, _ := rec["title"].(string); !strings.Contains(title, "演示五金店") {
				t.Errorf("dry-run log title = %q, want it to name the scope", title)
			}
		case "notify: low-stock alert sent":
			sawSent = true
			if n, _ := rec["sku_count"].(float64); n != 2 {
				t.Errorf("alert-sent sku_count = %v, want 2", n)
			}
		}
	}
	if !sawDryRun {
		t.Errorf("no dry-run push log emitted\nlog:\n%s", buf.String())
	}
	if !sawSent {
		t.Errorf("no alert-sent log emitted\nlog:\n%s", buf.String())
	}

	// Emit the captured dry-run push log to the test output so the acceptance
	// log line is visible in `go test -v` runs.
	t.Logf("captured dry-run push log:\n%s", strings.TrimSpace(buf.String()))
}

func TestDispatcher_NoNotifiers_NoOp(t *testing.T) {
	d := NewDispatcher(nil) // no notifiers, nil logger falls back to Default
	if d.Configured() {
		t.Fatal("empty dispatcher should not report configured")
	}
	// Must not panic and must be a silent no-op.
	d.DispatchLowStock(context.Background(), "x", sampleLines())
}

func TestNewDispatcher_DropsTypedNilNotifiers(t *testing.T) {
	// NewFeishuNotifier("") returns a typed nil (*FeishuNotifier). Passing it
	// must not count as a live notifier nor panic on dispatch.
	d := NewDispatcher(slog.Default(), NewFeishuNotifier(""), NewWeComNotifier(""))
	if d.Configured() {
		t.Fatal("dispatcher with only nil notifiers should not be configured")
	}
}
