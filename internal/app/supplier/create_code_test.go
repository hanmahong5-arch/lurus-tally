package supplier_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	appsupp "github.com/hanmahong5-arch/lurus-tally/internal/app/supplier"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/supplier"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/codegen"
)

// codeConflictRepo fails the first `conflicts` Create calls with
// ErrDuplicateName (the real repo's translation of any unique violation) and
// records every code it was handed.
type codeConflictRepo struct {
	*fakeRepo
	conflicts int
	codes     []string
}

func (r *codeConflictRepo) Create(ctx context.Context, s *domain.Supplier) error {
	r.codes = append(r.codes, s.Code)
	if len(r.codes) <= r.conflicts {
		return appsupp.ErrDuplicateName
	}
	return r.fakeRepo.Create(ctx, s)
}

func fixedCodes(b ...byte) codegen.Source {
	return codegen.Source{
		Now:  func() time.Time { return time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC) },
		Rand: bytes.NewReader(b),
	}
}

func TestCreateUseCase_Execute_EmptyCodeIsGenerated(t *testing.T) {
	s, err := appsupp.NewCreateUseCase(newFakeRepo()).Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Name: "深圳供应商A"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !regexp.MustCompile(`^S\d{6}[2-9A-HJ-NP-Z]{4}$`).MatchString(s.Code) {
		t.Errorf("code = %q, want S + yymmdd + 4 unambiguous symbols", s.Code)
	}
}

func TestCreateUseCase_Execute_GeneratedCodeRetriesOnConflict(t *testing.T) {
	repo := &codeConflictRepo{fakeRepo: newFakeRepo(), conflicts: 3}
	uc := appsupp.NewCreateUseCase(repo).WithCodeSource(fixedCodes(0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3))

	s, err := uc.Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Name: "深圳供应商A"})
	if err != nil {
		t.Fatalf("unexpected error after 3 retries: %v", err)
	}
	if fmt.Sprint(repo.codes) != "[S2610102222 S2610103333 S2610104444 S2610105555]" || s.Code != "S2610105555" {
		t.Errorf("attempted codes = %v, returned %q", repo.codes, s.Code)
	}
}

func TestCreateUseCase_Execute_GeneratedCodeGivesUpAfterThreeRetries(t *testing.T) {
	repo := &codeConflictRepo{fakeRepo: newFakeRepo(), conflicts: 100}
	_, err := appsupp.NewCreateUseCase(repo).Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Name: "N"})
	if !errors.Is(err, appsupp.ErrDuplicateName) || len(repo.codes) != 4 {
		t.Fatalf("err=%v attempts=%d, want ErrDuplicateName after 4 attempts", err, len(repo.codes))
	}
}

func TestCreateUseCase_Execute_ExplicitCodeConflictNotRetried(t *testing.T) {
	repo := &codeConflictRepo{fakeRepo: newFakeRepo(), conflicts: 1}
	_, err := appsupp.NewCreateUseCase(repo).Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Code: "S001", Name: "N"})
	if !errors.Is(err, appsupp.ErrDuplicateName) || fmt.Sprint(repo.codes) != "[S001]" {
		t.Fatalf("err=%v codes=%v, want one S001 attempt and ErrDuplicateName", err, repo.codes)
	}
}
