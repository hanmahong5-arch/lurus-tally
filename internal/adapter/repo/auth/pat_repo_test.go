package auth_test

import (
	"database/sql"
	"testing"

	repoauth "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/auth"
	appauth "github.com/hanmahong5-arch/lurus-tally/internal/app/auth"
)

// TestPATRepo_InterfaceSatisfied is a compile-time proof that *Repo implements
// the Repository port. Integration tests against a live Postgres are added in
// a follow-up commit once Phase 2 HTTP endpoints land.
func TestPATRepo_InterfaceSatisfied(t *testing.T) {
	var _ appauth.Repository = repoauth.New((*sql.DB)(nil))
	t.Log("*repoauth.Repo satisfies appauth.Repository — compile-time check passed")
}
