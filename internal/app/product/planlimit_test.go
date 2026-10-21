package product

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/entitlement"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
)

type planGuardStub struct {
	deny   bool
	keys   []string
	deltas []int
}

func (g *planGuardStub) Check(_ context.Context, _ uuid.UUID, key string, delta int) error {
	g.keys = append(g.keys, key)
	g.deltas = append(g.deltas, delta)
	if g.deny {
		return &entitlement.ErrPlanLimitReached{Key: key, Used: 5, Limit: 5}
	}
	return nil
}

func TestCreate_PlanLimitReached_WritesNothing(t *testing.T) {
	repo := &recordingRepo{}
	g := &planGuardStub{deny: true}
	_, err := NewCreateUseCase(repo).WithPlanGuard(g).Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Name: "n"})
	if _, ok := entitlement.IsPlanLimit(err); !ok {
		t.Fatalf("want plan limit error, got %v", err)
	}
	if len(repo.created) != 0 {
		t.Fatal("nothing may be written")
	}
	if g.keys[0] != "max_skus" || g.deltas[0] != 1 {
		t.Fatalf("guard: %v %v", g.keys, g.deltas)
	}
}

func TestImport_PlanLimit_RejectsWholeBatch(t *testing.T) {
	h := newImportHarness(fakeCatalog{})
	g := &planGuardStub{deny: true}
	h.uc.WithPlanGuard(g)
	data := csvHead + "螺丝,L1,,,,1,,,,,\n水管,L2,,,,2,,,,,\n,L3,,,,,,,,,\n"
	_, err := h.uc.Execute(context.Background(), ImportRequest{TenantID: uuid.New(), Data: []byte(data)})
	if _, ok := entitlement.IsPlanLimit(err); !ok {
		t.Fatalf("want plan limit error, got %v", err)
	}
	if len(h.repo.created) != 0 {
		t.Fatal("whole batch must be refused")
	}
	if g.deltas[0] != 2 {
		t.Fatalf("delta must be the number of valid rows (2), got %v", g.deltas)
	}
}

func TestImport_PlanLimit_PassesWhenRoom(t *testing.T) {
	h := newImportHarness(fakeCatalog{})
	h.uc.WithPlanGuard(&planGuardStub{})
	res, err := h.uc.Execute(context.Background(), ImportRequest{TenantID: uuid.New(), Data: []byte(csvHead + "螺丝,L1,,,,1,,,,,\n")})
	if err != nil || res.Imported != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestUpdate_ReEnable_ChecksPlanLimit(t *testing.T) {
	tenant := uuid.New()
	orig := baseProduct(tenant)
	orig.Enabled = false
	repo := &fakeRepo{getByIDProduct: orig}
	g := &planGuardStub{deny: true}
	tru := true
	_, err := NewUpdateUseCase(repo).WithPlanGuard(g).Execute(context.Background(), tenant, orig.ID, domain.UpdateInput{Enabled: &tru})
	if _, ok := entitlement.IsPlanLimit(err); !ok {
		t.Fatalf("want plan limit error, got %v", err)
	}
	if repo.updatedP != nil {
		t.Fatal("nothing may be written")
	}
	if len(g.deltas) != 1 || g.keys[0] != "max_skus" || g.deltas[0] != 1 {
		t.Fatalf("guard: %v %v", g.keys, g.deltas)
	}
}

func TestUpdate_AlreadyEnabledOrDisable_SkipsGuard(t *testing.T) {
	tenant := uuid.New()
	g := &planGuardStub{deny: true}
	tru, fls := true, false

	on := baseProduct(tenant) // 已启用，再次设置启用不新增占用
	if _, err := NewUpdateUseCase(&fakeRepo{getByIDProduct: on}).WithPlanGuard(g).
		Execute(context.Background(), tenant, on.ID, domain.UpdateInput{Enabled: &tru}); err != nil {
		t.Fatalf("enabled->enabled: %v", err)
	}
	if _, err := NewUpdateUseCase(&fakeRepo{getByIDProduct: baseProduct(tenant)}).WithPlanGuard(g).
		Execute(context.Background(), tenant, on.ID, domain.UpdateInput{Enabled: &fls}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if len(g.keys) != 0 {
		t.Fatalf("guard must not be called: %v", g.keys)
	}
}
