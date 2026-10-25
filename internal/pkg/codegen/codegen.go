// Package codegen assigns human-readable business codes (product / supplier /
// customer / warehouse / unit / project) when the user leaves the code blank.
//
// Format: <prefix><yymmdd><4 symbols>, e.g. P261010A7K2. The symbols come from
// a 32-character upper-case alphabet without the look-alikes 0/O/1/I, so a
// code read aloud or copied by hand off a printed label is unambiguous.
//
// The date is the Asia/Shanghai calendar day, matching bill_no generation
// (see adapter/repo/bill.BillNoDay). A fixed +8 zone avoids LoadLocation so
// scratch images without tzdata stay correct. 32^4 ≈ 1M suffixes per prefix
// per day keeps collisions rare; Source.Create absorbs the rare one by
// drawing a fresh code on a unique violation.
package codegen

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"time"
)

// businessZone cuts code dates at Asia/Shanghai wall-clock days (same as bill numbers).
var businessZone = time.FixedZone("Asia/Shanghai", 8*3600)

// Alphabet is the suffix symbol set: 2-9 and A-Z minus O and I. Its length is
// exactly 32, so mapping a random byte with b&31 is unbiased.
const Alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// suffixLen is the number of random symbols after the date.
const suffixLen = 4

// MaxRetries is how many fresh codes Source.Create draws after a conflict on a
// generated code (so at most MaxRetries+1 create attempts in total).
const MaxRetries = 3

// Entity code prefixes.
const (
	PrefixProduct   = "P"
	PrefixSupplier  = "S"
	PrefixCustomer  = "C"
	PrefixWarehouse = "W"
	PrefixUnit      = "U"
	PrefixProject   = "PJ"
)

// Generate builds prefix + t.Format("060102") + 4 symbols drawn from rnd. It is
// a pure function of its inputs; the caller owns the clock and the randomness.
func Generate(prefix string, t time.Time, rnd io.Reader) (string, error) {
	var b [suffixLen]byte
	if _, err := io.ReadFull(rnd, b[:]); err != nil {
		return "", fmt.Errorf("codegen: read random: %w", err)
	}
	out := make([]byte, 0, len(prefix)+6+suffixLen)
	out = append(out, prefix...)
	out = t.AppendFormat(out, "060102")
	for _, v := range b {
		out = append(out, Alphabet[v&31])
	}
	return string(out), nil
}

// Source bundles the clock and randomness used for generated codes. Production
// code uses Default(); tests inject a fixed clock and a deterministic reader.
type Source struct {
	Now  func() time.Time
	Rand io.Reader
}

// Default returns the production source: Asia/Shanghai wall clock + crypto/rand.
func Default() Source {
	return Source{Now: func() time.Time { return time.Now().In(businessZone) }, Rand: rand.Reader}
}

// Next returns a freshly generated code for prefix.
func (s Source) Next(prefix string) (string, error) {
	return Generate(prefix, s.Now(), s.Rand)
}

// Create persists an entity whose code may be server-assigned.
//
// When explicit is non-empty it is used as-is for a single create call and any
// error (a conflict included) is returned untouched — a user-chosen code that
// collides must surface as the usual 409, never be silently replaced.
//
// When explicit is empty a code is generated; if create fails with an error
// isConflict recognises as a code collision, a new code is drawn and create is
// called again, up to MaxRetries times. The last error is returned when every
// attempt collides. Any other error is returned immediately.
func (s Source) Create(prefix, explicit string, isConflict func(error) bool, create func(code string) error) error {
	if explicit != "" {
		return create(explicit)
	}
	var err error
	for attempt := 0; attempt <= MaxRetries; attempt++ {
		code, genErr := s.Next(prefix)
		if genErr != nil {
			return genErr
		}
		err = create(code)
		if err == nil || !isConflict(err) {
			return err
		}
	}
	return err
}

// IsUniqueViolation reports whether err's chain carries a PostgreSQL
// unique_violation (SQLSTATE 23505). It matches on the SQLState() method that
// pgconn.PgError exposes, so application packages can classify a conflict
// without importing the driver.
func IsUniqueViolation(err error) bool {
	type sqlStater interface {
		SQLState() string
	}
	var se sqlStater
	return errors.As(err, &se) && se.SQLState() == "23505"
}
