package metrics

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// dauSource is the narrow read surface the collector needs from the DAU store.
type dauSource interface {
	CountToday(ctx context.Context, event string) (int64, error)
	CountTotalToday(ctx context.Context) (int64, error)
}

// collectTimeout bounds the Redis round-trips one Prometheus scrape may
// trigger. PFCOUNT is O(1), so this is headroom against a slow link, not a
// tuning knob.
const collectTimeout = 2 * time.Second

// DAUCollector exposes per-user Daily-Active-User gauges sourced from Redis HLL
// keys. It implements prometheus.Collector so each value is computed lazily at
// scrape time (PFCOUNT) instead of being held in process memory. Metric names
// align exactly with bin/assumption-snapshot.sh, which reads them as plain
// gauges.
type DAUCollector struct {
	src dauSource
	log *slog.Logger

	palette *prometheus.Desc
	drawer  *prometheus.Desc
	total   *prometheus.Desc
}

// NewDAUCollector builds the collector over a DAU store. A nil logger falls
// back to slog.Default().
func NewDAUCollector(src dauSource, log *slog.Logger) *DAUCollector {
	if log == nil {
		log = slog.Default()
	}
	return &DAUCollector{
		src: src,
		log: log,
		palette: prometheus.NewDesc(
			"tally_palette_invocation_dau",
			"Unique users who invoked the ⌘K command palette today (HLL cardinality, UTC day).",
			nil, nil,
		),
		drawer: prometheus.NewDesc(
			"tally_ai_drawer_open_dau",
			"Unique users who opened the AI Drawer today (HLL cardinality, UTC day).",
			nil, nil,
		),
		total: prometheus.NewDesc(
			"tally_total_dau",
			"Unique active users today across all tracked product events (HLL cardinality, UTC day).",
			nil, nil,
		),
	}
}

// Describe sends the static metric descriptors.
func (c *DAUCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.palette
	ch <- c.drawer
	ch <- c.total
}

// Collect computes each gauge at scrape time. A Redis error for one gauge is
// logged and that series is omitted — emitting 0 would read as "zero users"
// when the truth is "no data", which would silently falsify the H3 ratio.
func (c *DAUCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), collectTimeout)
	defer cancel()

	c.emitEvent(ctx, ch, c.palette, "palette_invocation")
	c.emitEvent(ctx, ch, c.drawer, "ai_drawer_open")

	if v, err := c.src.CountTotalToday(ctx); err != nil {
		c.log.Warn("dau collector: total count failed", slog.String("error", err.Error()))
	} else {
		ch <- prometheus.MustNewConstMetric(c.total, prometheus.GaugeValue, float64(v))
	}
}

func (c *DAUCollector) emitEvent(ctx context.Context, ch chan<- prometheus.Metric, desc *prometheus.Desc, event string) {
	v, err := c.src.CountToday(ctx, event)
	if err != nil {
		c.log.Warn("dau collector: count failed",
			slog.String("event", event), slog.String("error", err.Error()))
		return
	}
	ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(v))
}
