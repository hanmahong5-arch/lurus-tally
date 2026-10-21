package bill

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakePOSRepo struct {
	out POSSummary
	err error
	got POSSummaryRange
}

func (f *fakePOSRepo) POSSummary(_ context.Context, q POSSummaryRange) (POSSummary, error) {
	f.got = q
	return f.out, f.err
}

func TestParsePOSDate_EmptyIsShanghaiToday(t *testing.T) {
	// 2026-10-09 23:30 UTC is 07:30 CST on the 10th: the shift is still the 10th.
	now := time.Date(2026, 10, 9, 23, 30, 0, 0, time.UTC)
	from, to, err := ParsePOSDate("", now)
	if err != nil {
		t.Fatal(err)
	}
	if !from.Equal(time.Date(2026, 10, 9, 16, 0, 0, 0, time.UTC)) || to.Sub(from) != 24*time.Hour {
		t.Fatalf("window = %v .. %v", from, to)
	}
}

func TestParsePOSDate_Explicit(t *testing.T) {
	from, _, err := ParsePOSDate("2026-01-05", time.Now())
	if err != nil || from.UTC().Format(time.RFC3339) != "2026-01-04T16:00:00Z" {
		t.Fatalf("from=%v err=%v", from, err)
	}
	if _, _, err := ParsePOSDate("2026/01/05", time.Now()); err == nil {
		t.Fatal("bad format must fail")
	}
}

func TestGetPOSSummary(t *testing.T) {
	ctx := context.Background()
	repo := &fakePOSRepo{}
	from, to, _ := ParsePOSDate("2026-10-10", time.Now())
	if _, err := GetPOSSummary(ctx, repo, uuid.Nil, from, to, nil); err == nil {
		t.Fatal("nil tenant must fail")
	}
	tid := uuid.New()
	out, err := GetPOSSummary(ctx, repo, tid, from, to, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Date != "2026-10-10" || out.ByPayType == nil || repo.got.TenantID != tid || !repo.got.From.Equal(from) {
		t.Fatalf("out=%+v got=%+v", out, repo.got)
	}
	repo.err = errors.New("boom")
	if _, err := GetPOSSummary(ctx, repo, tid, from, to, nil); err == nil {
		t.Fatal("repo error must surface")
	}
}

func TestGetPOSSummary_PassesCreator(t *testing.T) {
	repo := &fakePOSRepo{}
	from, to, _ := ParsePOSDate("2026-10-10", time.Now())
	me := uuid.New()
	if _, err := GetPOSSummary(context.Background(), repo, uuid.New(), from, to, &me); err != nil {
		t.Fatal(err)
	}
	if repo.got.CreatorID == nil || *repo.got.CreatorID != me {
		t.Fatalf("creator = %v, want %v", repo.got.CreatorID, me)
	}
}
