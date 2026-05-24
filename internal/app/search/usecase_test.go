package search_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	appsearch "github.com/hanmahong5-arch/lurus-tally/internal/app/search"
)

// stubEntityRepo is a test double for EntityRepo.
type stubEntityRepo struct {
	products  []appsearch.EntityResult
	suppliers []appsearch.EntityResult
	customers []appsearch.EntityResult
	bills     []appsearch.EntityResult
	errOn     appsearch.EntityType // force error for this type
}

func (s *stubEntityRepo) SearchProducts(_ context.Context, _ uuid.UUID, _ string, _ int) ([]appsearch.EntityResult, error) {
	if s.errOn == appsearch.EntityProduct {
		return nil, errors.New("db error")
	}
	return s.products, nil
}

func (s *stubEntityRepo) SearchSuppliers(_ context.Context, _ uuid.UUID, _ string, _ int) ([]appsearch.EntityResult, error) {
	if s.errOn == appsearch.EntitySupplier {
		return nil, errors.New("db error")
	}
	return s.suppliers, nil
}

func (s *stubEntityRepo) SearchCustomers(_ context.Context, _ uuid.UUID, _ string, _ int) ([]appsearch.EntityResult, error) {
	if s.errOn == appsearch.EntityCustomer {
		return nil, errors.New("db error")
	}
	return s.customers, nil
}

func (s *stubEntityRepo) SearchBills(_ context.Context, _ uuid.UUID, _ string, _ int) ([]appsearch.EntityResult, error) {
	if s.errOn == appsearch.EntityBill {
		return nil, errors.New("db error")
	}
	return s.bills, nil
}

var _ appsearch.EntityRepo = (*stubEntityRepo)(nil)

func tenantID() uuid.UUID { return uuid.MustParse("00000000-0000-0000-0000-000000000001") }

// table-driven test cases -------------------------------------------------------

func TestSearchEntitiesUseCase_Execute_BlankQueryReturnsEmpty(t *testing.T) {
	uc := appsearch.NewSearchEntitiesUseCase(&stubEntityRepo{})
	resp, err := uc.Execute(context.Background(), appsearch.SearchRequest{
		TenantID: tenantID(),
		Q:        "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Groups) != 0 {
		t.Fatalf("want 0 groups for blank query, got %d", len(resp.Groups))
	}
}

func TestSearchEntitiesUseCase_Execute_ReturnsOnlyNonEmptyGroups(t *testing.T) {
	repo := &stubEntityRepo{
		products: []appsearch.EntityResult{
			{Type: appsearch.EntityProduct, ID: "p1", Label: "Widget", Sublabel: "W-001"},
		},
		// suppliers/customers/bills return nil — should be omitted from groups
	}
	uc := appsearch.NewSearchEntitiesUseCase(repo)
	resp, err := uc.Execute(context.Background(), appsearch.SearchRequest{
		TenantID: tenantID(),
		Q:        "widget",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Groups) != 1 {
		t.Fatalf("want 1 group, got %d", len(resp.Groups))
	}
	if resp.Groups[0].Type != appsearch.EntityProduct {
		t.Errorf("want product group, got %s", resp.Groups[0].Type)
	}
	if len(resp.Groups[0].Items) != 1 {
		t.Errorf("want 1 item, got %d", len(resp.Groups[0].Items))
	}
}

func TestSearchEntitiesUseCase_Execute_AllGroupsReturned(t *testing.T) {
	repo := &stubEntityRepo{
		products:  []appsearch.EntityResult{{Type: appsearch.EntityProduct, ID: "p1", Label: "A"}},
		suppliers: []appsearch.EntityResult{{Type: appsearch.EntitySupplier, ID: "s1", Label: "B"}},
		customers: []appsearch.EntityResult{{Type: appsearch.EntityCustomer, ID: "c1", Label: "C"}},
		bills:     []appsearch.EntityResult{{Type: appsearch.EntityBill, ID: "b1", Label: "D"}},
	}
	uc := appsearch.NewSearchEntitiesUseCase(repo)
	resp, err := uc.Execute(context.Background(), appsearch.SearchRequest{
		TenantID: tenantID(),
		Q:        "foo",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Groups) != 4 {
		t.Fatalf("want 4 groups, got %d", len(resp.Groups))
	}
}

func TestSearchEntitiesUseCase_Execute_DefaultLimit(t *testing.T) {
	// Zero limit → defaults to 5 in the use case; repo is called with 5.
	// We only verify the use case does not error and returns results.
	repo := &stubEntityRepo{
		products: []appsearch.EntityResult{{Type: appsearch.EntityProduct, ID: "p1", Label: "A"}},
	}
	uc := appsearch.NewSearchEntitiesUseCase(repo)
	resp, err := uc.Execute(context.Background(), appsearch.SearchRequest{
		TenantID: tenantID(),
		Q:        "a",
		Limit:    0, // trigger default
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Groups) != 1 {
		t.Fatalf("want 1 group, got %d", len(resp.Groups))
	}
}

func TestSearchEntitiesUseCase_Execute_RepoErrorPropagates(t *testing.T) {
	repo := &stubEntityRepo{errOn: appsearch.EntityProduct}
	uc := appsearch.NewSearchEntitiesUseCase(repo)
	_, err := uc.Execute(context.Background(), appsearch.SearchRequest{
		TenantID: tenantID(),
		Q:        "anything",
	})
	if err == nil {
		t.Fatal("expected error from repo, got nil")
	}
}
