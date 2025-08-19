// Store (1A spec)
// Responsibilities:
// - Hold an in‑memory map from key(string) -> Entry
// - Provide basic operations: Set, Get, Delete, Size, (optional) Keys
// Concurrency: none yet (single-threaded). Mutex comes in 1D.
//
// Methods to implement in 1A (signatures you will write later):
// - Set(key string, value string) -> Entry
// Behavior: if key is new, version=1 and createdAt=updatedAt=now.
// if key exists, version++, updatedAt=now, createdAt unchanged.
// Returns a copy/snapshot of the stored Entry after the write.
// - Get(key string) -> (Entry, boolFound)
// Behavior: does not modify state (no TTL yet). boolFound=false if missing.
// - Delete(key string) -> (boolRemoved)
// Behavior: true if something was deleted; false if key didn’t exist.
// - Size() -> int
// Behavior: number of live keys currently in the map.
// - Keys() -> []string (optional in 1A; ordering unspecified)

package store

import "time"

type Store struct {
	data map[string]Entry
}

// CREATE A NEW STORE
func NewStore() *Store {
	data := make(map[string]Entry)
	s := Store{
		data: data,
	}
	return &s
}

// GET THE ENTRY FROM THE STORE BASED ON THE KEY
func (s *Store) Get(key string) (Entry, bool) {
	if entry, ok := s.data[key]; ok {
		return entry, true
	}
	return Entry{}, false
}

// SET THE KEY IN THE STORE, IF KEY IN STORE UPDATE
func (s *Store) Set(key string, value string) Entry {
	now := time.Now()

	if existing, ok := s.data[key]; ok {
		// Existing key: bump version, update time, keep createdAt
		newEntry := Entry{
			Value:     value,
			CreatedAt: existing.CreatedAt,
			UpdatedAt: now,
			Version:   existing.Version + 1,
		}
		s.data[key] = newEntry
		return newEntry
	}

	// New key: version 1, createdAt = updatedAt = now
	newEntry := Entry{
		Value:     value,
		CreatedAt: now,
		UpdatedAt: now,
		Version:   1,
	}
	s.data[key] = newEntry
	return newEntry
}
