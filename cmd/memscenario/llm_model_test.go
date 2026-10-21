package main

import "testing"

func TestProbeModel(t *testing.T) {
	t.Setenv("MODEL", "")
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("empty MODEL must stop the probe with an explicit error")
			}
		}()
		probeModel()
	}()

	t.Setenv("MODEL", "probe-model")
	if got := probeModel(); got != "probe-model" {
		t.Fatalf("probeModel() = %q, want probe-model", got)
	}
}
