package stock_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// multiRepo keeps one snapshot per product so a batch count can touch several.
type multiRepo struct {
	*mockRepo
	snaps map[uuid.UUID]*domain.Snapshot
}

func (m *multiRepo) SelectForUpdate(_ context.Context, _ *sql.Tx, _, p, _ uuid.UUID) (*domain.Snapshot, error) {
	return m.snaps[p], nil
}

func (m *multiRepo) UpsertSnapshot(_ context.Context, _ *sql.Tx, s *domain.Snapshot) error {
	m.snaps[s.ProductID] = s
	return nil
}

func newAdjust(snaps map[uuid.UUID]*domain.Snapshot) (*appstock.AdjustUseCase, *multiRepo) {
	repo := &multiRepo{mockRepo: newMockRepo(nil), snaps: snaps}
	calc := appstock.NewCalculator(nil, repo)
	rec := appstock.NewRecordMovementUseCase(repo, calc, nil, nil)
	return appstock.NewAdjustUseCase(repo, rec), repo
}

func snapOf(p uuid.UUID, qty, cost string) *domain.Snapshot {
	return &domain.Snapshot{ID: uuid.New(), TenantID: testTenantID, ProductID: p, WarehouseID: testWarehouseID,
		OnHandQty: d(qty), AvailableQty: d(qty), UnitCost: d(cost), CostStrategy: "wac"}
}

func TestAdjust_TenToSeven_WritesMinusThreeKeepsAvgCost(t *testing.T) {
	p := uuid.New()
	uc, repo := newAdjust(map[uuid.UUID]*domain.Snapshot{p: snapOf(p, "10", "5.5")})

	res, err := uc.Execute(context.Background(), appstock.AdjustRequest{
		TenantID: testTenantID, WarehouseID: testWarehouseID,
		Items:  []appstock.AdjustItem{{ProductID: p, CountedQty: d("7")}},
		Remark: "月底盘点",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Adjusted != 1 || res.Unchanged != 0 || !res.Lines[0].Delta.Equal(d("-3")) || !res.Lines[0].Before.Equal(d("10")) {
		t.Fatalf("result = %+v", res)
	}
	if len(repo.movements) != 1 {
		t.Fatalf("movements = %d", len(repo.movements))
	}
	m := repo.movements[0]
	if m.Direction != domain.DirectionAdjust || m.ReferenceType != domain.RefAdjust || !m.QtyBase.Equal(d("-3")) || m.Note != "月底盘点" {
		t.Fatalf("movement = %+v", m)
	}
	if m.ReferenceID == nil {
		t.Fatal("reference_id is NOT NULL in the schema")
	}
	got := repo.snaps[p]
	if !got.OnHandQty.Equal(d("7")) || !got.UnitCost.Equal(d("5.5")) {
		t.Fatalf("snapshot qty=%s cost=%s, want 7 / 5.5", got.OnHandQty, got.UnitCost)
	}
}

func TestAdjust_NoSnapshotYet_CountBecomesPositiveAdjust(t *testing.T) {
	p := uuid.New()
	uc, repo := newAdjust(map[uuid.UUID]*domain.Snapshot{})
	res, err := uc.Execute(context.Background(), appstock.AdjustRequest{
		TenantID: testTenantID, WarehouseID: testWarehouseID,
		Items: []appstock.AdjustItem{{ProductID: p, CountedQty: d("4"), UnitCost: d("2")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Lines[0].Delta.Equal(d("4")) || !repo.snaps[p].OnHandQty.Equal(d("4")) {
		t.Fatalf("res=%+v snap=%v", res, repo.snaps[p])
	}
}

func TestAdjust_EqualCountWritesNothing(t *testing.T) {
	p := uuid.New()
	uc, repo := newAdjust(map[uuid.UUID]*domain.Snapshot{p: snapOf(p, "10", "1")})
	res, err := uc.Execute(context.Background(), appstock.AdjustRequest{
		TenantID: testTenantID, WarehouseID: testWarehouseID,
		Items: []appstock.AdjustItem{{ProductID: p, CountedQty: d("10.000")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Adjusted != 0 || res.Unchanged != 1 || len(repo.movements) != 0 {
		t.Fatalf("res=%+v movements=%d", res, len(repo.movements))
	}
}

func TestAdjust_Validation(t *testing.T) {
	p := uuid.New()
	uc, repo := newAdjust(map[uuid.UUID]*domain.Snapshot{})
	base := appstock.AdjustRequest{TenantID: testTenantID, WarehouseID: testWarehouseID}
	cases := map[string]appstock.AdjustRequest{
		"empty":      base,
		"negative":   {TenantID: testTenantID, WarehouseID: testWarehouseID, Items: []appstock.AdjustItem{{ProductID: p, CountedQty: d("-1")}}},
		"dup":        {TenantID: testTenantID, WarehouseID: testWarehouseID, Items: []appstock.AdjustItem{{ProductID: p, CountedQty: d("1")}, {ProductID: p, CountedQty: d("2")}}},
		"nowh":       {TenantID: testTenantID, Items: []appstock.AdjustItem{{ProductID: p, CountedQty: d("1")}}},
		"nilproduct": {TenantID: testTenantID, WarehouseID: testWarehouseID, Items: []appstock.AdjustItem{{CountedQty: d("1")}}},
	}
	for name, req := range cases {
		_, err := uc.Execute(context.Background(), req)
		var ae *appstock.AdjustError
		if !errors.As(err, &ae) || ae.Msg == "" {
			t.Errorf("%s: want *AdjustError with Chinese message, got %v", name, err)
		}
	}
	if len(repo.movements) != 0 {
		t.Fatal("invalid requests must write nothing")
	}
}

func TestAdjust_ManyItems_AllInOneTx(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	uc, repo := newAdjust(map[uuid.UUID]*domain.Snapshot{a: snapOf(a, "5", "2"), b: snapOf(b, "5", "3")})
	res, err := uc.Execute(context.Background(), appstock.AdjustRequest{
		TenantID: testTenantID, WarehouseID: testWarehouseID,
		Items: []appstock.AdjustItem{{ProductID: a, CountedQty: d("9")}, {ProductID: b, CountedQty: d("1")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Adjusted != 2 || len(repo.movements) != 2 {
		t.Fatalf("res=%+v", res)
	}
	if repo.movements[0].ReferenceID == nil || *repo.movements[0].ReferenceID != res.AdjustID {
		t.Fatal("all lines share the adjustment id")
	}
}

// ownRepo adds the tenant-ownership check on top of multiRepo.
type ownRepo struct {
	*multiRepo
	whOK     bool
	products int
}

func (o *ownRepo) CheckOwned(_ context.Context, _ *sql.Tx, _, _ uuid.UUID, _ []uuid.UUID) (bool, int, error) {
	return o.whOK, o.products, nil
}

func TestAdjust_ForeignOrDeletedIDs_Rejected(t *testing.T) {
	cases := []struct {
		name     string
		whOK     bool
		products int
	}{
		{"foreign warehouse", false, 1},
		{"foreign or deleted product", true, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := uuid.New()
			base := &multiRepo{mockRepo: newMockRepo(nil), snaps: map[uuid.UUID]*domain.Snapshot{}}
			repo := &ownRepo{multiRepo: base, whOK: c.whOK, products: c.products}
			calc := appstock.NewCalculator(nil, repo)
			rec := appstock.NewRecordMovementUseCase(repo, calc, nil, nil)
			uc := appstock.NewAdjustUseCase(repo, rec)
			_, err := uc.Execute(context.Background(), appstock.AdjustRequest{
				TenantID: testTenantID, WarehouseID: testWarehouseID,
				Items: []appstock.AdjustItem{{ProductID: p, CountedQty: d("5")}},
			})
			var ae *appstock.AdjustError
			if !errors.As(err, &ae) {
				t.Fatalf("err = %v, want AdjustError", err)
			}
			if len(base.movements) != 0 || len(base.snaps) != 0 {
				t.Fatalf("wrote data despite rejection")
			}
		})
	}
}

// costRepo adds the fallback price lookup on top of multiRepo.
type costRepo struct {
	*multiRepo
	cost string
}

func (c *costRepo) FallbackCost(_ context.Context, _ *sql.Tx, _, _, _ uuid.UUID) (decimal.Decimal, error) {
	return d(c.cost), nil
}

func newCostAdjust(cost string) (*appstock.AdjustUseCase, *costRepo) {
	repo := &costRepo{multiRepo: &multiRepo{mockRepo: newMockRepo(nil), snaps: map[uuid.UUID]*domain.Snapshot{}}, cost: cost}
	calc := appstock.NewCalculator(nil, repo)
	rec := appstock.NewRecordMovementUseCase(repo, calc, nil, nil)
	return appstock.NewAdjustUseCase(repo, rec), repo
}

func TestAdjust_EmptyBook_UsesFallbackPurchasePrice(t *testing.T) {
	p := uuid.New()
	uc, repo := newCostAdjust("8.3")
	_, err := uc.Execute(context.Background(), appstock.AdjustRequest{
		TenantID: testTenantID, WarehouseID: testWarehouseID,
		Items: []appstock.AdjustItem{{ProductID: p, CountedQty: d("5")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := repo.snaps[p]; !got.OnHandQty.Equal(d("5")) || !got.UnitCost.Equal(d("8.3")) {
		t.Fatalf("snapshot qty=%s cost=%s, want 5 / 8.3", got.OnHandQty, got.UnitCost)
	}
}

func TestAdjust_KeepersPriceBeatsFallback(t *testing.T) {
	p := uuid.New()
	uc, repo := newCostAdjust("8.3")
	if _, err := uc.Execute(context.Background(), appstock.AdjustRequest{
		TenantID: testTenantID, WarehouseID: testWarehouseID,
		Items: []appstock.AdjustItem{{ProductID: p, CountedQty: d("5"), UnitCost: d("9")}},
	}); err != nil {
		t.Fatal(err)
	}
	if !repo.snaps[p].UnitCost.Equal(d("9")) {
		t.Fatalf("cost = %s, want 9", repo.snaps[p].UnitCost)
	}
}

func TestAdjust_EmptyBookNoPrice_AsksForCostAndWritesNothing(t *testing.T) {
	p, q := uuid.New(), uuid.New()
	uc, repo := newCostAdjust("0")
	repo.snaps[q] = snapOf(q, "3", "2")
	_, err := uc.Execute(context.Background(), appstock.AdjustRequest{
		TenantID: testTenantID, WarehouseID: testWarehouseID,
		Items: []appstock.AdjustItem{{ProductID: p, CountedQty: d("5")}, {ProductID: q, CountedQty: d("4")}},
	})
	var ae *appstock.AdjustError
	if !errors.As(err, &ae) || ae.Code != appstock.AdjustCostRequired || len(ae.ProductIDs) != 1 || ae.ProductIDs[0] != p {
		t.Fatalf("err = %v, want cost_required for the empty product only", err)
	}
}

func TestAdjust_KeptSnapshotCostIsUsedWhenBookIsEmpty(t *testing.T) {
	p := uuid.New()
	uc, repo := newCostAdjust("0")
	repo.snaps[p] = snapOf(p, "0", "6")
	if _, err := uc.Execute(context.Background(), appstock.AdjustRequest{
		TenantID: testTenantID, WarehouseID: testWarehouseID,
		Items: []appstock.AdjustItem{{ProductID: p, CountedQty: d("2")}},
	}); err != nil {
		t.Fatal(err)
	}
	if !repo.snaps[p].UnitCost.Equal(d("6")) {
		t.Fatalf("cost = %s, want 6", repo.snaps[p].UnitCost)
	}
}

func TestAdjust_NegativeUnitCostRejected(t *testing.T) {
	uc, _ := newAdjust(map[uuid.UUID]*domain.Snapshot{})
	_, err := uc.Execute(context.Background(), appstock.AdjustRequest{
		TenantID: testTenantID, WarehouseID: testWarehouseID,
		Items: []appstock.AdjustItem{{ProductID: uuid.New(), CountedQty: d("1"), UnitCost: d("-1")}},
	})
	var ae *appstock.AdjustError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v", err)
	}
}
