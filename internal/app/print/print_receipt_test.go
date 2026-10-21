package print_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	adapterprint "github.com/hanmahong5-arch/lurus-tally/internal/adapter/print"
	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
	appprint "github.com/hanmahong5-arch/lurus-tally/internal/app/print"
	domainbill "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

type fakePrinter struct {
	status  adapterprint.PrinterStatus
	printed []string
	times   int
}

func (f *fakePrinter) Print(_ context.Context, _ string, content string, times int) (string, error) {
	f.printed = append(f.printed, content)
	f.times = times
	return "order-1", nil
}
func (f *fakePrinter) Status(context.Context, string) (adapterprint.PrinterStatus, error) {
	return f.status, nil
}
func (f *fakePrinter) Bind(context.Context, string, string, string) error { return nil }

type fakeSettings struct{ s appacct.PrintSettings }

func (f fakeSettings) Execute(context.Context, uuid.UUID) (appacct.PrintSettings, error) {
	return f.s, nil
}

type fakeBills struct{ head *domainbill.BillHead }

func (f fakeBills) GetBill(context.Context, uuid.UUID, uuid.UUID) (*domainbill.BillHead, error) {
	return f.head, nil
}
func (f fakeBills) GetBillItems(context.Context, uuid.UUID, uuid.UUID) ([]*domainbill.BillItem, error) {
	return []*domainbill.BillItem{{
		ProductID: uuid.Nil, UnitName: "个", Qty: decimal.NewFromInt(2),
		UnitPrice: decimal.NewFromFloat(3.5), LineAmount: decimal.NewFromInt(7),
	}}, nil
}

type fakeNames struct{}

func (fakeNames) ProductNames(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	return map[uuid.UUID]string{ids[0]: "矿泉水"}, nil
}

func configured() fakeSettings {
	return fakeSettings{appacct.PrintSettings{ShopName: "老王店", Printer: &appacct.PrinterSettings{SN: "SN1", Key: "k", Paper: "58"}}}
}

func saleHead() *domainbill.BillHead {
	return &domainbill.BillHead{BillNo: "SO-1", BillType: domainbill.BillTypeSale, TotalAmount: decimal.NewFromInt(7),
		PaidAmount: decimal.NewFromInt(10), CreatedAt: time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)}
}

func TestPrintReceipt_Unavailable(t *testing.T) {
	uc := appprint.New(fakeBills{saleHead()}, fakeNames{}, configured(), nil)
	if _, err := uc.PrintReceipt(context.Background(), uuid.New(), uuid.New(), 1); !errors.Is(err, appprint.ErrCloudPrintUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestPrintReceipt_NotConfigured(t *testing.T) {
	uc := appprint.New(fakeBills{saleHead()}, fakeNames{}, fakeSettings{}, &fakePrinter{})
	if _, err := uc.PrintReceipt(context.Background(), uuid.New(), uuid.New(), 1); !errors.Is(err, appprint.ErrPrinterNotConfigured) {
		t.Fatalf("err = %v", err)
	}
}

func TestPrintReceipt_OfflineCarriesStatusText(t *testing.T) {
	p := &fakePrinter{status: adapterprint.PrinterStatus{Text: "离线。"}}
	uc := appprint.New(fakeBills{saleHead()}, fakeNames{}, configured(), p)
	_, err := uc.PrintReceipt(context.Background(), uuid.New(), uuid.New(), 1)
	if !errors.Is(err, appprint.ErrPrinterOffline) || !strings.Contains(err.Error(), "离线。") {
		t.Fatalf("err = %v", err)
	}
	if len(p.printed) != 0 {
		t.Fatal("离线时不应下发打印")
	}
}

func TestPrintReceipt_Success(t *testing.T) {
	p := &fakePrinter{status: adapterprint.PrinterStatus{Online: true, Normal: true}}
	uc := appprint.New(fakeBills{saleHead()}, fakeNames{}, configured(), p)
	id, err := uc.PrintReceipt(context.Background(), uuid.New(), uuid.New(), 9)
	if err != nil || id != "order-1" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if p.times != 3 {
		t.Errorf("份数应封顶 3, got %d", p.times)
	}
	c := p.printed[0]
	for _, want := range []string{"老王店", "SO-1", "矿泉水", "3.50", "找零", "3.00"} {
		if !strings.Contains(c, want) {
			t.Errorf("小票缺少 %q: %s", want, c)
		}
	}
}

func TestPrintReceipt_RejectsNonSale(t *testing.T) {
	h := saleHead()
	h.BillType = domainbill.BillTypePurchase
	p := &fakePrinter{status: adapterprint.PrinterStatus{Online: true, Normal: true}}
	uc := appprint.New(fakeBills{h}, fakeNames{}, configured(), p)
	if _, err := uc.PrintReceipt(context.Background(), uuid.New(), uuid.New(), 1); !errors.Is(err, appprint.ErrNotSaleBill) {
		t.Fatalf("err = %v", err)
	}
}

func TestStatus(t *testing.T) {
	info, _ := appprint.New(nil, nil, configured(), nil).Status(context.Background(), uuid.New())
	if info.CloudPrintAvailable || info.Configured {
		t.Errorf("无云打印时应全为假: %+v", info)
	}
	p := &fakePrinter{status: adapterprint.PrinterStatus{Online: true, Normal: true, Text: "在线，工作状态正常。"}}
	info, _ = appprint.New(nil, nil, configured(), p).Status(context.Background(), uuid.New())
	if !info.CloudPrintAvailable || !info.Configured || !info.Online {
		t.Errorf("%+v", info)
	}
}

type fakePayTypes struct {
	pt string
	ok bool
}

func (f fakePayTypes) LatestPayType(context.Context, uuid.UUID, uuid.UUID) (string, bool, error) {
	return f.pt, f.ok, nil
}

type fakeMembers map[uuid.UUID]string

func (f fakeMembers) MemberNames(context.Context, uuid.UUID) (map[uuid.UUID]string, error) {
	return f, nil
}

func TestPrintReceipt_PayMethodAndCashier(t *testing.T) {
	creator := uuid.New()
	h := saleHead()
	h.CreatorID = creator
	p := &fakePrinter{status: adapterprint.PrinterStatus{Online: true, Normal: true}}
	uc := appprint.New(fakeBills{h}, fakeNames{}, configured(), p).
		WithReceiptExtras(fakePayTypes{"wechat", true}, fakeMembers{creator: "张三"})
	if _, err := uc.PrintReceipt(context.Background(), uuid.New(), uuid.New(), 1); err != nil {
		t.Fatal(err)
	}
	c := p.printed[0]
	for _, want := range []string{"付款：微信", "收银：张三"} {
		if !strings.Contains(c, want) {
			t.Errorf("小票缺少 %q: %s", want, c)
		}
	}
}

func TestPrintReceipt_NoPaymentOrNameLeavesLinesOff(t *testing.T) {
	h := saleHead()
	h.CreatorID = uuid.New()
	p := &fakePrinter{status: adapterprint.PrinterStatus{Online: true, Normal: true}}
	uc := appprint.New(fakeBills{h}, fakeNames{}, configured(), p).
		WithReceiptExtras(fakePayTypes{}, fakeMembers{h.CreatorID: "未命名成员"})
	if _, err := uc.PrintReceipt(context.Background(), uuid.New(), uuid.New(), 1); err != nil {
		t.Fatal(err)
	}
	c := p.printed[0]
	if strings.Contains(c, "付款：") || strings.Contains(c, "收银：") {
		t.Errorf("不应出现付款/收银行: %s", c)
	}
}

func TestPrintReceipt_PayMethodMapping(t *testing.T) {
	for pt, want := range map[string]string{"cash": "现金", "alipay": "支付宝", "card": "银行卡", "transfer": "其他"} {
		p := &fakePrinter{status: adapterprint.PrinterStatus{Online: true, Normal: true}}
		uc := appprint.New(fakeBills{saleHead()}, fakeNames{}, configured(), p).WithReceiptExtras(fakePayTypes{pt, true}, nil)
		if _, err := uc.PrintReceipt(context.Background(), uuid.New(), uuid.New(), 1); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(p.printed[0], "付款："+want) {
			t.Errorf("%s: want %s in %s", pt, want, p.printed[0])
		}
	}
}
