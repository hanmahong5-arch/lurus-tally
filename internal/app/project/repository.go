// Package project contains application-layer use cases for projects.
package project

import (
	"context"
	"errors"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/project"
)

// ErrNotFound is returned when the requested project does not exist.
var ErrNotFound = errors.New("project not found")

// ErrDuplicateCode is returned when a project code already exists for the tenant.
var ErrDuplicateCode = errors.New("project duplicate code")

// Repository abstracts the persistence layer for Project.
type Repository interface {
	Create(ctx context.Context, p *domain.Project) error
	GetByID(ctx context.Context, tenantID, id uuid.UUID) (*domain.Project, error)
	List(ctx context.Context, f domain.ListFilter) ([]*domain.Project, int, error)
	Update(ctx context.Context, p *domain.Project) error
	Delete(ctx context.Context, tenantID, id uuid.UUID) error
	Restore(ctx context.Context, tenantID, id uuid.UUID) (*domain.Project, error)
}
