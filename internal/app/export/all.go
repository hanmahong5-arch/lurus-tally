package export

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
)

// allPageSize is how many rows each table is read per query. Tables are walked
// by primary key, so memory stays bounded by one page whatever the shop size.
const allPageSize = 1000

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// sheetCol is one CSV column. cost marks the columns only people holding
// cost.view may receive; without it the column is neither selected nor written.
type sheetCol struct {
	name string
	expr string
	cost bool
}

// sheet is one CSV file inside the zip: a keyset-paged read of one table.
type sheet struct {
	file string
	cols []sheetCol
	from string // FROM clause, including joins
	idEx string // unique, uuid-typed ordering key
	// where must scope the tenant ($1) and drop soft-deleted rows.
	where string
	note  string // extra line for the readme
}

func money(expr string) string { return "ROUND(" + expr + ", 2)::text" }

func day(expr string) string {
	return "to_char(" + expr + " AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD')"
}

func billSheet(file, subTypes string, purchase bool) sheet {
	return sheet{
		file: file,
		cols: []sheetCol{
			{name: "单号", expr: "bh.bill_no"},
			{name: "类型", expr: "bh.sub_type"},
			{name: "状态", expr: "CASE bh.status WHEN 0 THEN '草稿' WHEN 2 THEN '已审核' WHEN 9 THEN '已取消' ELSE bh.status::text END"},
			{name: "日期", expr: day("bh.bill_date")},
			{name: "合作方", expr: "COALESCE(p.name, '')"},
			{name: "仓库", expr: "COALESCE(wh.name, '')"},
			{name: "总额", expr: money("bh.total_amount"), cost: purchase},
			{name: "已付", expr: money("bh.paid_amount"), cost: purchase},
			{name: "备注", expr: "COALESCE(bh.remark, '')"},
		},
		from: `tally.bill_head bh
			LEFT JOIN tally.partner p ON p.id = bh.partner_id AND p.tenant_id = bh.tenant_id
			LEFT JOIN tally.warehouse wh ON wh.id = bh.warehouse_id AND wh.tenant_id = bh.tenant_id`,
		idEx:  "bh.id",
		where: "bh.tenant_id = $1 AND bh.deleted_at IS NULL AND bh.sub_type IN (" + subTypes + ")",
	}
}

func itemSheet(file, subTypes string, purchase bool) sheet {
	unitPrice := "COALESCE(bi.unit_price::text, '')"
	if purchase {
		unitPrice = "COALESCE(" + money("bi.unit_price") + ", '')"
	}
	return sheet{
		file: file,
		cols: []sheetCol{
			{name: "单号", expr: "bh.bill_no"},
			{name: "类型", expr: "bh.sub_type"},
			{name: "日期", expr: day("bh.bill_date")},
			{name: "商品编码", expr: "COALESCE(pr.code, '')"},
			{name: "商品名", expr: "COALESCE(pr.name, '')"},
			{name: "单位", expr: "COALESCE(bi.unit_name, '')"},
			{name: "数量", expr: "bi.qty::text"},
			{name: "单价", expr: unitPrice, cost: purchase},
			{name: "进价", expr: "COALESCE(bi.purchase_price::text, '')", cost: true},
			{name: "金额", expr: "COALESCE(" + money("bi.line_amount") + ", '')", cost: purchase},
			{name: "仓库", expr: "COALESCE(wh.name, '')"},
			{name: "备注", expr: "COALESCE(bi.remark, '')"},
		},
		from: `tally.bill_item bi
			JOIN tally.bill_head bh ON bh.id = bi.head_id AND bh.tenant_id = bi.tenant_id
			LEFT JOIN tally.product pr ON pr.id = bi.product_id AND pr.tenant_id = bi.tenant_id
			LEFT JOIN tally.warehouse wh ON wh.id = bi.warehouse_id AND wh.tenant_id = bi.tenant_id`,
		idEx:  "bi.id",
		where: "bi.tenant_id = $1 AND bi.deleted_at IS NULL AND bh.deleted_at IS NULL AND bh.sub_type IN (" + subTypes + ")",
	}
}

func partnerSheet(file, types, balanceName, balanceCol string) sheet {
	return sheet{
		file: file,
		cols: []sheetCol{
			{name: "名称", expr: "p.name"},
			{name: "编码", expr: "COALESCE(p.code, '')"},
			{name: "联系人", expr: "COALESCE(p.contact_name, '')"},
			{name: "电话", expr: "COALESCE(p.phone, '')"},
			{name: "手机", expr: "COALESCE(p.mobile, '')"},
			{name: "邮箱", expr: "COALESCE(p.email, '')"},
			{name: "地址", expr: "COALESCE(p.address, '')"},
			{name: "税号", expr: "COALESCE(p.tax_no, '')"},
			{name: balanceName, expr: money("p." + balanceCol)},
			{name: "备注", expr: "COALESCE(p.remark, '')"},
		},
		from:  "tally.partner p",
		idEx:  "p.id",
		where: "p.tenant_id = $1 AND p.deleted_at IS NULL AND p.partner_type IN (" + types + ")",
	}
}

const (
	purchaseTypes = "'采购', '采购退货'"
	saleTypes     = "'销售', '销售退货'"
)

// allSheets lists every file of the export, in zip order.
var allSheets = []sheet{
	{
		file: "商品.csv",
		cols: []sheetCol{
			{name: "编码", expr: "p.code"},
			{name: "名称", expr: "p.name"},
			{name: "分类", expr: "COALESCE(c.name, '')"},
			{name: "规格", expr: "COALESCE(p.spec, '')"},
			{name: "型号", expr: "COALESCE(p.model, '')"},
			{name: "品牌", expr: "COALESCE(p.brand, '')"},
			{name: "条码", expr: "COALESCE(sk.bar_code, '')"},
			{name: "单位", expr: "COALESCE(sk.unit_name, '')"},
			{name: "进价", expr: "COALESCE(sk.purchase_price::text, '')", cost: true},
			{name: "零售价", expr: "COALESCE(sk.retail_price::text, '')"},
			{name: "批发价", expr: "COALESCE(sk.wholesale_price::text, '')"},
			{name: "状态", expr: "CASE WHEN p.enabled THEN '启用' ELSE '停用' END"},
			{name: "备注", expr: "COALESCE(p.remark, '')"},
		},
		from: `tally.product p
			LEFT JOIN tally.product_category c ON c.id = p.category_id AND c.tenant_id = p.tenant_id
			LEFT JOIN LATERAL (
				SELECT s.bar_code, s.unit_name, s.purchase_price, s.retail_price, s.wholesale_price
				FROM tally.product_sku s
				WHERE s.product_id = p.id AND s.tenant_id = p.tenant_id AND s.deleted_at IS NULL
				ORDER BY s.is_default DESC, s.created_at ASC LIMIT 1) sk ON true`,
		idEx:  "p.id",
		where: "p.tenant_id = $1 AND p.deleted_at IS NULL",
		note:  "每个商品一行，价格取默认规格。",
	},
	partnerSheet("客户.csv", "'customer', 'both', 'member'", "应收余额", "ar_balance"),
	partnerSheet("供应商.csv", "'supplier', 'both'", "应付余额", "ap_balance"),
	{
		file: "仓库.csv",
		cols: []sheetCol{
			{name: "名称", expr: "w.name"},
			{name: "地址", expr: "COALESCE(w.address, '')"},
			{name: "默认仓库", expr: "CASE WHEN w.is_default THEN '是' ELSE '否' END"},
			{name: "状态", expr: "CASE WHEN w.enabled THEN '启用' ELSE '停用' END"},
			{name: "备注", expr: "COALESCE(w.remark, '')"},
		},
		from:  "tally.warehouse w",
		idEx:  "w.id",
		where: "w.tenant_id = $1 AND w.deleted_at IS NULL",
	},
	{
		file: "库存.csv",
		cols: []sheetCol{
			{name: "商品编码", expr: "COALESCE(p.code, '')"},
			{name: "商品名", expr: "COALESCE(p.name, '')"},
			{name: "仓库", expr: "COALESCE(wh.name, '')"},
			{name: "在库", expr: "ss.on_hand_qty::text"},
			{name: "单位成本", expr: "ss.unit_cost::text", cost: true},
			{name: "库存金额", expr: money("ss.on_hand_qty * ss.unit_cost"), cost: true},
		},
		from: `tally.stock_snapshot ss
			LEFT JOIN tally.product p ON p.id = ss.product_id AND p.tenant_id = ss.tenant_id
			LEFT JOIN tally.warehouse wh ON wh.id = ss.warehouse_id AND wh.tenant_id = ss.tenant_id`,
		idEx:  "ss.id",
		where: "ss.tenant_id = $1",
	},
	billSheet("采购单.csv", purchaseTypes, true),
	itemSheet("采购明细.csv", purchaseTypes, true),
	billSheet("销售单.csv", saleTypes, false),
	itemSheet("销售明细.csv", saleTypes, false),
	{
		file: "收付款.csv",
		cols: []sheetCol{
			{name: "单号", expr: "COALESCE(bh.bill_no, ph.bill_no, '')"},
			{name: "收付款方", expr: "COALESCE(p.name, '')"},
			{name: "金额", expr: money("CASE WHEN bh.sub_type IN ('销售退货', '采购退货') THEN -ph.amount ELSE ph.amount END")},
			{name: "方式", expr: "CASE ph.pay_type WHEN 'cash' THEN '现金' WHEN 'wechat' THEN '微信' WHEN 'alipay' THEN '支付宝' WHEN 'card' THEN '银行卡' WHEN 'credit' THEN '赊账' WHEN 'transfer' THEN '转账' ELSE ph.pay_type END"},
			{name: "时间", expr: "to_char(ph.pay_date AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD HH24:MI:SS')"},
			{name: "收付", expr: "CASE WHEN bh.sub_type IN (" + saleTypes + ") THEN '收' WHEN bh.sub_type IN (" + purchaseTypes + ") THEN '付' ELSE '' END"},
			{name: "关联单据类型", expr: "COALESCE(bh.sub_type, '')"},
			{name: "备注", expr: "COALESCE(ph.remark, '')"},
		},
		from: `tally.payment_head ph
			LEFT JOIN tally.bill_head bh ON bh.id = ph.related_bill_id AND bh.tenant_id = ph.tenant_id
			LEFT JOIN tally.partner p ON p.id = ph.partner_id AND p.tenant_id = ph.tenant_id`,
		idEx:  "ph.id",
		where: "ph.tenant_id = $1 AND ph.deleted_at IS NULL",
		note:  "退货单的付款记为负数；方式为赊账的不算实际收到或付出的钱。",
	},
	{
		file: "发票.csv",
		cols: []sheetCol{
			{name: "方向", expr: "CASE iv.direction WHEN 'out' THEN '销项' ELSE '进项' END"},
			{name: "发票号", expr: "iv.invoice_no"},
			{name: "发票代码", expr: "COALESCE(iv.invoice_code, '')"},
			{name: "发票类型", expr: "iv.invoice_type"},
			{name: "往来单位", expr: "COALESCE(p.name, '')"},
			{name: "开票日期", expr: "to_char(iv.issue_date, 'YYYY-MM-DD')"},
			{name: "不含税金额", expr: money("iv.amount_excl_tax")},
			{name: "税额", expr: money("iv.tax_amount")},
			{name: "价税合计", expr: money("iv.total_amount")},
			{name: "状态", expr: "CASE iv.status WHEN 'issued' THEN '有效' WHEN 'void' THEN '作废' WHEN 'red' THEN '红冲' ELSE iv.status END"},
			{name: "备注", expr: "COALESCE(iv.remark, '')"},
		},
		from:  "tally.invoice iv LEFT JOIN tally.partner p ON p.id = iv.partner_id AND p.tenant_id = iv.tenant_id",
		idEx:  "iv.id",
		where: "iv.tenant_id = $1 AND iv.deleted_at IS NULL",
	},
}

// visible returns the columns showCost allows.
func (s sheet) visible(showCost bool) []sheetCol {
	out := make([]sheetCol, 0, len(s.cols))
	for _, c := range s.cols {
		if c.cost && !showCost {
			continue
		}
		out = append(out, c)
	}
	return out
}

func (s sheet) query(cols []sheetCol) string {
	exprs := make([]string, 0, len(cols)+1)
	for _, c := range cols {
		exprs = append(exprs, c.expr)
	}
	exprs = append(exprs, s.idEx+"::text")
	return "SELECT " + strings.Join(exprs, ", ") + "\nFROM " + s.from +
		"\nWHERE " + s.where + " AND " + s.idEx + " > $2::uuid\nORDER BY " + s.idEx + " ASC\nLIMIT $3"
}

// AllExportUseCase streams every table of one shop as a zip of CSV files.
type AllExportUseCase struct {
	db       *sql.DB
	log      *slog.Logger
	pageSize int
}

// NewAllExportUseCase creates an AllExportUseCase.
func NewAllExportUseCase(db *sql.DB, log *slog.Logger) *AllExportUseCase {
	if log == nil {
		log = slog.Default()
	}
	return &AllExportUseCase{db: db, log: log, pageSize: allPageSize}
}

type requestIDKey struct{}

// WithRequestID tags ctx with the request id so export failures can be traced.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

func requestIDFrom(ctx context.Context) string {
	s, _ := ctx.Value(requestIDKey{}).(string)
	return s
}

// Execute writes the zip to w and returns the total data rows written. Without
// showCost the purchase price, cost, stock value and purchase amount columns
// are not even read from the database. When a table fails, the zip is still
// closed with 导出失败.txt and a 说明.txt ending in 完整性：失败, so a partial
// archive can never pass as a complete backup; the error is returned as well.
func (uc *AllExportUseCase) Execute(ctx context.Context, tenantID uuid.UUID, showCost bool, now time.Time, w io.Writer) (int, error) {
	zw := zip.NewWriter(w)
	counts := make([]int, len(allSheets))
	total := 0
	var failErr error
	failIdx := -1
	for i, s := range allSheets {
		n, err := uc.writeSheet(ctx, zw, tenantID, s, showCost, now)
		if err != nil {
			failErr = fmt.Errorf("export all: %s: %w", s.file, err)
			failIdx = i
			uc.log.Error("export all: table failed",
				slog.String("tenant_id", tenantID.String()),
				slog.String("request_id", requestIDFrom(ctx)),
				slog.String("file", s.file),
				slog.String("error", err.Error()))
			break
		}
		counts[i] = n
		total += n
	}
	if failErr != nil {
		if err := writeTextFile(zw, "导出失败.txt", failureNote(failIdx, counts, now), now); err != nil {
			return total, failErr
		}
	}
	// Written last: the integrity verdict is only known once every table is done.
	shop := uc.shopName(ctx, tenantID)
	body := readme(shop, now, counts, showCost, failIdx)
	if err := writeTextFile(zw, "说明.txt", body, now); err != nil {
		if failErr != nil {
			return total, failErr
		}
		return total, fmt.Errorf("export all: readme: %w", err)
	}
	if err := zw.Close(); err != nil {
		if failErr != nil {
			return total, failErr
		}
		return total, fmt.Errorf("export all: close zip: %w", err)
	}
	return total, failErr
}

func writeTextFile(zw *zip.Writer, name, body string, now time.Time) error {
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: now})
	if err != nil {
		return err
	}
	_, err = fw.Write(append(append([]byte{}, utf8BOM...), []byte(body)...))
	return err
}

// failureNote says which table failed and which were exported before it.
func failureNote(failIdx int, counts []int, now time.Time) string {
	var b strings.Builder
	b.WriteString("本次导出没有完成，这个压缩包不是完整数据，请不要当作完整备份使用。\r\n")
	fmt.Fprintf(&b, "导出时间：%s\r\n", now.In(BusinessTZ).Format(dateTimeLayout))
	fmt.Fprintf(&b, "失败的表：%s\r\n", allSheets[failIdx].file)
	done := make([]string, 0, failIdx)
	for i := 0; i < failIdx; i++ {
		done = append(done, fmt.Sprintf("%s（%d 行）", allSheets[i].file, counts[i]))
	}
	if len(done) == 0 {
		b.WriteString("已导出的表：无\r\n")
	} else {
		fmt.Fprintf(&b, "已导出的表：%s\r\n", strings.Join(done, "、"))
	}
	fmt.Fprintf(&b, "未导出或不完整的表：%s\r\n", strings.Join(sheetFiles(failIdx), "、"))
	b.WriteString("请重新导出或联系客服。\r\n")
	return b.String()
}

func sheetFiles(from int) []string {
	out := make([]string, 0, len(allSheets)-from)
	for _, s := range allSheets[from:] {
		out = append(out, s.file)
	}
	return out
}

func (uc *AllExportUseCase) shopName(ctx context.Context, tenantID uuid.UUID) string {
	var name string
	err := dbscope.From(ctx, uc.db).QueryRowContext(ctx, `SELECT name FROM tally.tenant WHERE id = $1`, tenantID).Scan(&name)
	if err != nil {
		uc.log.Warn("export all: shop name unavailable", slog.String("error", err.Error()))
		return ""
	}
	return name
}

func (uc *AllExportUseCase) writeSheet(ctx context.Context, zw *zip.Writer, tenantID uuid.UUID, s sheet, showCost bool, now time.Time) (int, error) {
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: s.file, Method: zip.Deflate, Modified: now})
	if err != nil {
		return 0, err
	}
	if _, err := fw.Write(utf8BOM); err != nil {
		return 0, err
	}
	cols := s.visible(showCost)
	cw := csv.NewWriter(fw)
	header := make([]string, len(cols))
	for i, c := range cols {
		header[i] = c.name
	}
	if err := cw.Write(header); err != nil {
		return 0, err
	}
	q := s.query(cols)
	last := uuid.Nil.String()
	count := 0
	for {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		n, lastID, err := uc.page(ctx, cw, q, tenantID, last, len(cols))
		if err != nil {
			return count, err
		}
		count += n
		cw.Flush()
		if err := cw.Error(); err != nil {
			return count, err
		}
		if n < uc.pageSize {
			return count, nil
		}
		last = lastID
	}
}

// page reads one page after lastID and writes its rows. The last column of the
// query is the ordering key and is not written.
func (uc *AllExportUseCase) page(ctx context.Context, cw *csv.Writer, q string, tenantID uuid.UUID, lastID string, ncols int) (int, string, error) {
	rows, err := dbscope.From(ctx, uc.db).QueryContext(ctx, q, tenantID, lastID, uc.pageSize)
	if err != nil {
		return 0, lastID, err
	}
	defer func() { _ = rows.Close() }()
	n := 0
	vals := make([]sql.NullString, ncols+1)
	ptrs := make([]any, ncols+1)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	rec := make([]string, ncols)
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return n, lastID, err
		}
		for i := 0; i < ncols; i++ {
			rec[i] = vals[i].String
		}
		if err := cw.Write(rec); err != nil {
			return n, lastID, err
		}
		lastID = vals[ncols].String
		n++
	}
	return n, lastID, rows.Err()
}

// failIdx < 0 means every table was exported.
func readme(shop string, now time.Time, counts []int, showCost bool, failIdx int) string {
	var b strings.Builder
	b.WriteString("Tally 进销存 - 全部数据导出\r\n")
	if shop != "" {
		fmt.Fprintf(&b, "店名：%s\r\n", shop)
	}
	fmt.Fprintf(&b, "导出时间：%s\r\n", now.In(BusinessTZ).Format(dateTimeLayout))
	b.WriteString("每个 csv 文件都可以直接用 Excel 或 WPS 表格打开，第一行是表头。\r\n")
	if !showCost {
		b.WriteString("你的角色没有查看成本的权限，所以进价、成本、库存金额和采购金额没有导出。\r\n")
	}
	b.WriteString("已删除的记录不在其中。\r\n\r\n各表行数与字段：\r\n")
	for i, s := range allSheets {
		if failIdx >= 0 && i >= failIdx {
			fmt.Fprintf(&b, "\r\n%s（未导出或不完整）\r\n", s.file)
			continue
		}
		cols := s.visible(showCost)
		names := make([]string, len(cols))
		for j, c := range cols {
			names[j] = c.name
		}
		fmt.Fprintf(&b, "\r\n%s（%d 行）\r\n字段：%s\r\n", s.file, counts[i], strings.Join(names, "、"))
		if s.note != "" {
			fmt.Fprintf(&b, "说明：%s\r\n", s.note)
		}
	}
	if failIdx >= 0 {
		b.WriteString("\r\n完整性：失败\r\n")
	} else {
		b.WriteString("\r\n完整性：成功\r\n")
	}
	return b.String()
}
