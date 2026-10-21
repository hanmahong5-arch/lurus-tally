package sku

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
)

func newMock(t *testing.T) (*Repo, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db), mock, db
}

func dec(s string) *decimal.Decimal { d := decimal.RequireFromString(s); return &d }

func TestUpsertDefaultSKU_EmptyIsNoop(t *testing.T) {
	r, mock, _ := newMock(t)
	if err := r.UpsertDefaultSKU(context.Background(), uuid.New(), uuid.New(), appsku.DefaultPrices{}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpsertDefaultSKU_UpdatesExistingRow(t *testing.T) {
	r, mock, _ := newMock(t)
	mock.ExpectExec("UPDATE tally.product_sku").
		WithArgs("12.5", nil, nil, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.UpsertDefaultSKU(context.Background(), uuid.New(), uuid.New(), appsku.DefaultPrices{Retail: dec("12.5")}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpsertDefaultSKU_InsertsWhenNoRow(t *testing.T) {
	r, mock, _ := newMock(t)
	mock.ExpectExec("UPDATE tally.product_sku").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO tally.product_sku").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "3", nil, "1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.UpsertDefaultSKU(context.Background(), uuid.New(), uuid.New(), appsku.DefaultPrices{Retail: dec("3"), Purchase: dec("1")}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpsertDefaultSKU_ClearWritesZero(t *testing.T) {
	r, mock, _ := newMock(t)
	mock.ExpectExec("UPDATE tally.product_sku").
		WithArgs(nil, "0", nil, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.UpsertDefaultSKU(context.Background(), uuid.New(), uuid.New(), appsku.DefaultPrices{Wholesale: dec("0")}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// The SKU write must ride the surrounding transaction, so a later failure in the
// same unit of work rolls the SKU back with the product.
func TestInTx_UpsertJoinsTransactionAndRollsBack(t *testing.T) {
	r, mock, _ := newMock(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE tally.product_sku").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()
	boom := errors.New("later step failed")
	err := r.InTx(context.Background(), func(ctx context.Context) error {
		if err := r.UpsertDefaultSKU(ctx, uuid.New(), uuid.New(), appsku.DefaultPrices{Retail: dec("1")}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListDefaultSKUs_EmptyInputSkipsQuery(t *testing.T) {
	r, mock, _ := newMock(t)
	got, err := r.ListDefaultSKUs(context.Background(), uuid.New(), nil)
	if err != nil || got != nil {
		t.Fatalf("got=%v err=%v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateRetailPrice_NotFoundWhenNoRowsAffected(t *testing.T) {
	r, mock, _ := newMock(t)
	mock.ExpectExec("UPDATE tally.product_sku").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := r.UpdateRetailPrice(context.Background(), uuid.New(), uuid.New(), decimal.NewFromInt(5)); err == nil {
		t.Fatal("expected not-found error")
	}
}
