package auth

import (
	"strings"
	"testing"
	"time"
)

// --- hashSecret: hand-computed pin + determinism/order guards ---
//
// Hand-computed independently via `sha256sum` on the shell (not by reading
// back hashSecret's own output), for prefix="prefix12" secret="secretsecretsecretsecretsecret12":
//   sha256("prefix12secretsecretsecretsecretsecret12") = 9550c33cc74a049399da9c5a2eda758f63c0a0e4aa1dce5a2633860c926add9b

func TestHashSecret_HandComputedPin(t *testing.T) {
	const prefix = "prefix12"
	const secret = "secretsecretsecretsecretsecret12"
	const wantHex = "9550c33cc74a049399da9c5a2eda758f63c0a0e4aa1dce5a2633860c926add9b"

	got := hashSecret(prefix, secret)
	if got != wantHex {
		t.Fatalf("hashSecret(%q,%q) = %q, want hand-computed %q", prefix, secret, got, wantHex)
	}
	if len(got) != 64 {
		t.Fatalf("hashSecret len = %d, want 64 hex chars (sha256)", len(got))
	}
}

func TestHashSecret_DeterminismAndOrderSensitivity(t *testing.T) {
	const prefix = "prefix12"
	const secret = "secretsecretsecretsecretsecret12"

	h1 := hashSecret(prefix, secret)
	h2 := hashSecret(prefix, secret)
	if h1 != h2 {
		t.Errorf("hashSecret not deterministic: %q vs %q", h1, h2)
	}

	// Swapping prefix/secret must NOT produce the same digest — this guards
	// the pat.go doc's `prefix||secret` concatenation invariant (not, say,
	// secret||prefix or an order-independent combination).
	swapped := hashSecret(secret, prefix)
	if h1 == swapped {
		t.Errorf("hashSecret(prefix,secret) == hashSecret(secret,prefix) = %q; concatenation order not honored", h1)
	}
}

// --- PAT.IsActive: expiry exactly == now boundary ---

func TestPAT_IsActive_ExpiryExactlyNowIsInactive(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	exact := now // same instant, not after `now`
	p := PAT{ExpiresAt: &exact}
	if p.IsActive(now) {
		t.Errorf("IsActive(now) with ExpiresAt==now = true, want false (expiry==now is INACTIVE per pat.go:65)")
	}
}

func TestPAT_IsActive_OneNanosecondPastVsFuture(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)

	justPast := now.Add(-time.Nanosecond)
	if (&PAT{ExpiresAt: &justPast}).IsActive(now) {
		t.Errorf("IsActive with ExpiresAt 1ns before now = true, want false")
	}

	justFuture := now.Add(time.Nanosecond)
	if !(&PAT{ExpiresAt: &justFuture}).IsActive(now) {
		t.Errorf("IsActive with ExpiresAt 1ns after now = false, want true")
	}
}

// --- ParseBearer: exact off-by-one boundary around PrefixLen+SecretLen ---

func TestParseBearer_BoundaryLengths(t *testing.T) {
	const want = PrefixLen + SecretLen

	oneShort := Scheme + strings.Repeat("a", want-1)
	if _, _, ok := ParseBearer(oneShort); ok {
		t.Errorf("ParseBearer accepted body of length %d (want-1), expected ok=false", want-1)
	}

	oneLong := Scheme + strings.Repeat("a", want+1)
	if _, _, ok := ParseBearer(oneLong); ok {
		t.Errorf("ParseBearer accepted body of length %d (want+1), expected ok=false", want+1)
	}

	exact := Scheme + strings.Repeat("a", want)
	prefix, secret, ok := ParseBearer(exact)
	if !ok {
		t.Fatalf("ParseBearer rejected exact-length body of %d chars", want)
	}
	if len(prefix) != PrefixLen || len(secret) != SecretLen {
		t.Errorf("ParseBearer split lengths = (%d,%d), want (%d,%d)", len(prefix), len(secret), PrefixLen, SecretLen)
	}
	if prefix != strings.Repeat("a", PrefixLen) || secret != strings.Repeat("a", SecretLen) {
		t.Errorf("ParseBearer split content mismatch: prefix=%q secret=%q", prefix, secret)
	}
}

// --- Verify: single hex-char tamper on the expected hash must flip the result ---

func TestVerify_SingleCharTamperedHash(t *testing.T) {
	const prefix = "prefix12"
	const secret = "secretsecretsecretsecretsecret12"
	good := hashSecret(prefix, secret)

	if !Verify(prefix, secret, good) {
		t.Fatalf("Verify(correct hash) = false, want true")
	}

	// Flip exactly one hex character (0 -> 1, wrapping to 2 if it was 1) so the
	// tampered hash always differs from `good` in exactly one position.
	tamperedChars := []byte(good)
	pos := 0
	if tamperedChars[pos] == '0' {
		tamperedChars[pos] = '1'
	} else {
		tamperedChars[pos] = '0'
	}
	tampered := string(tamperedChars)
	if tampered == good {
		t.Fatalf("test setup bug: tampered hash equals good hash")
	}
	if Verify(prefix, secret, tampered) {
		t.Errorf("Verify accepted a hash differing by a single hex char: good=%q tampered=%q", good, tampered)
	}
}

func TestVerify_ComparesHashNotPlaintext(t *testing.T) {
	// Verify must compare the HASH, not the raw (prefix,secret) plaintext:
	// passing the plaintext secret in place of expectedHexHash must not match.
	const prefix = "prefix12"
	const secret = "secretsecretsecretsecretsecret12"
	if Verify(prefix, secret, secret) {
		t.Errorf("Verify(prefix, secret, secret) = true; Verify must compare hashSecret(prefix,secret), not raw plaintext")
	}
}

// --- GenerateToken / randURLSafe error propagation ---
//
// NOTE: as of this Go toolchain, crypto/rand.Read hardens against a broken
// entropy source by calling runtime fatal() (an unrecoverable process crash,
// see https://go.dev/issue/66821) rather than returning an error when the
// underlying io.Reader fails — even after swapping the package-level
// crypto/rand.Reader var to a always-failing io.Reader. That makes the
// `if err != nil` branches in randURLSafe (pat.go:118) and both call sites in
// GenerateToken (pat.go:78-79, 82-83) structurally unreachable from a normal
// unit test in-process (the process would crash, not fail the test). This
// matches the brief's own note that this path is untestable without faking
// crypto/rand at a lower level than a Go test can safely do; left uncovered
// by design rather than mocked to fake a passing number.

func TestRandURLSafe_SuccessLengths(t *testing.T) {
	prefix, err := randURLSafe(rawPrefixBytes)
	if err != nil {
		t.Fatalf("randURLSafe(rawPrefixBytes): %v", err)
	}
	if len(prefix) != PrefixLen {
		t.Errorf("randURLSafe(rawPrefixBytes) len = %d, want %d", len(prefix), PrefixLen)
	}

	secret, err := randURLSafe(rawSecretBytes)
	if err != nil {
		t.Fatalf("randURLSafe(rawSecretBytes): %v", err)
	}
	if len(secret) != SecretLen {
		t.Errorf("randURLSafe(rawSecretBytes) len = %d, want %d", len(secret), SecretLen)
	}
}

// --- Round-trip: Generate -> Parse -> Verify, cross-checked against a
// manually re-derived hash (not the value GenerateToken itself returned). ---

func TestGenerateParseVerify_RoundTripCrossChecked(t *testing.T) {
	plaintext, prefix, hash, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	gotPrefix, gotSecret, ok := ParseBearer(plaintext)
	if !ok {
		t.Fatalf("ParseBearer(%q) ok=false", plaintext)
	}
	if gotPrefix != prefix {
		t.Errorf("parsed prefix = %q, want %q", gotPrefix, prefix)
	}

	// Independently re-derive the hash from the parsed parts using the same
	// (unexported) primitive rather than trusting GenerateToken's own hash
	// output — this cross-checks the prefix||secret concatenation invariant.
	rederived := hashSecret(gotPrefix, gotSecret)
	if rederived != hash {
		t.Errorf("re-derived hash %q != GenerateToken hash %q", rederived, hash)
	}

	if !Verify(gotPrefix, gotSecret, hash) {
		t.Errorf("Verify(parsed prefix/secret, hash) = false, want true")
	}
	if Verify(gotPrefix, gotSecret+"x", hash) {
		t.Errorf("Verify accepted secret with appended char")
	}
}
