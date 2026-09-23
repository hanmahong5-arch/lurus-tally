package main

// Customer-memory replay ("熟客记忆") against a real Postgres and memorus.
//
//	CUSTOMERS=1 DATABASE_URL=postgres://tally:tallysecret@127.0.0.1:5432/lurus?sslmode=disable \
//	  MEMORUS_URL=http://127.0.0.1:18767/api/v1 [SCENARIO=holdout] [NOATTR=1] go run ./cmd/memscenario
//
// DATABASE_URL must be a superuser (migrations + seeding). Reads run as a
// NOSUPERUSER role on a connection pinned with app.tenant_id, the way the
// HTTP middleware does it, so row-level security applies. Each run seeds a new
// tenant; memorus must be fresh per run.
//
// NOATTR=1 is the negative control: memories are written without a customer
// and recalled without a customer filter.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	repoai "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/lifecycle"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
)

const appRole, appPassword = "memscenario_app", "memscenario"

func runCustomers() {
	ctx := context.Background()
	sc, label := devCustomers(), "dev"
	if os.Getenv("SCENARIO") == "holdout" {
		sc, label = holdoutCustomers(), "holdout"
	}
	noAttr := os.Getenv("NOATTR") == "1"

	dsn := os.Getenv("DATABASE_URL")
	must(lifecycle.RunMigrations(ctx, dsn, nil), "migrate")
	admin, err := sql.Open("pgx", dsn)
	must(err, "open admin")
	defer admin.Close() //nolint:errcheck

	tenant := uuid.New()
	partners := seed(ctx, admin, tenant, sc)
	decoyTenant := uuid.New()
	seedDecoy(ctx, admin, decoyTenant, sc)

	// Reads: a NOSUPERUSER role on a tenant-pinned connection (RLS applies).
	appDB, err := sql.Open("pgx", roleDSN(dsn))
	must(err, "open app")
	defer appDB.Close() //nolint:errcheck
	conn, err := appDB.Conn(ctx)
	must(err, "app conn")
	defer conn.Close() //nolint:errcheck
	_, err = conn.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, false)", tenant.String())
	must(err, "set tenant")
	ctx = dbscope.With(ctx, conn)
	sales := repoai.NewSQLSaleRepo(appDB)

	fmt.Printf("== scenario=%s noattr=%v tenant=%s\n", label, noAttr, tenant)
	if os.Getenv("ALIASES") == "1" {
		al := devAliases()
		if label == "holdout" {
			al = holdoutAliases()
		}
		runAliases(ctx, sales, tenant, sc, al, label, noAttr)
		return
	}
	p1 := purchases(ctx, sales, tenant, sc, partners)
	m := memories(ctx, sales, tenant, sc, noAttr)
	fmt.Printf("SCORE scenario=%s noattr=%v | purchases: exact=%d/%d ambiguous_ok=%v | memory: own_facts=%d/%d foreign_lines=%d stale_lines=%d injected_lines=%d\n",
		label, noAttr, p1.exact, p1.total, p1.ambiguousOK, m.found, m.want, m.foreign, m.stale, m.lines)
}

func must(err error, what string) {
	if err != nil {
		panic(fmt.Sprintf("%s: %v", what, err))
	}
}

func roleDSN(dsn string) string {
	u, err := url.Parse(dsn)
	must(err, "parse dsn")
	u.User = url.UserPassword(appRole, appPassword)
	return u.String()
}

// seed writes the scenario's tenant, customers, products and bills, and the
// read-only role. Returns partner name → id.
func seed(ctx context.Context, db *sql.DB, tenant uuid.UUID, sc customerScenario) map[string]uuid.UUID {
	exec := func(what, q string, args ...any) {
		_, err := db.ExecContext(ctx, q, args...)
		must(err, what)
	}
	exec("role", `DO $$ BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '`+appRole+`') THEN
			CREATE ROLE `+appRole+` LOGIN PASSWORD '`+appPassword+`' NOSUPERUSER NOBYPASSRLS;
		END IF; END $$`)
	exec("grant schema", `GRANT USAGE ON SCHEMA tally TO `+appRole)
	exec("grant select", `GRANT SELECT ON ALL TABLES IN SCHEMA tally TO `+appRole)

	exec("tenant", `INSERT INTO tally.tenant (id, name, status) VALUES ($1, $2, 1)`, tenant, "memscenario "+tenant.String()[:8])
	wh := uuid.New()
	exec("warehouse", `INSERT INTO tally.warehouse (id, tenant_id, name, enabled, is_default) VALUES ($1, $2, '主仓', true, true)`, wh, tenant)
	products := map[string]uuid.UUID{}
	i := 0
	for name := range sc.products {
		i++
		id := uuid.New()
		exec("product", `INSERT INTO tally.product (id, tenant_id, code, name, enabled, lead_time_days) VALUES ($1, $2, $3, $4, true, 7)`,
			id, tenant, fmt.Sprintf("P%03d", i), name)
		products[name] = id
	}
	partners := map[string]uuid.UUID{}
	for _, c := range sc.customers {
		id := uuid.New()
		exec("partner", `INSERT INTO tally.partner (id, tenant_id, name, code, partner_type, enabled) VALUES ($1, $2, $3, $4, 'customer', true)`,
			id, tenant, c.name, c.code)
		partners[c.name] = id
	}
	now := time.Now().UTC()
	for _, b := range sc.bills {
		insertBill(ctx, db, tenant, wh, b, now, products, partners)
	}
	return partners
}

func insertBill(ctx context.Context, db *sql.DB, tenant, wh uuid.UUID, b bill, now time.Time,
	products, partners map[string]uuid.UUID) {
	head := uuid.New()
	total := decimal.Zero
	for _, l := range b.lines {
		total = total.Add(decimal.RequireFromString(l.qty).Mul(decimal.RequireFromString(l.price)))
	}
	var partner any
	remark := ""
	if b.quick {
		remark = b.customer
	} else {
		partner = partners[b.customer]
	}
	status := 2
	if b.draft {
		status = 0
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO tally.bill_head (id, tenant_id, bill_no, bill_type, sub_type, status, partner_id,
		    creator_id, bill_date, total_amount, remark, warehouse_id)
		VALUES ($1, $2, $3, '出库', '销售', $4, $5, $6, $7, $8, NULLIF($9, ''), $10)`,
		head, tenant, b.no, status, partner, uuid.New(), now.AddDate(0, 0, -b.daysAgo), total.String(), remark, wh)
	must(err, "bill_head "+b.no)
	for n, l := range b.lines {
		amt := decimal.RequireFromString(l.qty).Mul(decimal.RequireFromString(l.price))
		_, err := db.ExecContext(ctx, `
			INSERT INTO tally.bill_item (tenant_id, head_id, product_id, qty, unit_price, line_amount, line_no)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			tenant, head, products[l.product], l.qty, l.price, amt.String(), n+1)
		must(err, "bill_item "+b.no)
	}
}

func seedDecoy(ctx context.Context, db *sql.DB, tenant uuid.UUID, sc customerScenario) {
	_, err := db.ExecContext(ctx, `INSERT INTO tally.tenant (id, name, status) VALUES ($1, 'decoy', 1)`, tenant)
	must(err, "decoy tenant")
	wh, prod, partner := uuid.New(), uuid.New(), uuid.New()
	_, err = db.ExecContext(ctx, `INSERT INTO tally.warehouse (id, tenant_id, name, enabled, is_default) VALUES ($1, $2, 'X', true, true)`, wh, tenant)
	must(err, "decoy wh")
	_, err = db.ExecContext(ctx, `INSERT INTO tally.product (id, tenant_id, code, name, enabled, lead_time_days) VALUES ($1, $2, 'X1', $3, true, 7)`, prod, tenant, sc.decoyProduct)
	must(err, "decoy product")
	_, err = db.ExecContext(ctx, `INSERT INTO tally.partner (id, tenant_id, name, partner_type, enabled) VALUES ($1, $2, $3, 'customer', true)`, partner, tenant, sc.decoyCustomer)
	must(err, "decoy partner")
	insertBill(ctx, db, tenant, wh, bill{no: "SL-X001", customer: sc.decoyCustomer, daysAgo: 0,
		lines: []line{{sc.decoyProduct, "1", "1499"}}}, time.Now().UTC(),
		map[string]uuid.UUID{sc.decoyProduct: prod}, map[string]uuid.UUID{sc.decoyCustomer: partner})
}

// --- metric 1: "X 上次买了什么" ---

type purchaseScore struct {
	exact, total int
	ambiguousOK  bool
}

type toolResult struct {
	Found     bool `json:"found"`
	Ambiguous bool `json:"ambiguous"`
	Customer  struct {
		Name string `json:"name"`
	} `json:"customer"`
	Candidates []struct {
		Name string `json:"name"`
	} `json:"candidates"`
	Bills []struct {
		BillNo string `json:"bill_no"`
		Date   string `json:"date"`
		Total  string `json:"total"`
		Items  []struct {
			Product   string `json:"product"`
			Qty       string `json:"qty"`
			UnitPrice string `json:"unit_price"`
			Amount    string `json:"amount"`
		} `json:"items"`
	} `json:"bills"`
}

func dispatch(ctx context.Context, sales *repoai.SQLSaleRepo, tenant uuid.UUID, name string) (toolResult, string) {
	reg := ai.NewRegistry(nil, nil, sales, nil)
	res := reg.Dispatch(ctx, tenant, llmclient.ToolCall{ID: "c", Type: "function",
		Function: llmclient.ToolCallFunction{Name: "customer_recent_purchases", Arguments: fmt.Sprintf(`{"customer":%q}`, name)}})
	var tr toolResult
	_ = json.Unmarshal([]byte(res.Content), &tr)
	return tr, res.Content
}

// expectedBills renders what the books say for customer name: approved bills
// with its partner id or its name on a quick checkout, newest first, top 5.
func expectedBills(sc customerScenario, name string) []string {
	type eb struct {
		daysAgo int
		s       string
	}
	var bs []eb
	now := time.Now().UTC()
	for _, b := range sc.bills {
		if b.customer != name || b.draft {
			continue
		}
		total := decimal.Zero
		var items []string
		for _, l := range b.lines {
			q, p := decimal.RequireFromString(l.qty), decimal.RequireFromString(l.price)
			total = total.Add(q.Mul(p))
			items = append(items, fmt.Sprintf("%s×%s@%s=%s", l.product, q.String(), p.StringFixed(2), q.Mul(p).StringFixed(2)))
		}
		bs = append(bs, eb{b.daysAgo, fmt.Sprintf("%s %s %s [%s]", b.no, now.AddDate(0, 0, -b.daysAgo).Format("2006-01-02"),
			total.StringFixed(2), strings.Join(items, " "))})
	}
	sort.SliceStable(bs, func(i, j int) bool { return bs[i].daysAgo < bs[j].daysAgo })
	out := []string{}
	for i, b := range bs {
		if i == 5 {
			break
		}
		out = append(out, b.s)
	}
	return out
}

func gotBills(tr toolResult) []string {
	out := []string{}
	for _, b := range tr.Bills {
		var items []string
		for _, it := range b.Items {
			q, _ := decimal.NewFromString(it.Qty)
			items = append(items, fmt.Sprintf("%s×%s@%s=%s", it.Product, q.String(), it.UnitPrice, it.Amount))
		}
		out = append(out, fmt.Sprintf("%s %s %s [%s]", b.BillNo, b.Date, b.Total, strings.Join(items, " ")))
	}
	return out
}

func purchases(ctx context.Context, sales *repoai.SQLSaleRepo, tenant uuid.UUID, sc customerScenario, _ map[string]uuid.UUID) purchaseScore {
	var s purchaseScore
	for _, name := range sc.purchaseProbes {
		s.total++
		tr, raw := dispatch(ctx, sales, tenant, name)
		want, got := expectedBills(sc, name), gotBills(tr)
		ok := tr.Found && tr.Customer.Name == name && strings.Join(want, "\n") == strings.Join(got, "\n")
		if ok {
			s.exact++
		}
		fmt.Printf("PURCHASE ok=%v q=%s上次买了什么\n  want: %s\n  got:  %s\n", ok, name,
			strings.Join(want, "\n        "), strings.Join(got, "\n        "))
		if !ok {
			fmt.Printf("  raw: %s\n", raw)
		}
	}
	tr, raw := dispatch(ctx, sales, tenant, sc.ambiguousProbe)
	var names []string
	for _, c := range tr.Candidates {
		names = append(names, c.Name)
	}
	sort.Strings(names)
	s.ambiguousOK = tr.Ambiguous && strings.Join(names, ",") == strings.Join(sc.ambiguousWant, ",")
	fmt.Printf("PURCHASE ambiguous ok=%v q=%s → %s\n", s.ambiguousOK, sc.ambiguousProbe, raw)
	return s
}

// --- metric 2/3: "接待 X 要注意什么" ---

type memoryScore struct {
	found, want, foreign, stale, lines int
}

func memories(ctx context.Context, sales *repoai.SQLSaleRepo, tenant uuid.UUID, sc customerScenario, noAttr bool) memoryScore {
	c, err := memorusclient.New(memorusclient.Config{BaseURL: os.Getenv("MEMORUS_URL"), APIKey: "probekey"})
	if err != nil || c == nil {
		panic(fmt.Sprint("memorus client: ", err))
	}
	var resolver ai.CustomerResolver = sales
	if noAttr {
		resolver = nil
	}
	user := tenant.String()

	// What the orchestrator does after each chat turn.
	turn := func(f fact) {
		customer := ai.ResolveCustomer(ctx, resolver, tenant, f.text)
		summary := ai.BuildMemorySummary(tenant, f.text, "（助手回答略）")
		ai.AsyncWriteMemory(c, user, summary, ai.MemoryWriteMeta(tenant, customer))
		time.Sleep(400 * time.Millisecond)
	}
	for _, f := range sc.session1 {
		turn(f)
	}
	for _, f := range sc.session2 {
		turn(f)
	}

	all := append(append([]fact{}, sc.session1...), sc.session2...)
	var s memoryScore
	for _, p := range sc.memoryProbes {
		customer := ai.ResolveCustomer(ctx, resolver, tenant, p.q)
		block := ai.AugmentWithCustomerMemory(c, ctx, user, p.q, customer)
		injected := strings.TrimSuffix(block, p.q)

		wantKeys := map[string]bool{}
		for _, f := range all {
			if f.owner == p.about && !f.stale {
				wantKeys[f.key] = true
			}
		}
		found := 0
		for k := range wantKeys {
			if strings.Contains(injected, k) {
				found++
			}
		}
		foreign, stale, lines := 0, 0, 0
		for _, l := range strings.Split(injected, "\n") {
			if !strings.HasPrefix(l, "• ") {
				continue
			}
			lines++
			for _, f := range all {
				if f.key == "" || !strings.Contains(l, f.key) {
					continue
				}
				if f.owner != "" && f.owner != p.about {
					foreign++
					break
				}
				if f.owner == p.about && f.stale {
					stale++
					break
				}
			}
		}
		s.found += found
		s.want += len(wantKeys)
		s.foreign += foreign
		s.stale += stale
		s.lines += lines
		resolved := "-"
		if customer != nil {
			resolved = customer.Name
		}
		fmt.Printf("MEMORY q=%q resolved=%s own=%d/%d foreign=%d stale=%d\n%s\n",
			p.q, resolved, found, len(wantKeys), foreign, stale, indent(injected))
	}
	return s
}

// runAliases asks about customers the way shop owners name them (老张, 王老板).
func runAliases(ctx context.Context, sales *repoai.SQLSaleRepo, tenant uuid.UUID,
	sc customerScenario, al aliasScenario, label string, noAttr bool) {
	ok := 0
	for _, p := range al.purchases {
		tr, raw := dispatch(ctx, sales, tenant, p.alias)
		var pass bool
		switch {
		case p.want != "":
			pass = tr.Found && tr.Customer.Name == p.want &&
				strings.Join(expectedBills(sc, p.want), "\n") == strings.Join(gotBills(tr), "\n")
		case len(p.cands) > 0:
			var names []string
			for _, c := range tr.Candidates {
				names = append(names, c.Name)
			}
			sort.Strings(names)
			pass = tr.Ambiguous && strings.Join(names, ",") == strings.Join(p.cands, ",")
		default:
			pass = !tr.Found && !tr.Ambiguous
		}
		if pass {
			ok++
		}
		fmt.Printf("ALIAS-PURCHASE ok=%v q=%s want=%q cands=%v\n  raw: %s\n", pass, p.alias, p.want, p.cands, raw)
	}
	sc.session2 = append(append([]fact{}, sc.session2...), al.session3...)
	sc.memoryProbes = al.memory
	m := memories(ctx, sales, tenant, sc, noAttr)
	fmt.Printf("SCORE aliases scenario=%s noattr=%v | purchases: %d/%d | memory: own_facts=%d/%d foreign_lines=%d stale_lines=%d injected_lines=%d\n",
		label, noAttr, ok, len(al.purchases), m.found, m.want, m.foreign, m.stale, m.lines)
}
