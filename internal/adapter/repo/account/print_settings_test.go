package account

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
)

func TestPrintSettingsRepo_PutKeepsOtherKeys(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	id := uuid.New()
	mock.ExpectExec(`UPDATE tally.tenant\s+SET settings = jsonb_set\(COALESCE\(settings, '\{\}'::jsonb\), '\{print\}'`).
		WithArgs(id, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewPrintSettingsRepo(db).PutPrint(context.Background(), id, appacct.PrintSettings{ShopName: "老王"}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPrintSettingsRepo_PutUnknownTenantIsNotFound(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectExec(`UPDATE tally.tenant`).WillReturnResult(sqlmock.NewResult(0, 0))
	err := NewPrintSettingsRepo(db).PutPrint(context.Background(), uuid.New(), appacct.PrintSettings{})
	if !errors.Is(err, appacct.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestPrintSettingsRepo_GetDecodesAndMapsNoRows(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	id := uuid.New()
	mock.ExpectQuery(`SELECT COALESCE`).WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"p"}).AddRow([]byte(`{"shop_name":"老王","receipt_footer":"谢谢"}`)))
	s, err := NewPrintSettingsRepo(db).GetPrint(context.Background(), id)
	if err != nil || s.ShopName != "老王" || s.ReceiptFooter != "谢谢" {
		t.Fatalf("got %+v %v", s, err)
	}
	mock.ExpectQuery(`SELECT COALESCE`).WithArgs(id).WillReturnError(sql.ErrNoRows)
	if _, err := NewPrintSettingsRepo(db).GetPrint(context.Background(), id); !errors.Is(err, appacct.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
