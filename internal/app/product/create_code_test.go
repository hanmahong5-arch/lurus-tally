package product

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

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/codegen"
)

// conflictRepo fails the first `conflicts` Create calls with a wrapped unique
// violation (as the real repo returns it) and records every code it was handed.
type conflictRepo struct {
	fakeRepo
	conflicts int
	codes     []string
}

func (r *conflictRepo) Create(_ context.Context, p *domain.Product) error {
	r.codes = append(r.codes, p.Code)
	if len(r.codes) <= r.conflicts {
		return fmt.Errorf("product repo create: %w", &pgconn.PgError{Code: "23505"})
	}
	r.createdP = p
	return nil
}

func fixedCodes(b ...byte) codegen.Source {
	return codegen.Source{
		Now:  func() time.Time { return time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC) },
		Rand: bytes.NewReader(b),
	}
}

var productCodeRe = regexp.MustCompile(`^P\d{6}[2-9A-HJ-NP-Z]{4}$`)

func TestCreateUseCase_Execute_EmptyCodeIsGenerated(t *testing.T) {
	repo := &fakeRepo{}
	uc := NewCreateUseCase(repo)

	got, err := uc.Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Name: "n1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !productCodeRe.MatchString(got.Code) {
		t.Errorf("code = %q, want P + yymmdd + 4 unambiguous symbols", got.Code)
	}
	if repo.createdP == nil || repo.createdP.Code != got.Code {
		t.Errorf("repo did not persist the generated code: %+v", repo.createdP)
	}
}

func TestCreateUseCase_Execute_GeneratedCodeDeterministicWithInjectedSource(t *testing.T) {
	repo := &fakeRepo{}
	uc := NewCreateUseCase(repo).WithCodeSource(fixedCodes(0, 1, 2, 3))

	got, err := uc.Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Name: "n1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Code != "P2610102345" {
		t.Errorf("code = %q, want P2610102345", got.Code)
	}
}

func TestCreateUseCase_Execute_GeneratedCodeRetriesOnConflict(t *testing.T) {
	repo := &conflictRepo{conflicts: 2}
	uc := NewCreateUseCase(repo).WithCodeSource(fixedCodes(0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2))

	got, err := uc.Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Name: "n1"})
	if err != nil {
		t.Fatalf("unexpected error after retries: %v", err)
	}
	want := []string{"P2610102222", "P2610103333", "P2610104444"}
	if fmt.Sprint(repo.codes) != fmt.Sprint(want) {
		t.Errorf("attempted codes = %v, want %v", repo.codes, want)
	}
	if got.Code != "P2610104444" {
		t.Errorf("returned code = %q, want the one that persisted", got.Code)
	}
}

func TestCreateUseCase_Execute_GeneratedCodeGivesUpAfterThreeRetries(t *testing.T) {
	repo := &conflictRepo{conflicts: 100}
	uc := NewCreateUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Name: "n1"})
	if !codegen.IsUniqueViolation(err) {
		t.Fatalf("err = %v, want the unique violation to surface", err)
	}
	if len(repo.codes) != 4 {
		t.Errorf("attempts = %d, want 4 (1 + 3 retries)", len(repo.codes))
	}
}

func TestCreateUseCase_Execute_ExplicitCodeConflictNotRetried(t *testing.T) {
	repo := &conflictRepo{conflicts: 1}
	uc := NewCreateUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Code: "MY-1", Name: "n1"})
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23505" {
		t.Fatalf("err = %v, want the unique violation passed through", err)
	}
	if len(repo.codes) != 1 || repo.codes[0] != "MY-1" {
		t.Errorf("attempted codes = %v, want exactly [MY-1]", repo.codes)
	}
}
