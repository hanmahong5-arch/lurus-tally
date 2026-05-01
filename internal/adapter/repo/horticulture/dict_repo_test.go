package horticulture_test

import (
	"database/sql"
	"testing"

	repohort "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/horticulture"
	apphort "github.com/hanmahong5-arch/lurus-tally/internal/app/horticulture"
)

// TestDictRepo_InterfaceSatisfied is a compile-time proof that *Repo satisfies Repository.
// Integration tests against a live Postgres require TEST_DSN env var and the -tags integration flag.
func TestDictRepo_InterfaceSatisfied(t *testing.T) {
	var _ apphort.Repository = repohort.New((*sql.DB)(nil))
	t.Log("*repohort.Repo satisfies apphort.Repository — compile-time check passed")
}
