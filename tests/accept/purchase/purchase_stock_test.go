//go:build accept

package acceptpurchase

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	repopayment "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/payment"
	repostock "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/stock"
	repounit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/unit"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domainbill "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/lifecycle"
)

func acceptDSN() string {
	if d := os.Getenv("TALLY_ACCEPT_PURCHASE_DSN"); d != "" {
		return d
	}
	return "postgres:///tally_accept_purchase?host=/var/run/postgresql&sslmode=disable"
}

type flowLog struct {
	TenantID        string             `json:"tenant_id"`
	ProductID       string             `json:"product_id"`
	WarehouseID     string             `json:"warehouse_id"`
	Flows           []flowObservation  `json:"flows"`
	FinalOnHand     string             `json:"final_on_hand"`
	Batches         []batchObservation `json:"batches"`
	FinalPO1Payable string             `json:"final_po1_payable"`
}

type flowObservation struct {
	Step           string `json:"step"`
	BillID         string `json:"bill_id,omitempty"`
	BillNo         string `json:"bill_no,omitempty"`
	Status         int16  `json:"status"`
	OnHand         string `json:"on_hand"`
	TotalAmount    string `json:"total_amount,omitempty"`
	PaidAmount     string `json:"paid_amount,omitempty"`
	Payable        string `json:"payable,omitempty"`
	BatchRemaining string `json:"batch_remaining,omitempty"`
}

type batchObservation struct {
	SourceItemID string `json:"source_item_id"`
	QtyBase      string `json:"qty_base"`
	QtyRemaining string `json:"qty_remaining"`
}

func TestAcceptPurchaseAndStock(t *testing.T) {
	ctx := context.Background()
	dsn := acceptDSN()
	if err := lifecycle.RunMigrations(ctx, dsn, nil); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	tenantID := uuid.New()
	creatorID := uuid.New()
	warehouseID := uuid.New()
	productID := uuid.New()
	paperSupplier := uuid.New()
	steelSupplier := uuid.New()

	mustExec(t, ctx, db, `INSERT INTO tally.tenant (id, name, status) VALUES ($1, $2, 1)`,
		tenantID, "验收采购库存 "+tenantID.String()[:8])
	mustExec(t, ctx, db, `
		INSERT INTO tally.warehouse (id, tenant_id, name, enabled, is_default)
		VALUES ($1, $2, '主仓库', true, true)`, warehouseID, tenantID)
	mustExec(t, ctx, db, `
		INSERT INTO tally.partner (id, tenant_id, name, partner_type, enabled)
		VALUES ($1, $2, '华东纸业批发', 'supplier', true),
		       ($3, $2, '宝钢钢材供应', 'supplier', true)`,
		paperSupplier, tenantID, steelSupplier)
	mustExec(t, ctx, db, `
		INSERT INTO tally.product (id, tenant_id, code, name, enabled, lead_time_days)
		VALUES ($1, $2, 'PKG-UNIV-001', '通用包装纸箱', true, 3)`,
		productID, tenantID)

	billRepo := repobill.New(db)
	stockRepo := repostock.New(db)
	unitRepo := repounit.New(db)
	payRepo := repopayment.New(db)
	wac := appstock.NewCalculator(nil, stockRepo)
	recordMv := appstock.NewRecordMovementUseCase(stockRepo, wac, nil, nil)
	createUC := appbill.NewCreatePurchaseDraftUseCase(billRepo)
	approveUC := appbill.NewApprovePurchaseUseCase(billRepo, recordMv, unitRepo).
		WithPayments(payRepo).WithBatches(billRepo)
	returnUC := appbill.NewPurchaseReturnUseCase(billRepo, recordMv).
		WithUnitRepo(unitRepo).WithPayments(payRepo).WithBatches(billRepo)

	log := flowLog{
		TenantID: tenantID.String(), ProductID: productID.String(), WarehouseID: warehouseID.String(),
	}

	observe := func(step string, billID uuid.UUID, item1ID uuid.UUID) {
		obs := flowObservation{Step: step}
		if billID != uuid.Nil {
			head, err := billRepo.GetBill(ctx, tenantID, billID)
			if err != nil {
				t.Fatalf("%s GetBill: %v", step, err)
			}
			obs.BillID = billID.String()
			obs.BillNo = head.BillNo
			obs.Status = int16(head.Status)
			obs.TotalAmount = head.TotalAmount.StringFixed(2)
			obs.PaidAmount = head.PaidAmount.StringFixed(2)
			obs.Payable = head.TotalAmount.Sub(head.PaidAmount).StringFixed(2)
		}
		obs.OnHand = onHand(t, ctx, db, tenantID, productID, warehouseID)
		if item1ID != uuid.Nil {
			obs.BatchRemaining = batchRemaining(t, ctx, db, tenantID, item1ID)
		}
		log.Flows = append(log.Flows, obs)
		if step == "po1_approved" {
			log.FinalPO1Payable = obs.Payable
		}
	}

	qty100 := decimal.NewFromInt(100)
	price1250 := decimal.RequireFromString("12.50")
	draft1, err := createUC.Execute(ctx, appbill.CreatePurchaseDraftRequest{
		TenantID: tenantID, CreatorID: creatorID, PartnerID: &paperSupplier, WarehouseID: &warehouseID,
		BillDate: time.Now().UTC(),
		Items: []appbill.CreatePurchaseItemInput{{
			ProductID: productID, Qty: qty100, UnitPrice: price1250,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	observe("draft_po1", draft1.BillID, uuid.Nil)

	if err := approveUC.Execute(ctx, tenantID, draft1.BillID, creatorID); err != nil {
		t.Fatal(err)
	}
	items1, err := billRepo.GetBillItems(ctx, tenantID, draft1.BillID)
	if err != nil || len(items1) != 1 {
		t.Fatalf("po1 items: %v", err)
	}
	line1ID := items1[0].ID
	observe("po1_approved", draft1.BillID, line1ID)

	qty50 := decimal.NewFromInt(50)
	draft2, err := createUC.Execute(ctx, appbill.CreatePurchaseDraftRequest{
		TenantID: tenantID, CreatorID: creatorID, PartnerID: &steelSupplier, WarehouseID: &warehouseID,
		BillDate: time.Now().UTC(),
		Items: []appbill.CreatePurchaseItemInput{{
			ProductID: productID, Qty: qty50, UnitPrice: decimal.RequireFromString("13.00"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := approveUC.Execute(ctx, tenantID, draft2.BillID, creatorID); err != nil {
		t.Fatal(err)
	}
	observe("po2_approved", draft2.BillID, line1ID)

	retOut, err := returnUC.Execute(ctx, appbill.PurchaseReturnRequest{
		TenantID: tenantID, CreatorID: creatorID, PurchaseBillID: draft1.BillID,
		Items: []appbill.PurchaseReturnLine{{BillItemID: line1ID, Qty: decimal.NewFromInt(30)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	observe("purchase_return", retOut.BillID, line1ID)

	log.FinalOnHand = onHand(t, ctx, db, tenantID, productID, warehouseID)
	log.Batches = listBatches(t, ctx, db, tenantID, productID, warehouseID)

	if log.Flows[0].Status != int16(domainbill.StatusDraft) {
		t.Fatalf("draft status %d", log.Flows[0].Status)
	}
	if log.Flows[0].OnHand != "0" {
		t.Fatalf("draft on_hand %s", log.Flows[0].OnHand)
	}
	if log.Flows[1].OnHand != "100" || log.Flows[1].Payable != "1250.00" {
		t.Fatalf("after po1: %+v", log.Flows[1])
	}
	if log.Flows[1].BatchRemaining != "100" {
		t.Fatalf("batch after po1 %s", log.Flows[1].BatchRemaining)
	}
	if log.Flows[2].OnHand != "150" {
		t.Fatalf("after po2 on_hand %s", log.Flows[2].OnHand)
	}
	if log.FinalOnHand != "120" {
		t.Fatalf("final on_hand %s", log.FinalOnHand)
	}
	po1, _ := billRepo.GetBill(ctx, tenantID, draft1.BillID)
	if !po1.TotalAmount.Sub(po1.PaidAmount).Equal(decimal.RequireFromString("875.00")) {
		t.Fatalf("po1 payable %s", po1.TotalAmount.Sub(po1.PaidAmount))
	}

	raw, _ := json.Marshal(log)
	t.Logf("ACCEPT_PURCHASE_REPORT=%s", string(raw))
}

func mustExec(t *testing.T, ctx context.Context, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(ctx, q, args...); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

func onHand(t *testing.T, ctx context.Context, db *sql.DB, tenant, product, wh uuid.UUID) string {
	t.Helper()
	var s sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT on_hand_qty::text FROM tally.stock_snapshot
		WHERE tenant_id = $1 AND product_id = $2 AND warehouse_id = $3`,
		tenant, product, wh).Scan(&s)
	if err == sql.ErrNoRows {
		return "0"
	}
	if err != nil {
		t.Fatal(err)
	}
	if !s.Valid {
		return "0"
	}
	return decimal.RequireFromString(s.String).StringFixed(0)
}

func batchRemaining(t *testing.T, ctx context.Context, db *sql.DB, tenant, sourceItem uuid.UUID) string {
	t.Helper()
	var rem string
	err := db.QueryRowContext(ctx, `
		SELECT qty_remaining::text FROM tally.receipt_batch
		WHERE tenant_id = $1 AND source_item_id = $2`, tenant, sourceItem).Scan(&rem)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return decimal.RequireFromString(rem).StringFixed(0)
}

func listBatches(t *testing.T, ctx context.Context, db *sql.DB, tenant, product, wh uuid.UUID) []batchObservation {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
		SELECT source_item_id, qty_base::text, qty_remaining::text
		FROM tally.receipt_batch
		WHERE tenant_id = $1 AND product_id = $2 AND warehouse_id = $3
		ORDER BY received_at`, tenant, product, wh)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []batchObservation
	for rows.Next() {
		var b batchObservation
		if err := rows.Scan(&b.SourceItemID, &b.QtyBase, &b.QtyRemaining); err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}