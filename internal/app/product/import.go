package product

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"golang.org/x/text/encoding/simplifiedchinese"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/entitlement"
	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/codegen"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

// MaxImportRows caps one file so a single request stays a bounded unit of work: rows
// are written one transaction each and the request has a 30 s deadline.
const MaxImportRows = 2000

// exampleRowPrefixes mark the sample row of the downloaded template.
var exampleRowPrefixes = []string{"示例：", "示例:"}

// ImportLocker serialises real imports of one tenant, so two uploads of the same file
// cannot both pass the duplicate check and then both write. The lock is released by the
// returned func. Catalogs that do not implement it are used without locking.
type ImportLocker interface {
	LockImport(ctx context.Context, tenantID uuid.UUID) (release func(), err error)
}

// ImportError is a whole-file failure (unreadable file, missing name column, too many
// rows) with a plain Chinese message; per-row problems are reported in ImportResult.
type ImportError struct{ Msg string }

func (e *ImportError) Error() string { return e.Msg }

// ImportCatalog answers which identifiers already exist among the tenant's active products.
type ImportCatalog interface {
	ExistingCodes(ctx context.Context, tenantID uuid.UUID, codes []string) (map[string]bool, error)
	ExistingBarcodes(ctx context.Context, tenantID uuid.UUID, barcodes []string) (map[string]bool, error)
	// ExistingNameSpecs keys are NameSpecKey(name, spec).
	ExistingNameSpecs(ctx context.Context, tenantID uuid.UUID, names []string) (map[string]bool, error)
}

// ImportUnits resolves unit names (system + tenant units, keyed by lower-cased name).
type ImportUnits interface {
	UnitIDsByName(ctx context.Context, tenantID uuid.UUID) (map[string]uuid.UUID, error)
	CreateUnit(ctx context.Context, tenantID uuid.UUID, name string) (uuid.UUID, error)
}

// ImportWarehouses resolves warehouse names; DefaultID is uuid.Nil when the tenant has none.
type ImportWarehouses interface {
	WarehousesByName(ctx context.Context, tenantID uuid.UUID) (byName map[string]uuid.UUID, defaultID uuid.UUID, err error)
}

// OpeningStockInput is one opening balance line (stock movement reference_type=init).
type OpeningStockInput struct {
	TenantID    uuid.UUID
	ProductID   uuid.UUID
	WarehouseID uuid.UUID
	Qty         decimal.Decimal
	UnitCost    decimal.Decimal
}

// OpeningStock books an opening balance through InventoryCalculator.ApplyMovement. It
// must join the transaction carried by ctx so the product and its stock commit together.
type OpeningStock interface {
	RecordOpening(ctx context.Context, in OpeningStockInput) error
}

// ImportRequest is one upload. DryRun validates and reports without writing anything.
type ImportRequest struct {
	TenantID uuid.UUID
	Data     []byte
	DryRun   bool
}

// ImportRow is the outcome for one data row (Line is the 1-based line in the file,
// the header being line 1). Status is "ok" or "error".
type ImportRow struct {
	Line     int      `json:"line"`
	Name     string   `json:"name"`
	Code     string   `json:"code,omitempty"`
	Status   string   `json:"status"`
	Errors   []string `json:"errors,omitempty"`
	Imported bool     `json:"imported"`
}

// ImportResult summarises an upload. After a real import, Imported rows were written
// and rows with Status "error" were skipped.
type ImportResult struct {
	DryRun       bool        `json:"dry_run"`
	Total        int         `json:"total"`
	Valid        int         `json:"valid"`
	Invalid      int         `json:"invalid"`
	Imported     int         `json:"imported"`
	CreatedUnits []string    `json:"created_units,omitempty"`
	Rows         []ImportRow `json:"rows"`
}

// ImportUseCase bulk-creates products (plus default SKU prices, barcode and opening stock)
// from a CSV file. It never creates a bill or a payable.
type ImportUseCase struct {
	create     *CreateUseCase
	catalog    ImportCatalog
	units      ImportUnits
	warehouses ImportWarehouses
	opening    OpeningStock
	tx         TxRunner
	codes      codegen.Source
	guard      PlanGuard
}

// WithPlanGuard enables the max_skus plan check for a whole file: the batch is
// refused when the rows that would be created exceed the plan's remaining room.
func (uc *ImportUseCase) WithPlanGuard(g PlanGuard) *ImportUseCase {
	uc.guard = g
	return uc
}

// NewImportUseCase wires the use case. create should carry a price writer; tx makes each
// row (product, SKU, opening stock) all-or-nothing.
func NewImportUseCase(create *CreateUseCase, catalog ImportCatalog, units ImportUnits, wh ImportWarehouses, opening OpeningStock, tx TxRunner) *ImportUseCase {
	return &ImportUseCase{create: create, catalog: catalog, units: units, warehouses: wh, opening: opening, tx: tx, codes: codegen.Default()}
}

// WithCodeSource replaces the clock/randomness used for generated codes (tests).
func (uc *ImportUseCase) WithCodeSource(s codegen.Source) *ImportUseCase {
	uc.codes = s
	return uc
}

// NameSpecKey is the identity used to detect "same product" when a row has no code or barcode.
func NameSpecKey(name, spec string) string {
	return strings.ToLower(strings.TrimSpace(name)) + "\x00" + strings.ToLower(strings.TrimSpace(spec))
}

// importLine is one parsed, not yet cross-checked data row.
type importLine struct {
	line      int
	name      string
	code      string
	barcode   string
	spec      string
	unit      string
	retail    string
	wholesale string
	purchase  string
	qty       decimal.Decimal
	hasQty    bool
	cost      *decimal.Decimal
	genCode   string // server-assigned code when the row has none (real import only)
	warehouse string
	errs      []string
}

// Execute parses, validates and (unless DryRun) imports the file. Rows that fail
// validation are reported and skipped; the rest are imported, each in its own transaction.
func (uc *ImportUseCase) Execute(ctx context.Context, req ImportRequest) (*ImportResult, error) {
	if req.TenantID == uuid.Nil {
		return nil, fmt.Errorf("import products: tenant_id is required")
	}
	lines, err := parseImportCSV(req.Data)
	if err != nil {
		return nil, err
	}

	units, err := uc.units.UnitIDsByName(ctx, req.TenantID)
	if err != nil {
		return nil, fmt.Errorf("import products: load units: %w", err)
	}
	whByName, defaultWH, err := uc.warehouses.WarehousesByName(ctx, req.TenantID)
	if err != nil {
		return nil, fmt.Errorf("import products: load warehouses: %w", err)
	}
	if !req.DryRun {
		if lk, ok := uc.catalog.(ImportLocker); ok {
			release, err := lk.LockImport(ctx, req.TenantID)
			if err != nil {
				return nil, fmt.Errorf("import products: lock: %w", err)
			}
			defer release()
		}
	}
	if err := uc.crossCheck(ctx, req.TenantID, lines, whByName, defaultWH); err != nil {
		return nil, err
	}

	if uc.guard != nil {
		valid := 0
		for _, l := range lines {
			if len(l.errs) == 0 {
				valid++
			}
		}
		if valid > 0 {
			if err := uc.guard.Check(ctx, req.TenantID, entitlement.KeyMaxSKUs, valid); err != nil {
				return nil, err
			}
		}
	}

	res := &ImportResult{DryRun: req.DryRun, Total: len(lines), Rows: make([]ImportRow, 0, len(lines))}
	for _, l := range lines {
		row := ImportRow{Line: l.line, Name: l.name, Code: l.code, Status: "ok"}
		if len(l.errs) > 0 {
			row.Status, row.Errors = "error", l.errs
			res.Invalid++
		} else {
			res.Valid++
		}
		res.Rows = append(res.Rows, row)
	}
	if req.DryRun {
		for _, l := range lines {
			if len(l.errs) == 0 && l.unit != "" && units[strings.ToLower(l.unit)] == uuid.Nil {
				res.CreatedUnits = appendUnique(res.CreatedUnits, l.unit)
			}
		}
		return res, nil
	}

	if err := uc.assignCodes(ctx, req.TenantID, lines); err != nil {
		return nil, err
	}

	// Real import: create the missing units once, then write row by row.
	for i, l := range lines {
		if len(l.errs) > 0 {
			continue
		}
		key := strings.ToLower(l.unit)
		if l.unit != "" && units[key] == uuid.Nil {
			id, err := uc.units.CreateUnit(ctx, req.TenantID, l.unit)
			if err != nil {
				slog.Warn("import products: create unit failed", "tenant_id", req.TenantID, "unit", l.unit, "err", err)
				res.Rows[i].Status, res.Rows[i].Errors = "error", []string{"单位「" + l.unit + "」创建失败，请稍后重试"}
				res.Valid--
				res.Invalid++
				continue
			}
			units[key] = id
			res.CreatedUnits = append(res.CreatedUnits, l.unit)
		}
		code, err := uc.writeRow(ctx, req.TenantID, l, units[key], whByName, defaultWH)
		if err != nil {
			res.Rows[i].Status, res.Rows[i].Errors = "error", []string{rowErrorText(l, err)}
			res.Valid--
			res.Invalid++
			continue
		}
		res.Rows[i].Code, res.Rows[i].Imported = code, true
		res.Imported++
	}
	return res, nil
}

// crossCheck adds the errors that need the database or the whole file: duplicate
// code / barcode / name+spec (in the file and against existing products), unknown
// warehouse, opening stock without any warehouse.
func (uc *ImportUseCase) crossCheck(ctx context.Context, tenantID uuid.UUID, lines []*importLine, whByName map[string]uuid.UUID, defaultWH uuid.UUID) error {
	var codes, barcodes, names []string
	for _, l := range lines {
		if len(l.errs) > 0 {
			continue
		}
		if l.code != "" {
			codes = append(codes, l.code)
		}
		if l.barcode != "" {
			barcodes = append(barcodes, l.barcode)
		}
		if l.code == "" && l.barcode == "" {
			names = append(names, l.name)
		}
	}
	exCodes, err := uc.catalog.ExistingCodes(ctx, tenantID, codes)
	if err != nil {
		return fmt.Errorf("import products: check codes: %w", err)
	}
	exBarcodes, err := uc.catalog.ExistingBarcodes(ctx, tenantID, barcodes)
	if err != nil {
		return fmt.Errorf("import products: check barcodes: %w", err)
	}
	exNames, err := uc.catalog.ExistingNameSpecs(ctx, tenantID, names)
	if err != nil {
		return fmt.Errorf("import products: check names: %w", err)
	}

	seenCode := map[string]int{}
	seenBarcode := map[string]int{}
	seenName := map[string]int{}
	for _, l := range lines {
		if len(l.errs) > 0 {
			continue
		}
		if l.code != "" {
			if exCodes[l.code] {
				l.errs = append(l.errs, "编码「"+l.code+"」已经有商品在用了")
			} else if first, dup := seenCode[strings.ToLower(l.code)]; dup {
				l.errs = append(l.errs, fmt.Sprintf("编码「%s」和第 %d 行重复", l.code, first))
			}
			if _, ok := seenCode[strings.ToLower(l.code)]; !ok {
				seenCode[strings.ToLower(l.code)] = l.line
			}
		}
		if l.barcode != "" {
			if exBarcodes[l.barcode] {
				l.errs = append(l.errs, "条码「"+l.barcode+"」已经有商品在用了")
			} else if first, dup := seenBarcode[l.barcode]; dup {
				l.errs = append(l.errs, fmt.Sprintf("条码「%s」和第 %d 行重复", l.barcode, first))
			}
			if _, ok := seenBarcode[l.barcode]; !ok {
				seenBarcode[l.barcode] = l.line
			}
		}
		if l.code == "" && l.barcode == "" {
			k := NameSpecKey(l.name, l.spec)
			if exNames[k] {
				l.errs = append(l.errs, "已经有同名同规格的商品「"+l.name+"」，如果确实是另一种商品，请加上规格或编码")
			} else if first, dup := seenName[k]; dup {
				l.errs = append(l.errs, fmt.Sprintf("和第 %d 行是同名同规格的商品，请加上规格或编码区分", first))
			}
			if _, ok := seenName[k]; !ok {
				seenName[k] = l.line
			}
		}
		if l.hasQty && l.qty.IsPositive() {
			if l.warehouse != "" {
				if _, ok := whByName[strings.ToLower(l.warehouse)]; !ok {
					l.errs = append(l.errs, "找不到名为「"+l.warehouse+"」的仓库，请先在「仓库」里新建，或把仓库列留空使用默认仓库")
				}
			} else if defaultWH == uuid.Nil {
				l.errs = append(l.errs, "填了期初数量，但还没有仓库，请先在「仓库」里新建一个")
			}
		}
	}
	return nil
}

// assignCodes draws a code for every valid row without one, distinct from the codes in the
// file, from each other and from existing products. It happens up front because a unique
// violation aborts the row transaction, so the create use case cannot retry inside it
// (and a 1000-row file has a real chance of colliding 4-symbol suffixes).
func (uc *ImportUseCase) assignCodes(ctx context.Context, tenantID uuid.UUID, lines []*importLine) error {
	used := map[string]bool{}
	for _, l := range lines {
		if l.code != "" {
			used[l.code] = true
		}
	}
	var pending []*importLine
	for _, l := range lines {
		if len(l.errs) == 0 && l.code == "" {
			pending = append(pending, l)
		}
	}
	for round := 0; len(pending) > 0; round++ {
		if round >= 10 {
			return fmt.Errorf("import products: could not assign unique codes")
		}
		drawn := make([]string, 0, len(pending))
		for _, l := range pending {
			c, err := uc.codes.Next(codegen.PrefixProduct)
			if err != nil {
				return err
			}
			if used[c] {
				l.genCode = ""
				continue
			}
			l.genCode = c
			used[c] = true
			drawn = append(drawn, c)
		}
		existing, err := uc.catalog.ExistingCodes(ctx, tenantID, drawn)
		if err != nil {
			return fmt.Errorf("import products: check generated codes: %w", err)
		}
		var next []*importLine
		for _, l := range pending {
			if l.genCode == "" || existing[l.genCode] {
				next = append(next, l)
			}
		}
		pending = next
	}
	return nil
}

// writeRow creates one product (+ SKU prices, barcode, opening stock) in a single
// transaction and returns the product code.
func (uc *ImportUseCase) writeRow(ctx context.Context, tenantID uuid.UUID, l *importLine, unitID uuid.UUID, whByName map[string]uuid.UUID, defaultWH uuid.UUID) (string, error) {
	code := l.code
	if code == "" {
		code = l.genCode
	}
	in := domain.CreateInput{TenantID: tenantID, Code: code, Name: l.name, Spec: l.spec}
	if unitID != uuid.Nil {
		in.DefaultUnitID = &unitID
	}
	if l.barcode != "" {
		b, _ := json.Marshal(map[string]string{"barcode": l.barcode})
		in.Attributes = b
	}
	prices := appsku.PriceInput{}
	if l.retail != "" {
		prices.Retail = &l.retail
	}
	if l.wholesale != "" {
		prices.Wholesale = &l.wholesale
	}
	if l.purchase != "" {
		prices.Purchase = &l.purchase
	}

	err := runAtomic(uc.tx, ctx, func(ctx context.Context) error {
		p, err := uc.create.ExecuteWithPrices(ctx, in, prices)
		if err != nil {
			return err
		}
		if !l.hasQty || !l.qty.IsPositive() {
			return nil
		}
		wh := defaultWH
		if l.warehouse != "" {
			wh = whByName[strings.ToLower(l.warehouse)]
		}
		cost := decimal.Zero
		switch {
		case l.cost != nil:
			cost = *l.cost
		case l.purchase != "":
			if d, perr := decimalutil.Parse(cleanNumber(l.purchase), "purchase"); perr == nil {
				cost = d
			}
		}
		return uc.opening.RecordOpening(ctx, OpeningStockInput{
			TenantID: tenantID, ProductID: p.ID, WarehouseID: wh, Qty: l.qty, UnitCost: cost,
		})
	})
	if err != nil {
		return "", err
	}
	return code, nil
}

func rowErrorText(l *importLine, err error) string {
	var pe *appsku.PriceError
	if errors.As(err, &pe) {
		return pe.Msg
	}
	if codegen.IsUniqueViolation(err) {
		return "编码或名称和已有商品冲突，请换一个编码后重试"
	}
	slog.Warn("import products: row failed", "line", l.line, "err", err)
	return "这一行保存失败，请稍后重试"
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// ---- CSV parsing ----------------------------------------------------------

var sciNotation = regexp.MustCompile(`^\d(\.\d+)?[eE][+-]?\d+$`)

// header aliases (after trimming spaces and a trailing * ) -> canonical field.
var importColumns = map[string]string{
	"名称": "name", "商品名称": "name",
	"编码": "code", "商品编码": "code",
	"条码": "barcode", "商品条码": "barcode",
	"规格": "spec",
	"单位": "unit", "单位名": "unit",
	"零售价": "retail", "批发价": "wholesale", "进价": "purchase",
	"期初数量": "qty", "期初成本": "cost",
	"仓库": "warehouse",
}

// DecodeCSVText turns uploaded bytes into UTF-8 text: a UTF-8 BOM is dropped and a
// file that is not valid UTF-8 is read as GBK/GB18030 (Excel saves CSV that way in China).
func DecodeCSVText(data []byte) (string, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(data) {
		return string(data), nil
	}
	out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data)
	if err != nil {
		return "", &ImportError{Msg: "无法识别文件编码，请用 Excel 另存为「CSV」格式后重试"}
	}
	return string(out), nil
}

func cleanNumber(s string) string {
	r := strings.NewReplacer(",", "", "，", "", "¥", "", "￥", "", " ", "")
	return r.Replace(strings.TrimSpace(s))
}

func newImportReader(text string) *csv.Reader {
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true
	return r
}

// importField maps one header cell to its canonical field.
func importField(h string) (string, bool) {
	f, ok := importColumns[strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(h), "*"))]
	return f, ok
}

// HasCostColumns reports whether the header row the importer will read has a
// purchase price or opening cost column. It shares the importer's reader and
// header rules, so a blank first line or "进价 *" cannot slip past it.
func HasCostColumns(data []byte) bool {
	text, err := DecodeCSVText(data)
	if err != nil {
		return false
	}
	header, err := newImportReader(text).Read()
	if err != nil {
		return false
	}
	for _, h := range header {
		if f, ok := importField(h); ok && (f == "purchase" || f == "cost") {
			return true
		}
	}
	return false
}

func parseImportCSV(data []byte) ([]*importLine, error) {
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) || bytes.HasPrefix(data, []byte{0xD0, 0xCF, 0x11, 0xE0}) {
		return nil, &ImportError{Msg: "暂不支持直接上传 Excel 文件，请在 Excel 里选「另存为」，保存类型选「CSV」后再上传"}
	}
	text, err := DecodeCSVText(data)
	if err != nil {
		return nil, err
	}
	r := newImportReader(text)

	header, err := r.Read()
	if errors.Is(err, io.EOF) {
		return nil, &ImportError{Msg: "文件是空的，请使用下载的模板填写后再上传"}
	}
	if err != nil {
		return nil, &ImportError{Msg: "文件内容无法读取，请用 Excel 另存为「CSV」格式后重试"}
	}
	col := map[string]int{}
	for i, h := range header {
		if f, ok := importField(h); ok {
			if _, dup := col[f]; !dup {
				col[f] = i
			}
		}
	}
	if _, ok := col["name"]; !ok {
		return nil, &ImportError{Msg: "表格第一行里找不到「名称」这一列，请使用下载的模板填写"}
	}
	get := func(rec []string, f string) string {
		i, ok := col[f]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}

	var out []*importLine
	line := 1
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		line++
		if err != nil {
			return nil, &ImportError{Msg: fmt.Sprintf("第 %d 行格式不正确，无法读取，请检查引号和逗号", line)}
		}
		blank := true
		for _, c := range rec {
			if strings.TrimSpace(c) != "" {
				blank = false
				break
			}
		}
		if blank {
			continue
		}
		if len(out) >= MaxImportRows {
			return nil, &ImportError{Msg: fmt.Sprintf("一次最多导入 %d 行，请拆成几个文件分别导入", MaxImportRows)}
		}
		l := &importLine{
			line: line, name: get(rec, "name"), code: get(rec, "code"), barcode: unwrapTextCell(get(rec, "barcode")),
			spec: get(rec, "spec"), unit: get(rec, "unit"), warehouse: get(rec, "warehouse"),
			retail: cleanNumber(get(rec, "retail")), wholesale: cleanNumber(get(rec, "wholesale")), purchase: cleanNumber(get(rec, "purchase")),
		}
		validateLine(l, get(rec, "qty"), get(rec, "cost"))
		out = append(out, l)
	}
	if len(out) == 0 {
		return nil, &ImportError{Msg: "表格里没有商品数据，请在模板的第二行开始填写"}
	}
	return out, nil
}

// validateLine checks everything decidable from the row alone.
func validateLine(l *importLine, qtyRaw, costRaw string) {
	bad := func(msg string) { l.errs = append(l.errs, msg) }
	if l.name == "" {
		bad("名称不能为空")
	}
	for _, p := range exampleRowPrefixes {
		if strings.HasPrefix(l.name, p) {
			bad("这是模板里的示例行，请删除后再导入")
			break
		}
	}
	for _, f := range []struct {
		label, v string
		max      int
	}{{"名称", l.name, 128}, {"编码", l.code, 128}, {"规格", l.spec, 128}, {"条码", l.barcode, 100}, {"单位", l.unit, 50}} {
		if utf8.RuneCountInString(f.v) > f.max {
			bad(fmt.Sprintf("%s太长了，最多 %d 个字", f.label, f.max))
		}
	}
	if l.barcode != "" && sciNotation.MatchString(l.barcode) {
		bad("条码「" + l.barcode + "」被表格软件变成了科学计数法，请把条码列的格式设为「数值」、小数位数 0，或者重新输入条码后再保存")
	}
	if _, err := appsku.ParsePrices(appsku.PriceInput{Retail: &l.retail, Wholesale: &l.wholesale, Purchase: &l.purchase}); err != nil {
		bad(err.Error())
	}
	if q := cleanNumber(qtyRaw); q != "" {
		d, err := decimalutil.Parse(q, "期初数量")
		switch {
		case err != nil:
			bad("期初数量格式不正确，请输入数字")
		case d.IsNegative():
			bad("期初数量不能为负数")
		case d.GreaterThanOrEqual(decimal.New(1, 12)):
			bad("期初数量太大了")
		default:
			l.qty, l.hasQty = d, true
		}
	}
	if c := cleanNumber(costRaw); c != "" {
		d, err := decimalutil.Parse(c, "期初成本")
		switch {
		case err != nil:
			bad("期初成本格式不正确，请输入数字")
		case d.IsNegative():
			bad("期初成本不能为负数")
		case d.GreaterThanOrEqual(decimal.New(1, 12)):
			bad("期初成本太大了")
		default:
			l.cost = &d
		}
	}
}

// unwrapTextCell strips the ="..." wrapper the template uses to keep long digit strings
// (barcodes) from being turned into scientific notation by spreadsheet software.
func unwrapTextCell(s string) string {
	if strings.HasPrefix(s, `="`) && strings.HasSuffix(s, `"`) && len(s) >= 3 {
		return strings.TrimSpace(s[2 : len(s)-1])
	}
	return s
}
