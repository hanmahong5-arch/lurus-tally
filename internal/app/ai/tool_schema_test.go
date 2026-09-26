package ai

import (
	"encoding/json"
	"flag"
	"os"
	"reflect"
	"sort"
	"testing"
)

var updateToolSchemas = flag.Bool("update-tool-schemas", false, "rewrite testdata/tool_schemas.golden.json")

// allToolDefs is every tool the model can be offered, memory tools included.
func allToolDefs() map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for _, d := range append(ToolDefs(), rememberCustomerFactDef(), recallAsOfDef()) {
		out[d.Function.Name] = d.Function.Parameters
	}
	return out
}

// canonical decodes a schema and sorts every "required" list, so two schemas
// that differ only in key or required order compare equal.
func canonical(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	var walk func(any)
	walk = func(x any) {
		switch m := x.(type) {
		case map[string]any:
			if req, ok := m["required"].([]any); ok {
				sort.Slice(req, func(i, j int) bool { return req[i].(string) < req[j].(string) })
			}
			for _, c := range m {
				walk(c)
			}
		case []any:
			for _, c := range m {
				walk(c)
			}
		}
	}
	walk(v)
	return v
}

// The parameter schemas are the contract with the model. They are generated
// from the argument structs the tools parse into; this pins them to the
// reviewed snapshot, so a struct edit that changes what the model is told
// fails here instead of silently changing tool selection.
// Regenerate deliberately: go test ./internal/app/ai -run TestToolSchemas -update-tool-schemas
func TestToolSchemas_MatchTheGolden(t *testing.T) {
	const path = "testdata/tool_schemas.golden.json"
	got := allToolDefs()
	if *updateToolSchemas {
		b, _ := json.MarshalIndent(got, "", "  ")
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]json.RawMessage
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("%d tools, golden has %d", len(got), len(want))
	}
	for name, w := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("tool %s missing", name)
			continue
		}
		if !reflect.DeepEqual(canonical(t, g), canonical(t, w)) {
			t.Errorf("%s schema changed:\n got %s\nwant %s", name, g, w)
		}
	}
}
