package horticulture

import (
	"context"
	"fmt"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/horticulture"
)

const (
	defaultLimit = 20
	maxLimit     = 200
	minLimit     = 1
)

// ListUseCase lists nursery dictionary entries with filtering and pagination.
type ListUseCase struct {
	repo Repository
}

// NewListUseCase constructs a ListUseCase.
func NewListUseCase(repo Repository) *ListUseCase {
	return &ListUseCase{repo: repo}
}

// Execute applies limit clamping and delegates to the repository.
func (uc *ListUseCase) Execute(ctx context.Context, f domain.ListFilter) ([]*domain.NurseryDict, int, error) {
	if f.Limit <= 0 {
		f.Limit = defaultLimit
	} else if f.Limit > maxLimit {
		f.Limit = maxLimit
	}
	items, total, err := uc.repo.List(ctx, f)
	if err != nil {
		return nil, 0, fmt.Errorf("nursery dict list: %w", err)
	}
	return items, total, nil
}
