//go:build integration

package integration

import (
	"context"
	"sort"
	"testing"
	"time"

	airepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/ai"
	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
)

// TestSQLReal_CustomerSales runs the "上次买了什么" queries against the real
// schema: which bills count as a customer's purchases, and which names match.
func TestSQLReal_CustomerSales(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()
	ctx := context.Background()

	tenantID := insertTenant(t, db, ctx)
	water := insertProduct(t, db, ctx, tenantID, "矿泉水", "W1")
	zhang := insertPartner(t, db, ctx, tenantID, "张三", "customer")
	feng := insertPartner(t, db, ctx, tenantID, "张三丰", "customer")
	insertPartner(t, db, ctx, tenantID, "张三供应", "supplier")

	now := time.Now().UTC()
	older := insertBillHead(t, db, ctx, tenantID, "出库", "销售", 2, &zhang, now.AddDate(0, 0, -9))
	insertBillItem(t, db, ctx, tenantID, older, water, 2, 32, 20)
	newer := insertBillHead(t, db, ctx, tenantID, "出库", "销售", 2, &zhang, now.AddDate(0, 0, -1))
	insertBillItem(t, db, ctx, tenantID, newer, water, 5, 30, 20)
	draft := insertBillHead(t, db, ctx, tenantID, "出库", "销售", 0, &zhang, now)
	insertBillItem(t, db, ctx, tenantID, draft, water, 99, 1, 1)
	fengBill := insertBillHead(t, db, ctx, tenantID, "出库", "销售", 2, &feng, now)
	insertBillItem(t, db, ctx, tenantID, fengBill, water, 7, 32, 20)
	quick := insertBillHead(t, db, ctx, tenantID, "出库", "销售", 2, nil, now.AddDate(0, 0, -3))
	insertBillItem(t, db, ctx, tenantID, quick, water, 1, 33, 20)
	if _, err := db.ExecContext(ctx, `UPDATE tally.bill_head SET remark = '张三' WHERE id = $1`, quick); err != nil {
		t.Fatal(err)
	}

	other := insertTenant(t, db, ctx)
	otherWater := insertProduct(t, db, ctx, other, "矿泉水", "W1")
	otherZhang := insertPartner(t, db, ctx, other, "张三", "customer")
	ob := insertBillHead(t, db, ctx, other, "出库", "销售", 2, &otherZhang, now)
	insertBillItem(t, db, ctx, other, ob, otherWater, 3, 32, 20)

	repo := airepo.NewSQLSaleRepo(db)

	t.Run("MatchCustomers", func(t *testing.T) {
		got, err := repo.MatchCustomers(ctx, tenantID, "张三")
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, c := range got {
			names = append(names, c.Name)
		}
		sort.Strings(names)
		want := []string{"张三", "张三", "张三丰"} // partner, walk-in (quick checkout), partner
		if len(names) != len(want) {
			t.Fatalf("names = %v, want %v (supplier and other tenant excluded)", names, want)
		}
		for i := range want {
			if names[i] != want[i] {
				t.Fatalf("names = %v, want %v", names, want)
			}
		}
	})

	t.Run("ListCustomerSales", func(t *testing.T) {
		bills, err := repo.ListCustomerSales(ctx, tenantID, appai.CustomerRef{ID: zhang, Name: "张三"}, 5)
		if err != nil {
			t.Fatal(err)
		}
		// Newest first: -1d (5×30), quick checkout -3d (1×33), -9d (2×32).
		// Not the draft, not 张三丰's, not the other tenant's.
		wantAmounts := []string{"150.00", "33.00", "64.00"}
		if len(bills) != len(wantAmounts) {
			t.Fatalf("got %d bills: %+v", len(bills), bills)
		}
		for i, b := range bills {
			if len(b.Lines) != 1 || b.Lines[0].Amount.StringFixed(2) != wantAmounts[i] || b.Lines[0].ProductName != "矿泉水" {
				t.Errorf("bill %d = %+v, want line amount %s", i, b, wantAmounts[i])
			}
		}
		if !bills[0].BillDate.After(bills[1].BillDate) || !bills[1].BillDate.After(bills[2].BillDate) {
			t.Errorf("not newest first: %+v", bills)
		}

		limited, err := repo.ListCustomerSales(ctx, tenantID, appai.CustomerRef{ID: zhang, Name: "张三"}, 1)
		if err != nil || len(limited) != 1 {
			t.Fatalf("limit 1: %d bills, err %v", len(limited), err)
		}
	})

	t.Run("MatchCustomersInText", func(t *testing.T) {
		got, err := repo.MatchCustomersInText(ctx, tenantID, "张三丰每次都要开专用发票")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("want 张三 and 张三丰 (the app layer drops the nested name), got %+v", got)
		}
		if r := appai.ResolveCustomer(ctx, repo, tenantID, "张三丰每次都要开专用发票"); r == nil || r.ID != feng {
			t.Fatalf("resolved %+v, want 张三丰", r)
		}
		none, err := repo.MatchCustomersInText(ctx, tenantID, "今天有哪些订单待发货")
		if err != nil || len(none) != 0 {
			t.Fatalf("want none, got %+v err %v", none, err)
		}
	})
}
