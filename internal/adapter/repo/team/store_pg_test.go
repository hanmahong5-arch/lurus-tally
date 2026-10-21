package team

// PG integration tests for the empty-shop probe and AbandonShop. Skipped when
// DATABASE_DSN is not set; they need the tally schema with migrations applied.

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	appteam "github.com/hanmahong5-arch/lurus-tally/internal/app/team"
)

type pgFixture struct {
	db     *sql.DB
	store  *Store
	tenant uuid.UUID
	sub    string
	wh     uuid.UUID
}

func newPGFixture(t *testing.T) *pgFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		t.Skip("DATABASE_DSN not set — skipping integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	f := &pgFixture{db: db, store: New(db), tenant: uuid.New(), sub: "team-pg-" + uuid.NewString(), wh: uuid.New()}
	t.Cleanup(func() {
		ctx := context.Background()
		for _, q := range []string{
			`DELETE FROM tally.stock_movement WHERE tenant_id=$1`,
			`DELETE FROM tally.stock_initial WHERE tenant_id=$1`,
			`DELETE FROM tally.stock_snapshot WHERE tenant_id=$1`,
			`DELETE FROM tally.bill_item WHERE tenant_id=$1`,
			`DELETE FROM tally.bill_head WHERE tenant_id=$1`,
			`DELETE FROM tally.product WHERE tenant_id=$1`,
			`DELETE FROM tally.partner WHERE tenant_id=$1`,
			`DELETE FROM tally.warehouse WHERE tenant_id=$1`,
		} {
			_, _ = db.ExecContext(ctx, q, f.tenant)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM tally.tenant_profile WHERE tenant_id=$1`, f.tenant)
		_, _ = db.ExecContext(ctx, `DELETE FROM tally.user_identity_mapping WHERE tenant_id=$1`, f.tenant)
		_, _ = db.ExecContext(ctx, `DELETE FROM tally.tenant WHERE id=$1`, f.tenant)
		_ = db.Close()
	})
	f.exec(t, `INSERT INTO tally.tenant (id, name, status) VALUES ($1, 'pg-team-shop', 1)`, f.tenant)
	f.exec(t, `INSERT INTO tally.user_identity_mapping (id, tenant_id, zitadel_sub, email, role, is_owner)
		VALUES (gen_random_uuid(), $1, $2, 'o@x.test', 'owner', true)`, f.tenant, f.sub)
	f.exec(t, `INSERT INTO tally.warehouse (id, tenant_id, name, is_sample) VALUES ($1,$2,'sample-wh',true)`, f.wh, f.tenant)
	f.exec(t, `INSERT INTO tally.partner (id, tenant_id, name, partner_type, is_sample) VALUES (gen_random_uuid(),$1,'sample-sup','supplier',true)`, f.tenant)
	return f
}

func (f *pgFixture) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := f.db.ExecContext(context.Background(), q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

// addDemo mimics the wizard's "fill in sample data": a DEMO product with opening
// stock and a DEMO sale.
func (f *pgFixture) addDemo(t *testing.T) {
	t.Helper()
	p, h := uuid.New(), uuid.New()
	f.exec(t, `INSERT INTO tally.product (id, tenant_id, code, name, remark) VALUES ($1,$2,'DEMO-1','demo','DEMO')`, p, f.tenant)
	f.exec(t, `INSERT INTO tally.stock_movement (tenant_id, product_id, warehouse_id, direction, qty_base, reference_type, reference_id)
		VALUES ($1,$2,$3,'in',10,'init',gen_random_uuid())`, f.tenant, p, f.wh)
	f.exec(t, `INSERT INTO tally.bill_head (id, tenant_id, bill_no, bill_type, sub_type, status, bill_date, remark, creator_id)
		VALUES ($1,$2,'DEMO-S-1','出库','销售',2,now(),'DEMO',gen_random_uuid())`, h, f.tenant)
}

func TestAbandonShop_OnlySampleData(t *testing.T) {
	f := newPGFixture(t)
	ctx := context.Background()
	f.addDemo(t)
	empty, err := f.store.ShopIsEmpty(ctx, f.tenant)
	if err != nil || !empty {
		t.Fatalf("ShopIsEmpty = %v, %v; want true", empty, err)
	}
	if err := f.store.AbandonShop(ctx, f.tenant, f.sub); err != nil {
		t.Fatalf("AbandonShop: %v", err)
	}
	var status int
	f.db.QueryRowContext(ctx, `SELECT status FROM tally.tenant WHERE id=$1`, f.tenant).Scan(&status)
	if status != 0 {
		t.Errorf("tenant status = %d, want 0", status)
	}
	if m, _ := f.store.MappingBySub(ctx, f.sub); m != nil {
		t.Errorf("subject still mapped after abandon")
	}
}

func TestAbandonShop_RealProductBlocks(t *testing.T) {
	f := newPGFixture(t)
	ctx := context.Background()
	f.addDemo(t)
	f.exec(t, `INSERT INTO tally.product (id, tenant_id, code, name) VALUES (gen_random_uuid(),$1,'P-1','真实商品')`, f.tenant)
	empty, err := f.store.ShopIsEmpty(ctx, f.tenant)
	if err != nil || empty {
		t.Fatalf("ShopIsEmpty = %v, %v; want false", empty, err)
	}
	if err := f.store.AbandonShop(ctx, f.tenant, f.sub); !errors.Is(err, appteam.ErrShopNotEmpty) {
		t.Fatalf("AbandonShop err = %v, want ErrShopNotEmpty", err)
	}
}

func TestAbandonShop_RealBillOnDemoProductBlocks(t *testing.T) {
	f := newPGFixture(t)
	ctx := context.Background()
	f.addDemo(t)
	f.exec(t, `INSERT INTO tally.bill_head (id, tenant_id, bill_no, bill_type, sub_type, status, bill_date, remark, creator_id)
		VALUES (gen_random_uuid(),$1,'S-REAL-1','出库','销售',2,now(),'',gen_random_uuid())`, f.tenant)
	if err := f.store.AbandonShop(ctx, f.tenant, f.sub); !errors.Is(err, appteam.ErrShopNotEmpty) {
		t.Fatalf("AbandonShop err = %v, want ErrShopNotEmpty", err)
	}
}

func TestAbandonShop_OtherMemberBlocks(t *testing.T) {
	f := newPGFixture(t)
	ctx := context.Background()
	f.exec(t, `INSERT INTO tally.user_identity_mapping (id, tenant_id, zitadel_sub, email, role, is_owner)
		VALUES (gen_random_uuid(), $1, $2, 's@x.test', 'clerk', false)`, f.tenant, f.sub+"-staff")
	if err := f.store.AbandonShop(ctx, f.tenant, f.sub); !errors.Is(err, appteam.ErrShopNotEmpty) {
		t.Fatalf("AbandonShop err = %v, want ErrShopNotEmpty", err)
	}
}

// The setup wizard's "generate first purchase" step turns a demo shortage into a
// draft purchase with a blank remark; it is still sample data.
func TestAbandonShop_ReplenishDraftOnDemoStaysAbandonable(t *testing.T) {
	f := newPGFixture(t)
	ctx := context.Background()
	f.addDemo(t)
	var p uuid.UUID
	if err := f.db.QueryRowContext(ctx, `SELECT id FROM tally.product WHERE tenant_id=$1 AND remark='DEMO'`, f.tenant).Scan(&p); err != nil {
		t.Fatalf("demo product: %v", err)
	}
	h := uuid.New()
	f.exec(t, `INSERT INTO tally.bill_head (id, tenant_id, bill_no, bill_type, sub_type, status, bill_date, remark, creator_id)
		VALUES ($1,$2,'PO-DRAFT-1','入库','采购',0,now(),'',gen_random_uuid())`, h, f.tenant)
	f.exec(t, `INSERT INTO tally.bill_item (tenant_id, head_id, product_id, warehouse_id, qty, line_no) VALUES ($1,$2,$3,$4,5,1)`,
		f.tenant, h, p, f.wh)
	empty, err := f.store.ShopIsEmpty(ctx, f.tenant)
	if err != nil || !empty {
		t.Fatalf("ShopIsEmpty = %v, %v; want true", empty, err)
	}
	if err := f.store.AbandonShop(ctx, f.tenant, f.sub); err != nil {
		t.Fatalf("AbandonShop: %v", err)
	}
}

// A draft that also holds a real product is the owner's own work.
func TestAbandonShop_DraftWithRealProductBlocks(t *testing.T) {
	f := newPGFixture(t)
	ctx := context.Background()
	f.addDemo(t)
	real, h := uuid.New(), uuid.New()
	f.exec(t, `INSERT INTO tally.product (id, tenant_id, code, name, remark) VALUES ($1,$2,'P-9','真实商品','')`, real, f.tenant)
	f.exec(t, `INSERT INTO tally.bill_head (id, tenant_id, bill_no, bill_type, sub_type, status, bill_date, remark, creator_id)
		VALUES ($1,$2,'PO-DRAFT-2','入库','采购',0,now(),'',gen_random_uuid())`, h, f.tenant)
	f.exec(t, `INSERT INTO tally.bill_item (tenant_id, head_id, product_id, warehouse_id, qty, line_no) VALUES ($1,$2,$3,$4,5,1)`,
		f.tenant, h, real, f.wh)
	if err := f.store.AbandonShop(ctx, f.tenant, f.sub); !errors.Is(err, appteam.ErrShopNotEmpty) {
		t.Fatalf("AbandonShop err = %v, want ErrShopNotEmpty", err)
	}
}

func TestProfileType(t *testing.T) {
	f := newPGFixture(t)
	ctx := context.Background()
	if pt, err := f.store.ProfileType(ctx, f.tenant); err != nil || pt != "" {
		t.Fatalf("no profile: %q %v", pt, err)
	}
	f.exec(t, `INSERT INTO tally.tenant_profile (id, tenant_id, profile_type, inventory_method, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, 'horticulture', 'wac', now(), now())`, f.tenant)
	if pt, err := f.store.ProfileType(ctx, f.tenant); err != nil || pt != "horticulture" {
		t.Fatalf("got %q %v", pt, err)
	}
}
