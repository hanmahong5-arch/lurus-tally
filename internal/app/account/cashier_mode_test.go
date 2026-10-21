package account_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
)

type fakeCashierRepo struct {
	rec     appacct.CashierRecord
	err     error
	profile string
	// raceHash simulates another device storing a PIN between our read and write.
	raceHash string
}

func (f *fakeCashierRepo) ProfileType(context.Context, uuid.UUID) (string, error) {
	return f.profile, nil
}
func (f *fakeCashierRepo) PutCashierFirst(_ context.Context, _ uuid.UUID, r appacct.CashierRecord) error {
	if f.err != nil {
		return f.err
	}
	if f.rec.PinHash != "" || f.raceHash != "" {
		return appacct.ErrPinAlreadySet
	}
	f.rec = r
	return nil
}

func (f *fakeCashierRepo) GetCashier(context.Context, uuid.UUID) (appacct.CashierRecord, error) {
	return f.rec, f.err
}
func (f *fakeCashierRepo) PutCashier(_ context.Context, _ uuid.UUID, r appacct.CashierRecord) error {
	if f.err != nil {
		return f.err
	}
	f.rec = r
	return nil
}

func TestCashierMode_FirstSetStoresHashedPin(t *testing.T) {
	r := &fakeCashierRepo{profile: "retail"}
	uc := appacct.NewSetCashierMode(r)
	st, err := uc.Execute(context.Background(), uuid.New(), "u1", "1234")
	if err != nil {
		t.Fatal(err)
	}
	if !st.HasPin {
		t.Fatalf("state: %+v", st)
	}
	if r.rec.PinHash == "" || r.rec.PinHash == "1234" {
		t.Fatal("pin must be stored hashed")
	}
	if r.rec.Enabled {
		t.Fatal("the on/off flag lives on the device, never on the tenant")
	}
}

func TestCashierMode_SetAgainNeedsSamePin(t *testing.T) {
	r := &fakeCashierRepo{profile: "retail"}
	uc := appacct.NewSetCashierMode(r)
	tid := uuid.New()
	_, _ = uc.Execute(context.Background(), tid, "u1", "1234")
	if _, err := uc.Execute(context.Background(), tid, "u1", "9999"); !errors.Is(err, appacct.ErrPinWrong) {
		t.Fatalf("want ErrPinWrong, got %v", err)
	}
	if _, err := uc.Execute(context.Background(), tid, "u1", "1234"); err != nil {
		t.Fatalf("same pin: %v", err)
	}
}

func TestCashierMode_PinFormat(t *testing.T) {
	uc := appacct.NewSetCashierMode(&fakeCashierRepo{profile: "retail"})
	for _, bad := range []string{"", "12", "abcd", "1234567", "12 34"} {
		if _, err := uc.Execute(context.Background(), uuid.New(), "u1", bad); !errors.Is(err, appacct.ErrPinInvalid) {
			t.Fatalf("pin %q: want ErrPinInvalid, got %v", bad, err)
		}
	}
}

func TestCashierMode_VerifyRightAndWrongPin(t *testing.T) {
	r := &fakeCashierRepo{profile: "retail"}
	uc := appacct.NewSetCashierMode(r)
	tid := uuid.New()
	_, _ = uc.Execute(context.Background(), tid, "u1", "1234")
	if err := uc.Verify(context.Background(), tid, "u1", "1234"); err != nil {
		t.Fatalf("right pin: %v", err)
	}
	if err := uc.Verify(context.Background(), tid, "u1", "0000"); !errors.Is(err, appacct.ErrPinWrong) {
		t.Fatalf("want ErrPinWrong, got %v", err)
	}
	if err := uc.Verify(context.Background(), tid, "u1", "ab"); !errors.Is(err, appacct.ErrPinInvalid) {
		t.Fatalf("want ErrPinInvalid, got %v", err)
	}
}

func TestCashierMode_VerifyWithoutStoredPin(t *testing.T) {
	uc := appacct.NewSetCashierMode(&fakeCashierRepo{profile: "retail"})
	if err := uc.Verify(context.Background(), uuid.New(), "u1", "1234"); !errors.Is(err, appacct.ErrPinNotSet) {
		t.Fatalf("want ErrPinNotSet, got %v", err)
	}
}

func TestCashierMode_VerifyLocksAfterRepeatedFailures(t *testing.T) {
	r := &fakeCashierRepo{profile: "retail"}
	uc := appacct.NewSetCashierMode(r)
	tid := uuid.New()
	_, _ = uc.Execute(context.Background(), tid, "u1", "1234")
	for i := 0; i < 5; i++ {
		_ = uc.Verify(context.Background(), tid, "u1", "0000")
	}
	if err := uc.Verify(context.Background(), tid, "u1", "1234"); !errors.Is(err, appacct.ErrPinLocked) {
		t.Fatalf("want ErrPinLocked, got %v", err)
	}
}

func TestCashierMode_OneMembersTyposDoNotLockAnother(t *testing.T) {
	r := &fakeCashierRepo{profile: "retail"}
	uc := appacct.NewSetCashierMode(r)
	tid := uuid.New()
	_, _ = uc.Execute(context.Background(), tid, "owner", "1234")
	for i := 0; i < 6; i++ {
		_ = uc.Verify(context.Background(), tid, "clerk", "0000")
	}
	if err := uc.Verify(context.Background(), tid, "clerk", "1234"); !errors.Is(err, appacct.ErrPinLocked) {
		t.Fatalf("clerk want ErrPinLocked, got %v", err)
	}
	if err := uc.Verify(context.Background(), tid, "owner", "1234"); err != nil {
		t.Fatalf("owner must not be locked, got %v", err)
	}
}

func TestGetCashierMode_OnlyReportsHasPin(t *testing.T) {
	// A tenant switched on by the old shared flag reads as "no PIN" and carries no flag.
	r := &fakeCashierRepo{profile: "retail", rec: appacct.CashierRecord{Enabled: true}}
	st, err := appacct.NewGetCashierMode(r).Execute(context.Background(), uuid.New())
	if err != nil || st.HasPin {
		t.Fatalf("%+v %v", st, err)
	}
	r.rec.PinHash = "x"
	st, _ = appacct.NewGetCashierMode(r).Execute(context.Background(), uuid.New())
	if !st.HasPin {
		t.Fatalf("%+v", st)
	}
}

func TestCashierMode_NonRetailCannotSetPin(t *testing.T) {
	for _, p := range []string{"horticulture", "cross_border", "hybrid", ""} {
		r := &fakeCashierRepo{profile: p}
		uc := appacct.NewSetCashierMode(r)
		if _, err := uc.Execute(context.Background(), uuid.New(), "u1", "1234"); !errors.Is(err, appacct.ErrCashierNotRetail) {
			t.Fatalf("profile %q: want ErrCashierNotRetail, got %v", p, err)
		}
		if r.rec.PinHash != "" {
			t.Fatalf("profile %q: nothing may be stored", p)
		}
	}
}

func TestCashierMode_FirstPinRaceLoserGetsAlreadySet(t *testing.T) {
	r := &fakeCashierRepo{profile: "retail", raceHash: "other-device"}
	uc := appacct.NewSetCashierMode(r)
	if _, err := uc.Execute(context.Background(), uuid.New(), "u1", "1111"); !errors.Is(err, appacct.ErrPinAlreadySet) {
		t.Fatalf("want ErrPinAlreadySet, got %v", err)
	}
}
