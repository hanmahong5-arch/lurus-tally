package lifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	repounit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/unit"
	repowarehouse "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/warehouse"
	appproduct "github.com/hanmahong5-arch/lurus-tally/internal/app/product"
)

func TestTruncRunes(t *testing.T) {
	if got := truncRunes("一二三四五", 3); got != "一二三" {
		t.Fatalf("got %q", got)
	}
	if got := truncRunes("盒", 20); got != "盒" {
		t.Fatalf("got %q", got)
	}
}

func TestImportOpeningStock_RequiresTransaction(t *testing.T) {
	err := importOpeningStock{}.RecordOpening(context.Background(), appproduct.OpeningStockInput{})
	if err == nil {
		t.Fatal("opening stock outside a row transaction must be refused")
	}
}

func TestImportUnits_UnitIDsByNameKeysNameAndCodeLowercased(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()
	id := uuid.New()
	mock.ExpectQuery("FROM tally.unit_def").WillReturnRows(sqlmock.NewRows(
		[]string{"id", "tenant_id", "code", "name", "unit_type", "is_system", "created_at", "updated_at"}).
		AddRow(id, uuid.Nil, "PCS", "个", "count", true, time.Now(), time.Now()))
	got, err := importUnits{repo: repounit.New(db)}.UnitIDsByName(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if got["个"] != id || got["pcs"] != id {
		t.Fatalf("got = %v", got)
	}
}

func TestImportUnits_CreateUnitRetriesOnCodeClash(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()
	mock.ExpectExec("INSERT INTO tally.unit_def").WillReturnError(&pgconn.PgError{Code: "23505"})
	mock.ExpectExec("INSERT INTO tally.unit_def").WillReturnResult(sqlmock.NewResult(0, 1))
	id, err := importUnits{repo: repounit.New(db)}.CreateUnit(context.Background(), uuid.New(), "盒")
	if err != nil || id == uuid.Nil {
		t.Fatalf("id=%v err=%v", id, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestImportWarehouses_NoWarehouseGivesNilDefault(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()
	mock.ExpectQuery("COUNT").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
	mock.ExpectQuery("FROM tally.warehouse").WillReturnRows(sqlmock.NewRows(
		[]string{"id", "tenant_id", "code", "name", "address", "manager", "is_default", "remark", "created_at", "updated_at", "deleted_at"}))
	mock.ExpectQuery("is_default DESC").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	byName, def, err := importWarehouses{repo: repowarehouse.New(db)}.WarehousesByName(context.Background(), uuid.New())
	if err != nil || def != uuid.Nil || len(byName) != 0 {
		t.Fatalf("byName=%v def=%v err=%v", byName, def, err)
	}
}
