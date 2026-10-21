// Package partnerimport is the shared CSV reading and bookkeeping behind the customer and
// supplier bulk imports: encoding detection (UTF-8 or GBK), header aliases, row-level
// Chinese errors and same-name de-duplication. Writing a row is left to the caller.
package partnerimport

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/width"

	appproduct "github.com/hanmahong5-arch/lurus-tally/internal/app/product"
)

// MaxRows caps one file so a request stays a bounded unit of work.
const MaxRows = 2000

// ImportError is a whole-file failure with a plain Chinese message.
type ImportError struct{ Msg string }

func (e *ImportError) Error() string { return e.Msg }

// Row is one parsed data row; Line is the 1-based line in the file (header = 1).
type Row struct {
	Line    int
	Name    string
	Contact string
	Phone   string
	Address string
	Remark  string
}

// RowError is a per-row failure reported to the user.
type RowError struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}

// Result summarises one upload.
type Result struct {
	Created int `json:"created"`
	// Skipped is the total of SkippedByName and SkippedByPhone (kept for older clients).
	Skipped        int `json:"skipped"`
	SkippedByName  int `json:"skipped_by_name"`
	SkippedByPhone int `json:"skipped_by_phone"`
	// SkippedRows explains each skipped row (name clash or phone clash).
	SkippedRows []RowError `json:"skipped_rows"`
	Errors      []RowError `json:"errors"`
}

// Outcome is what a writer reports for one row.
type Outcome int

const (
	// Created means the row was written.
	Created Outcome = iota
	// Duplicate means the row clashed with an existing record and was skipped.
	Duplicate
	// DuplicateMobile means the row's phone number already belongs to another record.
	DuplicateMobile
)

// Kind selects the header wording and example rows of the template.
type Kind struct {
	NameHeader string
	Examples   [][]string
}

var (
	// Customers is the customer template kind.
	Customers = Kind{NameHeader: "客户名称", Examples: [][]string{
		{"示例：张三五金店", "张三", "13800000000", "某某路 1 号", "老客户"},
		{"示例：李四建材行", "李四", "0571-88880000", "某某市场 2 号", ""},
	}}
	// Suppliers is the supplier template kind.
	Suppliers = Kind{NameHeader: "供应商名称", Examples: [][]string{
		{"示例：华东苗木场", "王经理", "13900000000", "某某镇苗圃路 8 号", "月结"},
		{"示例：永固五金厂", "赵经理", "0571-77770000", "某某工业园 3 号", ""},
	}}
)

var aliases = map[string]string{
	"名称": "name", "客户名称": "name", "供应商名称": "name", "客户": "name", "供应商": "name", "单位名称": "name",
	"联系人": "contact", "联系人姓名": "contact",
	"电话": "phone", "手机": "phone", "手机号": "phone", "联系电话": "phone", "电话号码": "phone",
	"地址": "address", "联系地址": "address",
	"备注": "remark", "说明": "remark",
}

var fieldOrder = []string{"name", "contact", "phone", "address", "remark"}
var limits = map[string]int{"name": 128, "contact": 100, "phone": 30, "address": 500, "remark": 500}
var labels = map[string]string{"name": "名称", "contact": "联系人", "phone": "电话", "address": "地址", "remark": "备注"}

// Template returns the CSV template (UTF-8 with BOM, two example rows).
func (k Kind) Template() string {
	var b strings.Builder
	b.WriteString("\xef\xbb\xbf")
	w := csv.NewWriter(&b)
	_ = w.Write([]string{k.NameHeader + "*", "联系人", "电话", "地址", "备注"})
	for _, r := range k.Examples {
		_ = w.Write(r)
	}
	w.Flush()
	return strings.ReplaceAll(b.String(), "\r\n", "\n")
}

// NameKey is the identity used to detect "same name": whitespace trimmed and collapsed,
// full-width folded to half-width, case folded.
func NameKey(s string) string {
	return strings.ToLower(width.Fold.String(CleanName(s)))
}

// CleanName trims and collapses whitespace in a name.
func CleanName(s string) string { return strings.Join(strings.Fields(s), " ") }

// Parse reads the file. Rows with an empty name are reported with their line number;
// fully blank rows and the template's example rows are ignored.
func Parse(data []byte) ([]Row, []RowError, error) {
	text, err := appproduct.DecodeCSVText(data)
	if err != nil {
		return nil, nil, &ImportError{Msg: "无法识别文件编码，请用 Excel 另存为「CSV」格式后重试"}
	}
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true

	var cols map[string]int
	line := 0
	var rows []Row
	var errs []RowError
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		line++
		if err != nil {
			errs = append(errs, RowError{Row: line, Message: "这一行格式不对，无法读取"})
			continue
		}
		if cols == nil {
			if blank(rec) {
				continue
			}
			cols = map[string]int{}
			for i, h := range rec {
				h = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(h), "*"))
				if f, ok := aliases[h]; ok {
					if _, dup := cols[f]; !dup {
						cols[f] = i
					}
				}
			}
			if _, ok := cols["name"]; !ok {
				return nil, nil, &ImportError{Msg: "表格第一行要有「名称」这一列（可以先下载模板再填写）"}
			}
			continue
		}
		if blank(rec) {
			continue
		}
		get := func(f string) string {
			i, ok := cols[f]
			if !ok || i >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[i])
		}
		name := get("name")
		if strings.HasPrefix(name, "示例：") || strings.HasPrefix(name, "示例:") {
			continue
		}
		if len(rows)+len(errs) >= MaxRows {
			return nil, nil, &ImportError{Msg: fmt.Sprintf("一次最多导入 %d 行，请拆成几个文件", MaxRows)}
		}
		row := Row{Line: line, Name: CleanName(name), Contact: get("contact"), Phone: get("phone"), Address: get("address"), Remark: get("remark")}
		if row.Name == "" {
			errs = append(errs, RowError{Row: line, Message: "名称不能为空"})
			continue
		}
		if msg := tooLong(row); msg != "" {
			errs = append(errs, RowError{Row: line, Message: msg})
			continue
		}
		rows = append(rows, row)
	}
	if cols == nil {
		return nil, nil, &ImportError{Msg: "文件是空的，请先下载模板再填写"}
	}
	return rows, errs, nil
}

func blank(rec []string) bool {
	for _, c := range rec {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

func tooLong(r Row) string {
	vals := map[string]string{"name": r.Name, "contact": r.Contact, "phone": r.Phone, "address": r.Address, "remark": r.Remark}
	for _, f := range fieldOrder {
		if n := utf8.RuneCountInString(vals[f]); n > limits[f] {
			return fmt.Sprintf("%s太长了（最多 %d 个字，现在 %d 个字）", labels[f], limits[f], n)
		}
	}
	return ""
}

// Run writes the parsed rows one by one. existing holds NameKey values already in the shop;
// names repeated inside the file are skipped too. write returns Duplicate for a clash it
// detects itself, or an error (reported against the row; the import goes on).
func Run(ctx context.Context, rows []Row, parseErrs []RowError, existing map[string]bool, write func(context.Context, Row) (Outcome, error)) Result {
	res := Result{Errors: append([]RowError{}, parseErrs...), SkippedRows: []RowError{}}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if ctx.Err() != nil {
			res.Errors = append(res.Errors, RowError{Row: row.Line, Message: "导入超时，这一行及之后的行没有导入，请重新上传同一个文件继续"})
			break
		}
		key := NameKey(row.Name)
		if existing[key] || seen[key] {
			res.skipName(row.Line)
			continue
		}
		out, err := write(ctx, row)
		if err != nil {
			res.Errors = append(res.Errors, RowError{Row: row.Line, Message: "保存失败，请稍后重试"})
			continue
		}
		seen[key] = true
		if out == Duplicate {
			res.skipName(row.Line)
			continue
		}
		if out == DuplicateMobile {
			res.Skipped++
			res.SkippedByPhone++
			res.SkippedRows = append(res.SkippedRows, RowError{Row: row.Line, Message: "手机号重复，已跳过（该手机号已属于另一位客户）"})
			continue
		}
		res.Created++
	}
	sort.SliceStable(res.Errors, func(i, j int) bool { return res.Errors[i].Row < res.Errors[j].Row })
	return res
}

func (r *Result) skipName(line int) {
	r.Skipped++
	r.SkippedByName++
	r.SkippedRows = append(r.SkippedRows, RowError{Row: line, Message: "名称重复，已跳过"})
}
