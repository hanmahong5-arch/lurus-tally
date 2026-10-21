package main

import (
	"os"
)

// probeModel returns the model name the LLM probes run against. Model names are
// deployment config, so there is no default: an unset MODEL stops the probe with
// a readable message instead of a bare client-construction error.
func probeModel() string {
	model := os.Getenv("MODEL")
	if model == "" {
		panic("MODEL is required when LLM=1 (the model name to probe)")
	}
	return model
}
