package export

import "testing"

func TestMoneyCell(t *testing.T) {
	cases := map[string]string{
		"10.0000":  "10.00",
		"10":       "10.00",
		"-5.5000":  "-5.50",
		"83.25":    "83.25",
		"41.6250":  "41.63", // rounded per row, as the monthly report rounds before summing
		"0.0050":   "0.01",
		"-0.0050":  "-0.01",
		"9.9999":   "10.00",
		"":         "",
		"not-a-no": "not-a-no",
	}
	for in, want := range cases {
		if got := moneyCell(in); got != want {
			t.Errorf("moneyCell(%q) = %q, want %q", in, got, want)
		}
	}
}
