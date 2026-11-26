//go:build integration

package integration

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	repopayment "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/payment"
	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

func TestMoney_PostgresImportAndConfirmBurst(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()
	db.SetMaxOpenConns(32)
	ctx := context.Background()
	tenant := insertTenant(t, db, ctx)
	creator := uuid.New()
	payRepo := repopayment.New(db)
	billRepo := repobill.New(db)
	uc := apppayment.NewMoneyUseCase(payRepo, billRepo, apppayment.NewRecordPaymentUseCase(billRepo, payRepo))

	wholesale := readMoneyCSV(t, "wholesale-alipay.csv")
	channel, account, lines, err := domainpayment.ParseMoneyFile(wholesale)
	if err != nil {
		t.Fatal(err)
	}
	inserted, skipped, err := payRepo.InsertNew(ctx, tenant, channel, account, lines)
	if err != nil || inserted != 2 || skipped != 0 {
		t.Fatalf("wholesale insert inserted=%d skipped=%d err=%v", inserted, skipped, err)
	}
	saleID := insertRecvBill(t, db, tenant, recvBill{
		billType: "出库", subType: "销售", status: 2, date: time.Now(), total: "2560.00", paid: "0",
	})
	buyID := insertRecvBill(t, db, tenant, recvBill{
		billType: "入库", subType: "采购", status: 2, date: time.Now(), total: "1280.00", paid: "0",
	})
	oneConfirm := time.Duration(0)
	for _, spec := range []struct {
		ext  string
		bill uuid.UUID
	}{{"W2", saleID}, {"W1", buyID}} {
		lineID := moneyLineID(t, db, tenant, spec.ext)
		start := time.Now()
		if err := uc.Confirm(ctx, tenant, creator, lineID, spec.bill); err != nil {
			t.Fatal(err)
		}
		oneConfirm += time.Since(start)
	}
	if got, paid := paymentCount(t, db, saleID), paidText(t, db, saleID); got != 1 || !sameMoney(paid, "2560.00") {
		t.Fatalf("sale payments=%d paid=%s", got, paid)
	}
	if got, paid := paymentCount(t, db, buyID), paidText(t, db, buyID); got != 1 || !sameMoney(paid, "1280.00") {
		t.Fatalf("purchase payments=%d paid=%s", got, paid)
	}

	burstRaw := readMoneyCSV(t, "burst-wholesale-alipay.csv")
	bChannel, bAccount, burstLines, err := domainpayment.ParseMoneyFile(burstRaw)
	if err != nil || len(burstLines) != 2000 {
		t.Fatalf("burst parse lines=%d err=%v", len(burstLines), err)
	}
	const importers = 8
	ins, skip, deadlocks, importElapsed := raceImport(t, payRepo, tenant, bChannel, bAccount, burstLines, importers)
	if ins != 2000 || skip != 2000*(importers-1) {
		t.Fatalf("race inserted=%d skipped=%d deadlocks=%d", ins, skip, deadlocks)
	}
	var storedCount int
	var storedSum string
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*), SUM(amount)::text FROM tally.money_line
		WHERE tenant_id = $1 AND account_ref = $2`, tenant, bAccount).Scan(&storedCount, &storedSum); err != nil {
		t.Fatal(err)
	}
	if storedCount != 2000 || !sameMoney(storedSum, "6500.00") {
		t.Fatalf("stored count=%d sum=%s", storedCount, storedSum)
	}
	replayStart := time.Now()
	againIns, againSkip, err := payRepo.InsertNew(ctx, tenant, bChannel, bAccount, burstLines)
	replayElapsed := time.Since(replayStart)
	if err != nil || againIns != 0 || againSkip != 2000 {
		t.Fatalf("replay inserted=%d skipped=%d err=%v", againIns, againSkip, err)
	}

	burstBill := insertRecvBill(t, db, tenant, recvBill{
		billType: "出库", subType: "销售", status: 2, date: time.Now(), total: "1280.00", paid: "0",
	})
	if _, _, err := payRepo.InsertNew(ctx, tenant, domainpayment.ChannelAlipay, "burst-confirm", []domainpayment.ParsedLine{{
		ExternalID: "CONFIRM-1", BookedAt: time.Now(), Amount: decimal.RequireFromString("1280.00"), Counterparty: "A客户",
	}}); err != nil {
		t.Fatal(err)
	}
	confirmLine := moneyLineID(t, db, tenant, "CONFIRM-1")
	const callers = 64
	errs := make([]error, callers)
	var wg sync.WaitGroup
	confirmStart := time.Now()
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = uc.Confirm(ctx, tenant, creator, confirmLine, burstBill)
		}(i)
	}
	wg.Wait()
	confirmElapsed := time.Since(confirmStart)
	for i, err := range errs {
		if err != nil {
			t.Errorf("confirm %d: %v", i, err)
		}
	}
	if got, paid := paymentCount(t, db, burstBill), paidText(t, db, burstBill); got != 1 || !sameMoney(paid, "1280.00") {
		t.Fatalf("burst payments=%d paid=%s", got, paid)
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM tally.money_line WHERE id = $1`, confirmLine).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != domainpayment.MoneyMatched {
		t.Fatalf("line status %s", status)
	}
	t.Logf("one_confirm_pair=%s import_workers=%d import=%s inserted=%d skipped=%d deadlocks=%d replay=%s confirm_callers=%d confirm=%s",
		oneConfirm, importers, importElapsed, ins, skip, deadlocks, replayElapsed, callers, confirmElapsed)
}

func raceImport(t *testing.T, repo *repopayment.Repo, tenant uuid.UUID, channel, account string, lines []domainpayment.ParsedLine, workers int) (inserted, skipped, deadlocks int, elapsed time.Duration) {
	t.Helper()
	type outcome struct {
		ins, skip, retries int
		err                error
	}
	out := make(chan outcome, workers)
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var last error
			retries := 0
			for attempt := 0; attempt < 4; attempt++ {
				ins, skip, err := repo.InsertNew(context.Background(), tenant, channel, account, lines)
				if err != nil && strings.Contains(err.Error(), "deadlock detected") {
					last = err
					retries++
					continue
				}
				out <- outcome{ins: ins, skip: skip, retries: retries, err: err}
				return
			}
			out <- outcome{retries: retries, err: last}
		}()
	}
	wg.Wait()
	close(out)
	elapsed = time.Since(start)
	for item := range out {
		if item.err != nil {
			t.Fatal(item.err)
		}
		inserted += item.ins
		skipped += item.skip
		deadlocks += item.retries
	}
	return inserted, skipped, deadlocks, elapsed
}

func readMoneyCSV(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "domain", "payment", "testdata", "industries", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func moneyLineID(t *testing.T, db *sql.DB, tenant uuid.UUID, externalID string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRowContext(context.Background(), `
		SELECT id FROM tally.money_line WHERE tenant_id = $1 AND external_id = $2`, tenant, externalID).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func paymentCount(t *testing.T, db *sql.DB, bill uuid.UUID) int {
	t.Helper()
	var n int
	err := db.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM tally.payment_head WHERE related_bill_id = $1 AND deleted_at IS NULL`, bill).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func sameMoney(got, want string) bool {
	g, errG := decimal.NewFromString(got)
	w, errW := decimal.NewFromString(want)
	return errG == nil && errW == nil && g.Equal(w)
}

func paidText(t *testing.T, db *sql.DB, bill uuid.UUID) string {
	t.Helper()
	var paid string
	err := db.QueryRowContext(context.Background(), `
		SELECT paid_amount::text FROM tally.bill_head WHERE id = $1`, bill).Scan(&paid)
	if err != nil {
		t.Fatal(err)
	}
	return paid
}
