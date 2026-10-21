package bill

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type fakeReceivablesRepo struct {
	rows []PartnerReceivable
	err  error
	got  ReceivablesQuery
}

func (f *fakeReceivablesRepo) PartnerReceivables(_ context.Context, q ReceivablesQuery) ([]PartnerReceivable, error) {
	f.got = q
	return f.rows, f.err
}

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestReceivables_Validation(t *testing.T) {
	uc := NewReceivablesUseCase(&fakeReceivablesRepo{})
	ctx := context.Background()
	if _, err := uc.Execute(ctx, uuid.Nil, ReceivableAR, nil); err == nil {
		t.Fatal("nil tenant must fail")
	}
	if _, err := uc.Execute(ctx, uuid.New(), "xx", nil); !errors.Is(err, ErrInvalidReceivableDirection) {
		t.Fatalf("want ErrInvalidReceivableDirection, got %v", err)
	}
}

func TestReceivables_DefaultsAsOfToTodayInShanghai(t *testing.T) {
	repo := &fakeReceivablesRepo{}
	uc := NewReceivablesUseCase(repo)
	uc.now = func() time.Time { return time.Date(2026, 10, 9, 17, 0, 0, 0, time.UTC) } // 01:00 CST on the 10th
	if _, err := uc.Execute(context.Background(), uuid.New(), ReceivableAP, nil); err != nil {
		t.Fatal(err)
	}
	if repo.got.AsOf.Format("2006-01-02") != "2026-10-10" || repo.got.Direction != ReceivableAP {
		t.Fatalf("query = %+v", repo.got)
	}
}

func TestReceivables_TotalsTieOut(t *testing.T) {
	pid := uuid.New()
	repo := &fakeReceivablesRepo{rows: []PartnerReceivable{
		{PartnerID: &pid, PartnerName: "甲", Unpaid: d("100.50"), B0to30: d("50"), B31to60: d("30.50"), B61to90: d("20"), B90Plus: d("0")},
		{PartnerName: "散客", Unpaid: d("10"), B0to30: d("0"), B31to60: d("0"), B61to90: d("0"), B90Plus: d("10")},
	}}
	out, err := NewReceivablesUseCase(repo).Execute(context.Background(), uuid.New(), ReceivableAR, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Total.Unpaid.Equal(d("110.50")) || !out.Total.B0to30.Equal(d("50")) || !out.Total.B90Plus.Equal(d("10")) {
		t.Fatalf("total = %+v", out.Total)
	}
	sum := out.Total.B0to30.Add(out.Total.B31to60).Add(out.Total.B61to90).Add(out.Total.B90Plus)
	if !sum.Equal(out.Total.Unpaid) {
		t.Fatalf("buckets %s != unpaid %s", sum, out.Total.Unpaid)
	}
}

func TestReceivables_RepoError(t *testing.T) {
	uc := NewReceivablesUseCase(&fakeReceivablesRepo{err: errors.New("boom")})
	if _, err := uc.Execute(context.Background(), uuid.New(), ReceivableAR, nil); err == nil {
		t.Fatal("repo error must surface")
	}
}
