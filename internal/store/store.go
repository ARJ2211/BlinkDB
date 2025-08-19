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

func (s *Store) Set(key string, value string) Entry {
	// If the key already exists in the store,
	// then return the existing one for now
	// TODO: IMPLEMENT UPDATE PATH LATER
	if existing, ok := s.data[key]; ok {
		return existing
	}

	now := time.Now()
	e := Entry{
		value:     value,
		createdAt: now,
		updatedAt: now,
		version:   1,
	}
	s.data[key] = e
	return e
}

func NewStore() *Store {
	// Create an empty store and return the
	// pointer.
	m := make(map[string]Entry)
	s := Store{
		data: m,
	}
	return &s
}
