package export

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

func zipTexts(t *testing.T, b []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		raw, _ := io.ReadAll(rc)
		_ = rc.Close()
		out[f.Name] = string(bytes.TrimPrefix(raw, utf8BOM))
	}
	return out
}

func lastLine(s string) string {
	s = strings.TrimRight(s, "\r\n")
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(s)
}

func TestAllExport_FailureWritesNoteAndMarksIncomplete(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()
	for i, s := range allSheets[:3] {
		if i == 2 {
			mock.ExpectQuery(`SELECT`).WillReturnError(errors.New("boom"))
			break
		}
		cols := s.visible(true)
		names := []string{}
		vals := []driver.Value{}
		for _, c := range cols {
			names = append(names, c.name)
			vals = append(vals, "v")
		}
		names = append(names, "id")
		vals = append(vals, uuid.NewString())
		mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows(names).AddRow(vals...))
	}
	mock.ExpectQuery(`FROM tally.tenant`).WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow("店"))

	var buf bytes.Buffer
	ctx := WithRequestID(context.Background(), "req-123")
	_, err := NewAllExportUseCase(db, nil).Execute(ctx, tenantID, true, time.Now(), &buf)
	if err == nil {
		t.Fatal("want error")
	}
	files := zipTexts(t, buf.Bytes())
	note, ok := files["导出失败.txt"]
	if !ok {
		t.Fatalf("missing 导出失败.txt, files=%v", files)
	}
	for _, want := range []string{allSheets[2].file, allSheets[0].file, allSheets[1].file, "请重新导出或联系客服"} {
		if !strings.Contains(note, want) {
			t.Errorf("note lacks %q: %s", want, note)
		}
	}
	if got := lastLine(files["说明.txt"]); got != "完整性：失败" {
		t.Errorf("last line = %q", got)
	}
}

func TestAllExport_SuccessHasNoFailureNote(t *testing.T) {
	out := runAll(t, true)
	if _, ok := out["导出失败.txt"]; ok {
		t.Error("unexpected 导出失败.txt")
	}
	if got := lastLine(out["说明.txt"][0][0]); got != "完整性：成功" {
		t.Errorf("last line = %q", got)
	}
}

func TestAllExport_PurchaseUnitPriceTwoDecimals(t *testing.T) {
	for _, s := range allSheets {
		if s.file != "采购明细.csv" {
			continue
		}
		for _, c := range s.cols {
			if c.name == "单价" {
				if !strings.Contains(c.expr, "ROUND(bi.unit_price, 2)::text") {
					t.Errorf("单价 expr = %s", c.expr)
				}
				return
			}
		}
	}
	t.Fatal("单价 column not found")
}
