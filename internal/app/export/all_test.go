package export

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/csv"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

func runAll(t *testing.T, showCost bool) map[string][][]string {
	t.Helper()
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()
	for _, s := range allSheets {
		cols := s.visible(showCost)
		names := make([]string, 0, len(cols)+1)
		vals := make([]driver.Value, 0, len(cols)+1)
		for _, c := range cols {
			names = append(names, c.name)
			vals = append(vals, "v-"+c.name)
		}
		names = append(names, "id")
		vals = append(vals, uuid.NewString())
		rows := sqlmock.NewRows(names).AddRow(vals...)
		mock.ExpectQuery(`SELECT`).WithArgs(tenantID, uuid.Nil.String(), allPageSize).WillReturnRows(rows)
	}
	mock.ExpectQuery(`FROM tally.tenant`).WithArgs(tenantID).
		WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow("绿荫苗圃"))

	var buf bytes.Buffer
	n, err := NewAllExportUseCase(db, nil).Execute(context.Background(), tenantID, showCost,
		time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC), &buf)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n != len(allSheets) {
		t.Fatalf("rows = %d, want %d", n, len(allSheets))
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	out := map[string][][]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		if !bytes.HasPrefix(b, utf8BOM) {
			t.Fatalf("%s: missing BOM", f.Name)
		}
		b = b[len(utf8BOM):]
		if strings.HasSuffix(f.Name, ".txt") {
			out[f.Name] = [][]string{{string(b)}}
			continue
		}
		recs, err := csv.NewReader(bytes.NewReader(b)).ReadAll()
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		out[f.Name] = recs
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAllExport_FilesAndHeaders(t *testing.T) {
	out := runAll(t, true)
	want := []string{"商品.csv", "客户.csv", "供应商.csv", "仓库.csv", "库存.csv", "采购单.csv", "采购明细.csv", "销售单.csv", "销售明细.csv", "收付款.csv", "发票.csv", "说明.txt"}
	if len(out) != len(want) {
		t.Fatalf("files = %d, want %d", len(out), len(want))
	}
	for _, f := range want {
		if _, ok := out[f]; !ok {
			t.Errorf("missing %s", f)
		}
	}
	if h := strings.Join(out["采购明细.csv"][0], ","); !strings.Contains(h, "进价") || !strings.Contains(h, "单价") {
		t.Errorf("cost viewer header = %s", h)
	}
	if len(out["商品.csv"]) != 2 {
		t.Errorf("商品.csv rows = %d", len(out["商品.csv"]))
	}
	txt := out["说明.txt"][0][0]
	for _, w := range []string{"绿荫苗圃", "2026-10-14 17:00:00", "商品.csv（1 行）", "字段："} {
		if !strings.Contains(txt, w) {
			t.Errorf("readme missing %q", w)
		}
	}
}

func TestAllExport_NoCost_DropsCostColumns(t *testing.T) {
	out := runAll(t, false)
	banned := map[string][]string{
		"采购明细.csv": {"进价", "单价", "金额"},
		"销售明细.csv": {"进价"},
		"采购单.csv":  {"总额", "已付"},
		"库存.csv":   {"单位成本", "库存金额"},
		"商品.csv":   {"进价"},
	}
	for f, cols := range banned {
		hdr := out[f][0]
		for _, b := range cols {
			for _, h := range hdr {
				if h == b {
					t.Errorf("%s still has column %s", f, b)
				}
			}
		}
	}
	// Sales amounts stay.
	found := false
	for _, h := range out["销售单.csv"][0] {
		if h == "总额" {
			found = true
		}
	}
	if !found {
		t.Error("销售单.csv lost 总额")
	}
}

func TestAllExport_Paginates(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()
	uc := NewAllExportUseCase(db, nil)
	uc.pageSize = 2
	s := allSheets[3] // 仓库
	cols := s.visible(true)
	names := make([]string, 0, len(cols)+1)
	for _, c := range cols {
		names = append(names, c.name)
	}
	names = append(names, "id")
	row := func(id string) []driver.Value {
		v := make([]driver.Value, 0, len(names))
		for range cols {
			v = append(v, "x")
		}
		return append(v, id)
	}
	id1, id2, id3 := uuid.NewString(), uuid.NewString(), uuid.NewString()
	mock.ExpectQuery(`LIMIT`).WithArgs(tenantID, uuid.Nil.String(), 2).
		WillReturnRows(sqlmock.NewRows(names).AddRow(row(id1)...).AddRow(row(id2)...))
	mock.ExpectQuery(`LIMIT`).WithArgs(tenantID, id2, 2).
		WillReturnRows(sqlmock.NewRows(names).AddRow(row(id3)...))
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	n, err := uc.writeSheet(context.Background(), zw, tenantID, s, true, time.Now())
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
