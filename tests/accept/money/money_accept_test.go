//go:build accept

package moneyaccept

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	invoicerepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/invoice"
	repopayment "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/payment"
	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	appinv "github.com/hanmahong5-arch/lurus-tally/internal/app/invoice"
	domaininv "github.com/hanmahong5-arch/lurus-tally/internal/domain/invoice"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
	"github.com/hanmahong5-arch/lurus-tally/internal/lifecycle"
)

const acceptDSNEnv = "TALLY_ACCEPT_MONEY_DSN"

func defaultAcceptDSN() string {
	if d := os.Getenv(acceptDSNEnv); d != "" {
		return d
	}
	return "host=/var/run/postgresql user=postgres dbname=tally_accept_money sslmode=disable"
}

func openAcceptDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", defaultAcceptDSN())
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		t.Fatalf("ping: %v (set %s if needed)", err, acceptDSNEnv)
	}
	return db
}

type moneyFlow struct {
	Flow   string `json:"flow"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type moneyReport struct {
	TenantID uuid.UUID   `json:"tenant_id"`
	Results  []moneyFlow `json:"results"`
}

func TestAcceptMoney_Flows(t *testing.T) {
	ctx := context.Background()
	db := openAcceptDB(t)
	defer func() { _ = db.Close() }()

	if err := lifecycle.RunMigrations(ctx, defaultAcceptDSN(), nil); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	tenant := uuid.MustParse("a1b2c3d4-e5f6-4789-a012-3456789abcde")
	creator := uuid.MustParse("b2c3d4e5-f6a7-4890-b123-456789abcdef")
	report := moneyReport{TenantID: tenant}

	ensureTenant(t, db, ctx, tenant)

	payRepo := repopayment.New(db)
	billRepo := repobill.New(db)
	moneyUC := apppayment.NewMoneyUseCase(payRepo, billRepo, apppayment.NewRecordPaymentUseCase(billRepo, payRepo))
	recordUC := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
	stmtUC := apppayment.NewStatementUseCase(billRepo)
	invRepo := invoicerepo.New(db)

	wholesale := readFixture(t, "wholesale-alipay.csv")
	channel, inserted, skipped, err := moneyUC.Import(ctx, tenant, "", wholesale)
	importOK := err == nil && channel == domainpayment.ChannelAlipay &&
		(inserted == 2 && skipped == 0 || inserted == 0 && skipped == 2)
	if !importOK {
		report.Results = append(report.Results, moneyFlow{
			Flow: "bank_import_wholesale", Status: "ISSUES",
			Detail: fmt.Sprintf("import err=%v channel=%s inserted=%d skipped=%d", err, channel, inserted, skipped),
		})
	} else {
		report.Results = append(report.Results, moneyFlow{
			Flow: "bank_import_wholesale", Status: "PASS",
			Detail: fmt.Sprintf("channel=%s inserted=%d skipped=%d", channel, inserted, skipped),
		})
	}

	customer := findOrInsertPartner(t, db, ctx, tenant, "A客户", "customer")
	supplier := findOrInsertPartner(t, db, ctx, tenant, "龙口老李", "supplier")
	saleID := findOrInsertBill(t, db, tenant, "ACCEPT-SALE-2560", billSpec{
		billType: "出库", subType: "销售", status: 2, partner: &customer,
		total: "2560.00", paid: "0",
	})
	buyID := findOrInsertBill(t, db, tenant, "ACCEPT-BUY-1280", billSpec{
		billType: "入库", subType: "采购", status: 2, partner: &supplier,
		total: "1280.00", paid: "0",
	})

	incomeLine := lineByExt(t, db, tenant, "W2")
	expenseLine := lineByExt(t, db, tenant, "W1")
	if err := moneyUC.Confirm(ctx, tenant, creator, incomeLine, saleID); err != nil {
		report.Results = append(report.Results, moneyFlow{Flow: "match_income_sale", Status: "ISSUES", Detail: err.Error()})
	} else if err := moneyUC.Confirm(ctx, tenant, creator, expenseLine, buyID); err != nil {
		report.Results = append(report.Results, moneyFlow{Flow: "match_expense_purchase", Status: "ISSUES", Detail: err.Error()})
	} else {
		inPay := paymentForBill(t, db, saleID)
		outPay := paymentForBill(t, db, buyID)
		inStat, _ := lineStatus(t, db, incomeLine)
		outStat, _ := lineStatus(t, db, expenseLine)
		report.Results = append(report.Results,
			moneyFlow{
				Flow: "match_income_sale", Status: "PASS",
				Detail: fmt.Sprintf("line=%s payment_id=%s amount=%s pay_type=%s line_status=%s bill_paid=%s",
					incomeLine, inPay.id, inPay.amount, inPay.payType, inStat, paidOnBill(t, db, saleID)),
			},
			moneyFlow{
				Flow: "match_expense_purchase", Status: "PASS",
				Detail: fmt.Sprintf("line=%s payment_id=%s amount=%s pay_type=%s line_status=%s bill_paid=%s",
					expenseLine, outPay.id, outPay.amount, outPay.payType, outStat, paidOnBill(t, db, buyID)),
			},
		)
	}

	burst := readFixture(t, "burst-wholesale-alipay.csv")
	_, bIns, bSkip, bErr := moneyUC.Import(ctx, tenant, "burst-accept-account", burst)
	var burstCount int
	var burstSum string
	_ = db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(amount)::text,'0') FROM tally.money_line
		WHERE tenant_id = $1 AND account_ref = $2`, tenant, "burst-accept-account").Scan(&burstCount, &burstSum)
	burstOK := bErr == nil && burstCount == 2000 && sameMoney(burstSum, "6500.00") &&
		(bIns == 2000 && bSkip == 0 || bIns == 0 && bSkip == 2000)
	if !burstOK {
		report.Results = append(report.Results, moneyFlow{
			Flow: "bank_import_burst_2000", Status: "ISSUES",
			Detail: fmt.Sprintf("err=%v inserted=%d skipped=%d stored=%d sum=%s", bErr, bIns, bSkip, burstCount, burstSum),
		})
	} else {
		report.Results = append(report.Results, moneyFlow{
			Flow: "bank_import_burst_2000", Status: "PASS",
			Detail: fmt.Sprintf("inserted=%d skipped=%d stored=%d sum=%s", bIns, bSkip, burstCount, burstSum),
		})
	}

	cashBill := findOrInsertBill(t, db, tenant, "ACCEPT-CASH-500", billSpec{
		billType: "出库", subType: "销售", status: 2, partner: &customer,
		total: "500.00", paid: "0",
	})
	cashPaid := paidOnBill(t, db, cashBill)
	if !sameMoney(cashPaid, "500") {
		if err := recordUC.Execute(ctx, apppayment.RecordPaymentRequest{
			TenantID: tenant, BillID: cashBill, CreatorID: creator,
			Amount: decimal.RequireFromString("500"), PayType: "cash", Remark: "accept cash",
		}); err != nil {
			report.Results = append(report.Results, moneyFlow{Flow: "cash_payment", Status: "ISSUES", Detail: err.Error()})
		}
		cashPaid = paidOnBill(t, db, cashBill)
	}
	if sameMoney(cashPaid, "500") {
		cp := paymentForBill(t, db, cashBill)
		report.Results = append(report.Results, moneyFlow{
			Flow: "cash_payment", Status: "PASS",
			Detail: fmt.Sprintf("payment_id=%s amount=%s pay_type=%s bill_paid=%s", cp.id, cp.amount, cp.payType, cashPaid),
		})
	} else if cashPaid != "500" {
		report.Results = append(report.Results, moneyFlow{Flow: "cash_payment", Status: "ISSUES", Detail: "bill not fully paid"})
	}

	if err := registerInvoice(t, ctx, invRepo, tenant, customer, saleID); err != nil {
		report.Results = append(report.Results, moneyFlow{Flow: "invoice_create", Status: "ISSUES", Detail: err.Error()})
	} else {
		items, total, err := invRepo.List(ctx, domaininv.ListFilter{TenantID: tenant, Direction: domaininv.DirectionOut, Keyword: "ACCEPT-INV", Limit: 5})
		if err != nil || total < 1 {
			report.Results = append(report.Results, moneyFlow{Flow: "invoice_create", Status: "ISSUES", Detail: fmt.Sprintf("list: %v total=%d", err, total)})
		} else {
			got := items[0]
			report.Results = append(report.Results, moneyFlow{
				Flow: "invoice_create", Status: "PASS",
				Detail: fmt.Sprintf("invoice_id=%s no=%s total=%s status=%s", got.ID, got.InvoiceNo, got.TotalAmount, got.Status),
			})
		}
	}

	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, -1)
	if _, err := db.ExecContext(ctx, `
		UPDATE tally.bill_head SET bill_date = $2
		WHERE tenant_id = $1 AND bill_no IN ('ACCEPT-SALE-2560','ACCEPT-CASH-500')`,
		tenant, from.Add(96*time.Hour)); err != nil {
		report.Results = append(report.Results, moneyFlow{Flow: "partner_statement_ar", Status: "ISSUES", Detail: err.Error()})
	} else {
		stmt, err := stmtUC.Execute(ctx, apppayment.StatementQuery{
			TenantID: tenant, Direction: apppayment.DirectionReceivable, PartnerID: customer, From: from, To: to,
		})
		if err != nil {
			report.Results = append(report.Results, moneyFlow{Flow: "partner_statement_ar", Status: "ISSUES", Detail: err.Error()})
		} else if len(stmt.Lines) < 2 {
			report.Results = append(report.Results, moneyFlow{
				Flow: "partner_statement_ar", Status: "ISSUES",
				Detail: fmt.Sprintf("expected statement lines, got %d closing=%s", len(stmt.Lines), stmt.Closing),
			})
		} else {
			report.Results = append(report.Results, moneyFlow{
				Flow: "partner_statement_ar", Status: "PASS",
				Detail: fmt.Sprintf("partner=%s opening=%s billed=%s paid=%s closing=%s lines=%d",
					stmt.PartnerName, stmt.Opening, stmt.Billed, stmt.Paid, stmt.Closing, len(stmt.Lines)),
			})
		}
	}

	b, _ := json.Marshal(report)
	t.Logf("ACCEPT_MONEY_REPORT %s", string(b))
	for _, r := range report.Results {
		if r.Status != "PASS" {
			t.Errorf("%s: %s — %s", r.Flow, r.Status, r.Detail)
		}
	}
}

func registerInvoice(t *testing.T, ctx context.Context, repo *invoicerepo.Repo, tenant, customer, bill uuid.UUID) error {
	t.Helper()
	_, total, err := repo.List(ctx, domaininv.ListFilter{TenantID: tenant, Direction: domaininv.DirectionOut, Keyword: "ACCEPT-INV-1", Limit: 1})
	if err == nil && total > 0 {
		return nil
	}
	inv, err := domaininv.NewInvoice(domaininv.CreateInput{
		TenantID: tenant, Direction: domaininv.DirectionOut, InvoiceNo: "ACCEPT-INV-1", InvoiceType: "数电普票",
		PartnerID: customer, IssueDate: time.Date(2026, 4, 4, 0, 0, 0, 0, time.UTC),
		AmountExclTax: decimal.RequireFromString("2265.49"), TaxAmount: decimal.RequireFromString("294.51"),
		Links: []domaininv.Link{{BillID: bill, Amount: decimal.RequireFromString("2560")}},
	}, time.Now().UTC())
	if err != nil {
		return err
	}
	err = repo.WithTx(ctx, func(tx *sql.Tx) error { return repo.Insert(ctx, tx, inv) })
	if errors.Is(err, appinv.ErrDuplicateNo) {
		return nil
	}
	return err
}

func ensureTenant(t *testing.T, db *sql.DB, ctx context.Context, id uuid.UUID) {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tally.tenant WHERE id = $1`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n > 0 {
		return
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO tally.tenant (id, name, status) VALUES ($1, $2, 1)`, id, "Accept Money Tenant"); err != nil {
		t.Fatal(err)
	}
}

type billSpec struct {
	billType, subType string
	status            int
	partner           *uuid.UUID
	total, paid       string
}

func findOrInsertPartner(t *testing.T, db *sql.DB, ctx context.Context, tenant uuid.UUID, name, partnerType string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRowContext(ctx, `
		SELECT id FROM tally.partner WHERE tenant_id = $1 AND name = $2 LIMIT 1`, tenant, name).Scan(&id)
	if err == nil {
		return id
	}
	id = uuid.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO tally.partner (id, tenant_id, name, partner_type, enabled)
		VALUES ($1, $2, $3, $4, true)`, id, tenant, name, partnerType); err != nil {
		t.Fatal(err)
	}
	return id
}

func findOrInsertBill(t *testing.T, db *sql.DB, tenant uuid.UUID, billNo string, b billSpec) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRowContext(context.Background(), `
		SELECT id FROM tally.bill_head WHERE tenant_id = $1 AND bill_no = $2`, tenant, billNo).Scan(&id)
	if err == nil {
		return id
	}
	id = uuid.New()
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO tally.bill_head
		    (id, tenant_id, bill_no, bill_type, sub_type, status, partner_id, creator_id, bill_date,
		     total_amount, paid_amount, remark)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'')`,
		id, tenant, billNo, b.billType, b.subType, b.status, b.partner, uuid.New(), time.Now(),
		b.total, b.paid)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	root := filepath.Join("..", "..", "..", "internal", "domain", "payment", "testdata", "industries", name)
	raw, err := os.ReadFile(root)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func lineByExt(t *testing.T, db *sql.DB, tenant uuid.UUID, ext string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRowContext(context.Background(), `
		SELECT id FROM tally.money_line WHERE tenant_id = $1 AND external_id = $2`, tenant, ext).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func lineStatus(t *testing.T, db *sql.DB, id uuid.UUID) (string, error) {
	t.Helper()
	var s string
	err := db.QueryRowContext(context.Background(), `SELECT status FROM tally.money_line WHERE id = $1`, id).Scan(&s)
	return s, err
}

type paySnap struct {
	id, amount, payType string
}

func paymentForBill(t *testing.T, db *sql.DB, bill uuid.UUID) paySnap {
	t.Helper()
	var id uuid.UUID
	var amount, payType string
	err := db.QueryRowContext(context.Background(), `
		SELECT id, amount::text, pay_type FROM tally.payment_head
		WHERE related_bill_id = $1 AND deleted_at IS NULL ORDER BY created_at LIMIT 1`, bill).
		Scan(&id, &amount, &payType)
	if err != nil {
		t.Fatal(err)
	}
	return paySnap{id.String(), amount, payType}
}

func paidOnBill(t *testing.T, db *sql.DB, bill uuid.UUID) string {
	t.Helper()
	var paid string
	if err := db.QueryRowContext(context.Background(), `SELECT paid_amount::text FROM tally.bill_head WHERE id = $1`, bill).Scan(&paid); err != nil {
		t.Fatal(err)
	}
	return paid
}

func sameMoney(got, want string) bool {
	g, errG := decimal.NewFromString(got)
	w, errW := decimal.NewFromString(want)
	return errG == nil && errW == nil && g.Equal(w)
}