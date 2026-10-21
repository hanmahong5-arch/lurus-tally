package tenant_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	repoTenant "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/tenant"
	appTenant "github.com/hanmahong5-arch/lurus-tally/internal/app/tenant"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
)

// racedStore simulates losing the bootstrap race: a concurrent request commits
// the same subject first, then our own insert fails on the unique key.
type racedStore struct {
	*stubBootstrapStore
	winnerType domain.ProfileType
}

func (s *racedStore) Bootstrap(ctx context.Context, in repoTenant.BootstrapInput) error {
	winner := in
	winner.ProfileType = s.winnerType
	if err := s.stubBootstrapStore.Bootstrap(ctx, winner); err != nil {
		return err
	}
	return &pgconn.PgError{Code: "23505"}
}

func TestChooseProfile_ConcurrentBootstrap_SameType_IsIdempotentSuccess(t *testing.T) {
	store := &racedStore{stubBootstrapStore: newStubBootstrapStore(), winnerType: domain.ProfileType("horticulture")}
	uc := appTenant.NewChooseProfileUseCase(store, nil, nil)
	p, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{IDPSubject: "sub-race-1", ProfileType: "horticulture"})
	if err != nil {
		t.Fatalf("want idempotent success, got %v", err)
	}
	if p == nil || p.ProfileType != "horticulture" {
		t.Fatalf("unexpected profile %+v", p)
	}
}

func TestChooseProfile_ConcurrentBootstrap_OtherType_ReturnsAlreadySet(t *testing.T) {
	store := &racedStore{stubBootstrapStore: newStubBootstrapStore(), winnerType: domain.ProfileType("retail")}
	uc := appTenant.NewChooseProfileUseCase(store, nil, nil)
	_, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{IDPSubject: "sub-race-2", ProfileType: "horticulture"})
	if !errors.Is(err, domain.ErrProfileAlreadySet) {
		t.Fatalf("want ErrProfileAlreadySet, got %v", err)
	}
}
