package customer_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	appcust "github.com/hanmahong5-arch/lurus-tally/internal/app/customer"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/customer"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/codegen"
)

// conflictRepo fails the first `conflicts` Create calls with a wrapped unique
// violation (as the real repo returns it) and records every code it was handed.
type conflictRepo struct {
	*fakeRepo
	conflicts int
	codes     []string
}

func (r *conflictRepo) Create(ctx context.Context, c *domain.Customer) error {
	r.codes = append(r.codes, c.Code)
	if len(r.codes) <= r.conflicts {
		return fmt.Errorf("customer repo create: %w", &pgconn.PgError{Code: "23505"})
	}
	return r.fakeRepo.Create(ctx, c)
}

func fixedCodes(b ...byte) codegen.Source {
	return codegen.Source{
		Now:  func() time.Time { return time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC) },
		Rand: bytes.NewReader(b),
	}
}

func TestCreateUseCase_Execute_EmptyCodeIsGenerated(t *testing.T) {
	c, err := appcust.NewCreateUseCase(newFakeRepo()).Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Name: "散客"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !regexp.MustCompile(`^C\d{6}[2-9A-HJ-NP-Z]{4}$`).MatchString(c.Code) {
		t.Errorf("code = %q, want C + yymmdd + 4 unambiguous symbols", c.Code)
	}
}

func TestCreateUseCase_Execute_GeneratedCodeRetriesOnConflict(t *testing.T) {
	repo := &conflictRepo{fakeRepo: newFakeRepo(), conflicts: 2}
	uc := appcust.NewCreateUseCase(repo).WithCodeSource(fixedCodes(0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2))

	c, err := uc.Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Name: "散客"})
	if err != nil {
		t.Fatalf("unexpected error after retries: %v", err)
	}
	if fmt.Sprint(repo.codes) != "[C2610102222 C2610103333 C2610104444]" || c.Code != "C2610104444" {
		t.Errorf("attempted codes = %v, returned %q", repo.codes, c.Code)
	}
}

func TestCreateUseCase_Execute_GeneratedCodeGivesUpAfterThreeRetries(t *testing.T) {
	repo := &conflictRepo{fakeRepo: newFakeRepo(), conflicts: 100}
	_, err := appcust.NewCreateUseCase(repo).Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Name: "N"})
	if !codegen.IsUniqueViolation(err) || len(repo.codes) != 4 {
		t.Fatalf("err=%v attempts=%d, want unique violation after 4 attempts", err, len(repo.codes))
	}
}

func TestCreateUseCase_Execute_ExplicitCodeConflictNotRetried(t *testing.T) {
	repo := &conflictRepo{fakeRepo: newFakeRepo(), conflicts: 1}
	_, err := appcust.NewCreateUseCase(repo).Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Code: "C001", Name: "N"})
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || fmt.Sprint(repo.codes) != "[C001]" {
		t.Fatalf("err=%v codes=%v, want one C001 attempt and the violation passed through", err, repo.codes)
	}
}
