package product

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"golang.org/x/text/encoding/simplifiedchinese"

	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/codegen"
)

type fakeCatalog struct {
	codes, barcodes, nameSpecs map[string]bool
}

func (f fakeCatalog) ExistingCodes(_ context.Context, _ uuid.UUID, in []string) (map[string]bool, error) {
	return pick(f.codes, in), nil
}
func (f fakeCatalog) ExistingBarcodes(_ context.Context, _ uuid.UUID, in []string) (map[string]bool, error) {
	return pick(f.barcodes, in), nil
}
func (f fakeCatalog) ExistingNameSpecs(_ context.Context, _ uuid.UUID, in []string) (map[string]bool, error) {
	out := map[string]bool{}
	for k := range f.nameSpecs {
		for _, n := range in {
			if strings.HasPrefix(k, strings.ToLower(n)+"\x00") {
				out[k] = true
			}
		}
	}
	return out, nil
}

func pick(set map[string]bool, in []string) map[string]bool {
	out := map[string]bool{}
	for _, s := range in {
		if set[s] {
			out[s] = true
		}
	}
	return out
}

type fakeUnits struct {
	known   map[string]uuid.UUID
	created []string
}

func (f *fakeUnits) UnitIDsByName(context.Context, uuid.UUID) (map[string]uuid.UUID, error) {
	m := map[string]uuid.UUID{}
	for k, v := range f.known {
		m[k] = v
	}
	return m, nil
}

func (f *fakeUnits) CreateUnit(_ context.Context, _ uuid.UUID, name string) (uuid.UUID, error) {
	f.created = append(f.created, name)
	return uuid.New(), nil
}

type fakeWarehouses struct {
	byName map[string]uuid.UUID
	def    uuid.UUID
}

func (f fakeWarehouses) WarehousesByName(context.Context, uuid.UUID) (map[string]uuid.UUID, uuid.UUID, error) {
	return f.byName, f.def, nil
}

type fakeOpening struct {
	got []OpeningStockInput
	err error
}

func (f *fakeOpening) RecordOpening(_ context.Context, in OpeningStockInput) error {
	if f.err != nil {
		return f.err
	}
	f.got = append(f.got, in)
	return nil
}

// recordingRepo wraps fakeRepo to keep every created product.
type recordingRepo struct {
	fakeRepo
	created []*domain.Product
}

func (r *recordingRepo) Create(ctx context.Context, p *domain.Product) error {
	r.created = append(r.created, p)
	return r.fakeRepo.Create(ctx, p)
}

type importHarness struct {
	uc      *ImportUseCase
	repo    *recordingRepo
	skus    *fakeSKUWriter
	units   *fakeUnits
	opening *fakeOpening
	tx      *fakeTx
	wh      uuid.UUID
}

func newImportHarness(cat fakeCatalog) *importHarness {
	h := &importHarness{repo: &recordingRepo{}, skus: &fakeSKUWriter{}, units: &fakeUnits{known: map[string]uuid.UUID{"个": uuid.New()}},
		opening: &fakeOpening{}, tx: &fakeTx{}, wh: uuid.New()}
	create := NewCreateUseCase(h.repo).WithPriceWriter(h.skus).WithTx(h.tx)
	src := codegen.Source{Now: func() time.Time { return time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC) }, Rand: &countingReader{}}
	h.uc = NewImportUseCase(create, cat, h.units, fakeWarehouses{byName: map[string]uuid.UUID{"主仓": h.wh}, def: h.wh}, h.opening, h.tx).WithCodeSource(src)
	return h
}

// countingReader yields a distinct base-32 number per 4-byte block, so generated
// codes never collide unless the use case itself replays a block.
type countingReader struct{ n uint32 }

func (c *countingReader) Read(p []byte) (int, error) {
	for i := 0; i+4 <= len(p); i += 4 {
		for j := 0; j < 4; j++ {
			p[i+j] = byte((c.n >> (5 * uint(j))) & 31)
		}
		c.n++
	}
	return len(p), nil
}

const csvHead = "名称*,编码,条码,规格,单位名,零售价,批发价,进价,期初数量,期初成本,仓库\n"

func TestImport_DryRun_ReportsAndWritesNothing(t *testing.T) {
	h := newImportHarness(fakeCatalog{})
	data := csvHead + "螺丝,L1,690111,M4,盒,1.5,1.2,0.8,10,0.9,\n水管,,,,个,20,,,,,\n"
	res, err := h.uc.Execute(context.Background(), ImportRequest{TenantID: uuid.New(), Data: []byte(data), DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || res.Total != 2 || res.Valid != 2 || res.Invalid != 0 || res.Imported != 0 {
		t.Fatalf("res = %+v", res)
	}
	if len(h.repo.created) != 0 || h.skus.calls != 0 || len(h.opening.got) != 0 || len(h.units.created) != 0 || h.tx.opened != 0 {
		t.Fatal("dry run must not write anything")
	}
	if len(res.CreatedUnits) != 1 || res.CreatedUnits[0] != "盒" {
		t.Fatalf("dry run should announce the unit that would be created, got %v", res.CreatedUnits)
	}
}

func TestImport_GBKFile_CommitsProductPricesBarcodeUnitAndOpeningStock(t *testing.T) {
	h := newImportHarness(fakeCatalog{})
	gbk, err := simplifiedchinese.GBK.NewEncoder().String(csvHead + "螺丝,L1,690111,M4,盒,1.5,1.2,0.8,10,0.9,主仓\n")
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.uc.Execute(context.Background(), ImportRequest{TenantID: uuid.New(), Data: []byte(gbk)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 1 || res.Rows[0].Code != "L1" || !res.Rows[0].Imported {
		t.Fatalf("res = %+v", res)
	}
	p := h.repo.created[0]
	var attrs map[string]string
	_ = json.Unmarshal(p.Attributes, &attrs)
	if p.Name != "螺丝" || p.Spec != "M4" || attrs["barcode"] != "690111" || p.DefaultUnitID == nil {
		t.Fatalf("product = %+v attrs=%s", p, p.Attributes)
	}
	if len(h.units.created) != 1 || h.units.created[0] != "盒" || len(res.CreatedUnits) != 1 {
		t.Fatalf("units created = %v", h.units.created)
	}
	if h.skus.calls != 1 || h.skus.prices.Retail.String() != "1.5" || h.skus.prices.Wholesale.String() != "1.2" || h.skus.prices.Purchase.String() != "0.8" {
		t.Fatalf("sku prices = %+v", h.skus.prices)
	}
	if len(h.opening.got) != 1 {
		t.Fatalf("opening stock = %+v", h.opening.got)
	}
	o := h.opening.got[0]
	if o.ProductID != p.ID || o.WarehouseID != h.wh || !o.Qty.Equal(decimal.NewFromInt(10)) || o.UnitCost.String() != "0.9" {
		t.Fatalf("opening = %+v", o)
	}
	if h.tx.opened != 1 {
		t.Fatalf("product, sku and stock must share one tx per row, opened=%d", h.tx.opened)
	}
}

func TestImport_OpeningCostFallsBackToPurchasePrice(t *testing.T) {
	h := newImportHarness(fakeCatalog{})
	data := csvHead + "A,,,,个,5,,3.5,4,,\n"
	if _, err := h.uc.Execute(context.Background(), ImportRequest{TenantID: uuid.New(), Data: []byte(data)}); err != nil {
		t.Fatal(err)
	}
	if got := h.opening.got[0].UnitCost.String(); got != "3.5" {
		t.Fatalf("cost = %s, want purchase price 3.5", got)
	}
}

func TestImport_UTF8WithBOM_AndNoOpeningStockWhenQtyBlank(t *testing.T) {
	h := newImportHarness(fakeCatalog{})
	data := "\xEF\xBB\xBF" + csvHead + "A,,,,个,5,,,,,\n"
	res, err := h.uc.Execute(context.Background(), ImportRequest{TenantID: uuid.New(), Data: []byte(data)})
	if err != nil || res.Imported != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(h.opening.got) != 0 {
		t.Fatal("no quantity, no stock movement")
	}
	if !strings.HasPrefix(res.Rows[0].Code, "P") {
		t.Fatalf("generated code = %q", res.Rows[0].Code)
	}
}

func TestImport_DuplicatesAgainstDBAndInsideFile_ReportedInChinese(t *testing.T) {
	h := newImportHarness(fakeCatalog{
		codes:     map[string]bool{"L1": true},
		barcodes:  map[string]bool{"6902": true},
		nameSpecs: map[string]bool{NameSpecKey("水管", ""): true},
	})
	data := csvHead +
		"a,L1,,,,,,,,,\n" + // existing code
		"b,L2,6902,,,,,,,,\n" + // existing barcode
		"c,L3,,,,,,,,,\n" +
		"d,L3,,,,,,,,,\n" + // duplicate of line 4 inside the file
		"水管,,,,,,,,,,\n" + // same name+spec already exists
		"e,,,,,,,,,,\n" +
		"e,,,,,,,,,,\n" // same name+spec twice in file
	res, err := h.uc.Execute(context.Background(), ImportRequest{TenantID: uuid.New(), Data: []byte(data), DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]string{2: "编码「L1」", 3: "条码「6902」", 5: "和第 4 行重复", 6: "同名同规格", 8: "第 7 行"}
	for line, frag := range want {
		var row *ImportRow
		for i := range res.Rows {
			if res.Rows[i].Line == line {
				row = &res.Rows[i]
			}
		}
		if row == nil || row.Status != "error" || !strings.Contains(strings.Join(row.Errors, ";"), frag) {
			t.Errorf("line %d: want error containing %q, got %+v", line, frag, row)
		}
	}
	if res.Valid != 2 || res.Invalid != 5 {
		t.Fatalf("valid=%d invalid=%d", res.Valid, res.Invalid)
	}
}

func TestImport_SecondRunOfSameFile_IsAllDuplicates(t *testing.T) {
	cat := fakeCatalog{codes: map[string]bool{"L1": true, "L2": true}}
	h := newImportHarness(cat)
	data := csvHead + "a,L1,,,,,,,,,\nb,L2,,,,,,,,,\n"
	res, _ := h.uc.Execute(context.Background(), ImportRequest{TenantID: uuid.New(), Data: []byte(data), DryRun: true})
	if res.Invalid != 2 || res.Valid != 0 || len(h.repo.created) != 0 {
		t.Fatalf("res = %+v", res)
	}
}

func TestImport_RowValidation(t *testing.T) {
	h := newImportHarness(fakeCatalog{})
	long := strings.Repeat("长", 129)
	data := csvHead +
		",C1,,,,,,,,,\n" + // missing name
		"x,,,,,abc,,,,,\n" + // bad price
		"x2,,,,,-1,,,,,\n" + // negative price
		"y,,,,,,,,-3,,\n" + // negative qty
		"z,,,,,,,,2,,没有的仓\n" + // unknown warehouse
		long + ",,,,,,,,,,\n" + // name too long
		"b,,6.90E+12,,,,,,,,\n" // excel scientific barcode
	res, err := h.uc.Execute(context.Background(), ImportRequest{TenantID: uuid.New(), Data: []byte(data), DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Invalid != 7 {
		t.Fatalf("invalid = %d rows=%+v", res.Invalid, res.Rows)
	}
	for _, r := range res.Rows {
		msg := strings.Join(r.Errors, ";")
		if msg == "" || strings.ContainsAny(msg, "abcdefghijklmnopqrstuvwxyz") && !strings.Contains(msg, "CSV") {
			t.Errorf("line %d error must be plain Chinese: %q", r.Line, msg)
		}
	}
}

func TestImport_NoWarehouse_OpeningQtyIsRowError(t *testing.T) {
	h := newImportHarness(fakeCatalog{})
	h.uc.warehouses = fakeWarehouses{byName: map[string]uuid.UUID{}, def: uuid.Nil}
	res, _ := h.uc.Execute(context.Background(), ImportRequest{TenantID: uuid.New(), Data: []byte(csvHead + "a,,,,,,,,5,,\nb,,,,,,,,,,\n"), DryRun: true})
	if res.Invalid != 1 || res.Valid != 1 {
		t.Fatalf("res = %+v", res)
	}
}

func TestImport_OpeningStockFailure_RollsBackRowOnly(t *testing.T) {
	h := newImportHarness(fakeCatalog{})
	h.opening.err = errors.New("db down")
	res, err := h.uc.Execute(context.Background(), ImportRequest{TenantID: uuid.New(), Data: []byte(csvHead + "a,,,,,,,,5,,\nb,,,,,,,,,,\n")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 1 || res.Invalid != 1 || res.Rows[0].Status != "error" || res.Rows[1].Status != "ok" {
		t.Fatalf("res = %+v", res)
	}
	if h.tx.errs[0] == nil || h.tx.errs[1] != nil {
		t.Fatalf("row 1 tx must fail and row 2 succeed: %v", h.tx.errs)
	}
	if strings.Contains(strings.Join(res.Rows[0].Errors, ""), "db down") {
		t.Fatal("internal error text must not leak")
	}
}

func TestImport_FileLevelErrors(t *testing.T) {
	h := newImportHarness(fakeCatalog{})
	cases := map[string]string{
		"empty":      "",
		"noname":     "编码,条码\nA,1\n",
		"headeronly": csvHead,
	}
	for name, data := range cases {
		_, err := h.uc.Execute(context.Background(), ImportRequest{TenantID: uuid.New(), Data: []byte(data), DryRun: true})
		var ie *ImportError
		if !errors.As(err, &ie) {
			t.Errorf("%s: want *ImportError, got %v", name, err)
		}
	}
}

func TestImport_TooManyRows(t *testing.T) {
	h := newImportHarness(fakeCatalog{})
	var b bytes.Buffer
	b.WriteString(csvHead)
	for i := 0; i <= MaxImportRows; i++ {
		fmt.Fprintf(&b, "n%d,C%d,,,,,,,,,\n", i, i)
	}
	_, err := h.uc.Execute(context.Background(), ImportRequest{TenantID: uuid.New(), Data: b.Bytes(), DryRun: true})
	var ie *ImportError
	if !errors.As(err, &ie) {
		t.Fatalf("err = %v", err)
	}
}

func TestImport_ThousandRows_GeneratedCodesAllDistinct(t *testing.T) {
	h := newImportHarness(fakeCatalog{})
	var b bytes.Buffer
	b.WriteString(csvHead)
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&b, "商品%d,,,,个,%d,,,5,1,\n", i, i+1)
	}
	res, err := h.uc.Execute(context.Background(), ImportRequest{TenantID: uuid.New(), Data: b.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 1000 || len(h.opening.got) != 1000 {
		t.Fatalf("imported=%d opening=%d", res.Imported, len(h.opening.got))
	}
	seen := map[string]bool{}
	for _, r := range res.Rows {
		if seen[r.Code] {
			t.Fatalf("duplicate generated code %s", r.Code)
		}
		seen[r.Code] = true
	}
}

func TestImport_PriceErrorIsPlainChinese(t *testing.T) {
	var pe *appsku.PriceError
	_, err := appsku.ParsePrices(appsku.PriceInput{Retail: strp("x")})
	if !errors.As(err, &pe) {
		t.Fatal("sentinel expected")
	}
}

func strp(s string) *string { return &s }

func TestImport_GeneratedCodeCollidingWithExistingProductIsRedrawn(t *testing.T) {
	first, err := codegen.Generate(codegen.PrefixProduct, time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC), bytes.NewReader([]byte{0, 0, 0, 0}))
	if err != nil {
		t.Fatal(err)
	}
	h := newImportHarness(fakeCatalog{codes: map[string]bool{first: true}})
	res, err := h.uc.Execute(context.Background(), ImportRequest{TenantID: uuid.New(), Data: []byte(csvHead + "a,,,,,,,,,,\n")})
	if err != nil || res.Imported != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if res.Rows[0].Code == first {
		t.Fatal("colliding generated code must be redrawn")
	}
}

func TestImport_ExcelWorkbookIsRejectedWithHint(t *testing.T) {
	h := newImportHarness(fakeCatalog{})
	_, err := h.uc.Execute(context.Background(), ImportRequest{TenantID: uuid.New(), Data: []byte("PK\x03\x04rest"), DryRun: true})
	var ie *ImportError
	if !errors.As(err, &ie) || !strings.Contains(ie.Msg, "CSV") {
		t.Fatalf("err = %v", err)
	}
}

func TestImport_TemplateExampleRow_IsRejected_AndWrappedBarcodeUnwrapped(t *testing.T) {
	h := newImportHarness(fakeCatalog{})
	data := csvHead +
		"示例：不锈钢螺丝,,\"=\"\"6901234567890\"\"\",M4,个,1,,,100,,\n" +
		"真商品,,\"=\"\"6901234567891\"\"\",,个,1,,,,,\n"
	res, err := h.uc.Execute(context.Background(), ImportRequest{TenantID: uuid.New(), Data: []byte(data)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 1 || res.Rows[0].Status != "error" || !strings.Contains(strings.Join(res.Rows[0].Errors, ""), "示例行") {
		t.Fatalf("res = %+v", res)
	}
	if len(h.repo.created) != 1 || !strings.Contains(string(h.repo.created[0].Attributes), `"6901234567891"`) {
		t.Fatalf("barcode not unwrapped: %+v", h.repo.created)
	}
}

type lockCatalog struct {
	fakeCatalog
	locked, released *int
}

func (l lockCatalog) LockImport(context.Context, uuid.UUID) (func(), error) {
	*l.locked++
	return func() { *l.released++ }, nil
}

func TestImport_RealRunTakesTenantLock_DryRunDoesNot(t *testing.T) {
	var locked, released int
	h := newImportHarness(fakeCatalog{})
	h.uc.catalog = lockCatalog{locked: &locked, released: &released}
	data := csvHead + "水管,,,,个,20,,,,,\n"
	if _, err := h.uc.Execute(context.Background(), ImportRequest{TenantID: uuid.New(), Data: []byte(data), DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if locked != 0 {
		t.Fatal("dry run must not lock")
	}
	if _, err := h.uc.Execute(context.Background(), ImportRequest{TenantID: uuid.New(), Data: []byte(data)}); err != nil {
		t.Fatal(err)
	}
	if locked != 1 || released != 1 {
		t.Fatalf("locked=%d released=%d", locked, released)
	}
}
