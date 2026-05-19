// Package project contains the domain entity for landscaping/engineering projects.
package project

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ProjectStatus represents the lifecycle state of a project.
type ProjectStatus string

const (
	StatusActive    ProjectStatus = "active"
	StatusPaused    ProjectStatus = "paused"
	StatusCompleted ProjectStatus = "completed"
	StatusCancelled ProjectStatus = "cancelled"
	StatusArchived  ProjectStatus = "archived"
)

// ErrIllegalProjectStatusTransition is returned by CanTransitionTo when the requested
// status transition is not part of the defined state machine.
var ErrIllegalProjectStatusTransition = errors.New("project: illegal status transition")

// String returns the string value of the ProjectStatus.
func (s ProjectStatus) String() string { return string(s) }

// CanTransitionTo returns nil if the state machine allows moving from s to next.
// Legal transitions:
//   - active    → completed ✓
//   - active    → archived  ✓
//   - completed → archived  ✓
//   - archived  → active    ✓ (un-archive)
//
// All other transitions — including completed → active — return ErrIllegalProjectStatusTransition.
func (s ProjectStatus) CanTransitionTo(next ProjectStatus) error {
	switch s {
	case StatusActive:
		if next == StatusCompleted || next == StatusArchived {
			return nil
		}
	case StatusCompleted:
		if next == StatusArchived {
			return nil
		}
	case StatusArchived:
		if next == StatusActive {
			return nil
		}
	}
	return ErrIllegalProjectStatusTransition
}

// Project is the domain entity for a landscaping/engineering project.
type Project struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	Code           string
	Name           string
	CustomerID     *uuid.UUID
	ContractAmount *string // stored as string to avoid float precision loss; NUMERIC(18,2)
	StartDate      *time.Time
	EndDate        *time.Time
	Status         ProjectStatus
	Address        string
	Manager        string
	Remark         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}

// Validate enforces domain invariants.
func (p *Project) Validate() error {
	if p.Name == "" {
		return errors.New("name is required")
	}
	if p.Code == "" {
		return errors.New("code is required")
	}
	if p.StartDate != nil && p.EndDate != nil {
		if p.EndDate.Before(*p.StartDate) {
			return errors.New("end_date must be on or after start_date")
		}
	}
	return nil
}

// CreateInput carries fields for creating a new Project.
type CreateInput struct {
	TenantID       uuid.UUID
	Code           string
	Name           string
	CustomerID     *uuid.UUID
	ContractAmount *string
	StartDate      *time.Time
	EndDate        *time.Time
	Status         ProjectStatus
	Address        string
	Manager        string
	Remark         string
}

// UpdateInput carries mutable fields (nil pointer = do not update).
type UpdateInput struct {
	Code           *string
	Name           *string
	CustomerID     *uuid.UUID
	ContractAmount *string
	StartDate      *time.Time
	EndDate        *time.Time
	Status         *ProjectStatus
	Address        *string
	Manager        *string
	Remark         *string
}

// ListFilter controls list queries.
type ListFilter struct {
	TenantID   uuid.UUID
	Query      string // ILIKE on name or code
	Status     *ProjectStatus
	CustomerID *uuid.UUID
	Limit      int
	Offset     int
}
