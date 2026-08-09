package ai_test

// Supplemental coverage #2 for internal/adapter/handler/ai.
//
// handler_test.go and zz_coverage_test.go already exist and are not touched
// here (concurrent-session WIP risk / do-not-edit rule). At the time this
// file was added, `go tool cover -func` showed handler.go's Chat func at
// 97.7%: the only uncovered statements were the retry-after sub-second clamp
// (handler.go:157-159, `if seconds < 1 { seconds = 1 }`). Every existing
// rate-limit test used a >=1s window, so int(retryAfter.Seconds()) was
// already >=1 and the clamp branch never executed. This file adds the one
// missing case: a sub-second window whose computed retryAfter truncates to
// 0 seconds, forcing the clamp to 1.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	handlerai "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmgateway"
)

type zz2FakeIncrer struct {
	count int64
}

func (f *zz2FakeIncrer) Incr(_ context.Context, _ string) (int64, error) {
	f.count++
	return f.count, nil
}

func (f *zz2FakeIncrer) Expire(_ context.Context, _ string, _ time.Duration) error {
	return nil
}

type zz2StubOrchestrator struct {
	streamOut *appai.ChatOutput
}

func (s *zz2StubOrchestrator) StreamChat(_ context.Context, _ appai.ChatInput, _ func(string)) (*appai.ChatOutput, error) {
	return s.streamOut, nil
}

func (s *zz2StubOrchestrator) ConfirmPlan(_ context.Context, _, _, _ uuid.UUID) (*domainai.Plan, *appai.ExecutionResult, error) {
	return nil, nil, nil
}

func (s *zz2StubOrchestrator) CancelPlan(_ context.Context, _, _ uuid.UUID) error { return nil }

func (s *zz2StubOrchestrator) ListPlans(_ context.Context, _ uuid.UUID, _ string) ([]*domainai.Plan, error) {
	return nil, nil
}

// TestZZ2_Chat_RateLimiter_SubSecondWindow_ClampsRetryAfterToOne verifies the
// `if seconds < 1 { seconds = 1 }` clamp: with a sub-second window, the
// computed retryAfter truncates to 0 whole seconds via int(Duration.Seconds()),
// so the handler must still report Retry-After: 1 (never 0 — a 0-second
// Retry-After would tell clients to hammer the endpoint immediately).
func TestZZ2_Chat_RateLimiter_SubSecondWindow_ClampsRetryAfterToOne(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := &zz2FakeIncrer{}
	window := 200 * time.Millisecond
	rl := llmgateway.NewRateLimiter(store, 1, window)
	tenantID := uuid.New()

	// Exhaust the single allowed slot in this sub-second window.
	if allowed, _, err := rl.Allow(context.Background(), tenantID); err != nil || !allowed {
		t.Fatalf("setup: expected first call allowed, got allowed=%v err=%v", allowed, err)
	}

	stub := &zz2StubOrchestrator{streamOut: &appai.ChatOutput{}}
	h := handlerai.NewWithLimiter(stub, rl)

	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, tenantID)
		c.Next()
	})
	h.RegisterRoutes(e.Group("/api/v1"))

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/chat", strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d body=%s", rec.Code, rec.Body.String())
	}
	retryAfter := rec.Header().Get("Retry-After")
	var seconds int
	if _, err := fmt.Sscanf(retryAfter, "%d", &seconds); err != nil {
		t.Fatalf("Retry-After header not parseable: %q", retryAfter)
	}
	// window < 1s guarantees the pre-clamp value truncates to 0; the handler
	// must clamp it up to 1, never emit 0.
	if seconds != 1 {
		t.Errorf("expected clamped Retry-After=1 for sub-second window, got %d (header=%q)", seconds, retryAfter)
	}
}
