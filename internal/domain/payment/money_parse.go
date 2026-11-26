package payment

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// ParseMoneyFile reads an Alipay account statement or a WeChat fund or trade bill.
// The account is the Alipay account named in the file header, when the file has one.
func ParseMoneyFile(raw []byte) (channel, account string, lines []ParsedLine, err error) {
	text, err := moneyText(raw)
	if err != nil {
		return "", "", nil, err
	}
	return parseMoneyText(text)
}

func moneyText(raw []byte) (string, error) {
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", ErrMoneyEmpty
	}
	if utf8.Valid(raw) {
		return string(raw), nil
	}
	out, _, err := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), raw)
	if err != nil || !utf8.Valid(out) {
		return "", ErrMoneyFormat
	}
	return string(out), nil
}

func parseMoneyText(text string) (channel, account string, lines []ParsedLine, err error) {
	rows, err := readMoneyCSV(text)
	if err != nil {
		return "", "", nil, ErrMoneyFormat
	}
	headerAt, kind := findMoneyHeader(rows)
	if headerAt < 0 {
		return "", "", nil, ErrMoneyFormat
	}
	account = accountBefore(rows, headerAt)
	if len([]rune(account)) > 64 {
		return "", "", nil, ErrMoneyAccount
	}
	header := rows[headerAt]
	seen := map[string]decimal.Decimal{}
	for i := headerAt + 1; i < len(rows); i++ {
		if stopMoneyRow(rows[i]) {
			break
		}
		line, skip, err := parseMoneyRow(kind, header, rows[i], i+1)
		if err != nil {
			return "", "", nil, err
		}
		if skip {
			continue
		}
		if prev, ok := seen[line.ExternalID]; ok {
			if !prev.Equal(line.Amount) {
				return "", "", nil, &ParseError{Msg: "流水号 " + line.ExternalID + " 出现了两次，金额还不一样"}
			}
			continue
		}
		seen[line.ExternalID] = line.Amount
		lines = append(lines, line)
		if len(lines) > moneyLineMax {
			return "", "", nil, &ParseError{Msg: "一次最多导入 2000 笔，请按月份拆开"}
		}
	}
	if len(lines) == 0 {
		return "", "", nil, ErrMoneyEmpty
	}
	channel = ChannelWechat
	if kind == "alipay" || kind == "aliuser" {
		channel = ChannelAlipay
	}
	return channel, account, lines, nil
}

func findMoneyHeader(rows [][]string) (int, string) {
	for i, row := range rows {
		switch {
		case col(row, "账务流水号") >= 0 && col(row, "收入金额") >= 0:
			return i, "alipay"
		case col(row, "资金流水单号") >= 0 && col(row, "收支金额") >= 0:
			return i, "fund"
		case col(row, "微信订单号") >= 0 && col(row, "应结订单金额") >= 0:
			return i, "trade"
		case col(row, "交易单号") >= 0 && col(row, "收/支") >= 0:
			return i, "wxuser"
		case col(row, "交易号") >= 0 && col(row, "收/支") >= 0:
			return i, "aliuser"
		}
	}
	return -1, ""
}

func parseMoneyRow(kind string, header, row []string, n int) (ParsedLine, bool, error) {
	if blankRow(row) {
		return ParsedLine{}, true, nil
	}
	switch kind {
	case "alipay":
		{
			amt, ok := alipayAmount(row, header)
			return finishMoneyRow(n, cell(row, col(header, "账务流水号")), cell(row, col(header, "发生时间")),
				firstNonEmpty(cell(row, col(header, "对方账号")), cell(row, col(header, "对方名称"))),
				joinParts(cell(row, col(header, "商品名称")), cell(row, col(header, "业务类型")), cell(row, col(header, "备注"))),
				amt, ok)
		}
	case "fund":
		{
			amt, ok := fundAmount(row, header)
			return finishMoneyRow(n, cell(row, col(header, "资金流水单号")), cell(row, col(header, "记账时间")), "",
				joinParts(cell(row, col(header, "业务名称")), cell(row, col(header, "业务类型")), cell(row, col(header, "备注"))),
				amt, ok)
		}
	case "aliuser":
		return walletRow(n, header, row, "交易号", "付款时间", "交易创建时间", "交易对方", "商品名称", "类型", "备注", "金额", "收/支", "交易状态")
	case "wxuser":
		return walletRow(n, header, row, "交易单号", "交易时间", "", "交易对方", "商品", "交易类型", "备注", "金额", "收/支", "当前状态")
	default:
		id := cell(row, col(header, "微信订单号"))
		refund, ok := moneyCell(cell(row, col(header, "退款金额")))
		if !ok {
			return ParsedLine{}, false, &ParseError{Msg: fmt.Sprintf("第%d行的金额认不出", n)}
		}
		amt, ok := moneyCell(cell(row, col(header, "应结订单金额")))
		if !ok {
			return ParsedLine{}, false, &ParseError{Msg: fmt.Sprintf("第%d行的金额认不出", n)}
		}
		if refund.IsPositive() {
			if rid := cell(row, col(header, "微信退款单号")); rid != "" {
				id = rid
			}
			amt = refund.Abs().Neg()
		} else if !tradeKeeps(cell(row, col(header, "交易状态"))) {
			return ParsedLine{}, true, nil
		}
		return finishMoneyRow(n, id, cell(row, col(header, "交易时间")), "", cell(row, col(header, "商品名称")), amt, true)
	}
}

func finishMoneyRow(n int, id, when, party, memo string, amt decimal.Decimal, ok bool) (ParsedLine, bool, error) {
	if !ok {
		return ParsedLine{}, false, &ParseError{Msg: fmt.Sprintf("第%d行的金额认不出", n)}
	}
	amt = amt.Round(2)
	if amt.IsZero() {
		return ParsedLine{}, true, nil
	}
	if id == "" {
		return ParsedLine{}, false, &ParseError{Msg: fmt.Sprintf("第%d行没有流水号", n)}
	}
	if len([]rune(id)) > 128 {
		return ParsedLine{}, false, &ParseError{Msg: fmt.Sprintf("第%d行的流水号太长", n)}
	}
	booked, ok := parseWhen(when)
	if !ok {
		return ParsedLine{}, false, &ParseError{Msg: fmt.Sprintf("第%d行的时间认不出", n)}
	}
	return ParsedLine{ExternalID: id, BookedAt: booked, Amount: amt, Counterparty: clip(party, 256), Memo: clip(memo, 512)}, false, nil
}

func alipayAmount(row, header []string) (decimal.Decimal, bool) {
	in, okIn := moneyCell(cell(row, col(header, "收入金额")))
	out, okOut := moneyCell(cell(row, col(header, "支出金额")))
	if !okIn || !okOut {
		return decimal.Zero, false
	}
	if out.IsNegative() {
		return in.Add(out), true
	}
	return in.Sub(out), true
}

func fundAmount(row, header []string) (decimal.Decimal, bool) {
	amt, ok := moneyCell(cell(row, col(header, "收支金额")))
	if !ok {
		return decimal.Zero, false
	}
	if strings.Contains(cell(row, col(header, "收支类型")), "支") {
		return amt.Abs().Neg(), true
	}
	return amt.Abs(), true
}

func tradeKeeps(status string) bool {
	return status == "" || strings.EqualFold(status, "SUCCESS") || strings.Contains(status, "成功")
}
