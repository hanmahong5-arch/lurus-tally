package job_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	jobapp "github.com/hanmahong5-arch/lurus-tally/internal/app/job"
	domainjob "github.com/hanmahong5-arch/lurus-tally/internal/domain/job"
)

// clk is a fixed clock so transitions are deterministic.
var clk = func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) }

// fakeRepo hands out queued jobs FIFO and records every Save. It mirrors the real
// Claim contract by transitioning the returned job to running (Start), but — being
// a test double — it ignores the type filter; the real adapter enforces it in SQL.
type fakeRepo struct {
	queue    []*domainjob.Job
	saved    []*domainjob.Job
	claimErr error
}

func (f *fakeRepo) Claim(_ context.Context, now time.Time, _ []string) (*domainjob.Job, error) {
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	if len(f.queue) == 0 {
		return nil, nil
	}
	j := f.queue[0]
	f.queue = f.queue[1:]
	if err := j.Start(now); err != nil {
		return nil, err
	}
	return j, nil
}

func (f *fakeRepo) Save(_ context.Context, j *domainjob.Job) error {
	f.saved = append(f.saved, j)
	return nil
}

func noopHandler(context.Context, *domainjob.Job, jobapp.Progress) ([]byte, error) {
	return nil, nil
}

func pendingJob(typ string, maxAttempts int) *domainjob.Job {
	return &domainjob.Job{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		Type:        typ,
		State:       domainjob.StatePending,
		MaxAttempts: maxAttempts,
	}
}

// TestRunOnce_NoJobIsNoop: an empty queue is a clean no-op, no save written.
func TestRunOnce_NoJobIsNoop(t *testing.T) {
	repo := &fakeRepo{}
	r := jobapp.NewRunner(repo, clk)
	r.Register("noop", noopHandler)

	processed, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if processed {
		t.Error("processed=true with empty queue, want false")
	}
	if len(repo.saved) != 0 {
		t.Errorf("saves=%d, want 0", len(repo.saved))
	}
}

// TestRunOnce_Success: a handler that returns output drives the job to succeeded
// with output, progress 100 and exactly one attempt; the handler observes its
// reported progress.
func TestRunOnce_Success(t *testing.T) {
	j := pendingJob("auto_procure", 3)
	repo := &fakeRepo{queue: []*domainjob.Job{j}}
	r := jobapp.NewRunner(repo, clk)

	var sawProgress int
	r.Register("auto_procure", func(_ context.Context, jb *domainjob.Job, report jobapp.Progress) ([]byte, error) {
		report(40)
		sawProgress = jb.Progress
		return []byte(`{"po":"PO-1"}`), nil
	})

	processed, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !processed {
		t.Fatal("processed=false, want true")
	}
	if j.State != domainjob.StateSucceeded {
		t.Errorf("state=%q, want succeeded", j.State)
	}
	if string(j.Output) != `{"po":"PO-1"}` {
		t.Errorf("output=%s, want PO-1 payload", j.Output)
	}
	if j.Progress != 100 {
		t.Errorf("progress=%d, want 100", j.Progress)
	}
	if j.Attempts != 1 {
		t.Errorf("attempts=%d, want 1", j.Attempts)
	}
	if sawProgress != 40 {
		t.Errorf("handler saw progress=%d, want 40", sawProgress)
	}
}

// TestRunOnce_FailureRetriesWhenAttemptsRemain covers a failed attempt that the
// domain returns to pending (StartedAt cleared so it is neither stale nor
// double-counted).
func TestRunOnce_FailureRetriesWhenAttemptsRemain(t *testing.T) {
	j := pendingJob("flaky", 2)
	repo := &fakeRepo{queue: []*domainjob.Job{j}}
	r := jobapp.NewRunner(repo, clk)
	r.Register("flaky", func(context.Context, *domainjob.Job, jobapp.Progress) ([]byte, error) {
		return nil, errors.New("boom")
	})

	processed, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !processed {
		t.Fatal("processed=false, want true")
	}
	if j.State != domainjob.StatePending {
		t.Errorf("state=%q, want pending (retry)", j.State)
	}
	if j.LastError != "boom" {
		t.Errorf("lastError=%q, want boom", j.LastError)
	}
	if j.Attempts != 1 {
		t.Errorf("attempts=%d, want 1", j.Attempts)
	}
	if j.StartedAt != nil {
		t.Error("StartedAt should be cleared on retry")
	}
}

// TestRunOnce_FailureTerminalWhenExhausted: failing the last allowed attempt is
// terminal.
func TestRunOnce_FailureTerminalWhenExhausted(t *testing.T) {
	j := pendingJob("doomed", 1)
	repo := &fakeRepo{queue: []*domainjob.Job{j}}
	r := jobapp.NewRunner(repo, clk)
	r.Register("doomed", func(context.Context, *domainjob.Job, jobapp.Progress) ([]byte, error) {
		return nil, errors.New("nope")
	})

	processed, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !processed {
		t.Fatal("processed=false, want true")
	}
	if j.State != domainjob.StateFailed {
		t.Errorf("state=%q, want failed", j.State)
	}
	if j.FinishedAt == nil {
		t.Error("FinishedAt should be set on terminal failure")
	}
}

// TestRunOnce_NoHandlerFailsJob: the defensive guard — a claimed job whose type
// has no handler is failed, not crashed.
func TestRunOnce_NoHandlerFailsJob(t *testing.T) {
	j := pendingJob("orphan", 1)
	repo := &fakeRepo{queue: []*domainjob.Job{j}}
	r := jobapp.NewRunner(repo, clk)
	r.Register("something_else", noopHandler)

	processed, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !processed {
		t.Fatal("processed=false, want true")
	}
	if j.State != domainjob.StateFailed {
		t.Errorf("state=%q, want failed (no handler)", j.State)
	}
	if j.LastError == "" {
		t.Error("LastError should record the missing handler")
	}
}

// TestRunOnce_ClaimErrorPropagates: an infrastructure fault on claim is returned
// to the caller (the tick should back off), not swallowed.
func TestRunOnce_ClaimErrorPropagates(t *testing.T) {
	repo := &fakeRepo{claimErr: errors.New("db down")}
	r := jobapp.NewRunner(repo, clk)
	if _, err := r.RunOnce(context.Background()); err == nil {
		t.Fatal("expected claim error to propagate")
	}
}

// TestRegister_RejectsNilAndDuplicate: wiring bugs fail fast at boot.
func TestRegister_RejectsNilAndDuplicate(t *testing.T) {
	r := jobapp.NewRunner(&fakeRepo{}, clk)
	mustPanic(t, "nil handler", func() { r.Register("x", nil) })
	r.Register("x", noopHandler)
	mustPanic(t, "duplicate", func() { r.Register("x", noopHandler) })
}

// TestTypes_StableSorted: Types is deterministic so claim offers a stable set.
func TestTypes_StableSorted(t *testing.T) {
	r := jobapp.NewRunner(&fakeRepo{}, clk)
	r.Register("zeta", noopHandler)
	r.Register("alpha", noopHandler)
	r.Register("mid", noopHandler)

	got := r.Types()
	want := []string{"alpha", "mid", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("Types()=%v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Types()=%v, want %v", got, want)
		}
	}
}

func mustPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: expected panic", name)
		}
	}()
	fn()
}
