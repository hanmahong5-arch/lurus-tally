package replenish

// PG integration test for the sales-velocity query. Skipped when DATABASE_DSN is
// not set; needs the tally schema with migrations applied.

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type velocityFixture struct {
	t              *testing.T
	db             *sql.DB
	ctx            context.Context
	tenant, wh, pr uuid.UUID
}

func newVelocityFixture(t *testing.T) *velocityFixture {
	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		t.Skip("DATABASE_DSN not set — skipping integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	f := &velocityFixture{t: t, db: db, ctx: context.Background(), tenant: uuid.New(), wh: uuid.New(), pr: uuid.New()}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM tally.stock_movement WHERE tenant_id=$1`,
			`DELETE FROM tally.bill_item WHERE tenant_id=$1`,
			`DELETE FROM tally.bill_head WHERE tenant_id=$1`,
			`DELETE FROM tally.product WHERE tenant_id=$1`,
			`DELETE FROM tally.warehouse WHERE tenant_id=$1`,
		} {
			_, _ = db.ExecContext(f.ctx, q, f.tenant)
		}
		_ = db.Close()
	})
	f.exec(`INSERT INTO tally.warehouse (id, tenant_id, name) VALUES ($1,$2,'wh')`, f.wh, f.tenant)
	f.exec(`INSERT INTO tally.product (id, tenant_id, code, name) VALUES ($1,$2,'P1','p')`, f.pr, f.tenant)
	return f
}

func (f *velocityFixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.db.ExecContext(f.ctx, q, args...); err != nil {
		f.t.Fatalf("exec %.40s: %v", q, err)
	}
}

func (f *velocityFixture) bill(no string) uuid.UUID {
	id := uuid.New()
	f.exec(`INSERT INTO tally.bill_head (id, tenant_id, bill_no, bill_type, sub_type, status, creator_id, bill_date)
		VALUES ($1,$2,$3,'出库','销售',2,$2,$4)`, id, f.tenant, no, time.Now())
	return id
}

// item inserts a bill_item under head and returns its id (a sample sale's movement points at it).
func (f *velocityFixture) item(head uuid.UUID) uuid.UUID {
	id := uuid.New()
	f.exec(`INSERT INTO tally.bill_item (id, tenant_id, head_id, product_id, qty, unit_price, line_no)
		VALUES ($1,$2,$3,$4,1,1,1)`, id, f.tenant, head, f.pr)
	return id
}

func (f *velocityFixture) out(refType string, ref uuid.UUID) {
	f.exec(`INSERT INTO tally.stock_movement (tenant_id, product_id, warehouse_id, direction, qty_base, reference_type, reference_id)
		VALUES ($1,$2,$3,'out',30,$4,$5)`, f.tenant, f.pr, f.wh, refType, ref)
}

func (f *velocityFixture) avg() string {
	f.t.Helper()
	rows, err := NewSQLSuggestionRepo(f.db).ListSuggestions(f.ctx, f.tenant)
	if err != nil {
		f.t.Fatal(err)
	}
	if len(rows) != 1 {
		f.t.Fatalf("rows = %d, want 1", len(rows))
	}
	return rows[0].AvgDailySales.String()
}

// A sale movement whose bill no longer exists (cleared sample sale) must not
// count toward the daily average; a live sale still does.
func TestListSuggestionsPG_VelocityIgnoresDanglingSale(t *testing.T) {
	f := newVelocityFixture(t)
	f.out("sale", f.bill("XS-V1"))
	f.out("sale", uuid.New())
	if got := f.avg(); got != "1" {
		t.Errorf("avg daily = %s, want 1 (only the live sale)", got)
	}
}

// A sample sale's movement points at its bill_item id; while the sample bill is
// still there it counts, once the bill (and its items) is gone it does not.
func TestListSuggestionsPG_VelocitySampleSaleByItemRef(t *testing.T) {
	f := newVelocityFixture(t)
	live := f.item(f.bill("DEMO-S-1"))
	f.out("sale", live)
	cleared := f.item(f.bill("DEMO-S-2"))
	f.out("sale", cleared)
	f.exec(`DELETE FROM tally.bill_item WHERE id=$1`, cleared)
	f.exec(`DELETE FROM tally.bill_head WHERE bill_no='DEMO-S-2' AND tenant_id=$1`, f.tenant)
	if got := f.avg(); got != "1" {
		t.Errorf("avg daily = %s, want 1 (live sample sale counts, cleared one does not)", got)
	}
}

// Purchase-return and stocktake-adjust outflows are not sales and stay out of the average.
func TestListSuggestionsPG_VelocityExcludesReturnAndAdjust(t *testing.T) {
	f := newVelocityFixture(t)
	f.out("sale", f.bill("XS-V2"))
	f.out("return", uuid.New())
	f.out("adjust", uuid.New())
	if got := f.avg(); got != "1" {
		t.Errorf("avg daily = %s, want 1 (return/adjust excluded)", got)
	}
}
