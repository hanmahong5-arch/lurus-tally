//go:build accept

package accept

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	repostock "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/stock"
	repounit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/unit"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domainbill "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/lifecycle"
)

var reportPath string

func TestMain(m *testing.M) {
	flag.StringVar(&reportPath, "accept-report", "", "write JSON flow results to this path")
	flag.Parse()
	os.Exit(m.Run())
}

type salesFlowResult struct {
	Name    string            `json:"name"`
	OK      bool              `json:"ok"`
	Details map[string]string `json:"details,omitempty"`
	Err     string            `json:"err,omitempty"`
}

type salesAcceptReport struct {
	Verdict string            `json:"verdict"`
	Tenant  string            `json:"tenant_id"`
	Flows   []salesFlowResult `json:"flows"`
}

func defaultDSN() string {
	if v := os.Getenv("TALLY_ACCEPT_SALES_DSN"); v != "" {
		return v
	}
	return "postgres:///tally_accept_sales?host=/var/run/postgresql&sslmode=disable"
}

func TestSalesAcceptance(t *testing.T) {
	ctx := context.Background()
	dsn := defaultDSN()
	if err := lifecycle.RunMigrations(ctx, dsn, nil); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	stockRepo := repostock.New(db)
	billRepo := repobill.New(db)
	unitRepo := repounit.New(db)
	wacCalc := appstock.NewCalculator(nil, stockRepo)
	recordMv := appstock.NewRecordMovementUseCase(stockRepo, wacCalc, nil, nil)

	approvePurchase := appbill.NewApprovePurchaseUseCase(billRepo, recordMv, unitRepo).WithBatches(billRepo)
	createPurchase := appbill.NewCreatePurchaseDraftUseCase(billRepo)
	createSale := appbill.NewCreateSaleUseCase(billRepo)
	approveSale := appbill.NewApproveSaleUseCase(billRepo, recordMv, unitRepo, nil).WithBatches(billRepo)
	returnSale := appbill.NewReturnSaleUseCase(
		billRepo,
		appbill.NewCreateReturnBillUseCase(billRepo).WithUnitRepo(unitRepo),
		appbill.NewApproveReturnBillUseCase(billRepo, recordMv).WithUnitRepo(unitRepo).WithBatches(billRepo),
	).WithUnitRepo(unitRepo)

	env := seedTenant(t, ctx, db)
	report := salesAcceptReport{Tenant: env.tenant.String(), Flows: nil}

	report.Flows = append(report.Flows, flowDraftThenApprove(t, ctx, db, env, createPurchase, approvePurchase, createSale, approveSale, billRepo))
	report.Flows = append(report.Flows, flowSaleReturn(t, ctx, db, env, createPurchase, approvePurchase, createSale, approveSale, returnSale, billRepo))
	report.Flows = append(report.Flows, flowBatchOldestFirst(t, ctx, db, env, createPurchase, approvePurchase, createSale, approveSale, billRepo))
	report.Flows = append(report.Flows, flowSaleFromPurchase(t, ctx, db, env, createPurchase, approvePurchase, createSale, approveSale, billRepo))

	report.Verdict = salesVerdict(report.Flows)
	if reportPath != "" {
		b, _ := json.MarshalIndent(report, "", "  ")
		if werr := os.WriteFile(reportPath, b, 0o644); werr != nil {
			t.Logf("write report: %v", werr)
		}
	}
	t.Logf("VERDICT=%s tenant=%s", report.Verdict, report.Tenant)
	for _, f := range report.Flows {
		if !f.OK {
			t.Errorf("flow %s: %s", f.Name, f.Err)
		}
	}
	if report.Verdict != "PASS" {
		t.Fatalf("acceptance %s", report.Verdict)
	}
}

func salesVerdict(flows []salesFlowResult) string {
	anyFail := false
	for _, f := range flows {
		if !f.OK {
			anyFail = true
		}
	}
	if anyFail {
		return "ISSUES"
	}
	return "PASS"
}

type tenantEnv struct {
	tenant     uuid.UUID
	warehouse  uuid.UUID
	customer   uuid.UUID
	supplier   uuid.UUID
	productA   uuid.UUID
	productB   uuid.UUID
	productC   uuid.UUID
	productD   uuid.UUID
	creator    uuid.UUID
	customerZN string
}

func seedTenant(t *testing.T, ctx context.Context, db *sql.DB) tenantEnv {
	t.Helper()
	e := tenantEnv{
		tenant:     uuid.New(),
		warehouse:  uuid.New(),
		customer:   uuid.New(),
		supplier:   uuid.New(),
		productA:   uuid.New(),
		productB:   uuid.New(),
		productC:   uuid.New(),
		productD:   uuid.New(),
		creator:    uuid.New(),
		customerZN: "杭州锦绣商贸有限公司",
	}
	salesExec(t, ctx, db, `INSERT INTO tally.tenant (id, name, status) VALUES ($1, $2, 1)`,
		e.tenant, "验收销售租户")
	salesExec(t, ctx, db, `
		INSERT INTO tally.warehouse (id, tenant_id, name, enabled, is_default)
		VALUES ($1, $2, '主仓', true, true)`, e.warehouse, e.tenant)
	salesExec(t, ctx, db, `
		INSERT INTO tally.partner (id, tenant_id, name, partner_type, enabled)
		VALUES ($1, $2, $3, 'customer', true)`, e.customer, e.tenant, e.customerZN)
	salesExec(t, ctx, db, `
		INSERT INTO tally.partner (id, tenant_id, name, partner_type, enabled)
		VALUES ($1, $2, '苏州瑞丰供应站', 'supplier', true)`, e.supplier, e.tenant)
	for _, p := range []struct {
		id   uuid.UUID
		code string
		name string
	}{
		{e.productA, "ACCEPT-A", "验收商品甲"},
		{e.productB, "ACCEPT-B", "验收商品乙"},
		{e.productC, "ACCEPT-C", "验收商品丙"},
		{e.productD, "ACCEPT-D", "验收商品丁"},
	} {
		salesExec(t, ctx, db, `
			INSERT INTO tally.product (id, tenant_id, code, name, enabled, lead_time_days)
			VALUES ($1, $2, $3, $4, true, 7)`, p.id, e.tenant, p.code, p.name)
	}
	salesExec(t, ctx, db, `
		INSERT INTO tally.tenant_profile (tenant_id, inventory_method, profile_type)
		VALUES ($1, 'wac', 'retail')
		ON CONFLICT (tenant_id) DO UPDATE SET inventory_method = EXCLUDED.inventory_method`,
		e.tenant)
	return e
}

func flowDraftThenApprove(t *testing.T, ctx context.Context, db *sql.DB, e tenantEnv,
	createPurchase *appbill.CreatePurchaseDraftUseCase, approvePurchase *appbill.ApprovePurchaseUseCase,
	createSale *appbill.CreateSaleUseCase, approveSale *appbill.ApproveSaleUseCase, billRepo *repobill.Repo,
) salesFlowResult {
	const name = "sale_draft_and_approve"
	fr := salesFlowResult{Name: name, Details: map[string]string{}}
	_, _ = createAndApprovePurchase(t, ctx, db, e, createPurchase, approvePurchase, e.productA, 20, "11.00", time.Now().UTC())
	onHandBefore := snapshotQty(t, ctx, db, e.tenant, e.productA, e.warehouse)
	draftBaseline := onHandBefore
	out, err := createSale.Execute(ctx, appbill.CreateSaleRequest{
		TenantID:  e.tenant,
		CreatorID: e.creator,
		PartnerID: &e.customer,
		Items: []appbill.SaleItem{{
			ProductID: e.productA, WarehouseID: e.warehouse, LineNo: 1,
			Qty: decimal.NewFromInt(3), UnitPrice: decimal.NewFromInt(50),
		}},
	})
	if err != nil {
		fr.Err = err.Error()
		return fr
	}
	st := billStatus(t, ctx, db, e.tenant, out.BillID)
	fr.Details["draft_bill_no"] = out.BillNo
	fr.Details["draft_status"] = fmt.Sprint(st)
	if st != int16(domainbill.StatusDraft) {
		fr.Err = fmt.Sprintf("want draft status 0, got %d", st)
		return fr
	}
	if !draftBaseline.Equal(snapshotQty(t, ctx, db, e.tenant, e.productA, e.warehouse)) {
		fr.Err = "draft sale changed on-hand"
		return fr
	}
	if err := approveSale.Execute(ctx, appbill.ApproveSaleRequest{
		TenantID: e.tenant, BillID: out.BillID, CreatorID: e.creator,
	}); err != nil {
		fr.Err = "approve: " + err.Error()
		return fr
	}
	st = billStatus(t, ctx, db, e.tenant, out.BillID)
	fr.Details["approved_status"] = fmt.Sprint(st)
	onHandAfter := snapshotQty(t, ctx, db, e.tenant, e.productA, e.warehouse)
	fr.Details["on_hand_before"] = onHandBefore.String()
	fr.Details["on_hand_after"] = onHandAfter.String()
	wantOnHand := onHandBefore.Sub(decimal.NewFromInt(3))
	if st != int16(domainbill.StatusApproved) {
		fr.Err = fmt.Sprintf("want approved 2, got %d", st)
		return fr
	}
	if !onHandAfter.Equal(wantOnHand) {
		fr.Err = fmt.Sprintf("on_hand %s want %s", onHandAfter, wantOnHand)
		return fr
	}
	fr.OK = true
	return fr
}

func flowSaleReturn(t *testing.T, ctx context.Context, db *sql.DB, e tenantEnv,
	createPurchase *appbill.CreatePurchaseDraftUseCase, approvePurchase *appbill.ApprovePurchaseUseCase,
	createSale *appbill.CreateSaleUseCase, approveSale *appbill.ApproveSaleUseCase,
	returnSale *appbill.ReturnSaleUseCase, billRepo *repobill.Repo,
) salesFlowResult {
	const name = "sale_return_restores_stock"
	fr := salesFlowResult{Name: name, Details: map[string]string{"customer": e.customerZN}}
	onHand0 := snapshotQty(t, ctx, db, e.tenant, e.productB, e.warehouse)
	purchaseID, _ := createAndApprovePurchase(t, ctx, db, e, createPurchase, approvePurchase, e.productB, 8, "18.00", time.Now().UTC().Add(-48*time.Hour))
	_ = purchaseID
	onHand1 := snapshotQty(t, ctx, db, e.tenant, e.productB, e.warehouse)
	fr.Details["on_hand_after_purchase"] = onHand1.String()

	saleOut, err := createSale.Execute(ctx, appbill.CreateSaleRequest{
		TenantID: e.tenant, CreatorID: e.creator, PartnerID: &e.customer,
		Items: []appbill.SaleItem{{
			ProductID: e.productB, WarehouseID: e.warehouse, LineNo: 1,
			Qty: decimal.NewFromInt(5), UnitPrice: decimal.NewFromInt(42),
		}},
	})
	if err != nil {
		fr.Err = err.Error()
		return fr
	}
	if err := approveSale.Execute(ctx, appbill.ApproveSaleRequest{
		TenantID: e.tenant, BillID: saleOut.BillID, CreatorID: e.creator,
	}); err != nil {
		fr.Err = "approve sale: " + err.Error()
		return fr
	}
	onHand2 := snapshotQty(t, ctx, db, e.tenant, e.productB, e.warehouse)
	fr.Details["sale_status"] = fmt.Sprint(billStatus(t, ctx, db, e.tenant, saleOut.BillID))
	fr.Details["on_hand_after_sale"] = onHand2.String()

	items, _ := billRepo.GetBillItems(ctx, e.tenant, saleOut.BillID)
	retOut, err := returnSale.Execute(ctx, appbill.ReturnSaleRequest{
		TenantID: e.tenant, CreatorID: e.creator, SaleBillID: saleOut.BillID,
		Items: []appbill.ReturnSaleLine{{BillItemID: items[0].ID, Qty: decimal.NewFromInt(2)}},
	})
	if err != nil {
		fr.Err = "return: " + err.Error()
		return fr
	}
	onHand3 := snapshotQty(t, ctx, db, e.tenant, e.productB, e.warehouse)
	fr.Details["return_bill_no"] = retOut.BillNo
	fr.Details["return_status"] = fmt.Sprint(billStatus(t, ctx, db, e.tenant, retOut.BillID))
	fr.Details["on_hand_after_return"] = onHand3.String()
	want := onHand2.Add(decimal.NewFromInt(2))
	if !onHand3.Equal(want) {
		fr.Err = fmt.Sprintf("return on_hand %s want %s", onHand3, want)
		return fr
	}
	if onHand0.IsZero() && onHand1.Equal(decimal.NewFromInt(8)) {
		fr.Details["purchase_stock_in"] = "8"
	}
	fr.OK = true
	return fr
}

func flowBatchOldestFirst(t *testing.T, ctx context.Context, db *sql.DB, e tenantEnv,
	createPurchase *appbill.CreatePurchaseDraftUseCase,
	approvePurchase *appbill.ApprovePurchaseUseCase,
	createSale *appbill.CreateSaleUseCase, approveSale *appbill.ApproveSaleUseCase,
	billRepo *repobill.Repo,
) salesFlowResult {
	const name = "batch_draw_oldest_receipt_first"
	fr := salesFlowResult{Name: name, Details: map[string]string{}}

	oldPurchase, oldItem := createAndApprovePurchase(t, ctx, db, e, createPurchase, approvePurchase, e.productC, 10, "12.00", time.Now().UTC())
	time.Sleep(2 * time.Second)
	newPurchase, newItem := createAndApprovePurchase(t, ctx, db, e, createPurchase, approvePurchase, e.productC, 10, "14.00", time.Now().UTC())
	oldRem := batchRemainingByItem(t, ctx, db, e.tenant, oldItem)
	newRem := batchRemainingByItem(t, ctx, db, e.tenant, newItem)
	fr.Details["batch_old_remaining_before"] = oldRem.String()
	fr.Details["batch_new_remaining_before"] = newRem.String()

	saleOut, err := createSale.Execute(ctx, appbill.CreateSaleRequest{
		TenantID: e.tenant, CreatorID: e.creator, PartnerID: &e.customer,
		Items: []appbill.SaleItem{{
			ProductID: e.productC, WarehouseID: e.warehouse, LineNo: 1,
			Qty: decimal.NewFromInt(15), UnitPrice: decimal.NewFromInt(30),
		}},
	})
	if err != nil {
		fr.Err = err.Error()
		return fr
	}
	if err := approveSale.Execute(ctx, appbill.ApproveSaleRequest{
		TenantID: e.tenant, BillID: saleOut.BillID, CreatorID: e.creator,
	}); err != nil {
		fr.Err = "approve: " + err.Error()
		return fr
	}
	oldAfter := batchRemainingByItem(t, ctx, db, e.tenant, oldItem)
	newAfter := batchRemainingByItem(t, ctx, db, e.tenant, newItem)
	fr.Details["batch_old_remaining_after"] = oldAfter.String()
	fr.Details["batch_new_remaining_after"] = newAfter.String()
	fr.Details["sale_status"] = fmt.Sprint(billStatus(t, ctx, db, e.tenant, saleOut.BillID))
	fr.Details["on_hand"] = snapshotQty(t, ctx, db, e.tenant, e.productC, e.warehouse).String()
	if !oldAfter.IsZero() {
		fr.Err = fmt.Sprintf("older batch should be empty, got %s", oldAfter)
		return fr
	}
	if !newAfter.Equal(decimal.NewFromInt(5)) {
		fr.Err = fmt.Sprintf("newer batch remaining %s want 5", newAfter)
		return fr
	}
	_ = oldPurchase
	_ = newPurchase
	fr.OK = true
	return fr
}

func createAndApprovePurchase(t *testing.T, ctx context.Context, db *sql.DB, e tenantEnv,
	createPurchase *appbill.CreatePurchaseDraftUseCase, approvePurchase *appbill.ApprovePurchaseUseCase,
	product uuid.UUID, qty int64, unitPrice string, billDate time.Time,
) (uuid.UUID, uuid.UUID) {
	t.Helper()
	out, err := createPurchase.Execute(ctx, appbill.CreatePurchaseDraftRequest{
		TenantID: e.tenant, CreatorID: e.creator, PartnerID: &e.supplier,
		WarehouseID: &e.warehouse, BillDate: billDate,
		Items: []appbill.CreatePurchaseItemInput{{
			ProductID: product, LineNo: 1, Qty: decimal.NewFromInt(qty),
			UnitPrice: decimal.RequireFromString(unitPrice),
		}},
	})
	if err != nil {
		t.Fatalf("create purchase: %v", err)
	}
	if err := approvePurchase.Execute(ctx, e.tenant, out.BillID, e.creator); err != nil {
		t.Fatalf("approve purchase: %v", err)
	}
	var itemID uuid.UUID
	if err := db.QueryRowContext(ctx, `SELECT id FROM tally.bill_item WHERE head_id = $1 AND tenant_id = $2 LIMIT 1`,
		out.BillID, e.tenant).Scan(&itemID); err != nil {
		t.Fatalf("purchase item id: %v", err)
	}
	return out.BillID, itemID
}

func salesExec(t *testing.T, ctx context.Context, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(ctx, q, args...); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

func billStatus(t *testing.T, ctx context.Context, db *sql.DB, tenant, bill uuid.UUID) int16 {
	t.Helper()
	var st int16
	if err := db.QueryRowContext(ctx, `SELECT status FROM tally.bill_head WHERE id = $1 AND tenant_id = $2`, bill, tenant).Scan(&st); err != nil {
		t.Fatalf("status: %v", err)
	}
	return st
}

func snapshotQty(t *testing.T, ctx context.Context, db *sql.DB, tenant, product, wh uuid.UUID) decimal.Decimal {
	t.Helper()
	var s string
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(on_hand_qty::text, '0') FROM tally.stock_snapshot
		WHERE tenant_id = $1 AND product_id = $2 AND warehouse_id = $3`, tenant, product, wh).Scan(&s)
	if err == sql.ErrNoRows {
		return decimal.Zero
	}
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return decimal.RequireFromString(s)
}

func batchRemainingByItem(t *testing.T, ctx context.Context, db *sql.DB, tenant, purchaseItem uuid.UUID) decimal.Decimal {
	t.Helper()
	var s string
	err := db.QueryRowContext(ctx, `
		SELECT qty_remaining::text FROM tally.receipt_batch
		WHERE tenant_id = $1 AND source_item_id = $2`, tenant, purchaseItem).Scan(&s)
	if err == sql.ErrNoRows {
		return decimal.Zero
	}
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	return decimal.RequireFromString(s)
}

func pinnedDrawQty(t *testing.T, ctx context.Context, db *sql.DB, tenant, saleItem, purchaseItem uuid.UUID) (decimal.Decimal, error) {
	t.Helper()
	var sum string
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(d.qty_base)::text, '0')
		FROM tally.batch_draw d
		JOIN tally.receipt_batch b ON b.id = d.batch_id AND b.tenant_id = d.tenant_id
		WHERE d.tenant_id = $1 AND d.sale_item_id = $2 AND b.source_item_id = $3`,
		tenant, saleItem, purchaseItem).Scan(&sum)
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.RequireFromString(sum), nil
}

func batchRemainingExcept(t *testing.T, ctx context.Context, db *sql.DB, tenant, product, skipItem uuid.UUID) string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
		SELECT source_item_id::text, qty_remaining::text FROM tally.receipt_batch
		WHERE tenant_id = $1 AND product_id = $2 AND source_item_id <> $3 AND qty_remaining > 0`,
		tenant, product, skipItem)
	if err != nil {
		t.Fatalf("batch except: %v", err)
	}
	defer rows.Close()
	var parts []string
	for rows.Next() {
		var src, rem string
		_ = rows.Scan(&src, &rem)
		parts = append(parts, src+":"+rem)
	}
	return strings.Join(parts, ";")
}