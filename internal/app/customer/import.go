package customer

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/partnerimport"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/customer"
)

// ImportUseCase bulk-creates customers from a CSV file, skipping names already in the shop.
type ImportUseCase struct {
	repo   Repository
	create *CreateUseCase
}

// NewImportUseCase wires the use case.
func NewImportUseCase(repo Repository, create *CreateUseCase) *ImportUseCase {
	return &ImportUseCase{repo: repo, create: create}
}

// Execute parses data and creates every new customer. Whole-file problems come back as
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
			return nil, fmt.Errorf("customer import list: %w", lerr)
		}
		total = t
		if len(items) == 0 {
			break
		}
		for _, c := range items {
			existing[partnerimport.NameKey(c.Name)] = true
		}
		off += len(items)
	}
	res := partnerimport.Run(ctx, rows, perrs, existing, func(ctx context.Context, r partnerimport.Row) (partnerimport.Outcome, error) {
		in := domain.CreateInput{TenantID: tenantID, Name: r.Name, ContactName: r.Contact, Address: r.Address, Remark: r.Remark}
		if looksLikeMobile(r.Phone) {
			in.Mobile = r.Phone
		} else {
			in.Phone = r.Phone
		}
		if _, err := uc.create.Execute(ctx, in); err != nil {
			var dup *DuplicateError
			if errors.As(err, &dup) {
				if dup.Kind == ErrDuplicateMobile {
					return partnerimport.DuplicateMobile, nil
				}
				return partnerimport.Duplicate, nil
			}
			return 0, err
		}
		return partnerimport.Created, nil
	})
	return &res, nil
}

func looksLikeMobile(s string) bool {
	if len(s) != 11 || s[0] != '1' {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
