// Entry represents a single version of a key.
// Invariants:
// - Version strictly increases by 1 per write/tombstone.
// - CreatedAt never changes for a live key lineage.
// - UpdatedAt is the write/delete time (monotonic per key).
// - ExpiresAt is zero when no TTL; tombstones always have zero ExpiresAt.
// - Deleted marks a tombstone (logical delete). Live entries have Deleted=false.

package store

import "time"

type Entry struct {
	Value     string
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt time.Time // zero => no TTL
	Version   int64

	// Deleted marks a tombstone in history. Tombstones never live in s.data,
	// only in s.history. For tombstones: Deleted=true, ExpiresAt=zero.
	Deleted bool
}
