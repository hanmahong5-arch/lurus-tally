package payment

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const (
	ChannelAlipay = "alipay"
	ChannelWechat = "wechat"
	MoneyOpen     = "open"
	MoneyMatched  = "matched"
	MoneyIgnored  = "ignored"
	MoneyAR       = "ar"
	MoneyAP       = "ap"
)

const moneyLineMax = 2000

var (
	ErrMoneyFormat    = errors.New("认不出这是支付宝或微信的账单，请导出 csv 再上传")
	ErrMoneyEmpty     = errors.New("账单里没有可以入账的流水")
	ErrMoneyAccount   = errors.New("账户标识不能超过 64 个字")
	ErrMoneyLine      = errors.New("找不到这笔流水")
	ErrMoneyBill      = errors.New("这张单据不是已审核的销售单或采购单")
	ErrMoneyDirection = errors.New("收款只能记到销售单，付款只能记到采购单")
	ErrMoneyClosed    = errors.New("这笔流水已经处理过了")
	ErrMoneyTaken     = errors.New("这笔流水刚刚被处理了，请刷新后再看")
)

// ParseError is a row the file named, shown to the person who uploaded it.
type ParseError struct{ Msg string }

func (e *ParseError) Error() string { return e.Msg }

// ParsedLine is one movement taken from an official statement file.
// Amount is signed: money in is positive, money out is negative.
type ParsedLine struct {
	ExternalID   string
	BookedAt     time.Time
	Amount       decimal.Decimal
	Counterparty string
	Memo         string
}

// MoneyLine is one stored statement movement.
type MoneyLine struct {
	ID           uuid.UUID
	Channel      string
	AccountRef   string
	ExternalID   string
	BookedAt     time.Time
	Amount       decimal.Decimal
	Counterparty string
	Memo         string
	Status       string
	BillID       *uuid.UUID
	PaymentID    *uuid.UUID
}

// MoneyBill is an approved sale or purchase that can still take a payment.
type MoneyBill struct {
	ID          uuid.UUID
	BillNo      string
	PartnerName string
	Direction   string
	Outstanding decimal.Decimal
	BillDate    time.Time
}
