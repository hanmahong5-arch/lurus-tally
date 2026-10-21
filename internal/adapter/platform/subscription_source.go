package platform

import (
	"context"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/subscription"
)

// subscriptionProductID is tally's identifier in the platform product registry
// (same value app/billing.ProductID sends).
const subscriptionProductID = "tally"

// SubscriptionSource adapts the platform client to subscription.Source.
type SubscriptionSource struct{ c *Client }

// NewSubscriptionSource wraps a platform client.
func NewSubscriptionSource(c *Client) *SubscriptionSource { return &SubscriptionSource{c: c} }

// Subscription returns the account subscription for the tally product, or nil
// when the account has none.
func (s *SubscriptionSource) Subscription(ctx context.Context, accountID int64) (*subscription.Snapshot, error) {
	ov, err := s.c.GetAccountOverview(ctx, accountID, subscriptionProductID)
	if err != nil {
		return nil, err
	}
	if ov == nil || ov.Subscription == nil {
		return nil, nil
	}
	return &subscription.Snapshot{
		PlanCode:  ov.Subscription.PlanCode,
		Status:    ov.Subscription.Status,
		ExpiresAt: ov.Subscription.ExpiresAt,
	}, nil
}
