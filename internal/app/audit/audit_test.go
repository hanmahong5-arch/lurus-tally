package audit

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSummary(t *testing.T) {
	cases := map[string]string{
		`{"unit_price":{"from":"12.00","to":"10.00"}}`: "单价 12.00 → 10.00",
		`{"reason":"录错了"}`:                             "原因：录错了",
		`{"qty":{"from":3,"to":5}}`:                    "数量 3 → 5",
		`{"secret_blob":"x"}`:                          "",
		`not json`:                                     "",
		``:                                             "",
	}
	for in, want := range cases {
		if got := Summary([]byte(in)); got != want {
			t.Errorf("Summary(%s)=%q want %q", in, got, want)
		}
	}
}

func TestLabels_NoEnglish(t *testing.T) {
	for a := range actionLabels {
		if l := ActionLabel(a); l == a || l == "" {
			t.Errorf("action %s label %q", a, l)
		}
	}
	if ActionLabel("weird.thing") != "其他操作" || ResourceLabel("weird") != "其他" {
		t.Fatal("unknown values must fall back to Chinese")
	}
}

type fakeRepo struct{ rows []Row }

func (f fakeRepo) Append(context.Context, Entry) error { return nil }
func (f fakeRepo) List(context.Context, uuid.UUID, Filter) ([]Row, int, error) {
	return f.rows, len(f.rows), nil
}

type fakeNames map[uuid.UUID]string

func (f fakeNames) MemberNames(context.Context, uuid.UUID) (map[uuid.UUID]string, error) {
	return f, nil
}

func TestList_ActorNames(t *testing.T) {
	boss, gone := uuid.New(), uuid.New()
	rid := uuid.New()
	repo := fakeRepo{rows: []Row{
		{ID: uuid.New(), UserID: boss, Action: ActionBillApproved, Resource: ResourcePurchaseBill, ResourceID: rid, ResourceNo: "PO-1", CreatedAt: time.Now()},
		{ID: uuid.New(), UserID: gone, Action: ActionBillVoided, Resource: ResourceSaleBill},
		{ID: uuid.New(), Action: ActionBillCreated, Resource: ResourceSaleBill},
	}}
	items, total, err := NewListUseCase(repo, fakeNames{boss: "老王"}).Execute(context.Background(), uuid.New(), Filter{})
	if err != nil || total != 3 {
		t.Fatalf("err=%v total=%d", err, total)
	}
	if items[0].ActorName != "老王" || items[0].ActionLabel != "审核" || items[0].ResourceLabel != "采购单" || items[0].ResourceNo != "PO-1" {
		t.Errorf("item0 %+v", items[0])
	}
	if items[1].ActorName != "已移出的成员" || items[2].ActorName != "系统" {
		t.Errorf("names %q %q", items[1].ActorName, items[2].ActorName)
	}
	if items[1].ResourceID != "" {
		t.Errorf("nil resource id must be empty, got %q", items[1].ResourceID)
	}
}
