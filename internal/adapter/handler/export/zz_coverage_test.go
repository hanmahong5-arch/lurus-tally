package export_test

// Additional coverage for internal/adapter/handler/export, focused on:
//   - business invariant: tenant gate blocks the Exporter from ever running
//     for cross-tenant/unauthenticated requests, and the exact tenant UUID
//     is threaded through to Execute (no silent tenant swap).
//   - technical: the mid-stream error path (fn returns an error after the
//     BOM/header bytes have already been flushed) must be logged via the
//     injected *slog.Logger and must NOT flip the already-written 200 status,
//     and the producer goroutine must be joined (via the done channel) before
//     the handler returns — exercised under -race.
//   - routing: each of Bills/Stock/Payments calls only its own Exporter.
//
// handler.go's `pw.Write(utf8BOM)` error branch (lines ~131-133) is NOT
// reachable from this black-box test: that branch only fires if the pipe's
// read side (pr) is already closed before the producer's first Write, but
// pr.Close() is deferred until AFTER streamCSV's io.Copy(c.Writer, pr) call
// returns, and io.Copy cannot return before its first Read — which is what
// unblocks that very Write. So the first Read always happens first, making
// the BOM write succeed before pr could ever be closed. This is confirmed
// dead code under the current single-request design (no external caller can
// reach pr/pw to close them out of band), not a gap in these tests.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	handlerexport "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/export"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ----- fakes -----

// spyExporter records whether/how it was invoked so tests can assert
// business invariants (never-called on 401, exact tenant threaded through,
// only the matching route's Exporter fires).
type spyExporter struct {
	mu       sync.Mutex
	called   bool
	gotTID   uuid.UUID
	body     string
	err      error // returned AFTER writing body, simulating a mid-stream failure
	delay    time.Duration
	finished atomic.Bool // set true right before Execute returns
}

func (s *spyExporter) Execute(_ context.Context, tenantID uuid.UUID, w io.Writer) (int, error) {
	s.mu.Lock()
	s.called = true
	s.gotTID = tenantID
	s.mu.Unlock()

	if s.delay > 0 {
		time.Sleep(s.delay)
	}

	if s.body != "" {
		if _, err := io.WriteString(w, s.body); err != nil {
			s.finished.Store(true)
			return 0, err
		}
	}
	defer s.finished.Store(true)
	if s.err != nil {
		return 0, s.err
	}
	return 1, nil
}

func (s *spyExporter) wasCalled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.called
}

func (s *spyExporter) tenantSeen() uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gotTID
}

// newLogBuf builds a slog.Logger backed by an in-memory buffer so tests can
// assert on emitted log lines without depending on global logging state.
func newLogBuf() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(h), &buf
}

func newAuthedEngine(bills, stock, payments handlerexport.Exporter, tenantID uuid.UUID, log *slog.Logger) *gin.Engine {
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, tenantID)
		c.Next()
	})
	h := handlerexport.New(bills, stock, payments, log)
	h.RegisterRoutes(api)
	return r
}

// ----- business invariant: tenant gate blocks the Exporter entirely -----

func TestZZ_NoTenant_ExporterNeverInvoked(t *testing.T) {
	bills := &spyExporter{body: "x\n"}
	stock := &spyExporter{body: "x\n"}
	payments := &spyExporter{body: "x\n"}

	r := gin.New() // no auth middleware => middleware.GetTenantID returns uuid.Nil
	h := handlerexport.New(bills, stock, payments, nil)
	h.RegisterRoutes(r.Group("/api/v1"))

	for _, path := range []string{
		"/api/v1/exports/bills.csv",
		"/api/v1/exports/stock.csv",
		"/api/v1/exports/payments.csv",
	} {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, path, nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("path %s: want 401, got %d", path, w.Code)
		}
		if !strings.Contains(w.Body.String(), "unauthorized") {
			t.Errorf("path %s: want body to mention unauthorized, got %q", path, w.Body.String())
		}
	}

	if bills.wasCalled() || stock.wasCalled() || payments.wasCalled() {
		t.Fatal("business invariant violated: an Exporter was invoked despite missing tenant (would leak cross-tenant CSV)")
	}
}

// ----- business invariant: exact tenant UUID reaches Execute, and routing is exact -----

func TestZZ_EachRoute_CallsOnlyItsOwnExporterWithExactTenant(t *testing.T) {
	cases := []struct {
		name string
		path string
		pick func(bills, stock, payments *spyExporter) *spyExporter
	}{
		{"bills", "/api/v1/exports/bills.csv", func(b, s, p *spyExporter) *spyExporter { return b }},
		{"stock", "/api/v1/exports/stock.csv", func(b, s, p *spyExporter) *spyExporter { return s }},
		{"payments", "/api/v1/exports/payments.csv", func(b, s, p *spyExporter) *spyExporter { return p }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tenantID := uuid.New()
			bills := &spyExporter{body: "b\n"}
			stock := &spyExporter{body: "s\n"}
			payments := &spyExporter{body: "p\n"}

			r := newAuthedEngine(bills, stock, payments, tenantID, nil)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, tc.path, nil)
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("want 200, got %d", w.Code)
			}

			target := tc.pick(bills, stock, payments)
			if !target.wasCalled() {
				t.Fatalf("route %s: expected its Exporter to be called", tc.name)
			}
			if got := target.tenantSeen(); got != tenantID {
				t.Fatalf("route %s: Execute got tenantID %s, want %s", tc.name, got, tenantID)
			}

			for _, other := range []*spyExporter{bills, stock, payments} {
				if other == target {
					continue
				}
				if other.wasCalled() {
					t.Fatalf("route %s: an unrelated Exporter was also invoked (cross-domain leak)", tc.name)
				}
			}
		})
	}
}

// ----- technical: mid-stream error after headers/BOM are flushed -----

func TestZZ_ExporterErrorAfterWrite_LogsAndKeeps200_TruncatesBody(t *testing.T) {
	wantErr := errors.New("boom: row scan failed")
	partial := "单号,类型\n1,foo\n" // some real rows already flushed before the failure
	bills := &spyExporter{body: partial, err: wantErr}
	stock := &spyExporter{}
	payments := &spyExporter{}

	log, buf := newLogBuf()
	tenantID := uuid.New()
	r := newAuthedEngine(bills, stock, payments, tenantID, log)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/exports/bills.csv", nil)
	r.ServeHTTP(w, req)

	// Headers were already flushed with 200 before the error occurred, so the
	// status cannot change even though the body is truncated.
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (headers already sent), got %d", w.Code)
	}

	body := w.Body.Bytes()
	if len(body) < 3 || body[0] != 0xEF || body[1] != 0xBB || body[2] != 0xBF {
		t.Fatalf("want UTF-8 BOM prefix even on error path, got %v", body[:min(3, len(body))])
	}
	rest := string(body[3:])
	if !strings.Contains(rest, "1,foo") {
		t.Errorf("want the rows written before the failure to be present, got %q", rest)
	}
	// truncated: no trailing/extra rows beyond what the exporter wrote before erroring.
	if strings.Count(rest, "\n") > strings.Count(partial, "\n") {
		t.Errorf("body should be truncated at the error point, got extra content: %q", rest)
	}

	logs := buf.String()
	if !strings.Contains(logs, "export: CSV generation error") {
		t.Errorf("want producer-side error logged, got logs: %s", logs)
	}
	if !strings.Contains(logs, wantErr.Error()) {
		t.Errorf("want log to include underlying error message, got logs: %s", logs)
	}
	if !strings.Contains(logs, "bills-") {
		t.Errorf("want log to include the export filename, got logs: %s", logs)
	}
	// Because pw.CloseWithError propagates the same error to the blocked
	// io.Copy(c.Writer, pr) Read in streamCSV, the mid-stream/client-lost
	// Warn branch fires too (io.Copy returns a non-nil, non-EOF error).
	if !strings.Contains(logs, "export: client connection lost mid-stream") {
		t.Errorf("want io.Copy error path (Warn) also logged, got logs: %s", logs)
	}

	if !bills.wasCalled() {
		t.Fatal("bills Exporter should have been invoked")
	}
	if stock.wasCalled() || payments.wasCalled() {
		t.Fatal("only the bills route's Exporter should be invoked")
	}
}

func TestZZ_StockAndPayments_ErrorAfterWrite_AlsoLogged(t *testing.T) {
	// Same failure shape exercised for the other two routes so every
	// Bills/Stock/Payments handler is confirmed to share the identical
	// mid-stream error contract (not just Bills).
	cases := []struct {
		name string
		path string
	}{
		{"stock", "/api/v1/exports/stock.csv"},
		{"payments", "/api/v1/exports/payments.csv"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantErr := errors.New("boom: " + tc.name)
			failing := &spyExporter{body: "hdr\n", err: wantErr}
			log, buf := newLogBuf()
			var r *gin.Engine
			tenantID := uuid.New()
			switch tc.name {
			case "stock":
				r = newAuthedEngine(&spyExporter{}, failing, &spyExporter{}, tenantID, log)
			case "payments":
				r = newAuthedEngine(&spyExporter{}, &spyExporter{}, failing, tenantID, log)
			}

			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, tc.path, nil)
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("want 200, got %d", w.Code)
			}
			logs := buf.String()
			if !strings.Contains(logs, "export: CSV generation error") {
				t.Errorf("%s: want error logged, got: %s", tc.name, logs)
			}
			if !strings.Contains(logs, wantErr.Error()) {
				t.Errorf("%s: want underlying error message in logs, got: %s", tc.name, logs)
			}
			if !failing.wasCalled() {
				t.Fatalf("%s: Exporter should have been called", tc.name)
			}
		})
	}
}

// ----- technical: producer goroutine join (done channel) — no leak, no race -----

func TestZZ_SlowExporter_HandlerReturnsOnlyAfterProducerFinishes(t *testing.T) {
	// A slow Exporter that sleeps before writing/returning. streamCSV's defer
	// does `pr.Close(); <-done`, so ServeHTTP must not return until the
	// producer goroutine has actually finished — verified here by checking
	// the finished flag immediately after ServeHTTP returns, with no
	// synchronization other than the handler's own join. Run with -race to
	// confirm no data race between the producer goroutine and this check.
	tenantID := uuid.New()
	bills := &spyExporter{body: "slow,row\n", delay: 30 * time.Millisecond}
	r := newAuthedEngine(bills, &spyExporter{}, &spyExporter{}, tenantID, nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/exports/bills.csv", nil)
	r.ServeHTTP(w, req) // blocks until streamCSV's defer has joined the producer

	if !bills.finished.Load() {
		t.Fatal("handler returned before the producer goroutine finished (done channel join broken)")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.Bytes()
	if len(body) < 3 || body[0] != 0xEF || body[1] != 0xBB || body[2] != 0xBF {
		t.Fatalf("want BOM prefix, got %v", body[:min(3, len(body))])
	}
	if !strings.Contains(string(body[3:]), "slow,row") {
		t.Fatalf("want full row from slow exporter present once handler returns, got %q", body[3:])
	}
}

// ----- default logger path (log == nil) sanity, exercised under concurrent calls -----

func TestZZ_NilLogger_DefaultsAndDoesNotPanic_Concurrent(t *testing.T) {
	// New(...) with a nil logger falls back to slog.Default(); run several
	// concurrent error-producing requests through it to shake out any races
	// on the shared *Handler / default logger (run this file with -race).
	tenantID := uuid.New()
	r := newAuthedEngine(
		&spyExporter{body: "a\n", err: errors.New("x")},
		&spyExporter{body: "b\n"},
		&spyExporter{body: "c\n", err: errors.New("y")},
		tenantID, nil,
	)

	var wg sync.WaitGroup
	paths := []string{
		"/api/v1/exports/bills.csv",
		"/api/v1/exports/stock.csv",
		"/api/v1/exports/payments.csv",
	}
	for i := 0; i < 6; i++ {
		path := paths[i%len(paths)]
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, p, nil)
			r.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("path %s: want 200, got %d", p, w.Code)
			}
		}(path)
	}
	wg.Wait()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
