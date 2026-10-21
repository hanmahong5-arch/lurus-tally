package team

import (
	"context"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	domainteam "github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

func TestMemberNames_FallbackChain(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	tenant := uuid.New()
	u := uuid.New().String()
	mock.ExpectQuery(`FROM tally.user_identity_mapping WHERE tenant_id = \$1`).WithArgs(tenant).
		WillReturnRows(sqlmock.NewRows([]string{"sub", "email", "name"}).
			AddRow("1001", "boss@x.com", "老王").
			AddRow("1002", "lily@x.com", "").
			AddRow(u, "", "  ").
			AddRow("1004", "bad", ""))
	got, err := New(db).MemberNames(context.Background(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	want := map[uuid.UUID]string{
		domainteam.ActorIDFromSubject("1001"): "老王",
		domainteam.ActorIDFromSubject("1002"): "l***@x.com",
		uuid.MustParse(u):                     "未命名成员",
		domainteam.ActorIDFromSubject("1004"): "未命名成员",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q want %q", k, got[k], v)
		}
	}
}
