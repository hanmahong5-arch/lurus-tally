package auth

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateToken_ShapeAndUniqueness(t *testing.T) {
	plain1, prefix1, hash1, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	plain2, prefix2, hash2, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	if !strings.HasPrefix(plain1, Scheme) {
		t.Errorf("plain1 = %q, expected prefix %q", plain1, Scheme)
	}
	if len(plain1) != len(Scheme)+PrefixLen+SecretLen {
		t.Errorf("plain1 len = %d, want %d", len(plain1), len(Scheme)+PrefixLen+SecretLen)
	}
	if len(prefix1) != PrefixLen {
		t.Errorf("prefix1 len = %d, want %d", len(prefix1), PrefixLen)
	}
	if len(hash1) != 64 {
		t.Errorf("hash1 len = %d, want 64 (sha256 hex)", len(hash1))
	}

	if plain1 == plain2 || prefix1 == prefix2 || hash1 == hash2 {
		t.Errorf("token generation not unique: plain1=%q plain2=%q", plain1, plain2)
	}
}

func TestParseBearer_HappyPath(t *testing.T) {
	plain, prefix, _, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	gotPrefix, gotSecret, ok := ParseBearer(plain)
	if !ok {
		t.Fatalf("ParseBearer returned ok=false for %q", plain)
	}
	if gotPrefix != prefix {
		t.Errorf("prefix = %q, want %q", gotPrefix, prefix)
	}
	if len(gotSecret) != SecretLen {
		t.Errorf("secret len = %d, want %d", len(gotSecret), SecretLen)
	}
}

func TestParseBearer_Rejects(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"wrong-scheme", "Bearer foo"},
		{"jwt-shaped", "eyJ.eyJ.signature"},
		{"too-short", Scheme + "abc"},
		{"too-long", Scheme + strings.Repeat("x", PrefixLen+SecretLen+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, ok := ParseBearer(tc.in); ok {
				t.Errorf("ParseBearer(%q) = ok, want !ok", tc.in)
			}
		})
	}
}

func TestVerify_MatchAndMismatch(t *testing.T) {
	plain, prefix, hash, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	_, secret, _ := ParseBearer(plain)

	if !Verify(prefix, secret, hash) {
		t.Errorf("Verify of fresh token returned false")
	}
	if Verify(prefix, secret[:len(secret)-1]+"X", hash) {
		t.Errorf("Verify accepted tampered secret")
	}
	if Verify(prefix, secret, strings.Repeat("0", 64)) {
		t.Errorf("Verify accepted wrong hash")
	}
}

func TestPAT_IsActive(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	cases := []struct {
		name string
		pat  PAT
		want bool
	}{
		{"fresh-no-expiry", PAT{}, true},
		{"future-expiry", PAT{ExpiresAt: &future}, true},
		{"past-expiry", PAT{ExpiresAt: &past}, false},
		{"revoked", PAT{RevokedAt: &past}, false},
		{"revoked-trumps-future-expiry", PAT{ExpiresAt: &future, RevokedAt: &past}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.pat.IsActive(now); got != tc.want {
				t.Errorf("IsActive = %v, want %v", got, tc.want)
			}
		})
	}
}
