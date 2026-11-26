package intake

import (
	"crypto/sha256"
	"encoding/hex"
)

// Hash is the lowercase SHA-256 of b, used as a fingerprint.
func Hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
