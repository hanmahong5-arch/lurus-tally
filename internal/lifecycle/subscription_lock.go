package lifecycle

import (
	"database/sql"
	"os"
	"strings"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/platform"
	repsubscription "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/subscription"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/subscription"
)

// devSubscriptionStatusEnv forces the subscription status in dev mode so the
// read-only lock can be exercised without a platform (e.g. "expired").
const devSubscriptionStatusEnv = "TALLY_DEV_SUBSCRIPTION_STATUS"

// buildSubscriptionLock picks the read-only-shop checker: the dev override when
// set (dev mode only), the platform-backed one when a platform client exists,
// otherwise a checker that never locks (a missing platform never locks a shop).
func buildSubscriptionLock(devMode bool, plat *platform.Client, db *sql.DB) subscription.Checker {
	if devMode {
		if status := strings.TrimSpace(os.Getenv(devSubscriptionStatusEnv)); status != "" {
			return subscription.Fixed(subscription.Evaluate(&subscription.Snapshot{PlanCode: "dev", Status: status}))
		}
	}
	if plat == nil || db == nil {
		return subscription.Never{}
	}
	return subscription.NewService(repsubscription.NewResolver(db), platform.NewSubscriptionSource(plat))
}
