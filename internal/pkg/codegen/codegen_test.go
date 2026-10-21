package codegen_test

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/codegen"
)

var fixedNow = time.Date(2026, 10, 10, 15, 4, 5, 0, time.UTC)

func fixedClock() time.Time { return fixedNow }

func TestGenerate_FormatPrefixDateSuffix(t *testing.T) {
	// Byte i maps to Alphabet[i & 31]; 0..3 → "2345".
	got, err := codegen.Generate("P", fixedNow, bytes.NewReader([]byte{0, 1, 2, 3}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got != "P2610102345" {
		t.Errorf("Generate = %q, want P2610102345", got)
	}
}

func TestGenerate_MasksHighBitsIntoAlphabet(t *testing.T) {
	// 31 → last symbol; 32 and 255 wrap via &31 → first / last symbol.
	got, err := codegen.Generate("PJ", fixedNow, bytes.NewReader([]byte{31, 32, 255, 8}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "PJ261010" + string(codegen.Alphabet[31]) + string(codegen.Alphabet[0]) + string(codegen.Alphabet[31]) + string(codegen.Alphabet[8])
	if got != want {
		t.Errorf("Generate = %q, want %q", got, want)
	}
}

func TestGenerate_UsesTimeAsGiven(t *testing.T) {
	// The caller owns the clock: a time just past midnight in UTC+8 is still
	// the previous day in UTC, and Generate formats exactly what it is handed.
	cst := time.FixedZone("CST", 8*3600)
	local := time.Date(2026, 10, 11, 0, 30, 0, 0, cst)
	got, _ := codegen.Generate("S", local, bytes.NewReader([]byte{0, 0, 0, 0}))
	if !strings.HasPrefix(got, "S261011") {
		t.Errorf("Generate = %q, want date 261011 from the given time", got)
	}
}

func TestGenerate_ShortRandomSourceErrors(t *testing.T) {
	if _, err := codegen.Generate("P", fixedNow, bytes.NewReader([]byte{1, 2})); err == nil {
		t.Fatal("expected error when the random source runs dry")
	}
}

func TestAlphabet_ExcludesAmbiguousCharacters(t *testing.T) {
	if len(codegen.Alphabet) != 32 {
		t.Fatalf("len(Alphabet) = %d, want 32 (so byte&31 is unbiased)", len(codegen.Alphabet))
	}
	for _, bad := range "0O1I" {
		if strings.ContainsRune(codegen.Alphabet, bad) {
			t.Errorf("Alphabet contains ambiguous %q", bad)
		}
	}
	seen := map[rune]bool{}
	for _, r := range codegen.Alphabet {
		if seen[r] {
			t.Errorf("Alphabet has duplicate %q", r)
		}
		seen[r] = true
		if (r < '2' || r > '9') && (r < 'A' || r > 'Z') {
			t.Errorf("Alphabet has non upper-alnum %q", r)
		}
	}
}

func TestDefault_ProducesWellFormedCodes(t *testing.T) {
	re := regexp.MustCompile(`^C\d{6}[2-9A-HJ-NP-Z]{4}$`)
	src := codegen.Default()
	for i := 0; i < 200; i++ {
		got, err := src.Next("C")
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if !re.MatchString(got) {
			t.Fatalf("Next = %q, does not match %s", got, re)
		}
	}
}

func TestDefault_DateIsUTC(t *testing.T) {
	got, err := codegen.Default().Next("U")
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	// Tolerate a UTC midnight rollover between the two clock reads.
	before := time.Now().UTC().Add(-time.Minute).Format("060102")
	after := time.Now().UTC().Format("060102")
	if d := got[1:7]; d != before && d != after {
		t.Errorf("date part = %q, want UTC %q", d, after)
	}
}

// ---- Create ----

var errConflict = errors.New("sentinel: duplicate code")

func isConflict(err error) bool { return errors.Is(err, errConflict) }

func seqSource(b ...byte) codegen.Source {
	return codegen.Source{Now: fixedClock, Rand: bytes.NewReader(b)}
}

func TestCreate_ExplicitCodeUsedOnceConflictReturned(t *testing.T) {
	var codes []string
	err := seqSource().Create("P", "MY-CODE", isConflict, func(code string) error {
		codes = append(codes, code)
		return errConflict
	})
	if !errors.Is(err, errConflict) {
		t.Fatalf("err = %v, want the conflict passed through untouched", err)
	}
	if len(codes) != 1 || codes[0] != "MY-CODE" {
		t.Errorf("attempts = %v, want exactly [MY-CODE] (no retry for explicit codes)", codes)
	}
}

func TestCreate_GeneratedRetriesOnConflictThenSucceeds(t *testing.T) {
	src := seqSource(0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2)
	var codes []string
	err := src.Create("P", "", isConflict, func(code string) error {
		codes = append(codes, code)
		if len(codes) < 3 {
			return fmt.Errorf("wrapped: %w", errConflict)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := []string{"P2610102222", "P2610103333", "P2610104444"}
	if strings.Join(codes, ",") != strings.Join(want, ",") {
		t.Errorf("attempts = %v, want %v (a fresh code per retry)", codes, want)
	}
}

func TestCreate_GeneratedGivesUpAfterMaxRetries(t *testing.T) {
	src := seqSource(bytes.Repeat([]byte{5}, 4*(codegen.MaxRetries+2))...)
	attempts := 0
	err := src.Create("W", "", isConflict, func(string) error {
		attempts++
		return errConflict
	})
	if !errors.Is(err, errConflict) {
		t.Fatalf("err = %v, want the last conflict", err)
	}
	if attempts != codegen.MaxRetries+1 {
		t.Errorf("attempts = %d, want %d (1 + %d retries)", attempts, codegen.MaxRetries+1, codegen.MaxRetries)
	}
}

func TestCreate_GeneratedNonConflictErrorNotRetried(t *testing.T) {
	boom := errors.New("sentinel: connection reset")
	attempts := 0
	err := seqSource(0, 0, 0, 0, 0, 0, 0, 0).Create("S", "", isConflict, func(string) error {
		attempts++
		return boom
	})
	if !errors.Is(err, boom) || attempts != 1 {
		t.Errorf("err=%v attempts=%d, want boom after exactly 1 attempt", err, attempts)
	}
}

func TestCreate_RandomFailureSurfacesWithoutCallingCreate(t *testing.T) {
	called := false
	err := seqSource(1).Create("P", "", isConflict, func(string) error {
		called = true
		return nil
	})
	if err == nil || called {
		t.Errorf("err=%v called=%v, want an error and no create call", err, called)
	}
}

func TestIsUniqueViolation(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("x"), false},
		{"sqlstate 23505", sqlStateErr("23505"), true},
		{"wrapped 23505", fmt.Errorf("repo create: %w", sqlStateErr("23505")), true},
		{"other sqlstate", sqlStateErr("23503"), false},
		{"pgconn 23505", fmt.Errorf("x: %w", &pgconn.PgError{Code: "23505"}), true},
		{"pgconn 23503", &pgconn.PgError{Code: "23503"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := codegen.IsUniqueViolation(tc.err); got != tc.want {
				t.Errorf("IsUniqueViolation(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

type sqlStateErr string

func (e sqlStateErr) Error() string    { return "sqlstate " + string(e) }
func (e sqlStateErr) SQLState() string { return string(e) }
