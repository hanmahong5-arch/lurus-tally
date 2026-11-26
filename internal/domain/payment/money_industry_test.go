package payment_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

type industryFile struct {
	name    string
	channel string
	account string
	lines   int
	sum     string
	party   string
	memo    string
	notUTF8 bool
	refused bool
}

func TestIndustryFiles_ParseAndSuggest(t *testing.T) {
	files := []industryFile{
		{name: "retail-wechat.csv", channel: domain.ChannelWechat, lines: 4, sum: "86.50", party: "王阿姨"},
		{name: "wholesale-alipay.csv", channel: domain.ChannelAlipay, account: "2088123456789012", lines: 2, sum: "1280.00", party: "龙口老李"},
		{name: "factory-alipay.csv", channel: domain.ChannelAlipay, account: "208800009999", lines: 2, sum: "6400.00", party: "鞍钢经销"},
		{name: "restaurant-wechat-fund.csv", channel: domain.ChannelWechat, lines: 3, sum: "2244.50", memo: "午市"},
		{name: "pharmacy-alipay.csv", channel: domain.ChannelAlipay, account: "2088777666", lines: 2, sum: "-611.50", party: "九州通"},
		{name: "shop-wechat-trade.csv", channel: domain.ChannelWechat, lines: 3, sum: "89.90", memo: "卫衣"},
		{name: "garage-wechat.csv", channel: domain.ChannelWechat, lines: 2, sum: "80.00", party: "张师傅"},
		{name: "materials-alipay.csv", channel: domain.ChannelAlipay, account: "2088666001", lines: 2, sum: "1100.00", party: "工地老周"},
		{name: "apparel-wechat-trade.csv", channel: domain.ChannelWechat, lines: 1, sum: "459.00", memo: "连衣裙"},
		{name: "nursery-alipay.csv", channel: domain.ChannelAlipay, account: "2088111222", lines: 2, sum: "460.00", party: "园林处"},
		{name: "hardware-alipay-gbk.csv", channel: domain.ChannelAlipay, account: "2088555001", lines: 2, sum: "463.20", party: "五金老赵", notUTF8: true},
		{name: "florist-wechat-refund-status.csv", channel: domain.ChannelWechat, lines: 1, sum: "50.00", party: "顾客"},
		{name: "bank-refused.csv", refused: true},
	}
	for _, f := range files {
		t.Run(f.name, func(t *testing.T) {
			raw := readIndustry(t, f.name)
			if f.notUTF8 && utf8.Valid(raw) {
				t.Fatal("file is utf-8")
			}
			channel, account, lines, err := domain.ParseMoneyFile(raw)
			if f.refused {
				if err != domain.ErrMoneyFormat {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if channel != f.channel || account != f.account || len(lines) != f.lines {
				t.Fatalf("channel=%s account=%s lines=%d", channel, account, len(lines))
			}
			sum := decimal.Zero
			seenParty, seenMemo := false, f.memo == ""
			for _, ln := range lines {
				sum = sum.Add(ln.Amount)
				if f.party != "" && ln.Counterparty == f.party {
					seenParty = true
				}
				if f.memo != "" && strings.Contains(ln.Memo, f.memo) {
					seenMemo = true
				}
			}
			if !sum.Equal(decimal.RequireFromString(f.sum)) || (f.party != "" && !seenParty) || !seenMemo {
				t.Fatalf("sum=%s party=%v memo=%v", sum, seenParty, seenMemo)
			}
		})
	}
}

func TestIndustryWholesale_SuggestsTheNamedPurchase(t *testing.T) {
	_, _, lines, err := domain.ParseMoneyFile(readIndustry(t, "wholesale-alipay.csv"))
	if err != nil {
		t.Fatal(err)
	}
	var pay domain.ParsedLine
	found := false
	for _, ln := range lines {
		if ln.ExternalID == "W1" {
			pay = ln
			found = true
		}
	}
	if !found {
		t.Fatal("missing W1")
	}
	li := uuid.New()
	other := uuid.New()
	suggested, hits := domain.SuggestMoney(pay.Amount, pay.Counterparty, pay.Memo, []domain.MoneyBill{
		{ID: li, PartnerName: "龙口老李", Direction: domain.MoneyAP, Outstanding: decimal.RequireFromString("1280.00")},
		{ID: other, PartnerName: "别家纸行", Direction: domain.MoneyAP, Outstanding: decimal.RequireFromString("1280.00")},
	})
	if !suggested || len(hits) != 1 || hits[0].ID != li {
		t.Fatalf("hits = %+v suggested=%v", hits, suggested)
	}
}

func TestBurstWholesale_EveryParseKeepsTheChecksum(t *testing.T) {
	raw := readIndustry(t, "burst-wholesale-alipay.csv")
	wantSum := decimal.RequireFromString("6500.00")
	wantWhen := time.Date(2026, 5, 1, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	check := func() {
		t.Helper()
		channel, account, lines, err := domain.ParseMoneyFile(raw)
		if err != nil {
			t.Fatal(err)
		}
		if channel != domain.ChannelAlipay || account != "2088000001" || len(lines) != 2000 {
			t.Fatalf("channel=%s account=%s lines=%d", channel, account, len(lines))
		}
		if lines[0].ExternalID != "BW0001" || lines[0].Counterparty != "批发客户" || !lines[0].BookedAt.Equal(wantWhen) {
			t.Fatalf("first = %+v", lines[0])
		}
		sum := decimal.Zero
		for _, ln := range lines {
			sum = sum.Add(ln.Amount)
		}
		if !sum.Equal(wantSum) {
			t.Fatalf("sum = %s", sum)
		}
	}
	check()
	const workers = 32
	const rounds = 8
	errCh := make(chan error, workers)
	start := time.Now()
	for i := 0; i < workers; i++ {
		go func() {
			var bad error
			for r := 0; r < rounds; r++ {
				_, _, lines, err := domain.ParseMoneyFile(raw)
				if err != nil {
					bad = err
					break
				}
				if len(lines) != 2000 {
					bad = fmt.Errorf("lines %d", len(lines))
					break
				}
				sum := decimal.Zero
				for _, ln := range lines {
					sum = sum.Add(ln.Amount)
				}
				if !sum.Equal(wantSum) {
					bad = fmt.Errorf("sum %s", sum)
					break
				}
			}
			errCh <- bad
		}()
	}
	var failed int
	for i := 0; i < workers; i++ {
		if err := <-errCh; err != nil {
			failed++
			t.Error(err)
		}
	}
	elapsed := time.Since(start)
	if failed != 0 {
		t.Fatalf("failed workers %d", failed)
	}
	t.Logf("workers=%d rounds=%d lines=%d elapsed=%s", workers, rounds, workers*rounds*2000, elapsed)
}

func TestOneFileParse_FiveRuns(t *testing.T) {
	raw := readIndustry(t, "burst-wholesale-alipay.csv")
	want := decimal.RequireFromString("6500.00")
	parse := func() {
		t.Helper()
		_, _, lines, err := domain.ParseMoneyFile(raw)
		if err != nil {
			t.Fatal(err)
		}
		if len(lines) != 2000 {
			t.Fatalf("lines %d", len(lines))
		}
		sum := decimal.Zero
		for _, ln := range lines {
			sum = sum.Add(ln.Amount)
		}
		if !sum.Equal(want) {
			t.Fatalf("sum %s", sum)
		}
	}
	parse()
	const batch = 100
	runs := make([]time.Duration, 5)
	for i := range runs {
		start := time.Now()
		for j := 0; j < batch; j++ {
			parse()
		}
		runs[i] = time.Since(start)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i] < runs[j] })
	median := runs[2]
	t.Logf("batch=%d runs=%s median=%s per_file=%s", batch, runs, median, median/time.Duration(batch))
}

func readIndustry(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "industries", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
