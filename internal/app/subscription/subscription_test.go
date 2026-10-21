package subscription

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeAccts struct {
	id    int64
	found bool
	err   error
}

func (f fakeAccts) PlanAccountID(context.Context, uuid.UUID) (int64, bool, error) {
	return f.id, f.found, f.err
}

type fakeSrc struct {
	snap  *Snapshot
	err   error
	calls int
}

func (f *fakeSrc) Subscription(context.Context, int64) (*Snapshot, error) {
	f.calls++
	return f.snap, f.err
}

func TestEvaluate(t *testing.T) {
	lock := map[string]bool{"expired": true, "cancelled": true, "suspended": true,
		"pending": false, "trial": false, "active": false, "grace": false}
	for status, want := range lock {
		if got := Evaluate(&Snapshot{PlanCode: "pro", Status: status}).Locked; got != want {
			t.Errorf("%s: locked=%v want %v", status, got, want)
		}
	}
	if Evaluate(nil).Locked {
		t.Error("no subscription must not lock")
	}
	if Evaluate(&Snapshot{PlanCode: "free", Status: "active"}).Locked {
		t.Error("free must not lock")
	}
	if st := Evaluate(&Snapshot{Status: "expired"}); st.Reason != ReasonExpired {
		t.Errorf("reason=%q", st.Reason)
	}
}

func TestServiceCacheAndFallback(t *testing.T) {
	src := &fakeSrc{snap: &Snapshot{PlanCode: "pro", Status: "expired"}}
	s := NewService(fakeAccts{id: 7, found: true}, src)
	now := time.Now()
	s.now = func() time.Time { return now }
	tid := uuid.New()

	if !s.State(context.Background(), tid).Locked {
		t.Fatal("want locked")
	}
	now = now.Add(4 * time.Minute)
	s.State(context.Background(), tid)
	if src.calls != 1 {
		t.Fatalf("calls=%d, want 1 within TTL", src.calls)
	}
	now = now.Add(2 * time.Minute)
	src.err = errors.New("down")
	if !s.State(context.Background(), tid).Locked {
		t.Fatal("platform error must reuse cached verdict")
	}
	if src.calls != 2 {
		t.Fatalf("calls=%d, want 2", src.calls)
	}
}

func TestServiceErrorsDoNotLock(t *testing.T) {
	src := &fakeSrc{err: errors.New("down")}
	if NewService(fakeAccts{id: 7, found: true}, src).State(context.Background(), uuid.New()).Locked {
		t.Error("error with empty cache must not lock")
	}
	if NewService(fakeAccts{err: errors.New("db")}, src).State(context.Background(), uuid.New()).Locked {
		t.Error("resolver error must not lock")
	}
	s3 := NewService(fakeAccts{}, &fakeSrc{snap: &Snapshot{Status: "expired"}})
	if s3.State(context.Background(), uuid.New()).Locked {
		t.Error("no account must not lock")
	}
	if s3.State(context.Background(), uuid.Nil).Locked {
		t.Error("nil tenant must not lock")
	}
}

func TestInvalidateForcesRefetch(t *testing.T) {
	src := &fakeSrc{snap: &Snapshot{PlanCode: "pro", Status: "expired"}}
	s := NewService(fakeAccts{id: 7, found: true}, src)
	tid := uuid.New()
	if !s.State(context.Background(), tid).Locked {
		t.Fatal("want locked")
	}
	src.snap = &Snapshot{PlanCode: "pro", Status: "active"}
	if !s.State(context.Background(), tid).Locked {
		t.Fatal("cached verdict expected before invalidate")
	}
	s.Invalidate(tid)
	if s.State(context.Background(), tid).Locked {
		t.Fatal("after invalidate the platform answer must apply")
	}
	if src.calls != 2 {
		t.Fatalf("calls=%d, want 2", src.calls)
	}
	Never{}.Invalidate(tid)
	Fixed{}.Invalidate(tid)
}

func TestStaleFallbackCappedAt30Minutes(t *testing.T) {
	src := &fakeSrc{snap: &Snapshot{PlanCode: "pro", Status: "expired"}}
	s := NewService(fakeAccts{id: 7, found: true}, src)
	now := time.Now()
	s.now = func() time.Time { return now }
	tid := uuid.New()
	s.State(context.Background(), tid)
	src.err = errors.New("down")
	now = now.Add(29 * time.Minute)
	if !s.State(context.Background(), tid).Locked {
		t.Fatal("29m-old verdict still reused")
	}
	now = now.Add(2 * time.Minute)
	if s.State(context.Background(), tid).Locked {
		t.Fatal("31m-old verdict must no longer lock")
	}
}

type slowSrc struct {
	calls   atomic.Int32
	release chan struct{}
}

func (f *slowSrc) Subscription(context.Context, int64) (*Snapshot, error) {
	f.calls.Add(1)
	<-f.release
	return &Snapshot{Status: "expired"}, nil
}

func TestConcurrentStateSingleFetch(t *testing.T) {
	src := &slowSrc{release: make(chan struct{})}
	s := NewService(fakeAccts{id: 7, found: true}, src)
	tid := uuid.New()
	var wg sync.WaitGroup
	res := make([]bool, 10)
	for i := range res {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res[i] = s.State(context.Background(), tid).Locked
		}(i)
	}
	time.Sleep(100 * time.Millisecond)
	close(src.release)
	wg.Wait()
	if n := src.calls.Load(); n != 1 {
		t.Fatalf("platform calls=%d, want 1", n)
	}
	for i, l := range res {
		if !l {
			t.Fatalf("caller %d not locked", i)
		}
	}
}
