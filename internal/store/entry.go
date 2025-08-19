// Entry (1A spec)
// - value: string (the stored payload)
// - createdAt: timestamp set on the first Set for a key; never changes
// - updatedAt: timestamp set on every successful Set for that key
// - version: int64; first write = 1; increment by 1 on every Set (even if same value)
// - expiresAt: (reserved for later TTL milestones; unused in 1A)

package store

import "time"

type Entry struct {
	value     string
	createdAt time.Time
	updatedAt time.Time
	version   uint64
}

// Getters (exported so other packages can read values)
func (e Entry) Value() string        { return e.value }
func (e Entry) CreatedAt() time.Time { return e.createdAt }
func (e Entry) UpdatedAt() time.Time { return e.updatedAt }
func (e Entry) Version() uint64      { return e.version }
