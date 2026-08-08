// Regression guard for the migration-set shape. These tests need no database:
// they exercise the exact source-loading step RunMigrations does before it ever
// touches Postgres, which is where a malformed set fails.
//
// Why this exists: two branches independently numbered a migration 000053
// (usage_report_outbox, drop_dead_reorder_views) and the collision only surfaced
// when both merged. golang-migrate's iofs source refuses to load the WHOLE set on
// a duplicate version, so every integration test failed at RunMigrations and any
// deploy would have crash-looped at boot — for 37 days, silently, because the
// then-deployed image predated the collision. A DB-free unit test catches it in
// the fast job instead.
package lifecycle_test

import (
	"io/fs"
	"regexp"
	"strconv"
	"testing"

	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/hanmahong5-arch/lurus-tally/migrations"
)

// migrationName matches golang-migrate's required filename shape:
// <version>_<description>.<up|down>.sql
var migrationName = regexp.MustCompile(`^(\d+)_(.+)\.(up|down)\.sql$`)

// TestMigrations_SourceLoads is the oracle: it is the same iofs.New call
// RunMigrations makes, so a green here means the embedded set is loadable
// (no duplicate versions, no unparseable names) regardless of any DB.
func TestMigrations_SourceLoads(t *testing.T) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		t.Fatalf("iofs.New over migrations.FS failed — RunMigrations cannot load ANY "+
			"migration and the service will not boot: %v", err)
	}
	t.Cleanup(func() { _ = src.Close() })
}

// TestMigrations_NoDuplicateVersions reports WHICH files collide. iofs.New only
// names one of the pair, so this test exists to make the fix obvious rather than
// to add coverage.
func TestMigrations_NoDuplicateVersions(t *testing.T) {
	// version -> direction -> filename
	seen := map[uint64]map[string]string{}

	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("ReadDir migrations.FS: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := migrationName.FindStringSubmatch(e.Name())
		if m == nil {
			// data/ helpers and embed.go are not part of the set; anything else
			// that ends in .sql but does not parse would be silently ignored by
			// golang-migrate, which is itself a bug worth failing on.
			if len(e.Name()) > 4 && e.Name()[len(e.Name())-4:] == ".sql" {
				t.Errorf("%s does not match <version>_<name>.<up|down>.sql — golang-migrate "+
					"will silently ignore it", e.Name())
			}
			continue
		}
		version, err := strconv.ParseUint(m[1], 10, 64)
		if err != nil {
			t.Errorf("%s: unparseable version %q: %v", e.Name(), m[1], err)
			continue
		}
		if seen[version] == nil {
			seen[version] = map[string]string{}
		}
		if prev, dup := seen[version][m[3]]; dup {
			t.Errorf("duplicate migration version %d (%s): %s and %s — pick the next free "+
				"ID from doc/coord/migration-ledger.md and renumber the one that never applied",
				version, m[3], prev, e.Name())
			continue
		}
		seen[version][m[3]] = e.Name()
	}

	// Every version must have both directions: a missing down makes the migration
	// irreversible, which only shows up during an incident rollback.
	for version, dirs := range seen {
		if _, ok := dirs["up"]; !ok {
			t.Errorf("migration %d has a down but no up", version)
		}
		if _, ok := dirs["down"]; !ok {
			t.Errorf("migration %d (%s) has no down — irreversible", version, dirs["up"])
		}
	}
}
