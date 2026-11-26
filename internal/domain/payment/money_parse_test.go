package payment_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

func TestParseMoneyFile_AlipayIncomeAndExpense(t *testing.T) {
	raw := "" +
		"#账号：[2088001]\n" +
		"账务流水号,业务流水号,商户订单号,商品名称,发生时间,对方账号,收入金额（+元）,支出金额（-元）,账户余额（元）,交易渠道,业务类型,备注\n" +
		"A1,B1,C1,卫生纸,2026-01-02 10:00:00,龙口老李,1280.00,0.00,5000.00,支付宝,在线支付,货款\n" +
		"A2,B2,C2,房租,2026-01-03 11:00:00,房东,0.00,800.00,4200.00,支付宝,转账,房租\n" +
		"#-----------------------------------------账务明细列表结束------------------------------------\n"
	channel, account, lines, err := domain.ParseMoneyFile([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if channel != domain.ChannelAlipay || account != "2088001" || len(lines) != 2 {
		t.Fatalf("channel=%s account=%s lines=%d", channel, account, len(lines))
	}
	if !lines[0].Amount.Equal(decimal.RequireFromString("1280.00")) || lines[0].Counterparty != "龙口老李" || lines[0].ExternalID != "A1" {
		t.Fatalf("income line = %+v", lines[0])
	}
	if !lines[1].Amount.Equal(decimal.RequireFromString("-800.00")) || lines[1].Counterparty != "房东" {
		t.Fatalf("expense line = %+v", lines[1])
	}
	want := time.Date(2026, 1, 2, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
	if !lines[0].BookedAt.Equal(want) {
		t.Fatalf("booked at %s", lines[0].BookedAt)
	}
}

func TestParseMoneyFile_GBKAndDuplicate(t *testing.T) {
	text := "账务流水号,发生时间,对方账号,收入金额（+元）,支出金额（-元）,备注\n" +
		"A1,2026-01-02 10:00:00,老李,10.00,0.00,货款\n" +
		"A1,2026-01-02 10:00:00,老李,10.00,0.00,货款\n"
	encoded, _, err := transform.Bytes(simplifiedchinese.GB18030.NewEncoder(), []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(encoded, []byte(text)) {
		t.Fatal("fixture stayed utf-8")
	}
	_, _, lines, err := domain.ParseMoneyFile(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
}

func TestParseMoneyFile_DuplicateAmountConflicts(t *testing.T) {
	raw := "账务流水号,发生时间,对方账号,收入金额（+元）,支出金额（-元）\n" +
		"A1,2026-01-02 10:00:00,老李,10.00,0\n" +
		"A1,2026-01-02 10:00:00,老李,11.00,0\n"
	_, _, _, err := domain.ParseMoneyFile([]byte(raw))
	if err == nil || !strings.Contains(err.Error(), "A1") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseMoneyFile_WeChatFundSkipsSummary(t *testing.T) {
	raw := "记账时间,微信支付业务单号,资金流水单号,业务名称,业务类型,收支类型,收支金额(元),账户结余(元),资金变更提交申请人,备注,业务凭证号\n" +
		"`2026-01-02 10:00:00,`4200,`F1,交易,交易,收入,`1280.00,`5000.00,system,货款,`v1\n" +
		"`2026-01-04 12:00:00,`4201,`F2,退款,退款,支出,`15.50,`4984.50,system,退回,`v2\n" +
		"资金流水总笔数,2\n收入总笔数,1\n"
	channel, _, lines, err := domain.ParseMoneyFile([]byte("\uFEFF" + raw))
	if err != nil {
		t.Fatal(err)
	}
	if channel != domain.ChannelWechat || len(lines) != 2 {
		t.Fatalf("channel=%s lines=%d", channel, len(lines))
	}
	if lines[0].ExternalID != "F1" || !lines[0].Amount.Equal(decimal.RequireFromString("1280.00")) {
		t.Fatalf("in = %+v", lines[0])
	}
	if lines[1].ExternalID != "F2" || !lines[1].Amount.Equal(decimal.RequireFromString("-15.50")) {
		t.Fatalf("out = %+v", lines[1])
	}
}

func TestParseMoneyFile_WeChatTradeRefund(t *testing.T) {
	raw := "交易时间,微信订单号,交易状态,应结订单金额,退款金额,微信退款单号,商品名称\n" +
		"2026-02-01 09:00:00,W1,SUCCESS,20.00,0.00,,卫生纸\n" +
		"2026-02-02 09:00:00,W1,REFUND,20.00,20.00,R1,卫生纸\n" +
		"2026-02-03 09:00:00,W2,NOTPAY,9.00,0.00,,未付\n"
	_, _, lines, err := domain.ParseMoneyFile([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0].ExternalID != "W1" || lines[1].ExternalID != "R1" {
		t.Fatalf("%+v", lines)
	}
	if !lines[1].Amount.Equal(decimal.RequireFromString("-20.00")) {
		t.Fatalf("refund = %s", lines[1].Amount)
	}
}

func TestParseMoneyFile_PersonalAlipayAndWeChat(t *testing.T) {
	ali := "账号:[2088001]\n" +
		"交易号,付款时间,交易对方,商品名称,金额（元）,收/支,交易状态,备注\n" +
		"T1,2026-03-01 08:30:00,A客户,卫生纸,1280.00,收入,交易成功,货款\n" +
		"T2,2026-03-02 08:30:00,房东,房租,800.00,支出,交易成功,\n" +
		"T3,2026-03-03 08:30:00,路人,未付,9.00,支出,交易关闭,\n"
	channel, account, lines, err := domain.ParseMoneyFile([]byte(ali))
	if err != nil {
		t.Fatal(err)
	}
	if channel != domain.ChannelAlipay || account != "2088001" || len(lines) != 2 {
		t.Fatalf("ali channel=%s account=%s lines=%d", channel, account, len(lines))
	}
	if !lines[0].Amount.Equal(decimal.RequireFromString("1280.00")) || lines[0].Counterparty != "A客户" {
		t.Fatalf("in %+v", lines[0])
	}
	if !lines[1].Amount.Equal(decimal.RequireFromString("-800.00")) {
		t.Fatalf("out %s", lines[1].Amount)
	}
	wx := "交易时间,交易类型,交易对方,商品,收/支,金额(元),当前状态,交易单号,备注\n" +
		"2026-03-04 09:00:00,转账,龙口老李,/,支出,¥15.50,支付成功,WX1,货款\n"
	channel, _, lines, err = domain.ParseMoneyFile([]byte(wx))
	if err != nil {
		t.Fatal(err)
	}
	if channel != domain.ChannelWechat || len(lines) != 1 || lines[0].ExternalID != "WX1" || !lines[0].Amount.Equal(decimal.RequireFromString("-15.50")) {
		t.Fatalf("wx %+v", lines)
	}
}

func TestParseMoneyFile_RejectsUnknownAndEmpty(t *testing.T) {
	if _, _, _, err := domain.ParseMoneyFile([]byte("日期,摘要\n")); err != domain.ErrMoneyFormat {
		t.Fatalf("unknown = %v", err)
	}
	raw := "账务流水号,发生时间,收入金额（+元）,支出金额（-元）\n#结束\n"
	if _, _, _, err := domain.ParseMoneyFile([]byte(raw)); err != domain.ErrMoneyEmpty {
		t.Fatalf("empty = %v", err)
	}
}
