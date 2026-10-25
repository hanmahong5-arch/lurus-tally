package router

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	handleracct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/account"
	handleraudit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/audit"
	handlerAuth "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/auth"
	handlerbill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/bill"
	handlerbilling "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/billing"
	handlercurrency "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/currency"
	handlercust "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/customer"
	handlerdigest "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/digest"
	handlerexport "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/export"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/health"
	handlerhorticulture "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/horticulture"
	handlerimporting "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/importing"
	handlerinvoice "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/invoice"
	handlermetrics "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/metrics"
	handleronboarding "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/onboarding"
	handlerpayment "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/payment"
	handlerprint "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/print"
	handlerproduct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/product"
	handlerproject "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/project"
	handlerreplenish "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/replenish"
	handlerreports "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/reports"
	handlersearch "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/search"
	handlershopify "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/shopify"
	handlerstock "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/stock"
	handlersupp "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/supplier"
	handlerteam "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/team"
	handlerunit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/unit"
	handlerwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/warehouse"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	repoSearch "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/search"
	repounit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/unit"
	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appcurrency "github.com/hanmahong5-arch/lurus-tally/internal/app/currency"
	appcust "github.com/hanmahong5-arch/lurus-tally/internal/app/customer"
	appdigest "github.com/hanmahong5-arch/lurus-tally/internal/app/digest"
	apphort "github.com/hanmahong5-arch/lurus-tally/internal/app/horticulture"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/partnerimport"
	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	appprint "github.com/hanmahong5-arch/lurus-tally/internal/app/print"
	appproduct "github.com/hanmahong5-arch/lurus-tally/internal/app/product"
	appproject "github.com/hanmahong5-arch/lurus-tally/internal/app/project"
	appreplenish "github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
	appsearch "github.com/hanmahong5-arch/lurus-tally/internal/app/search"
	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	appsupp "github.com/hanmahong5-arch/lurus-tally/internal/app/supplier"
	apptenant "github.com/hanmahong5-arch/lurus-tally/internal/app/tenant"
	apptodo "github.com/hanmahong5-arch/lurus-tally/internal/app/todo"
	appunit "github.com/hanmahong5-arch/lurus-tally/internal/app/unit"
	appwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/app/warehouse"
	domainacct "github.com/hanmahong5-arch/lurus-tally/internal/domain/account"
	domainbill "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domaincurrency "github.com/hanmahong5-arch/lurus-tally/internal/domain/currency"
	domaincust "github.com/hanmahong5-arch/lurus-tally/internal/domain/customer"
	domainhort "github.com/hanmahong5-arch/lurus-tally/internal/domain/horticulture"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
	domainproduct "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
	domainproject "github.com/hanmahong5-arch/lurus-tally/internal/domain/project"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
	domainsupp "github.com/hanmahong5-arch/lurus-tally/internal/domain/supplier"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
	domainunit "github.com/hanmahong5-arch/lurus-tally/internal/domain/unit"
	domainwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/domain/warehouse"
)

// Sentinel values planted in the stubs. Purchase-side figures must never reach a
// role without cost.view; receivable figures never a role without payment.read.
const (
	secretPurchaseNo   = "PO-SECRET-77"
	secretPurchaseAmt  = "98765.43"
	secretUnitCost     = "31415.92"
	secretPurchasePx   = "27182.81"
	secretReplenishAmt = "13579.24"
	secretOverdueAmt   = "7654.32"
	secretUnpaidAmt    = "8765.21"
	secretCollectedAmt = "2468.13"
	secretUninvoiced   = "3141.59"
	secretReceivable   = "5555.55"
	secretShopTakings  = "44444.44"
	secretPinHash      = "PINHASH-SECRET-9"
	secretCashierName  = "CASHIER-NAME-SENTINEL"
)

var (
	probeCashierID = uuid.MustParse("77777777-7777-7777-7777-777777777777")
	probeTenant    = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	probeProduct   = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	probeWarehouse = uuid.MustParse("33333333-3333-3333-3333-333333333333")
	probePartner   = uuid.MustParse("44444444-4444-4444-4444-444444444444")
	probeSaleBill  = uuid.MustParse("55555555-5555-5555-5555-555555555555")
	probePurBill   = uuid.MustParse("66666666-6666-6666-6666-666666666666")
)

// ---- weekly summary and to-do ----

type stubWeekly struct{}

func (stubWeekly) Execute(context.Context, uuid.UUID) (appdigest.Summary, error) {
	return appdigest.Summary{
		ReplenishCount:     3,
		ReplenishAmountCNY: decimal.RequireFromString(secretReplenishAmt),
		GeneratedAt:        time.Now(),
	}, nil
}

type stubTodoUC struct{}

func (stubTodoUC) Execute(context.Context, uuid.UUID) (apptodo.Summary, error) {
	pid := uuid.New()
	bill := func(no, amt string) apptodo.BillSection {
		return apptodo.BillSection{Count: 1, Items: []apptodo.BillItem{{
			ID: uuid.New(), BillNo: no, PartnerName: "供应商甲",
			TotalAmount: decimal.RequireFromString(amt), BillDate: time.Now(),
		}}}
	}
	return apptodo.Summary{
		DraftPurchases: bill(secretPurchaseNo, secretPurchaseAmt),
		DraftSales:     bill("SL-1", "10.00"),
		OverdueCustomers: apptodo.DebtorSection{Count: 1, Items: []apptodo.DebtorItem{{
			PartnerID: &pid, PartnerName: "老王",
			Overdue: decimal.RequireFromString(secretOverdueAmt), Unpaid: decimal.RequireFromString(secretUnpaidAmt),
		}}},
		UninvoicedSales: bill("SL-2", secretUninvoiced),
		KPIs: apptodo.KPIs{
			TodaySales:       decimal.RequireFromString("20.00"),
			TodayCollected:   decimal.RequireFromString(secretCollectedAmt),
			ReceivableUnpaid: decimal.RequireFromString(secretReceivable),
			MonthUninvoiced:  decimal.RequireFromString(secretUninvoiced),
		},
		Total:   4,
		HasData: true,
	}, nil
}

// ---- products ----

type stubProductRepo struct{ appproduct.Repository }

func probeProductRow() *domainproduct.Product {
	return &domainproduct.Product{ID: probeProduct, TenantID: probeTenant, Code: "P-1", Name: "示例商品"}
}

func (stubProductRepo) GetByID(context.Context, uuid.UUID, uuid.UUID) (*domainproduct.Product, error) {
	return probeProductRow(), nil
}

func (stubProductRepo) List(context.Context, domainproduct.ListFilter) ([]*domainproduct.Product, int, error) {
	return []*domainproduct.Product{probeProductRow()}, 1, nil
}

type stubImporter struct{}

func (stubImporter) Execute(context.Context, appproduct.ImportRequest) (*appproduct.ImportResult, error) {
	return &appproduct.ImportResult{}, nil
}

type stubSKUPrices struct{}

func (stubSKUPrices) ListDefaultSKUs(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]appsku.DefaultSKU, error) {
	out := make([]appsku.DefaultSKU, 0, len(ids))
	for _, id := range ids {
		out = append(out, appsku.DefaultSKU{
			ProductID:      id,
			RetailPrice:    decimal.RequireFromString("19.90"),
			WholesalePrice: decimal.RequireFromString("15.50"),
			PurchasePrice:  decimal.RequireFromString(secretPurchasePx),
		})
	}
	return out, nil
}

// ---- stock ----

type stubStockRepo struct{ appstock.StockRepo }

func probeSnapshot() domainstock.Snapshot {
	return domainstock.Snapshot{
		ID: uuid.New(), TenantID: probeTenant, ProductID: probeProduct, WarehouseID: probeWarehouse,
		OnHandQty: decimal.NewFromInt(8), AvailableQty: decimal.NewFromInt(8),
		UnitCost: decimal.RequireFromString(secretUnitCost), CostStrategy: domainstock.CostStrategyWAC,
		UpdatedAt: time.Now(),
	}
}

func (stubStockRepo) GetSnapshot(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domainstock.Snapshot, error) {
	s := probeSnapshot()
	return &s, nil
}

func (stubStockRepo) ListSnapshots(context.Context, appstock.ListSnapshotsFilter) ([]domainstock.Snapshot, error) {
	return []domainstock.Snapshot{probeSnapshot()}, nil
}

func (stubStockRepo) ListMovements(context.Context, appstock.MovementFilter) ([]domainstock.Movement, error) {
	return []domainstock.Movement{{
		ID: uuid.New(), TenantID: probeTenant, ProductID: probeProduct, WarehouseID: probeWarehouse,
		QtyBase:  decimal.NewFromInt(2),
		UnitCost: decimal.RequireFromString(secretUnitCost), TotalCost: decimal.RequireFromString(secretPurchaseAmt),
		OccurredAt: time.Now(), CreatedAt: time.Now(),
	}}, nil
}

type stubSuggestions struct{}

func (stubSuggestions) ListSuggestions(context.Context, uuid.UUID) ([]appreplenish.RawRow, error) {
	return []appreplenish.RawRow{{
		ProductID: probeProduct, ProductName: "示例商品", ProductCode: "P-1",
		AvailableQty: decimal.NewFromInt(1), SafetyQty: decimal.NewFromInt(10),
		UnitCost:      decimal.RequireFromString(secretUnitCost),
		AvgDailySales: decimal.NewFromInt(2), LeadTimeDays: 7,
	}}, nil
}

// ---- bills ----

type stubBillRepo struct{ appbill.BillRepo }

func probeBill(id uuid.UUID, no string, sub domainbill.BillSubType, total string) domainbill.BillHead {
	t := decimal.RequireFromString(total)
	return domainbill.BillHead{
		ID: id, TenantID: probeTenant, BillNo: no, BillType: domainbill.BillTypeSale, SubType: sub,
		Status: domainbill.StatusApproved, PartnerID: &probePartner,
		Subtotal: t, TotalAmount: t, PaidAmount: decimal.Zero, BillDate: time.Now(), CreatedAt: time.Now(),
	}
}

func probeBills() []domainbill.BillHead {
	return []domainbill.BillHead{
		probeBill(probeSaleBill, "SL-20261011-0001", domainbill.BillSubTypeSale, "120.00"),
		probeBill(probePurBill, secretPurchaseNo, domainbill.BillSubTypePurchase, secretPurchaseAmt),
	}
}

func (stubBillRepo) GetBill(_ context.Context, _, id uuid.UUID) (*domainbill.BillHead, error) {
	for _, b := range probeBills() {
		if b.ID == id {
			return &b, nil
		}
	}
	return nil, appbill.ErrBillNotFound
}

func (stubBillRepo) GetBillItems(_ context.Context, _, id uuid.UUID) ([]*domainbill.BillItem, error) {
	price := "12.00"
	if id == probePurBill {
		price = secretPurchasePx
	}
	return []*domainbill.BillItem{{
		ID: uuid.New(), HeadID: id, ProductID: probeProduct, LineNo: 1,
		Qty: decimal.NewFromInt(10), UnitPrice: decimal.RequireFromString(price), LineAmount: decimal.RequireFromString("120.00"),
	}}, nil
}

// ListBills honours the sub type filter like the real repository, so a sale list
// that forgot to filter would surface the purchase bill planted above.
func (stubBillRepo) ListBills(_ context.Context, f appbill.BillListFilter) ([]domainbill.BillHead, int64, error) {
	var out []domainbill.BillHead
	for _, b := range probeBills() {
		if f.SubType == "" || b.SubType == f.SubType {
			out = append(out, b)
		}
	}
	return out, int64(len(out)), nil
}

func (stubBillRepo) SummarizeBills(_ context.Context, f appbill.BillListFilter) (appbill.BillListSummary, error) {
	if f.SubType == domainbill.BillSubTypePurchase {
		return appbill.BillListSummary{Count: 1, TotalAmount: decimal.RequireFromString(secretPurchaseAmt)}, nil
	}
	return appbill.BillListSummary{Count: 1, TotalAmount: decimal.RequireFromString("120.00")}, nil
}

// POSSummary gives the shop-wide takings only when no person is narrowed to.
func (stubBillRepo) POSSummary(_ context.Context, q appbill.POSSummaryRange) (appbill.POSSummary, error) {
	total := "50.00"
	if q.CreatorID == nil {
		total = secretShopTakings
	}
	t := decimal.RequireFromString(total)
	return appbill.POSSummary{Count: 1, TotalAmount: t, PaidAmount: t}, nil
}

// POSCashiers is the per-person split only the boss gets; the creator id is a fixed probe one.
func (stubBillRepo) POSCashiers(_ context.Context, _ appbill.POSSummaryRange) ([]appbill.POSCashierTotal, error) {
	t := decimal.RequireFromString("30.00")
	return []appbill.POSCashierTotal{{CreatorID: probeCashierID, Count: 1, TotalAmount: t, PaidAmount: t}}, nil
}

// stubMemberNames names the probe cashier with the sentinel.
type stubMemberNames struct{}

func (stubMemberNames) MemberNames(_ context.Context, _ uuid.UUID) (map[uuid.UUID]string, error) {
	return map[uuid.UUID]string{probeCashierID: secretCashierName}, nil
}

// LastPrices answers per sub type: the purchase history carries the secret price.
func (stubBillRepo) LastPrices(_ context.Context, q appbill.LastPriceQuery) ([]appbill.LastPrice, error) {
	price := "18.00"
	if q.SubType == string(domainbill.BillSubTypePurchase) {
		price = secretPurchasePx
	}
	return []appbill.LastPrice{{
		ProductID: probeProduct, UnitPrice: decimal.RequireFromString(price), Factor: decimal.NewFromInt(1),
		BillDate: time.Now(), Source: appbill.LastPriceSourceLast,
	}}, nil
}

type stubPaymentRepo struct{ apppayment.PaymentRepo }

func (stubPaymentRepo) ListByBill(context.Context, uuid.UUID, uuid.UUID) ([]*domainpayment.Payment, error) {
	return nil, nil
}

// ---- partners and projects ----

type stubCustomerRepo struct{ appcust.Repository }

func (stubCustomerRepo) GetByID(context.Context, uuid.UUID, uuid.UUID) (*domaincust.Customer, error) {
	return &domaincust.Customer{ID: probePartner, TenantID: probeTenant, Name: "老王"}, nil
}

func (stubCustomerRepo) List(context.Context, domaincust.ListFilter) ([]*domaincust.Customer, int, error) {
	return []*domaincust.Customer{{ID: probePartner, TenantID: probeTenant, Name: "老王"}}, 1, nil
}

// stubPartnerImporter satisfies the customer and supplier Importer interfaces; the
// router tests only probe the template download, which never reaches it.
type stubPartnerImporter struct{}

func (stubPartnerImporter) Execute(context.Context, uuid.UUID, []byte) (*partnerimport.Result, error) {
	return &partnerimport.Result{}, nil
}

type stubSupplierRepo struct{ appsupp.Repository }

func (stubSupplierRepo) GetByID(context.Context, uuid.UUID, uuid.UUID) (*domainsupp.Supplier, error) {
	return &domainsupp.Supplier{ID: probePartner, TenantID: probeTenant, Name: "供应商甲"}, nil
}

func (stubSupplierRepo) List(context.Context, domainsupp.ListFilter) ([]*domainsupp.Supplier, int, error) {
	return []*domainsupp.Supplier{{ID: probePartner, TenantID: probeTenant, Name: "供应商甲"}}, 1, nil
}

type stubProjectRepo struct{ appproject.Repository }

func probeProjectRow() *domainproject.Project {
	return &domainproject.Project{ID: probePartner, TenantID: probeTenant, Code: "PJ-1", Name: "示例工程"}
}

func (stubProjectRepo) GetByID(context.Context, uuid.UUID, uuid.UUID) (*domainproject.Project, error) {
	return probeProjectRow(), nil
}

func (stubProjectRepo) List(context.Context, domainproject.ListFilter) ([]*domainproject.Project, int, error) {
	return []*domainproject.Project{probeProjectRow()}, 1, nil
}

// ---- master data and own-account reads ----

type stubMe struct{}

func (stubMe) Execute(context.Context, apptenant.GetMeInput) (*apptenant.GetMeOutput, error) {
	return &apptenant.GetMeOutput{UserSub: "probe-sub", DisplayName: "我的名字", Role: "clerk", RoleLabel: "店员"}, nil
}

type stubWarehouseRepo struct{ appwarehouse.Repository }

func probeWarehouseRow() *domainwarehouse.Warehouse {
	return &domainwarehouse.Warehouse{ID: probeWarehouse, TenantID: probeTenant, Code: "W-1", Name: "主仓库", IsDefault: true}
}

func (stubWarehouseRepo) GetByID(context.Context, uuid.UUID, uuid.UUID) (*domainwarehouse.Warehouse, error) {
	return probeWarehouseRow(), nil
}

func (stubWarehouseRepo) List(context.Context, domainwarehouse.ListFilter) ([]*domainwarehouse.Warehouse, int, error) {
	return []*domainwarehouse.Warehouse{probeWarehouseRow()}, 1, nil
}

type stubUnitRepo struct{ appunit.Repository }

func (stubUnitRepo) List(context.Context, domainunit.ListFilter) ([]*domainunit.UnitDef, error) {
	return []*domainunit.UnitDef{{ID: uuid.New(), TenantID: probeTenant, Code: "kg", Name: "公斤", IsSystem: true}}, nil
}

type stubProductUnits struct{}

func (stubProductUnits) ProductUnits(context.Context, uuid.UUID, uuid.UUID) ([]repounit.ProductUnit, error) {
	return []repounit.ProductUnit{{ID: uuid.New(), Code: "box", Name: "整箱", Factor: decimal.NewFromInt(12)}}, nil
}

type stubCurrencyRepo struct{ appcurrency.CurrencyRepo }

func (stubCurrencyRepo) ListCurrencies(context.Context) ([]domaincurrency.Currency, error) {
	return []domaincurrency.Currency{{Code: "CNY", Name: "人民币", Symbol: "¥", Enabled: true}}, nil
}

func (stubCurrencyRepo) GetRateOn(context.Context, uuid.UUID, string, string, time.Time) (*domaincurrency.ExchangeRate, error) {
	return &domaincurrency.ExchangeRate{FromCurrency: "USD", ToCurrency: "CNY", Rate: decimal.RequireFromString("7.13"), Source: "manual"}, nil
}

func (stubCurrencyRepo) ListRateHistory(context.Context, uuid.UUID, string, string, int) ([]domaincurrency.ExchangeRate, error) {
	return []domaincurrency.ExchangeRate{{FromCurrency: "USD", ToCurrency: "CNY", Rate: decimal.RequireFromString("7.14"), Source: "manual"}}, nil
}

type stubDictRepo struct{ apphort.Repository }

func probeDictRow() *domainhort.NurseryDict {
	return &domainhort.NurseryDict{ID: probePartner, TenantID: probeTenant, Name: "红叶石楠", Type: domainhort.NurseryType("shrub")}
}

func (stubDictRepo) GetByID(context.Context, uuid.UUID, uuid.UUID) (*domainhort.NurseryDict, error) {
	return probeDictRow(), nil
}

func (stubDictRepo) List(context.Context, domainhort.ListFilter) ([]*domainhort.NurseryDict, int, error) {
	return []*domainhort.NurseryDict{probeDictRow()}, 1, nil
}

type stubSessionRepo struct{ appacct.SessionRepo }

func (stubSessionRepo) List(context.Context, uuid.UUID, string) ([]*domainacct.Session, error) {
	return []*domainacct.Session{{ID: uuid.New(), UserAgent: "probe-browser", CreatedAt: time.Now(), LastActive: time.Now()}}, nil
}

type stubProfileRepo struct{ appacct.ProfileRepo }

func (stubProfileRepo) Get(context.Context, uuid.UUID, string) (*domainacct.Profile, error) {
	return &domainacct.Profile{DisplayName: "我的档案", Phone: "13800000000"}, nil
}

func (stubProfileRepo) GetAvatar(context.Context, uuid.UUID, string) (string, []byte, error) {
	return "image/png", []byte("AVATAR-BYTES"), nil
}

type stubPrintRepo struct{ appacct.PrintSettingsRepo }

func (stubPrintRepo) GetPrint(context.Context, uuid.UUID) (appacct.PrintSettings, error) {
	return appacct.PrintSettings{ShopName: "示例小店", ReceiptFooter: "欢迎再来"}, nil
}

type stubCashierRepo struct{ appacct.CashierModeRepo }

func (stubCashierRepo) GetCashier(context.Context, uuid.UUID) (appacct.CashierRecord, error) {
	return appacct.CashierRecord{Enabled: true, PinHash: secretPinHash, Salt: secretPinHash}, nil
}

// ---- search (real repository over sqlmock) ----

// searchMatcher compares the statement the repository really issues: the two bill
// tags demand the purchase-hiding filter to be present or absent in the SQL text.
func searchMatcher(expected, actual string) error {
	switch expected {
	case "BILLS_HIDDEN":
		if strings.Contains(actual, "tally.bill_head") && strings.Contains(actual, "NOT LIKE '采购%'") {
			return nil
		}
		return fmt.Errorf("bill search must drop purchase bills, got: %s", actual)
	case "BILLS_OPEN":
		if strings.Contains(actual, "tally.bill_head") && !strings.Contains(actual, "NOT LIKE") {
			return nil
		}
		return fmt.Errorf("bill search must not filter for a role with cost.view, got: %s", actual)
	}
	if strings.Contains(actual, expected) {
		return nil
	}
	return fmt.Errorf("query %q does not contain %q", actual, expected)
}

// probeEnv is the router under test plus the mock behind the search repository.
type probeEnv struct {
	engine *gin.Engine
	mock   sqlmock.Sqlmock
}

// expectSearch queues the four statements of one search. The purchase bill row
// only comes back when the statement does not filter purchases out, as in the real
// database.
func (e *probeEnv) expectSearch(canSeeCost bool) {
	cols := []string{"id", "label", "sublabel"}
	for _, tag := range []string{"tally.product", "tally.supplier", "tally.partner"} {
		e.mock.ExpectQuery(tag).WillReturnRows(sqlmock.NewRows(cols).AddRow(uuid.NewString(), "示例", "X-1"))
	}
	rows := sqlmock.NewRows(cols).AddRow(probeSaleBill.String(), "SL-20261011-0001", "出库 · 销售")
	tag := "BILLS_HIDDEN"
	if canSeeCost {
		rows.AddRow(probePurBill.String(), secretPurchaseNo, "入库 · 采购")
		tag = "BILLS_OPEN"
	}
	e.mock.ExpectQuery(tag).WillReturnRows(rows)
}

// newProbeEnv builds the real route table with every optional module mounted the
// way lifecycle mounts it (print settings, cashier mode, receipts, team, last
// prices, receivables, purchase returns, to-do), so the GET enumeration sees the
// routes a deployed shop has. Handlers that can carry money run real use cases
// over stub repositories planted with sentinels.
func newProbeEnv() *probeEnv { return newProbeEnvGuarded(nil) }

// newProbeEnvGuarded is newProbeEnv with a subscription guard mounted the way
// lifecycle mounts it: on the main /api/v1 group and on the Shopify admin group.
func newProbeEnvGuarded(guard gin.HandlerFunc) *probeEnv {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(searchMatcher)))
	if err != nil {
		panic(err)
	}

	authMW := func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, probeTenant)
		c.Set(middleware.CtxKeyIDPSubject, "probe-sub")
		c.Set(middleware.CtxKeyShopRole, team.Role(c.GetHeader("X-Test-Role")))
		c.Next()
	}
	pass := func(c *gin.Context) { c.Next() }

	billRepo := stubBillRepo{}
	stockRepo := stubStockRepo{}

	ph := handlerproduct.New(nil, appproduct.NewListUseCase(stubProductRepo{}), appproduct.NewGetUseCase(stubProductRepo{}), nil, nil, nil).
		WithPriceLookup(stubSKUPrices{}).WithImporter(stubImporter{})
	sh := handlerstock.New(nil, appstock.NewGetSnapshotUseCase(stockRepo), appstock.NewListSnapshotsUseCase(stockRepo),
		appstock.NewListMovementsUseCase(stockRepo), appreplenish.NewListLowStockUseCase(stubSuggestions{})).
		WithAdjust(appstock.NewAdjustUseCase(nil, nil)).
		WithExtraRoutes(ph.RegisterImportRoutes).
		WithExtraRoutes(handlerprint.New(appprint.New(nil, nil, nil, nil)).RegisterRoutes)
	bh := handlerbill.New(nil, nil, nil, nil, appbill.NewListPurchasesUseCase(billRepo), appbill.NewGetPurchaseUseCase(billRepo), nil).
		WithReceivables(appbill.NewReceivablesUseCase(nil)).
		WithPurchaseReturn(appbill.NewPurchaseReturnUseCase(nil, nil)).
		WithLastPrices(appbill.NewLastPricesUseCase(billRepo).WithDefaultPrices(stubSKUPrices{}))
	saleh := handlerbill.NewSaleHandler(nil, nil, nil, appbill.NewListPurchasesUseCase(billRepo), billRepo, nil,
		apppayment.NewListPaymentsUseCase(stubPaymentRepo{})).WithMemberNames(stubMemberNames{})
	payh := handlerpayment.New(nil, nil).WithReceipts(apppayment.NewReceiptUseCase(nil, nil, nil), apppayment.NewStatementUseCase(nil))
	acct := handleracct.New(appacct.NewListSessions(stubSessionRepo{}), nil, nil, appacct.NewGetProfile(stubProfileRepo{}), nil, nil, appacct.NewGetAvatar(stubProfileRepo{})).
		WithPrintSettings(appacct.NewGetPrintSettings(stubPrintRepo{}), appacct.NewPutPrintSettings(nil)).
		WithCashierMode(appacct.NewGetCashierMode(stubCashierRepo{}), appacct.NewSetCashierMode(nil))
	ah := handlerAuth.New(nil, stubMe{}).WithTeam(handlerteam.New(nil))
	srch := handlersearch.New(appsearch.NewSearchEntitiesUseCase(repoSearch.New(db)))
	custh := handlercust.New(nil, appcust.NewGetByIDUseCase(stubCustomerRepo{}), appcust.NewListUseCase(stubCustomerRepo{}), nil, nil, nil).WithImporter(stubPartnerImporter{})
	suph := handlersupp.New(nil, appsupp.NewGetByIDUseCase(stubSupplierRepo{}), appsupp.NewListUseCase(stubSupplierRepo{}), nil, nil, nil).WithImporter(stubPartnerImporter{})
	projh := handlerproject.NewProjectHandler(nil, appproject.NewGetByIDUseCase(stubProjectRepo{}), appproject.NewListUseCase(stubProjectRepo{}), nil, nil, nil)
	unith := handlerunit.New(nil, appunit.NewListUseCase(stubUnitRepo{}), nil).WithProductUnits(stubProductUnits{})
	curh := handlercurrency.New(appcurrency.NewListCurrenciesUseCase(stubCurrencyRepo{}), appcurrency.NewGetRateUseCase(stubCurrencyRepo{}),
		nil, appcurrency.NewListRateHistoryUseCase(stubCurrencyRepo{}))
	dicth := handlerhorticulture.NewDictHandler(nil, apphort.NewGetByIDUseCase(stubDictRepo{}), apphort.NewListUseCase(stubDictRepo{}), nil, nil, nil)
	whh := handlerwarehouse.New(nil, appwarehouse.NewGetByIDUseCase(stubWarehouseRepo{}), appwarehouse.NewListUseCase(stubWarehouseRepo{}), nil, nil, nil)
	digh := handlerdigest.New(stubWeekly{}).WithTodo(stubTodoUC{}).WithAIEnabled(func() bool { return false })

	var guards []gin.HandlerFunc
	if guard != nil {
		guards = append(guards, guard)
	}
	engine := New(health.New("dev"), authMW, pass, pass,
		ph, unith, ah, handlerAuth.NewPATHandler(nil), sh, bh,
		curh, saleh, payh, handlerbilling.New(nil, nil, nil), nil,
		dicth, projh, handlermetrics.NewMetricsHandler(""),
		suph, whh, handlerexport.New(nil, nil, nil, nil), acct,
		handlerreplenish.New(nil), handlerreports.New(nil), srch, handlerimporting.New(nil, uuid.Nil), digh,
		handleronboarding.NewForTest(nil, nil), custh, handlerinvoice.New(nil, nil, nil, nil, nil), guards...)
	// The Shopify binding routes hang off their own group in lifecycle, outside router.New.
	shopifyGroup := engine.Group("/api/v1")
	if guard != nil {
		shopifyGroup.Use(authMW, guard)
	}
	handlershopify.New(nil, nil, nil).RegisterRoutes(shopifyGroup)
	// Likewise the operation log.
	handleraudit.New(nil).RegisterRoutes(engine.Group("/api/v1"))
	return &probeEnv{engine: engine, mock: mock}
}
