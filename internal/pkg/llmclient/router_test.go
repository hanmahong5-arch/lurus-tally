package llmclient

import "testing"

func TestRouter_NilAndEmpty_FallBackToDefault(t *testing.T) {
	var nilRouter *Router
	if got := nilRouter.Model(TaskExtract, "base"); got != "base" {
		t.Fatalf("nil router: got %q want base", got)
	}
	r := NewRouter(map[Task]string{TaskExtract: "", TaskChat: "  "})
	if r.Configured() {
		t.Fatal("blank entries must not count as configured")
	}
	if got := r.Model(TaskChat, "base"); got != "base" {
		t.Fatalf("blank chat route: got %q want base", got)
	}
}

func TestRouter_ConfiguredTaskWins(t *testing.T) {
	r := NewRouter(map[Task]string{TaskExtract: " small-model "})
	if !r.Configured() {
		t.Fatal("expected configured")
	}
	if got := r.Model(TaskExtract, "base"); got != "small-model" {
		t.Fatalf("extract: got %q", got)
	}
	if got := r.Model(TaskChat, "base"); got != "base" {
		t.Fatalf("chat unset must fall back: got %q", got)
	}
}
