// Package search implements the entity search use case for the ⌘K command palette.
package search

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// EntityType identifies which domain object a result represents.
type EntityType string

const (
	EntityProduct  EntityType = "product"
	EntitySupplier EntityType = "supplier"
	EntityCustomer EntityType = "customer"
	EntityBill     EntityType = "bill"
)

// EntityResult is a single match returned by the search use case.
type EntityResult struct {
	// Type identifies the entity domain.
	Type EntityType `json:"type"`
	// ID is the entity primary key (UUID string).
	ID string `json:"id"`
	// Label is the primary human-readable name (e.g. product name, bill_no).
	Label string `json:"label"`
	// Sublabel is a secondary hint (e.g. product code, bill type).
	Sublabel string `json:"sublabel"`
}

// EntityGroup is a labelled bucket of results for one entity type.
type EntityGroup struct {
	Type  EntityType     `json:"type"`
	Items []EntityResult `json:"items"`
}

// EntityRepo is the data-access contract for entity search.
// Implementations live in internal/adapter/repo/search/.
type EntityRepo interface {
	SearchProducts(ctx context.Context, tenantID uuid.UUID, q string, limit int) ([]EntityResult, error)
	SearchSuppliers(ctx context.Context, tenantID uuid.UUID, q string, limit int) ([]EntityResult, error)
	SearchCustomers(ctx context.Context, tenantID uuid.UUID, q string, limit int) ([]EntityResult, error)
	SearchBills(ctx context.Context, tenantID uuid.UUID, q string, limit int) ([]EntityResult, error)
}

// SearchEntitiesUseCase returns grouped entity results for a query string.
type SearchEntitiesUseCase struct {
	repo EntityRepo
}

// NewSearchEntitiesUseCase constructs the use case with the given repository.
func NewSearchEntitiesUseCase(repo EntityRepo) *SearchEntitiesUseCase {
	return &SearchEntitiesUseCase{repo: repo}
}

// SearchRequest carries the parameters for a palette entity search.
type SearchRequest struct {
	TenantID uuid.UUID
	Q        string
	// Limit is the maximum results per entity type (default 5 when zero).
	Limit int
}

// SearchResponse is the structured result returned to the handler.
type SearchResponse struct {
	Groups []EntityGroup `json:"groups"`
}

// Execute runs the search. Returns an empty response (not an error) when q is blank.
func (uc *SearchEntitiesUseCase) Execute(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	if req.Q == "" {
		return SearchResponse{Groups: []EntityGroup{}}, nil
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}

	type result struct {
		entityType EntityType
		items      []EntityResult
		err        error
	}

	// Run all four queries. Sequential is fine — each is a narrow ILIKE + LIMIT 5.
	type fetch struct {
		t  EntityType
		fn func() ([]EntityResult, error)
	}
	fetches := []fetch{
		{EntityProduct, func() ([]EntityResult, error) {
			return uc.repo.SearchProducts(ctx, req.TenantID, req.Q, limit)
		}},
		{EntitySupplier, func() ([]EntityResult, error) {
			return uc.repo.SearchSuppliers(ctx, req.TenantID, req.Q, limit)
		}},
		{EntityCustomer, func() ([]EntityResult, error) {
			return uc.repo.SearchCustomers(ctx, req.TenantID, req.Q, limit)
		}},
		{EntityBill, func() ([]EntityResult, error) {
			return uc.repo.SearchBills(ctx, req.TenantID, req.Q, limit)
		}},
	}

	var groups []EntityGroup
	for _, f := range fetches {
		items, err := f.fn()
		if err != nil {
			return SearchResponse{}, fmt.Errorf("entity search %s: %w", f.t, err)
		}
		if len(items) == 0 {
			continue
		}
		groups = append(groups, EntityGroup{Type: f.t, Items: items})
	}

	if groups == nil {
		groups = []EntityGroup{}
	}
	return SearchResponse{Groups: groups}, nil
}
