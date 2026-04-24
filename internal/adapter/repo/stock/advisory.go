package stock

import (
	"encoding/binary"
	"hash/fnv"

	"github.com/google/uuid"
)

// advisoryKey computes a stable int64 key for pg_advisory_xact_lock from the three UUIDs.
// Uses FNV-64a over the concatenated 48-byte raw UUID bytes.
func advisoryKey(tenantID, productID, warehouseID uuid.UUID) int64 {
	h := fnv.New64a()
	// uuid.UUID is [16]byte — write raw bytes directly for maximum entropy.
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, 0) // reset
	h.Write(tenantID[:])
	h.Write(productID[:])
	h.Write(warehouseID[:])
	return int64(h.Sum64())
}
