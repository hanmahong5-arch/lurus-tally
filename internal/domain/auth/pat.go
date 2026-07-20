// Package auth holds the Personal Access Token domain entity and the pure
// crypto helpers used by both the auth middleware and the PAT CRUD use cases.
//
// Token format on the wire:
//
//	tally_pat_<prefix><secret>
//	            ^^^^^^^^                       8 URL-safe chars (PrefixLen)
//	                    ^^^^^^^^^^^^^^^^^^^^^^ 32 URL-safe chars (SecretLen)
//
// Persistence stores only `prefix` (plaintext) and `hash = sha256(prefix||secret)` (hex).
// The plaintext token is shown to the user exactly once at creation time.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// Scheme is the bearer prefix that distinguishes a PAT from an OIDC JWT.
	Scheme = "tally_pat_"
	// PrefixLen is the length (in URL-safe chars) of the plaintext prefix used
	// for row lookup. 8 chars over 64-symbol alphabet ≈ 48 bits of entropy —
	// collision-resistant for the table size we expect.
	PrefixLen = 8
	// SecretLen is the length of the secret portion. 32 chars over 64-symbol
	// alphabet ≈ 192 bits of entropy — well beyond brute-force reach.
	SecretLen = 32

	rawPrefixBytes = 6  // base64 URL-encodes 6 bytes to exactly 8 chars
	rawSecretBytes = 24 // base64 URL-encodes 24 bytes to exactly 32 chars
)

// ErrMalformedToken means the bearer string does not match the tally_pat_ shape.
var ErrMalformedToken = errors.New("auth: malformed PAT bearer")

// PAT is the persistence + domain entity. The plaintext token is never stored;
// only Prefix (plaintext, for lookup) and Hash (sha256 hex of prefix||secret).
type PAT struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	Name       string
	Prefix     string
	Hash       string
	CreatedAt  time.Time
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// IsActive reports whether the token is currently usable: not revoked and
// either no expiry or expiry in the future relative to `now`.
func (p *PAT) IsActive(now time.Time) bool {
	if p.RevokedAt != nil {
		return false
	}
	if p.ExpiresAt != nil && !p.ExpiresAt.After(now) {
		return false
	}
	return true
}

// GenerateToken produces a fresh PAT pair.
// Returns:
//   - plaintext : the full tally_pat_<prefix><secret> string to show the user once
//   - prefix    : the lookup key persisted in plaintext
//   - hash      : sha256(prefix||secret) hex digest persisted as the verifier
func GenerateToken() (plaintext, prefix, hash string, err error) {
	prefix, err = randURLSafe(rawPrefixBytes)
	if err != nil {
		return "", "", "", err
	}
	secret, err := randURLSafe(rawSecretBytes)
	if err != nil {
		return "", "", "", err
	}
	hash = hashSecret(prefix, secret)
	plaintext = Scheme + prefix + secret
	return plaintext, prefix, hash, nil
}

// ParseBearer accepts the value of the Authorization header's bearer token and
// returns (prefix, secret, true) when it matches the PAT shape. The caller is
// expected to have already stripped "Bearer " (the middleware does this).
func ParseBearer(token string) (prefix, secret string, ok bool) {
	if !strings.HasPrefix(token, Scheme) {
		return "", "", false
	}
	body := token[len(Scheme):]
	if len(body) != PrefixLen+SecretLen {
		return "", "", false
	}
	return body[:PrefixLen], body[PrefixLen:], true
}

// Verify constant-time-compares the candidate (prefix, secret) against the
// stored hex digest. Returns true iff the digest matches.
func Verify(prefix, secret, expectedHexHash string) bool {
	got := hashSecret(prefix, secret)
	return subtle.ConstantTimeCompare([]byte(got), []byte(expectedHexHash)) == 1
}

func hashSecret(prefix, secret string) string {
	sum := sha256.Sum256([]byte(prefix + secret))
	return hex.EncodeToString(sum[:])
}

func randURLSafe(rawBytes int) (string, error) {
	buf := make([]byte, rawBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
