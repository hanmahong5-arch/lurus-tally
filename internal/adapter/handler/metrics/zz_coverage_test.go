package metrics_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	handlermetrics "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/metrics"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// --- Serve gate coverage -----------------------------------------------

func TestMetricsHandler_Serve_GateDisabled_AlwaysProxies(t *testing.T) {
	h := handlermetrics.NewMetricsHandler("")

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/metrics", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	h.Serve(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from promhttp when gate disabled, got %d", w.Code)
	}
}

func TestMetricsHandler_Serve_GateEnabled(t *testing.T) {
	const key = "s3cr3t-key"

	cases := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{
			name:       "missing header",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong token exact bearer form",
			authHeader: "Bearer wrong-token",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "correct bearer key proxies",
			authHeader: "Bearer " + key,
			wantStatus: http.StatusOK,
		},
		{
			name:       "no Bearer prefix at all (CutPrefix false branch)",
			authHeader: key,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "lowercase bearer prefix mismatch",
			authHeader: "bearer " + key,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "prefix present but key empty suffix",
			authHeader: "Bearer ",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := handlermetrics.NewMetricsHandler(key)

			req := httptest.NewRequest(http.MethodGet, "/internal/v1/metrics", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = req

			h.Serve(c)

			if w.Code != tc.wantStatus {
				t.Fatalf("%s: expected status %d, got %d (body=%s)", tc.name, tc.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

// --- DAUCollector coverage ------------------------------------------------

// fakeDAUSource is a hand-controlled stand-in for the dauSource interface;
// each field lets a test dictate the exact return values/errors so expected
// gauge values are computed by hand, not read back from the collector.
type fakeDAUSource struct {
	paletteCount int64
	paletteErr   error
	drawerCount  int64
	drawerErr    error
	totalCount   int64
	totalErr     error
}

func (f *fakeDAUSource) CountToday(_ context.Context, event string) (int64, error) {
	switch event {
	case "palette_invocation":
		return f.paletteCount, f.paletteErr
	case "ai_drawer_open":
		return f.drawerCount, f.drawerErr
	default:
		return 0, errors.New("unexpected event: " + event)
	}
}

func (f *fakeDAUSource) CountTotalToday(_ context.Context) (int64, error) {
	return f.totalCount, f.totalErr
}

func TestDAUCollector_Collect_HappyPath_HandAssertedValues(t *testing.T) {
	src := &fakeDAUSource{
		paletteCount: 3,
		drawerCount:  5,
		totalCount:   8,
	}
	collector := handlermetrics.NewDAUCollector(src, nil)

	if got := testutil.CollectAndCount(collector); got != 3 {
		t.Fatalf("expected 3 series in happy path, got %d", got)
	}

	assertGaugeValue(t, collector, "tally_palette_invocation_dau", 3)
	assertGaugeValue(t, collector, "tally_ai_drawer_open_dau", 5)
	assertGaugeValue(t, collector, "tally_total_dau", 8)
}

func TestDAUCollector_Collect_PaletteError_SeriesOmittedNotZero(t *testing.T) {
	src := &fakeDAUSource{
		paletteErr:  errors.New("redis: PFCOUNT boom"),
		drawerCount: 5,
		totalCount:  8,
	}
	collector := handlermetrics.NewDAUCollector(src, nil)

	// Only drawer + total should survive: 2 series, not 3, and definitely not
	// a palette gauge reading 0.
	if got := testutil.CollectAndCount(collector); got != 2 {
		t.Fatalf("expected 2 series when palette errors, got %d", got)
	}
	if got := testutil.CollectAndCount(collector, "tally_palette_invocation_dau"); got != 0 {
		t.Fatalf("palette series must be entirely absent on error, got count=%d", got)
	}
	assertGaugeValue(t, collector, "tally_ai_drawer_open_dau", 5)
	assertGaugeValue(t, collector, "tally_total_dau", 8)
}

func TestDAUCollector_Collect_DrawerError_SeriesOmittedNotZero(t *testing.T) {
	src := &fakeDAUSource{
		paletteCount: 3,
		drawerErr:    errors.New("redis: PFCOUNT boom"),
		totalCount:   8,
	}
	collector := handlermetrics.NewDAUCollector(src, nil)

	if got := testutil.CollectAndCount(collector); got != 2 {
		t.Fatalf("expected 2 series when drawer errors, got %d", got)
	}
	if got := testutil.CollectAndCount(collector, "tally_ai_drawer_open_dau"); got != 0 {
		t.Fatalf("drawer series must be entirely absent on error, got count=%d", got)
	}
	assertGaugeValue(t, collector, "tally_palette_invocation_dau", 3)
	assertGaugeValue(t, collector, "tally_total_dau", 8)
}

func TestDAUCollector_Collect_TotalError_SeriesOmittedNotZero(t *testing.T) {
	src := &fakeDAUSource{
		paletteCount: 3,
		drawerCount:  5,
		totalErr:     errors.New("redis: PFCOUNT boom"),
	}
	collector := handlermetrics.NewDAUCollector(src, nil)

	if got := testutil.CollectAndCount(collector); got != 2 {
		t.Fatalf("expected 2 series when total errors, got %d", got)
	}
	if got := testutil.CollectAndCount(collector, "tally_total_dau"); got != 0 {
		t.Fatalf("total series must be entirely absent on error, got count=%d", got)
	}
	assertGaugeValue(t, collector, "tally_palette_invocation_dau", 3)
	assertGaugeValue(t, collector, "tally_ai_drawer_open_dau", 5)
}

func TestDAUCollector_Collect_AllErrors_NoSeriesEmitted(t *testing.T) {
	src := &fakeDAUSource{
		paletteErr: errors.New("boom-1"),
		drawerErr:  errors.New("boom-2"),
		totalErr:   errors.New("boom-3"),
	}
	collector := handlermetrics.NewDAUCollector(src, nil)

	if got := testutil.CollectAndCount(collector); got != 0 {
		t.Fatalf("expected 0 series when every source call errors, got %d", got)
	}
}

func TestNewDAUCollector_NilLogger_FallsBackAndDoesNotPanic(t *testing.T) {
	src := &fakeDAUSource{
		paletteErr: errors.New("boom"), // exercises the c.log.Warn call on the fallback logger
		drawerErr:  errors.New("boom"),
		totalErr:   errors.New("boom"),
	}
	collector := handlermetrics.NewDAUCollector(src, nil)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Collect panicked with fallback logger: %v", r)
		}
	}()

	if got := testutil.CollectAndCount(collector); got != 0 {
		t.Fatalf("expected 0 series, got %d", got)
	}
}

func TestDAUCollector_Describe_EmitsExactlyThreeDescriptors(t *testing.T) {
	collector := handlermetrics.NewDAUCollector(&fakeDAUSource{}, nil)

	ch := make(chan *prometheus.Desc, 10)
	collector.Describe(ch)
	close(ch)

	count := 0
	for range ch {
		count++
	}
	if count != 3 {
		t.Fatalf("expected exactly 3 descriptors, got %d", count)
	}
}

// assertGaugeValue registers collector into a fresh registry, gathers it, and
// hand-checks that the named series carries exactly want.
func assertGaugeValue(t *testing.T, collector prometheus.Collector, name string, want float64) {
	t.Helper()

	reg := prometheus.NewRegistry()
	if err := reg.Register(collector); err != nil {
		t.Fatalf("register: %v", err)
	}
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		if len(mf.GetMetric()) != 1 {
			t.Fatalf("%s: expected exactly 1 metric, got %d", name, len(mf.GetMetric()))
		}
		got := mf.GetMetric()[0].GetGauge().GetValue()
		if got != want {
			t.Fatalf("%s: want %v, got %v", name, want, got)
		}
		return
	}
	t.Fatalf("series %s not found in gathered metric families", name)
}
