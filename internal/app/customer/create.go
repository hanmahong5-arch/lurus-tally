package customer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/customer"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/codegen"
)

// CreateUseCase creates a new customer.
type CreateUseCase struct {
	repo  Repository
	codes codegen.Source
}

// NewCreateUseCase constructs a CreateUseCase.
func NewCreateUseCase(repo Repository) *CreateUseCase {
	return &CreateUseCase{repo: repo, codes: codegen.Default()}
}

// WithCodeSource replaces the clock/randomness used for generated codes (tests).
func (uc *CreateUseCase) WithCodeSource(s codegen.Source) *CreateUseCase {
	uc.codes = s
	return uc
}

// NormalizeName trims and collapses runs of whitespace so "  王  五 " and "王 五"
// compare equal.
func NormalizeName(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Execute validates the input and creates a new Customer.
// An empty Code is server-assigned (C + yymmdd + 4 symbols, see codegen) and
// redrawn on a unique violation; an explicit Code is used verbatim and a clash
// surfaces as the repo's unique violation (409 at the handler).
//
// A live customer with the same name (case-insensitive, whitespace-normalised)
// or the same non-empty mobile is rejected with a *DuplicateError carrying the
// existing id. This is an application-level check: two concurrent creates can
// both pass it. No unique index backs it because existing tenants may already
// hold duplicates.
func (uc *CreateUseCase) Execute(ctx context.Context, in domain.CreateInput) (*domain.Customer, error) {
	now := time.Now().UTC()
	c := &domain.Customer{
		ID:          uuid.New(),
		TenantID:    in.TenantID,
		Code:        in.Code,
		Name:        NormalizeName(in.Name),
		ContactName: in.ContactName,
		Mobile:      strings.TrimSpace(in.Mobile),
		Phone:       in.Phone,
		Address:     in.Address,
		Remark:      in.Remark,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	if err := uc.checkDuplicate(ctx, c); err != nil {
		return nil, err
	}
	err := uc.codes.Create(codegen.PrefixCustomer, in.Code, codegen.IsUniqueViolation, func(code string) error {
		c.Code = code
		return uc.repo.Create(ctx, c)
	})
	if err != nil {
		return nil, fmt.Errorf("customer create: %w", err)
	}
	return c, nil
}

func (uc *CreateUseCase) checkDuplicate(ctx context.Context, c *domain.Customer) error {
	found, err := uc.repo.FindActiveByName(ctx, c.TenantID, c.Name)
	if err == nil {
		return &DuplicateError{Kind: ErrDuplicateName, ExistingID: found.ID, ExistingName: found.Name}
	}
	if !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("customer create: %w", err)
	}
	mobile := strings.TrimSpace(c.Mobile)
	if mobile == "" {
		return nil
	}
	found, err = uc.repo.FindActiveByMobile(ctx, c.TenantID, mobile)
	if err == nil {
		return &DuplicateError{Kind: ErrDuplicateMobile, ExistingID: found.ID, ExistingName: found.Name}
	}
	if !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("customer create: %w", err)
	}
	return nil
}
