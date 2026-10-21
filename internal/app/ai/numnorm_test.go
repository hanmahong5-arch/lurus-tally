package ai

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestParseCNNumber(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"0", "0", true},
		{"零", "0", true},
		{"一", "1", true},
		{"二", "2", true},
		{"两", "2", true},
		{"三", "3", true},
		{"九", "9", true},
		{"十", "10", true},
		{"十一", "11", true},
		{"十二", "12", true},
		{"十九", "19", true},
		{"二十", "20", true},
		{"二十五", "25", true},
		{"三十", "30", true},
		{"九十九", "99", true},
		{"百", "100", true},
		{"一百", "100", true},
		{"一百零五", "105", true},
		{"一百一十", "110", true},
		{"一百二十", "120", true},
		{"一百二十三", "123", true},
		{"一百二", "120", true},
		{"二百", "200", true},
		{"两百", "200", true},
		{"两百五", "250", true},
		{"三百五", "350", true},
		{"九百九十九", "999", true},
		{"一千", "1000", true},
		{"一千二", "1200", true},
		{"一千二百", "1200", true},
		{"一千零五", "1005", true},
		{"一千零五十", "1050", true},
		{"一千零一十", "1010", true},
		{"两千", "2000", true},
		{"两千五", "2500", true},
		{"三千五百", "3500", true},
		{"九千九百九十九", "9999", true},
		{"一万", "10000", true},
		{"万", "10000", true},
		{"一万二", "12000", true},
		{"一万二千", "12000", true},
		{"两万五", "25000", true},
		{"十万", "100000", true},
		{"十二万", "120000", true},
		{"一百二十万", "1200000", true},
		{"一万零五", "10005", true},
		{"一万零五百", "10500", true},
		{"半", "0.5", true},
		{"三点五", "3.5", true},
		{"零点五", "0.5", true},
		{"二点二五", "2.25", true},
		{"12", "12", true},
		{"3.5", "3.5", true},
		{"0.25", "0.25", true},
		{"100", "100", true},
		{"3万", "30000", true},
		{"1.5万", "15000", true},
		{"2千", "2000", true},
		{"2百", "200", true},
		{"〇", "0", true},
		{"", "", false},
		{"箱", "", false},
		{"abc", "", false},
		{"一二三", "", false},
		{"十十", "", false},
	}
	for _, c := range cases {
		got, ok := ParseCNNumber(c.in)
		if ok != c.ok {
			t.Errorf("ParseCNNumber(%q) ok=%v want %v", c.in, ok, c.ok)
			continue
		}
		if ok && !got.Equal(decimal.RequireFromString(c.want)) {
			t.Errorf("ParseCNNumber(%q)=%s want %s", c.in, got, c.want)
		}
	}
}

func TestExtractQuantities(t *testing.T) {
	type q struct {
		val   string
		unit  string
		each  bool
		money bool
	}
	cases := []struct {
		in   string
		want []q
	}{
		{"樱花十棵", []q{{"10", "棵", false, false}}},
		{"各十棵", []q{{"10", "棵", true, false}}},
		{"樱花和桃树各十棵", []q{{"10", "棵", true, false}}},
		{"两箱半", []q{{"2.5", "箱", false, false}}},
		{"来一千二百个", []q{{"1200", "个", false, false}}},
		{"要一千二个", []q{{"1200", "个", false, false}}},
		{"半箱", []q{{"0.5", "箱", false, false}}},
		{"一个半", []q{{"1.5", "个", false, false}}},
		{"三斤半", []q{{"3.5", "斤", false, false}}},
		{"2箱半", []q{{"2.5", "箱", false, false}}},
		{"12箱", []q{{"12", "箱", false, false}}},
		{"3.5公斤", []q{{"3.5", "公斤", false, false}}},
		{"20kg", []q{{"20", "kg", false, false}}},
		{"各2箱", []q{{"2", "箱", true, false}}},
		{"每棵三十块", []q{{"30", "块", false, true}}},
		{"一共五百元", []q{{"500", "元", false, true}}},
		{"进螺丝一百盒，锤子五把", []q{{"100", "盒", false, false}, {"5", "把", false, false}}},
		{"三天内到货", nil},
		{"没有数字", nil},
		{"十棵樱花，价格二十块一棵", []q{{"10", "棵", false, false}, {"20", "块", false, true}, {"1", "棵", false, false}}},
		{"两万五千棵", []q{{"25000", "棵", false, false}}},
		{"一百零五件", []q{{"105", "件", false, false}}},
		{"一万二袋", []q{{"12000", "袋", false, false}}},
	}
	for _, c := range cases {
		got := ExtractQuantities(c.in)
		if len(got) != len(c.want) {
			t.Errorf("ExtractQuantities(%q) len=%d want %d: %+v", c.in, len(got), len(c.want), got)
			continue
		}
		for i, w := range c.want {
			g := got[i]
			if !g.Value.Equal(decimal.RequireFromString(w.val)) || g.Unit != w.unit || g.Each != w.each || g.Money != w.money {
				t.Errorf("ExtractQuantities(%q)[%d]=%+v want %+v", c.in, i, g, w)
			}
		}
	}
}
