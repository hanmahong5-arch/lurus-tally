package warehouse_test

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	appwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/app/warehouse"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/warehouse"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/codegen"
)

// countingRepo records every code handed to Create and returns createErr.
type countingRepo struct {
	*fakeRepo
	codes []string
}

func (r *countingRepo) Create(ctx context.Context, w *domain.Warehouse) error {
	r.codes = append(r.codes, w.Code)
	return r.fakeRepo.Create(ctx, w)
}

func TestCreateUseCase_Execute_EmptyCodeIsGenerated(t *testing.T) {
	w, err := appwarehouse.NewCreateUseCase(newFakeRepo()).Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Name: "主仓"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !regexp.MustCompile(`^W\d{6}[2-9A-HJ-NP-Z]{4}$`).MatchString(w.Code) {
		t.Errorf("code = %q, want W + yymmdd + 4 unambiguous symbols", w.Code)
	}
}

func TestCreateUseCase_Execute_GeneratedCodeDeterministicWithInjectedSource(t *testing.T) {
	src := codegen.Source{
		Now:  func() time.Time { return time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC) },
		Rand: bytes.NewReader([]byte{0, 1, 2, 3}),
	}
	w, err := appwarehouse.NewCreateUseCase(newFakeRepo()).WithCodeSource(src).Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Name: "主仓"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Code != "W2610102345" {
		t.Errorf("code = %q, want W2610102345", w.Code)
	}
}

func TestCreateUseCase_Execute_ExplicitCodeKept(t *testing.T) {
	w, err := appwarehouse.NewCreateUseCase(newFakeRepo()).Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Code: "WH001", Name: "主仓"})
	if err != nil || w.Code != "WH001" {
		t.Fatalf("code=%v err=%v, want WH001 untouched", w, err)
	}
}

func TestCreateUseCase_Execute_DuplicateNameNotRetried(t *testing.T) {
	// Warehouse code carries no unique index, so the only unique violation is
	// the name — redrawing the generated code cannot help and must not happen.
	inner := newFakeRepo()
	inner.createErr = appwarehouse.ErrDuplicateName
	repo := &countingRepo{fakeRepo: inner}

	_, err := appwarehouse.NewCreateUseCase(repo).Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Name: "主仓"})
	if !errors.Is(err, appwarehouse.ErrDuplicateName) || len(repo.codes) != 1 {
		t.Fatalf("err=%v attempts=%d, want ErrDuplicateName after exactly 1 attempt", err, len(repo.codes))
	}
}
