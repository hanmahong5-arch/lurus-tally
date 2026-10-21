package unit

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/unit"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/codegen"
)

// conflictRepo fails the first `conflicts` Create calls with a wrapped unique
// violation (as the real repo returns it) and records every code it was handed.
type conflictRepo struct {
	fakeRepo
	conflicts int
	codes     []string
}

func (r *conflictRepo) Create(_ context.Context, u *domain.UnitDef) error {
	r.codes = append(r.codes, u.Code)
	if len(r.codes) <= r.conflicts {
		return fmt.Errorf("unit repo create: %w", &pgconn.PgError{Code: "23505"})
	}
	return nil
}

func fixedCodes(b ...byte) codegen.Source {
	return codegen.Source{
		Now:  func() time.Time { return time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC) },
		Rand: bytes.NewReader(b),
	}
}

func validUnitInput(code string) domain.CreateInput {
	return domain.CreateInput{TenantID: uuid.New(), Code: code, Name: "Piece", UnitType: domain.UnitTypeCount}
}

func TestCreateUseCase_Execute_EmptyCodeIsGenerated(t *testing.T) {
	repo := &fakeRepo{}
	got, err := NewCreateUseCase(repo).Execute(context.Background(), validUnitInput(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !regexp.MustCompile(`^U\d{6}[2-9A-HJ-NP-Z]{4}$`).MatchString(got.Code) {
		t.Errorf("code = %q, want U + yymmdd + 4 unambiguous symbols", got.Code)
	}
	if repo.lastCreated == nil || repo.lastCreated.Code != got.Code {
		t.Errorf("repo did not persist the generated code: %+v", repo.lastCreated)
	}
}

func TestCreateUseCase_Execute_GeneratedCodeRetriesOnConflict(t *testing.T) {
	repo := &conflictRepo{conflicts: 1}
	uc := NewCreateUseCase(repo).WithCodeSource(fixedCodes(0, 0, 0, 0, 1, 1, 1, 1))

	got, err := uc.Execute(context.Background(), validUnitInput(""))
	if err != nil {
		t.Fatalf("unexpected error after retry: %v", err)
	}
	if fmt.Sprint(repo.codes) != "[U2610102222 U2610103333]" || got.Code != "U2610103333" {
		t.Errorf("attempted codes = %v, returned %q", repo.codes, got.Code)
	}
}

func TestCreateUseCase_Execute_GeneratedCodeGivesUpAfterThreeRetries(t *testing.T) {
	repo := &conflictRepo{conflicts: 100}
	_, err := NewCreateUseCase(repo).Execute(context.Background(), validUnitInput(""))
	if !codegen.IsUniqueViolation(err) || len(repo.codes) != 4 {
		t.Fatalf("err=%v attempts=%d, want unique violation after 4 attempts", err, len(repo.codes))
	}
}

func TestCreateUseCase_Execute_ExplicitCodeConflictNotRetried(t *testing.T) {
	repo := &conflictRepo{conflicts: 1}
	_, err := NewCreateUseCase(repo).Execute(context.Background(), validUnitInput("PCS"))
	if !codegen.IsUniqueViolation(err) || fmt.Sprint(repo.codes) != "[PCS]" {
		t.Fatalf("err=%v codes=%v, want one PCS attempt and the violation passed through", err, repo.codes)
	}
}
