package ai

import (
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
)

// Memory is best effort: a failed recall or write never fails the turn. That
// also made failures invisible — writes ran in a goroutine that discarded the
// error, so memorus could be down for days with every turn "remembered". These
// counters (on /internal/v1/metrics) and the per-turn log line are the record.
var memoryOps = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "tally_ai_memory_ops_total",
		Help: "AI drawer memory operations, by op (recall|write|fact_write) and outcome (ok|error).",
	},
	[]string{"op", "outcome"},
)

func init() {
	prometheus.MustRegister(memoryOps)
}

const (
	memOpRecall    = "recall"
	memOpWrite     = "write"
	memOpFactWrite = "fact_write"
)

// countMemoryOp records one memory operation; a failed one is also logged.
func countMemoryOp(op string, err error) {
	if err == nil {
		memoryOps.WithLabelValues(op, "ok").Inc()
		return
	}
	memoryOps.WithLabelValues(op, "error").Inc()
	slog.Warn("ai memory op failed", "op", op, "err", err)
}

// panicError turns a recovered value into an error for countMemoryOp.
func panicError(r any) error {
	return fmt.Errorf("panic: %v", r)
}
