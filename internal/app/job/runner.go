// Package job is the application/orchestration layer of the durable async job
// engine (F2). It drives the pure domain state machine (internal/domain/job)
// over two ports — a Repo that claims and persists jobs, and a registry of
// Handlers that do the actual work — to execute one job per tick: claim a due
// job, run its handler, then record success (with output) or a failed attempt
// (which the domain turns into a retry or a terminal failure).
//
// It depends on interfaces and an injected Clock only, so the whole execution
// engine is unit-testable with fakes and no database or wall clock. The PG repo
// (SELECT ... FOR UPDATE SKIP LOCKED) and the polling worker loop are thin
// adapters over this layer, added later behind a migration reserved via the
// ledger. See _bmad-output/planning-artifacts/f1-f2-foundations-tdd-plan.md.
package job

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	domainjob "github.com/hanmahong5-arch/lurus-tally/internal/domain/job"
)

// Clock returns the current instant. It is injected so the runner is testable
// without the wall clock; production wiring passes time.Now.
type Clock func() time.Time

// Progress reports advisory 0..100 completion from inside a handler. The
// app-layer runner records it in memory (persisted with the job's next save); a
// worker adapter may wrap it to persist intermediate progress for live polling.
type Progress func(pct int)

// Handler executes one job's work. It returns the opaque output on success, or a
// non-nil error to record a failed attempt — the domain decides retry vs terminal
// from the job's remaining attempts. A long-running handler MUST observe ctx (and
// may consult j.CancelRequested) and return promptly when cancelled.
type Handler func(ctx context.Context, j *domainjob.Job, report Progress) (output []byte, err error)

// Repo is the durable job store. Claim atomically selects one due, claimable job
// of an enabled type and transitions it to running (the real adapter does this in
// a single SELECT ... FOR UPDATE SKIP LOCKED + UPDATE transaction so two workers
// never claim the same job); it returns (nil, nil) when nothing is runnable. Save
// persists a job after a domain transition.
type Repo interface {
	Claim(ctx context.Context, now time.Time, types []string) (*domainjob.Job, error)
	Save(ctx context.Context, j *domainjob.Job) error
}

// ErrNoHandler is recorded on a job whose type has no registered handler. Claim
// filters by registered types, so this is a defensive guard for the window where a
// handler is retired while a job of its type is still queued.
var ErrNoHandler = errors.New("job: no handler registered for type")

// Runner is the execution engine: a Repo, a type->Handler registry and a Clock.
type Runner struct {
	repo     Repo
	handlers map[string]Handler
	clock    Clock
}

// NewRunner constructs the engine. clock must be non-nil (fail fast at wiring).
func NewRunner(repo Repo, clock Clock) *Runner {
	if clock == nil {
		panic("job: nil clock")
	}
	return &Runner{repo: repo, handlers: make(map[string]Handler), clock: clock}
}

// Register binds a handler to a job type. A nil handler or a duplicate type is a
// wiring bug and panics at boot (fail fast) rather than failing a job at runtime.
func (r *Runner) Register(jobType string, h Handler) {
	if h == nil {
		panic("job: nil handler for type " + jobType)
	}
	if _, dup := r.handlers[jobType]; dup {
		panic("job: duplicate handler for type " + jobType)
	}
	r.handlers[jobType] = h
}

// Types returns the registered job types in stable order — the set a worker
// offers to Claim.
func (r *Runner) Types() []string {
	out := make([]string, 0, len(r.handlers))
	for t := range r.handlers {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// RunOnce claims at most one runnable job and drives it to a terminal or retry
// state. It returns (true, nil) when a job was processed and (false, nil) when
// none was claimable. A failure of the job's own work is recorded on the job (a
// failed attempt) and is NOT returned; only infrastructure faults (claim/save)
// are returned to the caller, which should back off and retry the tick.
func (r *Runner) RunOnce(ctx context.Context) (processed bool, err error) {
	j, err := r.repo.Claim(ctx, r.clock(), r.Types())
	if err != nil {
		return false, fmt.Errorf("job: claim: %w", err)
	}
	if j == nil {
		return false, nil
	}

	h, ok := r.handlers[j.Type]
	if !ok {
		return r.finishFailed(ctx, j, ErrNoHandler.Error()+": "+j.Type)
	}

	report := func(pct int) {
		// SetProgress only errors off the running state; the runner holds a
		// freshly claimed (running) job, so an error here is unreachable. Ignore
		// rather than abort real work for an advisory counter.
		if perr := j.SetProgress(r.clock(), pct); perr != nil {
			return
		}
	}

	output, runErr := h(ctx, j, report)
	if runErr != nil {
		return r.finishFailed(ctx, j, runErr.Error())
	}
	if serr := j.Succeed(r.clock(), output); serr != nil {
		return false, fmt.Errorf("job %s: succeed: %w", j.ID, serr)
	}
	if serr := r.repo.Save(ctx, j); serr != nil {
		return false, fmt.Errorf("job %s: save succeeded: %w", j.ID, serr)
	}
	return true, nil
}

// finishFailed records a failed attempt (the domain turns it into a retry or a
// terminal failure) and persists it.
func (r *Runner) finishFailed(ctx context.Context, j *domainjob.Job, reason string) (bool, error) {
	if ferr := j.Fail(r.clock(), reason); ferr != nil {
		return false, fmt.Errorf("job %s: fail: %w", j.ID, ferr)
	}
	if serr := r.repo.Save(ctx, j); serr != nil {
		return false, fmt.Errorf("job %s: save failed: %w", j.ID, serr)
	}
	return true, nil
}
