// Package usagereport bridges the LLM hot path to lurus-platform's metering
// ingest (POST /internal/v1/usage/events) for unified-billing Wave 2.
//
// It implements llmgateway.UsageSink: each completed LLM call enqueues a
// non-blocking event; a small worker pool drains the queue off the hot path,
// resolves the tenant's platform account id, and posts a usage event. Every
// failure mode (no account pinned, resolver error, platform down, queue full)
// is fail-open — metering must never break the AI product path. This is the
// "shadow" stance: report what we can, drop the rest loudly via metrics, never
// block or error the caller.
package usagereport

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/platform"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmgateway"
)

const (
	// ProductID is Tally's identifier in the platform product registry
	// (seeded by lurus-platform migration 025, canonicalized to the bare
	// form in migration 100).
	ProductID = "tally"
	// MetricLLMTokens is the metering metric name for LLM token spend.
	MetricLLMTokens = "llm_tokens"

	defaultBuffer  = 256
	defaultWorkers = 2
	defaultTimeout = 5 * time.Second
)

// EventPoster posts one usage event to platform. *platform.Client satisfies it.
type EventPoster interface {
	ReportUsageEvent(ctx context.Context, req platform.UsageEventRequest) error
}

// AccountResolver maps a Tally tenant UUID to its pinned platform account id.
// (id, false, nil) means no account is pinned yet (pre-Wave-2 tenant or
// platform was down at onboarding) — the event is skipped in shadow.
// *repo/tenant.TenantRepo satisfies it.
type AccountResolver interface {
	GetPlatformAccountID(ctx context.Context, tenantID uuid.UUID) (int64, bool, error)
}

// event is the hot-path capture. It deliberately carries only the tenant string
// and token counts — NOT the request context — because the worker posts on a
// fresh background context (the request ctx is cancelled the moment the LLM
// response returns).
type event struct {
	id               uuid.UUID // stable identity → idempotency key, shared by in-memory post and durable retry
	tenant           string
	model            string
	promptTokens     int
	completionTokens int
	occurredAt       time.Time
}

// Config tunes the reporter. Zero values fall back to sensible defaults.
type Config struct {
	BufferSize int           // queue depth (default 256)
	Workers    int           // drain concurrency (default 2)
	Timeout    time.Duration // per-post/ resolve timeout (default 5s)
	Logger     *slog.Logger
	// Store is the durable retry queue. When nil (degraded config) a
	// recoverable failure falls back to the legacy loud drop instead of queuing.
	Store UsageOutbox
}

// Reporter implements llmgateway.UsageSink.
type Reporter struct {
	poster   EventPoster
	resolver AccountResolver
	store    UsageOutbox // durable retry queue; nil → legacy drop (degraded config)
	logger   *slog.Logger
	timeout  time.Duration
	workers  int

	ch chan event
	wg sync.WaitGroup

	// cache holds positively-resolved account ids (tenant uuid string -> id).
	// Negative results are NOT cached so a later onboarding heal is picked up.
	mu    sync.RWMutex
	cache map[string]int64

	// Injectable for tests; default to real uuid / wall clock. newID mints the
	// per-event id that seeds the stable idempotency key (so an in-memory post
	// and its durable retry share one identity → platform dedups).
	newID func() uuid.UUID
	now   func() time.Time

	started atomic.Bool
}

// New builds a Reporter. Call Start to spin up workers and Stop to drain.
func New(poster EventPoster, resolver AccountResolver, cfg Config) *Reporter {
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = defaultBuffer
	}
	if cfg.Workers <= 0 {
		cfg.Workers = defaultWorkers
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Reporter{
		poster:   poster,
		resolver: resolver,
		store:    cfg.Store,
		logger:   logger,
		timeout:  cfg.Timeout,
		workers:  cfg.Workers,
		ch:       make(chan event, cfg.BufferSize),
		cache:    make(map[string]int64),
		newID:    func() uuid.UUID { return uuid.New() },
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// Start launches the worker pool. Idempotent — a second call is a no-op.
func (r *Reporter) Start() {
	if !r.started.CompareAndSwap(false, true) {
		return
	}
	for i := 0; i < r.workers; i++ {
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			for ev := range r.ch {
				r.process(ev)
			}
		}()
	}
}

// Record implements llmgateway.UsageSink. Non-blocking: it captures the tenant
// + tokens and enqueues, or drops (with a metric bump) when the queue is full.
// It never does network/DB work inline.
func (r *Reporter) Record(ctx context.Context, model string, promptTokens, completionTokens int) {
	if promptTokens <= 0 && completionTokens <= 0 {
		return // nothing billable
	}
	tenant := llmgateway.TenantFrom(ctx)
	if tenant == "" || tenant == "unknown" {
		metricSkipped.WithLabelValues("no_tenant").Inc()
		return // cannot attribute
	}
	select {
	case r.ch <- event{
		id:               r.newID(),
		tenant:           tenant,
		model:            model,
		promptTokens:     promptTokens,
		completionTokens: completionTokens,
		occurredAt:       r.now(),
	}:
		metricEnqueued.Inc()
	default:
		// Queue full — drop rather than block the LLM response path.
		metricDropped.Inc()
		r.logger.Warn("usage reporter queue full; dropped event",
			slog.String("tenant", tenant), slog.String("model", model))
	}
}

// process resolves the account and posts one event. All failures are non-fatal.
func (r *Reporter) process(ev event) {
	tid, err := uuid.Parse(ev.tenant)
	if err != nil {
		metricSkipped.WithLabelValues("bad_tenant").Inc()
		return
	}

	accountID, ok, reason := r.resolveAccount(tid)
	if !ok {
		// Unprovisioned tenant or resolver error — durably queue for retry
		// (re-resolved later) instead of silently dropping.
		r.enqueueForRetry(ev, tid, reason)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()

	req := platform.UsageEventRequest{
		AccountID:      accountID,
		ProductID:      ProductID,
		Metric:         MetricLLMTokens,
		Quantity:       int64(ev.promptTokens + ev.completionTokens),
		OccurredAt:     ev.occurredAt,
		IdempotencyKey: usageIdemKey(ev.id),
		Metadata: map[string]any{
			"tenant_id":         ev.tenant,
			"model":             ev.model,
			"prompt_tokens":     ev.promptTokens,
			"completion_tokens": ev.completionTokens,
			"source":            "ai_assistant",
		},
	}
	if err := r.poster.ReportUsageEvent(ctx, req); err != nil {
		metricPostErr.Inc()
		r.logger.Warn("usage event post failed; queued for retry",
			slog.String("tenant", ev.tenant),
			slog.Int64("account_id", accountID),
			slog.String("error", err.Error()))
		r.enqueueForRetry(ev, tid, "post_error")
		return
	}
	metricPosted.Inc()
}

// enqueueForRetry durably queues a recoverable-failure event so it is retried
// (with the account re-resolved) instead of silently dropped. With no durable
// store wired (degraded config) it falls back to the legacy loud drop, so the
// reporter still works — it just loses the at-least-once guarantee.
func (r *Reporter) enqueueForRetry(ev event, tid uuid.UUID, reason string) {
	if r.store == nil {
		metricSkipped.WithLabelValues(reason).Inc()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	if err := r.store.Enqueue(ctx, PendingUsageRow{
		ID:               ev.id,
		TenantID:         tid,
		Model:            ev.model,
		PromptTokens:     ev.promptTokens,
		CompletionTokens: ev.completionTokens,
		OccurredAt:       ev.occurredAt,
		Reason:           reason,
	}); err != nil {
		// Platform AND the local DB are both unreachable — genuine last-resort loss.
		metricEnqueueFailed.Inc()
		r.logger.Warn("usage reporter: durable enqueue failed; event lost",
			slog.String("tenant", ev.tenant),
			slog.String("reason", reason),
			slog.String("error", err.Error()))
		return
	}
	metricQueuedForRetry.WithLabelValues(reason).Inc()
}

// resolveAccount returns the tenant's platform account id, using the positive
// cache first. On failure it returns ok=false and a reason ("resolve_error" |
// "no_account") so the caller decides whether to queue or drop — the metric is
// no longer bumped here (a queued event is not "skipped").
func (r *Reporter) resolveAccount(tid uuid.UUID) (int64, bool, string) {
	key := tid.String()
	r.mu.RLock()
	if id, hit := r.cache[key]; hit {
		r.mu.RUnlock()
		return id, true, ""
	}
	r.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	id, ok, err := r.resolver.GetPlatformAccountID(ctx, tid)
	if err != nil {
		r.logger.Warn("usage reporter: account resolve failed",
			slog.String("tenant", key), slog.String("error", err.Error()))
		return 0, false, "resolve_error"
	}
	if !ok {
		return 0, false, "no_account"
	}
	r.mu.Lock()
	r.cache[key] = id
	r.mu.Unlock()
	return id, true, ""
}

// Stop closes the queue and waits for workers to drain, bounded by ctx. After
// Stop the Reporter must not be reused.
func (r *Reporter) Stop(ctx context.Context) {
	close(r.ch)
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		r.logger.Warn("usage reporter: stop timed out before drain")
	}
}
