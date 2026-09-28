//go:build live

package main

// Pass/fail gate over the customer-memory replay: runs the same code path as
// `CUSTOMERS=1 go run ./cmd/memscenario` (LLM=1 drives the real
// Orchestrator.Chat, i.e. the single chat loop the UI uses) and fails when a
// SCORE line falls below the thresholds below. Needs a fresh Postgres and a
// fresh memorus per run, as the replay does — memorus' scripts/eval/tally-
// replay.sh sets both up and runs this with GATE=1:
//
//	DATABASE_URL=... MEMORUS_URL=... [SCENARIO=holdout] [ALIASES=1 | LLM=1 [SLOTS=1]] \
//	  go test -tags=live -count=1 -timeout 60m -run TestLive ./cmd/memscenario/
//
// Thresholds: the deterministic replays must stay perfect (their results
// have been perfect since the fixes they gate); the LLM checks allow the
// model's run-to-run variance below the level measured when this gate was
// added (see llmCheckFloor).

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// llmCheckFloor: minimum share of the model-dependent results (LLM=1 checks,
// 10 questions × 3 repeats; SLOTS=1 replacements) that must pass. Measured
// when the gate was added: checks 30/30, replacements 5/5 and 4/5.
const llmCheckFloor = 0.80

func TestLive(t *testing.T) {
	for _, k := range []string{"DATABASE_URL", "MEMORUS_URL"} {
		if os.Getenv(k) == "" {
			t.Fatalf("%s is required (run via memorus scripts/eval/tally-replay.sh GATE=1)", k)
		}
	}
	out := captureStdout(t, runCustomers)
	mode := liveMode()
	checks, ok := liveThresholds[mode]
	if !ok {
		t.Fatalf("no thresholds for mode %q", mode)
	}
	for _, line := range checks {
		score := scoreLine(t, out, line.prefix)
		for _, c := range line.rules {
			if msg := c(score); msg != "" {
				t.Errorf("%s: %s\n  %s", mode, msg, score.line)
			}
		}
	}
}

func liveMode() string {
	switch {
	case os.Getenv("LLM") == "1" && os.Getenv("SLOTS") == "1":
		return "slots"
	case os.Getenv("LLM") == "1" && os.Getenv("SLOTS") == "":
		return "llm"
	case os.Getenv("LLM") == "1":
		return "slot-probe"
	case os.Getenv("ALIASES") == "1":
		return "aliases"
	case os.Getenv("SCENARIO") == "" || os.Getenv("SCENARIO") == "holdout":
		return "customers"
	}
	return "customers-" + os.Getenv("SCENARIO")
}

type gateRule func(s score) string

// scoredLine is one SCORE line of a run (found by prefix) and its rules.
type scoredLine struct {
	prefix string
	rules  []gateRule
}

// liveThresholds: per mode, every SCORE line the run prints that is gated.
var liveThresholds = map[string][]scoredLine{
	"customers": {{"SCORE scenario=", []gateRule{all("exact"), all("own_facts"), zero("foreign_lines"), zero("stale_lines")}}},
	"aliases":   {{"SCORE aliases ", []gateRule{all("purchases"), all("own_facts"), zero("foreign_lines"), zero("stale_lines")}}},
	"llm":       {{"SCORE llm ", []gateRule{all("statements_acknowledged"), atLeast("passed", llmCheckFloor)}}},
	// Which slot the model files a note under varies run to run on ambiguous
	// wording (王五的店在城东… went to "other" in 2 of 3 dev runs, with the
	// tool definitions unchanged), so replacements get the LLM floor; a wrong
	// supersede or a lost note of another slot must never happen.
	"slots": {{"SCORE slots ", []gateRule{atLeast("replaced", llmCheckFloor), zero("wrong_supersedes"), all("keep")}}},
	// v2 (change chains, as-of, attribution). The v2 customers replay writes
	// chat turns only (subject, no slot), so its numbers measure memorus'
	// rules + the arbitration model on untyped rows, not tally's typed-fact
	// recall: own_facts / held_then / customer_facts moved 48→43 / 8→5 /
	// 54→75 between two runs of the SAME code on 2026-09-28 (ARB=1 model
	// variance). Gated here is what must hold regardless: no stale (replaced)
	// value in a prompt beyond one line, and the newest value alone injected
	// for at least 80 % of the chains. The rest is printed for the record.
	"customers-v2-dev": {
		{"SCORE scenario=", []gateRule{atMost("stale_lines", 1)}},
		{"SCORE-CHAINS ", []gateRule{atLeast("latest_only", llmCheckFloor)}},
	},
	"customers-v2-holdout": {
		{"SCORE scenario=", []gateRule{atMost("stale_lines", 1)}},
		{"SCORE-CHAINS ", []gateRule{atLeast("latest_only", llmCheckFloor)}},
	},
	// The slot probe (LLM=1 SLOTS=probe): the model must call the tool for
	// every statement, and file it under the right customer nearly always.
	"slot-probe": {{"SCORE slotprobe ", []gateRule{all("tool_called"), atLeast("saved_to_right_customer", llmCheckFloor)}}},
}

// score is one SCORE line; fractions like `own_facts=3/3` or `passed 27/30`
// and counts like `stale_lines=0` are looked up by name.
type score struct {
	line  string
	frac  map[string][2]int
	count map[string]int
}

var (
	fracRe  = regexp.MustCompile(`([a-z_]+):?[= ](\d+)/(\d+)`)
	countRe = regexp.MustCompile(`([a-z_]+)[= ](\d+)(?:[ |]|$)`)
)

func parseScore(line string) score {
	s := score{line: line, frac: map[string][2]int{}, count: map[string]int{}}
	for _, m := range fracRe.FindAllStringSubmatch(line, -1) {
		a, _ := strconv.Atoi(m[2])
		b, _ := strconv.Atoi(m[3])
		s.frac[m[1]] = [2]int{a, b}
	}
	for _, m := range countRe.FindAllStringSubmatch(line, -1) {
		n, _ := strconv.Atoi(m[2])
		s.count[m[1]] = n
	}
	return s
}

func scoreLine(t *testing.T, out, prefix string) score {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return parseScore(l)
		}
	}
	t.Fatalf("no %q line in the run's output", prefix)
	return score{}
}

func all(name string) gateRule {
	return func(s score) string {
		f, ok := s.frac[name]
		switch {
		case !ok:
			return name + " missing"
		case f[1] == 0 || f[0] != f[1]:
			return name + " not all passed"
		}
		return ""
	}
}

func atLeast(name string, share float64) gateRule {
	return func(s score) string {
		f, ok := s.frac[name]
		if !ok {
			return name + " missing"
		}
		if f[1] == 0 || float64(f[0]) < share*float64(f[1]) {
			return name + " below " + strconv.FormatFloat(share, 'f', 2, 64)
		}
		return ""
	}
}

func zero(name string) gateRule {
	return atMost(name, 0)
}

func atMost(name string, max int) gateRule {
	return func(s score) string {
		n, ok := s.count[name]
		switch {
		case !ok:
			return name + " missing"
		case n > max:
			return name + " is " + strconv.Itoa(n) + " (at most " + strconv.Itoa(max) + ")"
		}
		return ""
	}
}

// captureStdout runs f and returns what it printed; the output still reaches
// the real stdout (the replay's out.log).
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.MultiWriter(&buf, orig), r)
		close(done)
	}()
	f()
	_ = w.Close()
	<-done
	return buf.String()
}

// TestGateRules checks the thresholds against SCORE lines in the formats the
// runs print (no services needed: go test -tags=live -run TestGateRules).
func TestGateRules(t *testing.T) {
	cases := []struct {
		mode, line string
		fails      int
	}{
		{"customers", "SCORE scenario=dev noattr=false | purchases: exact=6/6 ambiguous_ok=true | memory: own_facts=5/5 foreign_lines=0 stale_lines=0 injected_lines=9", 0},
		{"customers", "SCORE scenario=dev noattr=false | purchases: exact=5/6 ambiguous_ok=true | memory: own_facts=5/5 foreign_lines=1 stale_lines=0 injected_lines=9", 2},
		{"aliases", "SCORE aliases scenario=dev noattr=false | purchases: 6/6 | memory: own_facts=3/3 foreign_lines=0 stale_lines=0 injected_lines=4", 0},
		{"aliases", "SCORE aliases scenario=dev noattr=false | purchases: 5/6 | memory: own_facts=2/3 foreign_lines=0 stale_lines=2 injected_lines=4", 3},
		{"llm", "SCORE llm model=(orchestrator default) | statements_acknowledged 5/5 | checks passed 24/30", 0},
		{"llm", "SCORE llm model=(orchestrator default) | statements_acknowledged 4/5 | checks passed 23/30", 2},
		{"slots", "SCORE slots scenario=dev model=x | replaced 5/5 | wrong_supersedes 0 | keep 4/4 | tool_called 9/9 slot_right 9/9 | rows 12", 0},
		{"slots", "SCORE slots scenario=dev model=x | replaced 4/5 | wrong_supersedes 0 | keep 4/4 | tool_called 9/9 slot_right 8/9 | rows 12", 0},
		{"slots", "SCORE slots scenario=dev model=x | replaced 3/5 | wrong_supersedes 1 | keep 3/4 | tool_called 9/9 slot_right 9/9 | rows 12", 3},
		{"customers", "SCORE scenario=dev | nothing parsable", 4},
		// v2: one stale line is tolerated, two are not; the chain line is gated.
		{"customers-v2-dev", "SCORE scenario=v2-dev noattr=false | purchases: exact=6/6 ambiguous_ok=true | memory: own_facts=12/12 foreign_lines=0 stale_lines=1 injected_lines=20", 0},
		{"customers-v2-dev", "SCORE scenario=v2-dev noattr=false | purchases: exact=6/6 ambiguous_ok=true | memory: own_facts=11/12 foreign_lines=0 stale_lines=2 injected_lines=20", 1},
		{"customers-v2-dev", "SCORE-CHAINS scenario=v2-dev noattr=false | latest_only=9/10 latest_found=10/10 older_values_injected=1", 0},
		{"customers-v2-dev", "SCORE-CHAINS scenario=v2-dev noattr=false | latest_only=7/10 latest_found=10/10 older_values_injected=3", 1},
		{"slot-probe", "SCORE slotprobe scenario=dev model=x | statements_all_right 8/9 | attributes_right 8/9 | extra_calls 0 | tool_called 9/9 | saved_to_right_customer 8/9", 0},
		{"slot-probe", "SCORE slotprobe scenario=dev model=x | statements_all_right 8/9 | attributes_right 8/9 | extra_calls 0 | tool_called 8/9 | saved_to_right_customer 6/9", 2},
	}
	for _, c := range cases {
		s := parseScore(c.line)
		fails := 0
		for _, line := range liveThresholds[c.mode] {
			if !strings.HasPrefix(c.line, line.prefix) {
				continue
			}
			for _, r := range line.rules {
				if r(s) != "" {
					fails++
				}
			}
		}
		if fails != c.fails {
			t.Errorf("%s: %d rules failed, want %d: %s", c.mode, fails, c.fails, c.line)
		}
	}
}
