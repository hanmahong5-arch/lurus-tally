// Package job is the pure domain core of the durable async job engine (F2).
//
// It models a long-running, multi-step background task (e.g. auto-procure,
// auto-replenish) as a state machine with at-least-once retry, cooperative
// cancellation, progress and a staleness reaper. It has NO infrastructure
// dependencies: the repo (poll/claim via SELECT ... FOR UPDATE SKIP LOCKED) and
// the worker loop live in adapter/app layers and drive these transitions, so the
// rules here are unit-testable without a database. See
// _bmad-output/planning-artifacts/f1-f2-foundations-tdd-plan.md (F2.2).
package job

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// State is the lifecycle position of a job. The legal edges are:
//
//	pending  -> running                 (Start, a worker claimed it)
//	running  -> succeeded               (Succeed)
//	running  -> pending                 (Fail, attempts remain — retry)
//	running  -> failed                  (Fail, attempts exhausted — terminal)
//	pending  -> cancelled               (Cancel, before it ran)
//	running  -> cancelled               (Cancel, cooperative stop)
//
// succeeded / failed / cancelled are terminal: no further transition is legal.
type State string

const (
	StatePending   State = "pending"
	StateRunning   State = "running"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

// ErrInvalidTransition is returned when a transition is attempted from a state
// that does not permit it (e.g. succeeding a job that never started). The
// message names both the current state and the attempted action so a caller can
// act on it, never a bare "internal error".
var ErrInvalidTransition = errors.New("job: invalid state transition")

// Valid reports whether s is a known state.
func (s State) Valid() bool {
	switch s {
	case StatePending, StateRunning, StateSucceeded, StateFailed, StateCancelled:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether s admits no further transition.
func (s State) IsTerminal() bool {
	return s == StateSucceeded || s == StateFailed || s == StateCancelled
}

// Job is one durable async task. Pointers mark "not yet set" timestamps so the
// zero value is unambiguous (a job that has never started has StartedAt == nil,
// distinct from the zero time).
type Job struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	Type     string // handler key, e.g. "auto_procure"
	State    State

	Input    []byte // opaque JSON payload for the handler
	Output   []byte // opaque JSON result, set on success
	Progress int    // 0..100, advisory

	Attempts    int    // execution attempts so far (incremented on Start)
	MaxAttempts int    // cap; Fail past this is terminal
	LastError   string // last failure reason, for inspection

	ScheduledFor    *time.Time // nil = run asap; else not claimable until <= now
	StartedAt       *time.Time // set on Start, cleared on retry
	FinishedAt      *time.Time // set on terminal transition
	CancelRequested bool       // cooperative cancel flag, observed by the handler
	TimeoutSeconds  int        // a running job older than this is stale (reaper)

	CreatedAt time.Time
	UpdatedAt time.Time
}

// CanClaim reports whether a worker may pick this job up at instant now: it must
// be pending, have retries left, be due (no future schedule), and not have a
// pending cancel request (a cancel-requested pending job is reaped to cancelled
// instead of run).
func (j *Job) CanClaim(now time.Time) bool {
	if j.State != StatePending {
		return false
	}
	if j.RetriesExhausted() {
		return false
	}
	if j.CancelRequested {
		return false
	}
	if j.ScheduledFor != nil && j.ScheduledFor.After(now) {
		return false
	}
	return true
}

// RetriesExhausted reports whether no further attempt is allowed.
func (j *Job) RetriesExhausted() bool {
	return j.Attempts >= j.MaxAttempts
}

// IsStale reports whether a running job has exceeded its timeout at instant now
// and should be reaped back to pending (its worker likely died mid-execution).
// A job that is not running, or has no StartedAt, or has TimeoutSeconds <= 0 is
// never stale.
func (j *Job) IsStale(now time.Time) bool {
	if j.State != StateRunning || j.StartedAt == nil || j.TimeoutSeconds <= 0 {
		return false
	}
	deadline := j.StartedAt.Add(time.Duration(j.TimeoutSeconds) * time.Second)
	return now.After(deadline)
}

// Start moves a pending job to running, stamps StartedAt and counts the attempt.
func (j *Job) Start(now time.Time) error {
	if j.State != StatePending {
		return ErrInvalidTransition
	}
	j.State = StateRunning
	j.Attempts++
	j.StartedAt = &now
	j.FinishedAt = nil
	j.UpdatedAt = now
	return nil
}

// Succeed moves a running job to the terminal succeeded state with its output.
func (j *Job) Succeed(now time.Time, output []byte) error {
	if j.State != StateRunning {
		return ErrInvalidTransition
	}
	j.State = StateSucceeded
	j.Output = output
	j.Progress = 100
	j.FinishedAt = &now
	j.UpdatedAt = now
	return nil
}

// Fail records a failed execution of a running job. If attempts remain it goes
// back to pending for retry (StartedAt cleared so it is neither stale nor
// double-counted); once attempts are exhausted it becomes terminally failed.
func (j *Job) Fail(now time.Time, reason string) error {
	if j.State != StateRunning {
		return ErrInvalidTransition
	}
	j.LastError = reason
	j.UpdatedAt = now
	if j.RetriesExhausted() {
		j.State = StateFailed
		j.FinishedAt = &now
		return nil
	}
	j.State = StatePending
	j.StartedAt = nil
	return nil
}

// Cancel terminally cancels a job that has not yet finished (pending or running).
func (j *Job) Cancel(now time.Time) error {
	if j.State.IsTerminal() {
		return ErrInvalidTransition
	}
	j.State = StateCancelled
	j.FinishedAt = &now
	j.UpdatedAt = now
	return nil
}

// RequestCancel sets the cooperative cancel flag. A running handler observes it
// and stops; a pending job is reaped to cancelled at the next claim. It is a
// no-op error on terminal jobs (nothing left to cancel).
func (j *Job) RequestCancel(now time.Time) error {
	if j.State.IsTerminal() {
		return ErrInvalidTransition
	}
	j.CancelRequested = true
	j.UpdatedAt = now
	return nil
}

// SetProgress clamps pct into [0,100] and records it; only meaningful while
// running. Calling it on a non-running job is an invalid transition.
func (j *Job) SetProgress(now time.Time, pct int) error {
	if j.State != StateRunning {
		return ErrInvalidTransition
	}
	switch {
	case pct < 0:
		pct = 0
	case pct > 100:
		pct = 100
	}
	j.Progress = pct
	j.UpdatedAt = now
	return nil
}
