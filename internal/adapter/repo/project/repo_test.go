package project_test

import (
	"database/sql"
	"testing"

	repoproj "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/project"
	appproject "github.com/hanmahong5-arch/lurus-tally/internal/app/project"
)

// TestProjectRepo_InterfaceSatisfied is a compile-time proof that *Repo satisfies Repository.
// Integration tests against a live Postgres require TEST_DSN env var and the -tags integration flag.
func TestProjectRepo_InterfaceSatisfied(t *testing.T) {
	var _ appproject.Repository = repoproj.New((*sql.DB)(nil))
	t.Log("*repoproj.Repo satisfies appproject.Repository — compile-time check passed")
}
