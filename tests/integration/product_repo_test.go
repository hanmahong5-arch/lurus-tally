//go:build integration

// Product repository real-SQL tests: the dynamic List filter assembly (ILIKE
// search across name/code/mnemonic, JSONB @> attribute containment, enabled
// flag, limit/offset pagination with a full-count), per-tenant scoping on
// GetByID, and the soft-delete / restore lifecycle. Connects as the
// testcontainer superuser, so isolation here is the repo's explicit WHERE
// tenant_id clause (RLS-level isolation is covered by rls_e2e_test.go).
//
//	go test -v -tags integration -timeout 180s ./tests/integration/ -run TestProductRepo
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	productrepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/product"
	domainproduct "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
)

// insertProductFull inserts a product with the columns the List filters key on
// (mnemonic, enabled, attributes). The text columns the repo scans as plain
// (non-nullable) strings — manufacturer/model/spec/brand/color/shelf_position/
// remark — are set to ” to mirror what repo.Create writes in production;
// leaving them NULL would break the repo's scan. The shared insertProduct
// fixture is intentionally minimal, so this lives here.
func insertProductFull(t *testing.T, db *sql.DB, ctx context.Context, tenantID uuid.UUID, code, name, mnemonic string, enabled bool, attrsJSON string) uuid.UUID {
	t.Helper()
	if attrsJSON == "" {
		attrsJSON = "{}"
	}
	id := uuid.New()
	_, err := db.ExecContext(ctx, `
		INSERT INTO tally.product
		    (id, tenant_id, code, name, mnemonic, enabled, attributes, lead_time_days,
		     manufacturer, model, spec, brand, color, shelf_position, remark)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, 7,
		        '', '', '', '', '', '', '')
	`, id, tenantID, code, name, mnemonic, enabled, attrsJSON)
	if err != nil {
		t.Fatalf("insertProductFull(%s): %v", code, err)
	}
	return id
}

func boolPtr(b bool) *bool { return &b }

func TestProductRepo_List_SearchEnabledPagination(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()
	ctx := context.Background()
	repo := productrepo.New(db)

	tenantID := insertTenant(t, db, ctx)
	insertProductFull(t, db, ctx, tenantID, "WIDG-A", "Widget Alpha", "wga", true, "")
	insertProductFull(t, db, ctx, tenantID, "GAD-B", "Gadget Beta", "gdb", true, "")
	insertProductFull(t, db, ctx, tenantID, "GIZ-C", "Gizmo Gamma", "gzc", false, "")

	t.Run("ILIKE_name_case_insensitive", func(t *testing.T) {
		got, total, err := repo.List(ctx, domainproduct.ListFilter{TenantID: tenantID, Query: "alpha", Limit: 10})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if total != 1 || len(got) != 1 || got[0].Code != "WIDG-A" {
			t.Errorf("query 'alpha': total=%d rows=%d, want 1 WIDG-A", total, len(got))
		}
	})

	t.Run("ILIKE_matches_code", func(t *testing.T) {
		got, _, err := repo.List(ctx, domainproduct.ListFilter{TenantID: tenantID, Query: "gad", Limit: 10})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got) != 1 || got[0].Code != "GAD-B" {
			t.Errorf("query 'gad': rows=%d, want 1 GAD-B", len(got))
		}
	})

	t.Run("ILIKE_matches_mnemonic", func(t *testing.T) {
		got, _, err := repo.List(ctx, domainproduct.ListFilter{TenantID: tenantID, Query: "gzc", Limit: 10})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got) != 1 || got[0].Code != "GIZ-C" {
			t.Errorf("query 'gzc' (mnemonic): rows=%d, want 1 GIZ-C", len(got))
		}
	})

	t.Run("ILIKE_underscore_is_wildcard", func(t *testing.T) {
		// The repo wraps the raw query in %...% without escaping, so '_' behaves
		// as SQL's single-char wildcard. 'Wi_get' matches 'Widget' (_ = 'd').
		// Pins current (unescaped) behaviour — a future escaping fix breaks this
		// intentionally.
		got, _, err := repo.List(ctx, domainproduct.ListFilter{TenantID: tenantID, Query: "Wi_get", Limit: 10})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got) != 1 || got[0].Code != "WIDG-A" {
			t.Errorf("query 'Wi_get': rows=%d, want 1 WIDG-A (underscore wildcard)", len(got))
		}
	})

	t.Run("enabled_filter", func(t *testing.T) {
		on, totalOn, err := repo.List(ctx, domainproduct.ListFilter{TenantID: tenantID, Enabled: boolPtr(true), Limit: 10})
		if err != nil {
			t.Fatalf("List enabled=true: %v", err)
		}
		if totalOn != 2 || len(on) != 2 {
			t.Errorf("enabled=true: total=%d rows=%d, want 2", totalOn, len(on))
		}
		off, totalOff, err := repo.List(ctx, domainproduct.ListFilter{TenantID: tenantID, Enabled: boolPtr(false), Limit: 10})
		if err != nil {
			t.Fatalf("List enabled=false: %v", err)
		}
		if totalOff != 1 || len(off) != 1 || off[0].Code != "GIZ-C" {
			t.Errorf("enabled=false: total=%d rows=%d, want 1 GIZ-C", totalOff, len(off))
		}
	})

	t.Run("pagination_limit_offset_with_full_count", func(t *testing.T) {
		page1, total, err := repo.List(ctx, domainproduct.ListFilter{TenantID: tenantID, Limit: 2, Offset: 0})
		if err != nil {
			t.Fatalf("List page1: %v", err)
		}
		if total != 3 {
			t.Errorf("total = %d, want 3 (full count ignores limit/offset)", total)
		}
		if len(page1) != 2 {
			t.Errorf("page1 rows = %d, want 2", len(page1))
		}
		page2, _, err := repo.List(ctx, domainproduct.ListFilter{TenantID: tenantID, Limit: 2, Offset: 2})
		if err != nil {
			t.Fatalf("List page2: %v", err)
		}
		if len(page2) != 1 {
			t.Errorf("page2 rows = %d, want 1 (3 total, offset 2)", len(page2))
		}
		// No overlap between pages (ORDER BY created_at DESC is stable here).
		if len(page1) == 2 && len(page2) == 1 {
			if page1[0].ID == page2[0].ID || page1[1].ID == page2[0].ID {
				t.Errorf("pagination overlap: page2 id %s also on page1", page2[0].ID)
			}
		}
	})
}

func TestProductRepo_List_JSONBAttributeFilter(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()
	ctx := context.Background()
	repo := productrepo.New(db)

	tenantID := insertTenant(t, db, ctx)
	red := insertProductFull(t, db, ctx, tenantID, "RED-1", "Red Large", "rl", true, `{"color":"red","size":"L"}`)
	insertProductFull(t, db, ctx, tenantID, "BLU-1", "Blue Small", "bs", true, `{"color":"blue","size":"S"}`)

	t.Run("containment_single_key", func(t *testing.T) {
		got, total, err := repo.List(ctx, domainproduct.ListFilter{
			TenantID:         tenantID,
			AttributesFilter: json.RawMessage(`{"color":"red"}`),
			Limit:            10,
		})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if total != 1 || len(got) != 1 || got[0].ID != red {
			t.Errorf(`@> {"color":"red"}: total=%d rows=%d, want 1 RED-1`, total, len(got))
		}
	})

	t.Run("containment_multi_key", func(t *testing.T) {
		got, _, err := repo.List(ctx, domainproduct.ListFilter{
			TenantID:         tenantID,
			AttributesFilter: json.RawMessage(`{"color":"red","size":"L"}`),
			Limit:            10,
		})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got) != 1 || got[0].ID != red {
			t.Errorf(`@> {"color":"red","size":"L"}: rows=%d, want 1 RED-1`, len(got))
		}
	})

	t.Run("no_match", func(t *testing.T) {
		got, total, err := repo.List(ctx, domainproduct.ListFilter{
			TenantID:         tenantID,
			AttributesFilter: json.RawMessage(`{"color":"green"}`),
			Limit:            10,
		})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if total != 0 || len(got) != 0 {
			t.Errorf(`@> {"color":"green"}: total=%d rows=%d, want 0`, total, len(got))
		}
	})

	t.Run("empty_filter_is_ignored", func(t *testing.T) {
		// "{}" must be treated as no filter (the repo skips it), returning all.
		got, total, err := repo.List(ctx, domainproduct.ListFilter{
			TenantID:         tenantID,
			AttributesFilter: json.RawMessage(`{}`),
			Limit:            10,
		})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if total != 2 || len(got) != 2 {
			t.Errorf("empty {} filter: total=%d rows=%d, want 2 (no filtering)", total, len(got))
		}
	})
}

func TestProductRepo_GetByID_TenantIsolation(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()
	ctx := context.Background()
	repo := productrepo.New(db)

	tenantA := insertTenant(t, db, ctx)
	tenantB := insertTenant(t, db, ctx)
	prodA := insertProductFull(t, db, ctx, tenantA, "ISO-A", "Tenant A Product", "tap", true, "")

	if got, err := repo.GetByID(ctx, tenantA, prodA); err != nil || got == nil || got.ID != prodA {
		t.Errorf("GetByID(tenantA): err=%v product=%v, want the product", err, got)
	}
	// Tenant B must not see tenant A's product.
	_, err := repo.GetByID(ctx, tenantB, prodA)
	if !errors.Is(err, productrepo.ErrNotFound) {
		t.Errorf("GetByID(tenantB, prodA): err=%v, want ErrNotFound", err)
	}
}

func TestProductRepo_SoftDeleteAndRestore(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()
	ctx := context.Background()
	repo := productrepo.New(db)

	tenantID := insertTenant(t, db, ctx)
	id := insertProductFull(t, db, ctx, tenantID, "DEL-1", "Deletable", "del", true, "")

	// Delete → invisible to GetByID and List.
	if err := repo.Delete(ctx, tenantID, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.GetByID(ctx, tenantID, id); !errors.Is(err, productrepo.ErrNotFound) {
		t.Errorf("GetByID after delete: err=%v, want ErrNotFound", err)
	}
	if _, total, err := repo.List(ctx, domainproduct.ListFilter{TenantID: tenantID, Limit: 10}); err != nil || total != 0 {
		t.Errorf("List after delete: total=%d err=%v, want 0", total, err)
	}

	// Deleting an already-deleted row → ErrNotFound (no double-delete).
	if err := repo.Delete(ctx, tenantID, id); !errors.Is(err, productrepo.ErrNotFound) {
		t.Errorf("second Delete: err=%v, want ErrNotFound", err)
	}

	// Restore → visible again, returns the row.
	restored, err := repo.Restore(ctx, tenantID, id)
	if err != nil || restored == nil || restored.ID != id {
		t.Fatalf("Restore: err=%v product=%v, want the product", err, restored)
	}
	if _, err := repo.GetByID(ctx, tenantID, id); err != nil {
		t.Errorf("GetByID after restore: err=%v, want found", err)
	}

	// Restoring a row that is not deleted → ErrNotFound.
	if _, err := repo.Restore(ctx, tenantID, id); !errors.Is(err, productrepo.ErrNotFound) {
		t.Errorf("Restore of non-deleted: err=%v, want ErrNotFound", err)
	}
}
