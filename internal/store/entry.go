// Entry (1A spec)
// - value: string (the stored payload)
// - createdAt: timestamp set on the first Set for a key; never changes
// - updatedAt: timestamp set on every successful Set for that key
// - version: int64; first write = 1; increment by 1 on every Set (even if same value)
// - expiresAt: (reserved for later TTL milestones; unused in 1A)

package store

import "time"

type Entry struct {
	Value     string
	CreatedAt time.Time
	UpdatedAt time.Time
	Version   uint64
	ExpiresAt time.Time
}
