package warehouse_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/entitlement"
	appwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/app/warehouse"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/warehouse"
)

type denyGuard struct{ key string }

func (g *denyGuard) Check(_ context.Context, _ uuid.UUID, key string, _ int) error {
	g.key = key
	return &entitlement.ErrPlanLimitReached{Key: key, Used: 2, Limit: 2}
}

func TestCreateUseCase_PlanLimitReached(t *testing.T) {
	repo := newFakeRepo()
	g := &denyGuard{}
	_, err := appwarehouse.NewCreateUseCase(repo).WithPlanGuard(g).
		Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Name: "二仓"})
	if _, ok := entitlement.IsPlanLimit(err); !ok {
		t.Fatalf("want plan limit error, got %v", err)
	}
	if g.key != "max_warehouses" {
		t.Fatalf("key = %q", g.key)
	}
}
