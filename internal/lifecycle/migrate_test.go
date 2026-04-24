package lifecycle_test

import (
	"context"
	"io/fs"
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/lifecycle"
	"github.com/hanmahong5-arch/lurus-tally/migrations"
)

// TestRunMigrations_InvalidDSN_ReturnsError verifies that RunMigrations returns a non-nil error
// containing "migration failed" when the DSN is unreachable.
func TestRunMigrations_InvalidDSN_ReturnsError(t *testing.T) {
	ctx := context.Background()
	dsn := "postgres://invalid:invalid@127.0.0.1:1/nonexistent?sslmode=disable&connect_timeout=1"

	err := lifecycle.RunMigrations(ctx, dsn, nil)
	if err == nil {
		t.Fatal("expected error for unreachable DSN, got nil")
	}
	const want = "migration failed"
	if len(err.Error()) == 0 {
		t.Fatal("error message must not be empty")
	}
	// Verify the error contains the required prefix so operators know what happened.
	if !containsString(err.Error(), want) {
		t.Errorf("error %q does not contain %q", err.Error(), want)
	}
}

// TestRunMigrations_EmbedNotEmpty verifies that the embedded FS can be read
// and contains the first migration file with non-zero content.
func TestRunMigrations_EmbedNotEmpty(t *testing.T) {
	data, err := fs.ReadFile(migrations.FS, "000001_init_extensions.up.sql")
	if err != nil {
		t.Fatalf("expected to read 000001_init_extensions.up.sql from embed.FS: %v", err)
	}
	if len(data) == 0 {
		t.Error("000001_init_extensions.up.sql must not be empty")
	}
}

// TestMigrate_EmbedFS_LoadsMigrations verifies that migrations.FS contains at least 24 files
// (12 up + 12 down) matching *.sql.
func TestMigrate_EmbedFS_LoadsMigrations(t *testing.T) {
	var count int
	err := fs.WalkDir(migrations.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && len(path) > 4 && path[len(path)-4:] == ".sql" {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir migrations.FS: %v", err)
	}
	const want = 24
	if count < want {
		t.Errorf("expected at least %d SQL files in migrations.FS, got %d", want, count)
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
