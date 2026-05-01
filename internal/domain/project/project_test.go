package project_test

import (
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-tally/internal/domain/project"
)

func TestProject_Validate_RejectsEmptyName(t *testing.T) {
	p := &project.Project{Code: "P001"}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
	if err.Error() != "name is required" {
		t.Errorf("expected 'name is required', got %q", err.Error())
	}
}

func TestProject_Validate_RejectsEmptyCode(t *testing.T) {
	p := &project.Project{Name: "河道绿化"}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for empty code, got nil")
	}
	if err.Error() != "code is required" {
		t.Errorf("expected 'code is required', got %q", err.Error())
	}
}

func TestProject_Validate_RejectsEndBeforeStart(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	p := &project.Project{
		Name:      "河道绿化",
		Code:      "P001",
		StartDate: &start,
		EndDate:   &end,
	}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for end before start, got nil")
	}
	if err.Error() != "end_date must be on or after start_date" {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestProject_Validate_AcceptsNilDates(t *testing.T) {
	p := &project.Project{
		Name:      "河道绿化",
		Code:      "P001",
		StartDate: nil,
		EndDate:   nil,
	}
	if err := p.Validate(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestProject_Validate_AcceptsOnlyStartDate(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &project.Project{
		Name:      "河道绿化",
		Code:      "P001",
		StartDate: &start,
		EndDate:   nil,
	}
	if err := p.Validate(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestProjectStatus_AllValues(t *testing.T) {
	cases := []struct {
		status project.ProjectStatus
		want   string
	}{
		{project.StatusActive, "active"},
		{project.StatusPaused, "paused"},
		{project.StatusCompleted, "completed"},
		{project.StatusCancelled, "cancelled"},
	}
	for _, tc := range cases {
		if tc.status.String() != tc.want {
			t.Errorf("status %q: got %q, want %q", tc.status, tc.status.String(), tc.want)
		}
	}
}
