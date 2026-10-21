package customer_test

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	repocust "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/customer"
	appcust "github.com/hanmahong5-arch/lurus-tally/internal/app/customer"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/customer"
)

var customerCols = []string{"id", "tenant_id", "code", "name", "contact_name", "mobile", "phone", "address", "remark", "created_at", "updated_at", "deleted_at"}

func TestRepo_Create_WritesCustomerPartnerTypeAndNullsEmptyOptionals(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	c := &domain.Customer{ID: uuid.New(), TenantID: uuid.New(), Name: "散客", Mobile: "13800000000", CreatedAt: now, UpdatedAt: now}

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO tally.partner")).
		WithArgs(c.ID, c.TenantID, "customer", "散客", nil, nil, "13800000000", nil, nil, nil, now, now).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repocust.New(db).Create(context.Background(), c); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepo_Create_UniqueViolationStaysInChain(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO tally.partner")).
		WillReturnError(&pgconn.PgError{Code: "23505"})

	err = repocust.New(db).Create(context.Background(), &domain.Customer{ID: uuid.New(), TenantID: uuid.New(), Name: "N", Code: "C1"})
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23505" {
		t.Fatalf("err = %v, want a wrapped *pgconn.PgError 23505", err)
	}
}

func TestRepo_List_FiltersCustomerTypesSearchesFourColumnsOrdersByUpdatedAt(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	tenant := uuid.New()
	where := regexp.QuoteMeta("FROM tally.partner WHERE tenant_id = $1 AND deleted_at IS NULL AND partner_type IN ('customer','both','member') AND (name ILIKE $2 OR code ILIKE $2 OR mobile ILIKE $2 OR phone ILIKE $2)")

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) "+where).
		WithArgs(tenant, "%138%").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	now := time.Now().UTC()
	mock.ExpectQuery("SELECT id, .* "+where+regexp.QuoteMeta(" ORDER BY updated_at DESC, id DESC LIMIT $3 OFFSET $4")).
		WithArgs(tenant, "%138%", 2, 1).
		WillReturnRows(sqlmock.NewRows(customerCols).
			AddRow(uuid.New(), tenant, "C1", "A", nil, "13800000000", nil, nil, nil, now, now, nil).
			AddRow(uuid.New(), tenant, nil, "B", "李四", nil, "0571-138", "addr", "r", now, now, nil))

	items, total, err := repocust.New(db).List(context.Background(), domain.ListFilter{TenantID: tenant, Query: "138", Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 3 || len(items) != 2 {
		t.Fatalf("total=%d items=%d, want 3/2", total, len(items))
	}
	if items[0].Code != "C1" || items[0].ContactName != "" || items[1].Code != "" || items[1].ContactName != "李四" {
		t.Errorf("NULL columns not scanned to empty strings: %+v / %+v", items[0], items[1])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepo_GetByID_NoRowsIsNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	id, tenant := uuid.New(), uuid.New()
	mock.ExpectQuery(regexp.QuoteMeta("partner_type IN ('customer','both','member')")).
		WithArgs(id, tenant).
		WillReturnRows(sqlmock.NewRows(customerCols))

	_, err = repocust.New(db).GetByID(context.Background(), tenant, id)
	if !errors.Is(err, appcust.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRepo_FindActiveByName_NormalisesAndScopesToCustomerTypes(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	tenant, id := uuid.New(), uuid.New()
	now := time.Now().UTC()
	mock.ExpectQuery(regexp.QuoteMeta("tenant_id = $1 AND deleted_at IS NULL AND partner_type IN ('customer','both','member')")+
		".*"+regexp.QuoteMeta("lower(btrim(regexp_replace(name, '[[:space:]]+', ' ', 'g'))) = lower($2)")).
		WithArgs(tenant, "王 五").
		WillReturnRows(sqlmock.NewRows(customerCols).AddRow(id, tenant, "C1", "王 五", nil, nil, nil, nil, nil, now, now, nil))

	got, err := repocust.New(db).FindActiveByName(context.Background(), tenant, "王 五")
	if err != nil || got.ID != id {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestRepo_FindActiveByMobile_NoRowsIsNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	tenant := uuid.New()
	mock.ExpectQuery(regexp.QuoteMeta("AND btrim(mobile) = $2")).
		WithArgs(tenant, "138").
		WillReturnRows(sqlmock.NewRows(customerCols))

	_, err = repocust.New(db).FindActiveByMobile(context.Background(), tenant, "138")
	if !errors.Is(err, appcust.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
