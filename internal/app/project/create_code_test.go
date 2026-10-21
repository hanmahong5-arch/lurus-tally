package project_test

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	appproject "github.com/hanmahong5-arch/lurus-tally/internal/app/project"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/project"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/codegen"
)

func fixedCodes(b ...byte) codegen.Source {
	return codegen.Source{
		Now:  func() time.Time { return time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC) },
		Rand: bytes.NewReader(b),
	}
}

func TestCreateUseCase_Execute_EmptyCodeIsGenerated(t *testing.T) {
	p, err := appproject.NewCreateUseCase(newMemRepo()).Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Name: "河道绿化"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !regexp.MustCompile(`^PJ\d{6}[2-9A-HJ-NP-Z]{4}$`).MatchString(p.Code) {
		t.Errorf("code = %q, want PJ + yymmdd + 4 unambiguous symbols", p.Code)
	}
}

func TestCreateUseCase_Execute_GeneratedCodeRetriesPastExistingCode(t *testing.T) {
	repo := newMemRepo()
	tenantID := uuid.New()
	seedProject(t, repo, tenantID, "PJ2610102222", false)
	uc := appproject.NewCreateUseCase(repo).WithCodeSource(fixedCodes(0, 0, 0, 0, 1, 1, 1, 1))

	p, err := uc.Execute(context.Background(), domain.CreateInput{TenantID: tenantID, Name: "道路修缮"})
	if err != nil {
		t.Fatalf("unexpected error after retry: %v", err)
	}
	if p.Code != "PJ2610103333" {
		t.Errorf("code = %q, want the redrawn PJ2610103333", p.Code)
	}
}

func TestCreateUseCase_Execute_GeneratedCodeGivesUpAfterThreeRetries(t *testing.T) {
	repo := newMemRepo()
	tenantID := uuid.New()
	// The fixed source draws the same code every time; it is already taken.
	seedProject(t, repo, tenantID, "PJ2610102222", false)
	uc := appproject.NewCreateUseCase(repo).WithCodeSource(fixedCodes(bytes.Repeat([]byte{0}, 4*4)...))

	_, err := uc.Execute(context.Background(), domain.CreateInput{TenantID: tenantID, Name: "道路修缮"})
	if !errors.Is(err, appproject.ErrDuplicateCode) {
		t.Fatalf("err = %v, want ErrDuplicateCode once retries are exhausted", err)
	}
}

func TestCreateUseCase_Execute_ExplicitDuplicateCodeNotRetried(t *testing.T) {
	repo := newMemRepo()
	tenantID := uuid.New()
	seedProject(t, repo, tenantID, "P001", false)
	// An empty random source would fail any generation attempt, proving none is made.
	uc := appproject.NewCreateUseCase(repo).WithCodeSource(fixedCodes())

	_, err := uc.Execute(context.Background(), domain.CreateInput{TenantID: tenantID, Code: "P001", Name: "重复"})
	if !errors.Is(err, appproject.ErrDuplicateCode) {
		t.Fatalf("err = %v, want ErrDuplicateCode", err)
	}
}

func TestCreateUseCase_Execute_GeneratedCodeStillValidatesOtherFields(t *testing.T) {
	_, err := appproject.NewCreateUseCase(newMemRepo()).Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Name: ""})
	if err == nil || errors.Is(err, appproject.ErrDuplicateCode) {
		t.Fatalf("err = %v, want a validation error for the missing name", err)
	}
}
