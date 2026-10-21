package partnerimport_test

import (
	"context"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/partnerimport"
)

func gbk(t *testing.T, s string) []byte {
	t.Helper()
	out, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestParse_GBKFile(t *testing.T) {
	rows, errs, err := partnerimport.Parse(gbk(t, "客户名称,联系人,电话\n张三五金,张三,13800000000\n李四建材,李四,\n"))
	if err != nil || len(errs) != 0 {
		t.Fatalf("err=%v errs=%v", err, errs)
	}
	if len(rows) != 2 || rows[0].Name != "张三五金" || rows[0].Phone != "13800000000" || rows[1].Line != 3 {
		t.Fatalf("rows=%+v", rows)
	}
}

func TestParse_EmptyNameReportsLine(t *testing.T) {
	rows, errs, err := partnerimport.Parse([]byte("名称,联系人\n甲,a\n,b\n\n乙,c\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || len(errs) != 1 || errs[0].Row != 3 || errs[0].Message != "名称不能为空" {
		t.Fatalf("rows=%+v errs=%+v", rows, errs)
	}
}

func TestParse_MissingNameColumn(t *testing.T) {
	_, _, err := partnerimport.Parse([]byte("联系人,电话\na,b\n"))
	var ie *partnerimport.ImportError
	if err == nil || !strings.Contains(err.Error(), "名称") {
		t.Fatalf("err=%v", err)
	}
	_ = ie
}

func TestParse_TooLongName(t *testing.T) {
	_, errs, _ := partnerimport.Parse([]byte("名称\n" + strings.Repeat("长", 129) + "\n"))
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "太长") {
		t.Fatalf("errs=%+v", errs)
	}
}

func TestTemplate_HasBOMAndExamples(t *testing.T) {
	for _, k := range []partnerimport.Kind{partnerimport.Customers, partnerimport.Suppliers} {
		tpl := k.Template()
		if !strings.HasPrefix(tpl, "\xef\xbb\xbf") {
			t.Fatal("missing BOM")
		}
		if strings.Count(tpl, "示例：") != 2 {
			t.Fatalf("want 2 example rows: %q", tpl)
		}
		// Untouched examples import nothing.
		rows, errs, err := partnerimport.Parse([]byte(tpl))
		if err != nil || len(rows) != 0 || len(errs) != 0 {
			t.Fatalf("rows=%v errs=%v err=%v", rows, errs, err)
		}
	}
}

func TestRun_SkipsExistingAndFileDuplicates(t *testing.T) {
	rows := []partnerimport.Row{
		{Line: 2, Name: "张三 五金"}, {Line: 3, Name: "ＡＢＣ商行"}, {Line: 4, Name: "新客户"}, {Line: 5, Name: "新客户"},
	}
	existing := map[string]bool{partnerimport.NameKey("张三  五金"): true, partnerimport.NameKey("abc商行"): true}
	var written []string
	res := partnerimport.Run(context.Background(), rows, nil, existing, func(_ context.Context, r partnerimport.Row) (partnerimport.Outcome, error) {
		written = append(written, r.Name)
		return partnerimport.Created, nil
	})
	if res.Created != 1 || res.Skipped != 3 || res.SkippedByName != 3 || res.SkippedByPhone != 0 || len(written) != 1 || written[0] != "新客户" {
		t.Fatalf("res=%+v written=%v", res, written)
	}
}
