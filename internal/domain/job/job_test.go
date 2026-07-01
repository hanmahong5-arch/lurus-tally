package job_test

import (
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-tally/internal/domain/job"
)

var (
	t0    = time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	tLate = t0.Add(time.Hour)
)

func TestState_ValidAndTerminal(t *testing.T) {
	cases := []struct {
		s        job.State
		valid    bool
		terminal bool
	}{
		{job.StatePending, true, false},
		{job.StateRunning, true, false},
		{job.StateSucceeded, true, true},
		{job.StateFailed, true, true},
		{job.StateCancelled, true, true},
		{job.State("bogus"), false, false},
	}
	for _, c := range cases {
		if got := c.s.Valid(); got != c.valid {
			t.Errorf("%q.Valid()=%v, want %v", c.s, got, c.valid)
		}
		if got := c.s.IsTerminal(); got != c.terminal {
			t.Errorf("%q.IsTerminal()=%v, want %v", c.s, got, c.terminal)
		}
	}
}

func TestJob_CanClaim(t *testing.T) {
	future := tLate
	past := t0.Add(-time.Hour)
	cases := []struct {
		name string
		j    job.Job
		want bool
	}{
		{"pending due", job.Job{State: job.StatePending, MaxAttempts: 3}, true},
		{"pending scheduled in past", job.Job{State: job.StatePending, MaxAttempts: 3, ScheduledFor: &past}, true},
		{"pending scheduled in future", job.Job{State: job.StatePending, MaxAttempts: 3, ScheduledFor: &future}, false},
		{"running", job.Job{State: job.StateRunning, MaxAttempts: 3}, false},
		{"succeeded", job.Job{State: job.StateSucceeded, MaxAttempts: 3}, false},
		{"retries exhausted", job.Job{State: job.StatePending, Attempts: 3, MaxAttempts: 3}, false},
		{"cancel requested", job.Job{State: job.StatePending, MaxAttempts: 3, CancelRequested: true}, false},
	}
	for _, c := range cases {
		if got := c.j.CanClaim(t0); got != c.want {
			t.Errorf("%s: CanClaim=%v, want %v", c.name, got, c.want)
		}
	}
}

func TestJob_Start(t *testing.T) {
	// happy path: pending -> running
	j := job.Job{State: job.StatePending, MaxAttempts: 3}
	if err := j.Start(t0); err != nil {
		t.Fatalf("Start from pending: unexpected error %v", err)
	}
	if j.State != job.StateRunning {
		t.Errorf("state=%q, want running", j.State)
	}
	if j.Attempts != 1 {
		t.Errorf("Attempts=%d, want 1", j.Attempts)
	}
	if j.StartedAt == nil || !j.StartedAt.Equal(t0) {
		t.Errorf("StartedAt=%v, want %v", j.StartedAt, t0)
	}

	// illegal: cannot start from non-pending
	for _, s := range []job.State{job.StateRunning, job.StateSucceeded, job.StateFailed, job.StateCancelled} {
		bad := job.Job{State: s, MaxAttempts: 3}
		if err := bad.Start(t0); err == nil {
			t.Errorf("Start from %q: expected ErrInvalidTransition, got nil", s)
		}
	}
}

func TestJob_Succeed(t *testing.T) {
	j := job.Job{State: job.StateRunning, MaxAttempts: 3}
	out := []byte(`{"ok":true}`)
	if err := j.Succeed(tLate, out); err != nil {
		t.Fatalf("Succeed from running: unexpected error %v", err)
	}
	if j.State != job.StateSucceeded {
		t.Errorf("state=%q, want succeeded", j.State)
	}
	if j.Progress != 100 {
		t.Errorf("Progress=%d, want 100", j.Progress)
	}
	if string(j.Output) != string(out) {
		t.Errorf("Output=%s, want %s", j.Output, out)
	}
	if j.FinishedAt == nil || !j.FinishedAt.Equal(tLate) {
		t.Errorf("FinishedAt=%v, want %v", j.FinishedAt, tLate)
	}

	for _, s := range []job.State{job.StatePending, job.StateSucceeded, job.StateFailed, job.StateCancelled} {
		bad := job.Job{State: s}
		if err := bad.Succeed(tLate, nil); err == nil {
			t.Errorf("Succeed from %q: expected error, got nil", s)
		}
	}
}

func TestJob_Fail_RetriesThenTerminal(t *testing.T) {
	// attempts remain -> back to pending for retry
	retry := job.Job{State: job.StateRunning, Attempts: 1, MaxAttempts: 3, StartedAt: &t0}
	if err := retry.Fail(tLate, "boom"); err != nil {
		t.Fatalf("Fail with retries: unexpected error %v", err)
	}
	if retry.State != job.StatePending {
		t.Errorf("state=%q, want pending (retry)", retry.State)
	}
	if retry.LastError != "boom" {
		t.Errorf("LastError=%q, want boom", retry.LastError)
	}
	if retry.StartedAt != nil {
		t.Errorf("StartedAt=%v, want nil after retry reset", retry.StartedAt)
	}
	if retry.FinishedAt != nil {
		t.Errorf("FinishedAt=%v, want nil (not terminal)", retry.FinishedAt)
	}

	// attempts exhausted -> terminal failed
	dead := job.Job{State: job.StateRunning, Attempts: 3, MaxAttempts: 3, StartedAt: &t0}
	if err := dead.Fail(tLate, "final"); err != nil {
		t.Fatalf("Fail exhausted: unexpected error %v", err)
	}
	if dead.State != job.StateFailed {
		t.Errorf("state=%q, want failed", dead.State)
	}
	if dead.FinishedAt == nil {
		t.Error("FinishedAt: want set on terminal failure")
	}

	// illegal: Fail only from running
	bad := job.Job{State: job.StatePending, MaxAttempts: 3}
	if err := bad.Fail(tLate, "x"); err == nil {
		t.Error("Fail from pending: expected error, got nil")
	}
}

func TestJob_Cancel(t *testing.T) {
	for _, s := range []job.State{job.StatePending, job.StateRunning} {
		j := job.Job{State: s}
		if err := j.Cancel(tLate); err != nil {
			t.Fatalf("Cancel from %q: unexpected error %v", s, err)
		}
		if j.State != job.StateCancelled {
			t.Errorf("state=%q, want cancelled", j.State)
		}
		if j.FinishedAt == nil {
			t.Errorf("Cancel from %q: FinishedAt want set", s)
		}
	}
	for _, s := range []job.State{job.StateSucceeded, job.StateFailed, job.StateCancelled} {
		j := job.Job{State: s}
		if err := j.Cancel(tLate); err == nil {
			t.Errorf("Cancel from terminal %q: expected error, got nil", s)
		}
	}
}

func TestJob_RequestCancel(t *testing.T) {
	j := job.Job{State: job.StateRunning}
	if err := j.RequestCancel(t0); err != nil {
		t.Fatalf("RequestCancel running: unexpected error %v", err)
	}
	if !j.CancelRequested {
		t.Error("CancelRequested: want true")
	}
	done := job.Job{State: job.StateSucceeded}
	if err := done.RequestCancel(t0); err == nil {
		t.Error("RequestCancel terminal: expected error, got nil")
	}
}

func TestJob_SetProgress(t *testing.T) {
	cases := []struct {
		in, want int
	}{{-5, 0}, {0, 0}, {37, 37}, {100, 100}, {250, 100}}
	for _, c := range cases {
		j := job.Job{State: job.StateRunning}
		if err := j.SetProgress(t0, c.in); err != nil {
			t.Fatalf("SetProgress(%d): unexpected error %v", c.in, err)
		}
		if j.Progress != c.want {
			t.Errorf("SetProgress(%d): Progress=%d, want %d", c.in, j.Progress, c.want)
		}
	}
	bad := job.Job{State: job.StatePending}
	if err := bad.SetProgress(t0, 50); err == nil {
		t.Error("SetProgress on pending: expected error, got nil")
	}
}

func TestJob_IsStale(t *testing.T) {
	started := t0
	cases := []struct {
		name string
		j    job.Job
		now  time.Time
		want bool
	}{
		{"running past timeout", job.Job{State: job.StateRunning, StartedAt: &started, TimeoutSeconds: 60}, t0.Add(2 * time.Minute), true},
		{"running within timeout", job.Job{State: job.StateRunning, StartedAt: &started, TimeoutSeconds: 600}, t0.Add(time.Minute), false},
		{"not running", job.Job{State: job.StatePending, StartedAt: &started, TimeoutSeconds: 1}, tLate, false},
		{"no StartedAt", job.Job{State: job.StateRunning, TimeoutSeconds: 60}, tLate, false},
		{"zero timeout", job.Job{State: job.StateRunning, StartedAt: &started, TimeoutSeconds: 0}, tLate, false},
	}
	for _, c := range cases {
		if got := c.j.IsStale(c.now); got != c.want {
			t.Errorf("%s: IsStale=%v, want %v", c.name, got, c.want)
		}
	}
}

// TestJob_Lifecycle exercises a realistic claim -> fail-retry -> claim -> succeed
// path, the way the worker loop will drive it.
func TestJob_Lifecycle(t *testing.T) {
	j := job.Job{State: job.StatePending, MaxAttempts: 2}

	if !j.CanClaim(t0) {
		t.Fatal("fresh pending job should be claimable")
	}
	if err := j.Start(t0); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := j.Fail(t0.Add(time.Second), "transient"); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if j.State != job.StatePending || j.Attempts != 1 {
		t.Fatalf("after first failure: state=%q attempts=%d, want pending/1", j.State, j.Attempts)
	}
	if !j.CanClaim(t0.Add(2 * time.Second)) {
		t.Fatal("job with a retry left should be re-claimable")
	}
	if err := j.Start(t0.Add(3 * time.Second)); err != nil {
		t.Fatalf("re-Start: %v", err)
	}
	if j.Attempts != 2 {
		t.Fatalf("Attempts=%d, want 2", j.Attempts)
	}
	if err := j.Succeed(t0.Add(4*time.Second), []byte(`{}`)); err != nil {
		t.Fatalf("Succeed: %v", err)
	}
	if j.State != job.StateSucceeded {
		t.Fatalf("final state=%q, want succeeded", j.State)
	}
}
