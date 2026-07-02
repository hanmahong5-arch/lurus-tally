package platform

import (
	"context"
	"net/http"
	"time"
)

// UsageEventRequest mirrors the body of POST /internal/v1/usage/events
// (lurus-platform, scope usage:report). It is a pure append-only metering
// ingest — platform records the row and derives cost downstream; it performs
// no wallet debit, so shadow vs enforce is a platform-side concern and Tally
// always just reports.
//
// Field contract (platform usage_report_handler.go):
//   - AccountID  required, > 0
//   - ProductID  required, ≤ 64 chars (Tally = "tally")
//   - Metric     required, ≤ 64 chars (Tally = "llm_tokens")
//   - Quantity   ≥ 0
//   - OccurredAt optional; platform rejects > 7 days old or > 1h in the future
//   - IdempotencyKey optional, ≤ 128; dedupe is on (product_id, idempotency_key)
//   - Metadata   optional free JSON
type UsageEventRequest struct {
	AccountID      int64          `json:"account_id"`
	ProductID      string         `json:"product_id"`
	Metric         string         `json:"metric"`
	Quantity       int64          `json:"quantity"`
	OccurredAt     time.Time      `json:"occurred_at,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// ReportUsageEvent posts one metering event. Returns a *Error on validation,
// auth, or transport failure; callers in shadow mode treat any error as
// non-fatal (log + drop) so metering never breaks the product path.
func (c *Client) ReportUsageEvent(ctx context.Context, req UsageEventRequest) error {
	if req.AccountID <= 0 {
		return &Error{Code: ErrCodeInvalidParameter, Message: "account_id is required"}
	}
	if req.ProductID == "" || req.Metric == "" {
		return &Error{Code: ErrCodeInvalidParameter, Message: "product_id and metric are required"}
	}
	return c.do(ctx, http.MethodPost, "/internal/v1/usage/events", req, nil)
}
