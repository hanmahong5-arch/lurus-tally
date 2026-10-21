package account_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
)

type fakePrintRepo struct {
	got appacct.PrintSettings
	err error
}

func (f *fakePrintRepo) GetPrint(context.Context, uuid.UUID) (appacct.PrintSettings, error) {
	return f.got, f.err
}
func (f *fakePrintRepo) PutPrint(_ context.Context, _ uuid.UUID, s appacct.PrintSettings) error {
	if f.err != nil {
		return f.err
	}
	f.got = s
	return nil
}

func TestPutPrintSettings_TrimsAndStores(t *testing.T) {
	r := &fakePrintRepo{}
	out, err := appacct.NewPutPrintSettings(r).Execute(context.Background(), uuid.New(),
		appacct.PrintSettings{ShopName: "  老王五金  ", Phone: " 138 ", Address: "街1号", ReceiptFooter: "谢谢惠顾 "})
	if err != nil {
		t.Fatal(err)
	}
	if out.ShopName != "老王五金" || r.got.ShopName != "老王五金" || r.got.ReceiptFooter != "谢谢惠顾" {
		t.Fatalf("not trimmed: %+v", r.got)
	}
}

func TestPutPrintSettings_TooLong(t *testing.T) {
	r := &fakePrintRepo{}
	_, err := appacct.NewPutPrintSettings(r).Execute(context.Background(), uuid.New(),
		appacct.PrintSettings{ReceiptFooter: strings.Repeat("字", 201)})
	if !errors.Is(err, appacct.ErrPrintSettingsInvalid) {
		t.Fatalf("want invalid, got %v", err)
	}
	if r.got.ReceiptFooter != "" {
		t.Fatal("must not persist")
	}
}

func TestGetPrintSettings_PassesThrough(t *testing.T) {
	r := &fakePrintRepo{got: appacct.PrintSettings{ShopName: "甲"}}
	s, err := appacct.NewGetPrintSettings(r).Execute(context.Background(), uuid.New())
	if err != nil || s.ShopName != "甲" {
		t.Fatalf("got %+v %v", s, err)
	}
}

type fakeBinder struct {
	calls int
	err   error
}

func (b *fakeBinder) Bind(context.Context, string, string, string) error {
	b.calls++
	return b.err
}

func TestPrintSettings_OldJSONWithoutPrinterDecodes(t *testing.T) {
	var s appacct.PrintSettings
	if err := json.Unmarshal([]byte(`{"shop_name":"甲","phone":"1","address":"a","receipt_footer":"f"}`), &s); err != nil {
		t.Fatal(err)
	}
	if s.Printer != nil || s.ShopName != "甲" {
		t.Fatalf("旧数据应解码成无打印机: %+v", s)
	}
	out, _ := json.Marshal(s)
	if strings.Contains(string(out), "printer") {
		t.Fatalf("无打印机时不应输出 printer 字段: %s", out)
	}
}

func TestPutPrintSettings_PrinterBindsOnSNChangeAndMasksKey(t *testing.T) {
	r := &fakePrintRepo{}
	b := &fakeBinder{}
	uc := appacct.NewPutPrintSettings(r).WithBinder(b)
	out, err := uc.Execute(context.Background(), uuid.New(), appacct.PrintSettings{
		Printer: &appacct.PrinterSettings{SN: " 123456789 ", Key: "abcd1234", Name: "柜台", Paper: "80", AutoPrint: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.calls != 1 {
		t.Fatalf("首次保存应绑定一次, got %d", b.calls)
	}
	if out.Printer.Key != "****1234" || r.got.Printer.Key != "abcd1234" {
		t.Fatalf("回显应只剩尾 4 位而库里存全: out=%q stored=%q", out.Printer.Key, r.got.Printer.Key)
	}

	// 回传脱敏的 KEY、SN 不变: 沿用旧 KEY, 不再绑定。
	_, err = uc.Execute(context.Background(), uuid.New(), appacct.PrintSettings{
		Printer: &appacct.PrinterSettings{SN: "123456789", Key: "****1234", Paper: "80"},
	})
	if err != nil || b.calls != 1 || r.got.Printer.Key != "abcd1234" {
		t.Fatalf("err=%v calls=%d key=%q", err, b.calls, r.got.Printer.Key)
	}

	// 不带 printer 的旧客户端保存: 不得清掉打印机。
	if _, err = uc.Execute(context.Background(), uuid.New(), appacct.PrintSettings{ShopName: "乙"}); err != nil {
		t.Fatal(err)
	}
	if r.got.Printer == nil || r.got.Printer.SN != "123456789" {
		t.Fatalf("打印机被误清: %+v", r.got.Printer)
	}

	// SN 与 KEY 都留空: 解除。
	if _, err = uc.Execute(context.Background(), uuid.New(), appacct.PrintSettings{Printer: &appacct.PrinterSettings{}}); err != nil {
		t.Fatal(err)
	}
	if r.got.Printer != nil {
		t.Fatalf("应解除打印机: %+v", r.got.Printer)
	}
}

func TestPutPrintSettings_PrinterValidationAndBindFailure(t *testing.T) {
	bad := []appacct.PrinterSettings{
		{SN: "ab cd", Key: "k"},
		{SN: strings.Repeat("1", 33), Key: "k"},
		{SN: "123", Key: ""},
		{SN: "123", Key: "k", Paper: "70"},
	}
	for _, p := range bad {
		p := p
		_, err := appacct.NewPutPrintSettings(&fakePrintRepo{}).Execute(context.Background(), uuid.New(), appacct.PrintSettings{Printer: &p})
		if !errors.Is(err, appacct.ErrPrintSettingsInvalid) {
			t.Errorf("%+v: want invalid, got %v", p, err)
		}
	}
	r := &fakePrintRepo{}
	_, err := appacct.NewPutPrintSettings(r).WithBinder(&fakeBinder{err: errors.New("boom")}).
		Execute(context.Background(), uuid.New(), appacct.PrintSettings{Printer: &appacct.PrinterSettings{SN: "123", Key: "k"}})
	if !errors.Is(err, appacct.ErrPrinterBindFailed) || r.got.Printer != nil {
		t.Fatalf("绑定失败不得落库: err=%v got=%+v", err, r.got.Printer)
	}
}
