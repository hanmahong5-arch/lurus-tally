package supplier

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/supplier"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/codegen"
)

// CreateUseCase creates a new supplier.
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

// Execute validates the input and creates a new Supplier.
// An empty Code is server-assigned (S + yymmdd + 4 symbols, see codegen). The
// repo folds every unique violation (supplier name, or the partner mirror's
// code) into ErrDuplicateName, so a generated code is redrawn on that error; a
// genuine name clash simply fails again and still surfaces as ErrDuplicateName.
func (uc *CreateUseCase) Execute(ctx context.Context, in domain.CreateInput) (*domain.Supplier, error) {
	now := time.Now().UTC()
	s := &domain.Supplier{
		ID:        uuid.New(),
		TenantID:  in.TenantID,
		Code:      in.Code,
		Name:      in.Name,
		Contact:   in.Contact,
		Phone:     in.Phone,
		Email:     in.Email,
		Address:   in.Address,
		Remark:    in.Remark,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("supplier create validate: %w", err)
	}
	isDup := func(err error) bool { return errors.Is(err, ErrDuplicateName) }
	err := uc.codes.Create(codegen.PrefixSupplier, in.Code, isDup, func(code string) error {
		s.Code = code
		return uc.repo.Create(ctx, s)
	})
	if err != nil {
		if errors.Is(err, ErrDuplicateName) {
			return nil, ErrDuplicateName
		}
		return nil, fmt.Errorf("supplier create: %w", err)
	}
	return s, nil
}
