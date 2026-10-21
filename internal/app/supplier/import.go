package supplier

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/partnerimport"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/supplier"
)

// ImportUseCase bulk-creates suppliers from a CSV file, skipping names already in the shop.
type ImportUseCase struct {
	repo   Repository
	create *CreateUseCase
}

// NewImportUseCase wires the use case.
func NewImportUseCase(repo Repository, create *CreateUseCase) *ImportUseCase {
	return &ImportUseCase{repo: repo, create: create}
}

// Execute parses data and creates every new supplier. Whole-file problems come back as
// *partnerimport.ImportError.
func (uc *ImportUseCase) Execute(ctx context.Context, tenantID uuid.UUID, data []byte) (*partnerimport.Result, error) {
	rows, perrs, err := partnerimport.Parse(data)
	if err != nil {
		return nil, err
	}
	existing := map[string]bool{}
	for off, total := 0, 1; off < total; {
		items, t, lerr := uc.repo.List(ctx, domain.ListFilter{TenantID: tenantID, Limit: maxLimit, Offset: off})
		if lerr != nil {
			return nil, fmt.Errorf("supplier import list: %w", lerr)
		}
		total = t
		if len(items) == 0 {
			break
		}
		for _, s := range items {
			existing[partnerimport.NameKey(s.Name)] = true
		}
		off += len(items)
	}
	res := partnerimport.Run(ctx, rows, perrs, existing, func(ctx context.Context, r partnerimport.Row) (partnerimport.Outcome, error) {
		_, err := uc.create.Execute(ctx, domain.CreateInput{TenantID: tenantID, Name: r.Name, Contact: r.Contact, Phone: r.Phone, Address: r.Address, Remark: r.Remark})
		if errors.Is(err, ErrDuplicateName) {
			return partnerimport.Duplicate, nil
		}
		if err != nil {
			return 0, err
		}
		return partnerimport.Created, nil
	})
	return &res, nil
}
