package product

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	appproduct "github.com/hanmahong5-arch/lurus-tally/internal/app/product"
)

func TestExistingLookups_EmptyInputSkipsQuery(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()
	r := New(db)
	for _, f := range []func() (map[string]bool, error){
		func() (map[string]bool, error) { return r.ExistingCodes(context.Background(), uuid.New(), nil) },
		func() (map[string]bool, error) { return r.ExistingBarcodes(context.Background(), uuid.New(), nil) },
		func() (map[string]bool, error) { return r.ExistingNameSpecs(context.Background(), uuid.New(), nil) },
	} {
		got, err := f()
		if err != nil || len(got) != 0 {
			t.Fatalf("got=%v err=%v", got, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestExistingCodes_PassesValuesAsJSONArray(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()
	tid := uuid.New()
	mock.ExpectQuery("jsonb_array_elements_text").
		WithArgs(tid, `["A,1","带\"引号"]`).
		WillReturnRows(sqlmock.NewRows([]string{"code", "x"}).AddRow("A,1", ""))
	got, err := New(db).ExistingCodes(context.Background(), tid, []string{"A,1", `带"引号`})
	if err != nil {
		t.Fatal(err)
	}
	if !got["A,1"] || len(got) != 1 {
		t.Fatalf("got = %v", got)
	}
}

func TestExistingNameSpecs_KeyedByNameAndSpec(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()
	mock.ExpectQuery("FROM tally.product").
		WillReturnRows(sqlmock.NewRows([]string{"name", "spec"}).AddRow("水管", "").AddRow("水管", "DN20"))
	got, err := New(db).ExistingNameSpecs(context.Background(), uuid.New(), []string{"水管"})
	if err != nil {
		t.Fatal(err)
	}
	if !got[appproduct.NameSpecKey("水管", "")] || !got[appproduct.NameSpecKey("水管", "dn20")] || got[appproduct.NameSpecKey("水管", "DN25")] {
		t.Fatalf("got = %v", got)
	}
}
