// Package tenant implements use cases for tenant profile management.
package tenant

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
)

// getMeProfileRepository is the repo interface needed by GetMeUseCase.
type getMeProfileRepository interface {
	GetByTenantID(ctx context.Context, tenantID uuid.UUID) (*domain.TenantProfile, error)
}

// GetMeInput carries the authenticated user context.
type GetMeInput struct {
	TenantID uuid.UUID
	UserSub  string // Zitadel sub claim
}

// GetMeOutput is the response payload for GET /api/v1/me.
type GetMeOutput struct {
	UserSub     string `json:"user_id"`   // Zitadel sub
	TenantID    string `json:"tenant_id"`
	ProfileType string `json:"profile_type"` // "" when no profile set yet
}

// GetMeUseCase retrieves the current user's context from stored data.
type GetMeUseCase struct {
	repo getMeProfileRepository
}

// NewGetMeUseCase constructs the use case.
func NewGetMeUseCase(repo getMeProfileRepository) *GetMeUseCase {
	return &GetMeUseCase{repo: repo}
}

// Execute fetches the profile for the authenticated tenant and assembles the output.
func (uc *GetMeUseCase) Execute(ctx context.Context, in GetMeInput) (*GetMeOutput, error) {
	if in.TenantID == uuid.Nil && in.UserSub == "" {
		return nil, fmt.Errorf("get me: tenant_id or user sub is required")
	}

	var profileType string
	if in.TenantID != uuid.Nil {
		p, err := uc.repo.GetByTenantID(ctx, in.TenantID)
		if err != nil {
			return nil, fmt.Errorf("get me: fetch profile: %w", err)
		}
		if p != nil {
			profileType = string(p.ProfileType)
		}
	}

	return &GetMeOutput{
		UserSub:     in.UserSub,
		TenantID:    in.TenantID.String(),
		ProfileType: profileType,
	}, nil
}
