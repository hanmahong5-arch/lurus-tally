package llmgateway

import "testing"

func init() {
	// Priced stand-in used by the metrics tests; real names come from config.
	SetModelPrices("test-model:0.002:0.008")
}

func TestSetModelPrices_ReplacesTable(t *testing.T) {
	t.Cleanup(func() { SetModelPrices("test-model:0.002:0.008") })

	SetModelPrices("m-x:1:2")
	if got := costCNYFor("m-x", 1000, 1000); got != 3 {
		t.Errorf("cost m-x = %v, want 3", got)
	}
	if modelLabel("test-model") != "unknown" {
		t.Error("a model dropped from the table must collapse to the unknown label")
	}
	SetModelPrices("")
	if costCNYFor("m-x", 1000, 1000) != 0 || modelLabel("m-x") != "unknown" {
		t.Error("empty spec must clear the table")
	}
}

func TestParseModelPrices(t *testing.T) {
	got := parseModelPrices("m-a:0.002:0.008, m-b:1:2,bad,m-c:x:1,:1:1,m-d:-1:1")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (%v)", len(got), got)
	}
	if got["m-a"].prompt != 0.002 || got["m-a"].completion != 0.008 {
		t.Errorf("m-a = %+v", got["m-a"])
	}
	if got["m-b"].completion != 2 {
		t.Errorf("m-b = %+v", got["m-b"])
	}
	if len(parseModelPrices("")) != 0 {
		t.Error("empty spec must yield an empty table")
	}
}
