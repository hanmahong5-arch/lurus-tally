package llmclient

import "strings"

// Task names a kind of LLM work that may be served by a different model.
type Task string

const (
	// TaskChat is the tool-calling conversation loop.
	TaskChat Task = "chat"
	// TaskExtract is the spoken-order -> document-lines JSON extraction.
	TaskExtract Task = "extract"
)

// Router maps a task to a model name. The zero value / nil router routes every
// task to the caller's default model, so an unconfigured deployment behaves
// exactly as before routing existed.
type Router struct {
	models map[Task]string
}

// NewRouter builds a Router from task -> model. Blank entries are dropped.
func NewRouter(models map[Task]string) *Router {
	m := make(map[Task]string, len(models))
	for t, name := range models {
		if name = strings.TrimSpace(name); name != "" {
			m[t] = name
		}
	}
	return &Router{models: m}
}

// Configured reports whether at least one task has its own model.
func (r *Router) Configured() bool {
	return r != nil && len(r.models) > 0
}

// Model returns the model for task, or fallback when the task is not routed.
func (r *Router) Model(task Task, fallback string) string {
	if r == nil {
		return fallback
	}
	if m, ok := r.models[task]; ok {
		return m
	}
	return fallback
}
