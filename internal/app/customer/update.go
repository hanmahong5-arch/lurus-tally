package customer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/customer"
)

// UpdateUseCase applies partial updates to an existing customer.
type UpdateUseCase struct {
	repo Repository
}

// NewUpdateUseCase constructs an UpdateUseCase.
func NewUpdateUseCase(repo Repository) *UpdateUseCase {
	return &UpdateUseCase{repo: repo}
}

// Execute fetches the existing customer, applies non-nil fields, rejects
// name/mobile clashes with another live customer, validates, and persists.
func (uc *UpdateUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID, in domain.UpdateInput) (*domain.Customer, error) {
	c, err := uc.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("customer update fetch: %w", err)
	}

	if in.Code != nil {
		c.Code = *in.Code
	}
	if in.Name != nil {
		c.Name = NormalizeName(*in.Name)
	}
	if in.ContactName != nil {
		c.ContactName = *in.ContactName
	}
	if in.Mobile != nil {
		c.Mobile = strings.TrimSpace(*in.Mobile)
	}
	if in.Phone != nil {
		c.Phone = *in.Phone
	}
	if in.Address != nil {
		c.Address = *in.Address
	}
	if in.Remark != nil {
		c.Remark = *in.Remark
	}
	c.UpdatedAt = time.Now().UTC()

	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	if err := uc.checkDuplicate(ctx, c); err != nil {
		return nil, err
	}
	if err := uc.repo.Update(ctx, c); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("customer update: %w", err)
	}
	return c, nil
}

// checkDuplicate mirrors Create: same name or mobile on another live customer
// is a 409. Finding ourselves is fine (unchanged name/mobile).
func (uc *UpdateUseCase) checkDuplicate(ctx context.Context, c *domain.Customer) error {
	found, err := uc.repo.FindActiveByName(ctx, c.TenantID, c.Name)
	if err == nil && found.ID != c.ID {
		return &DuplicateError{Kind: ErrDuplicateName, ExistingID: found.ID, ExistingName: found.Name}
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("customer update: %w", err)
	}
	mobile := strings.TrimSpace(c.Mobile)
	if mobile == "" {
		return nil
	}
	found, err = uc.repo.FindActiveByMobile(ctx, c.TenantID, mobile)
	if err == nil && found.ID != c.ID {
		return &DuplicateError{Kind: ErrDuplicateMobile, ExistingID: found.ID, ExistingName: found.Name}
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("customer update: %w", err)
	}
	return nil
}
