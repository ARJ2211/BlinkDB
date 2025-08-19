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

type Clock struct {
	Now func() time.Time
}

type Store struct {
	data  map[string]Entry
	Clock Clock
}

// CREATE A NEW STORE
func NewStore() *Store {
	data := make(map[string]Entry)
	s := Store{
		data:  data,
		Clock: Clock{Now: time.Now},
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
	n := s.Clock.Now()
	if existing, ok := s.data[key]; ok {
		// Existing key: bump version, update time, keep createdAt
		newEntry := Entry{
			Value:     value,
			CreatedAt: existing.CreatedAt,
			UpdatedAt: n,
			Version:   existing.Version + 1,
		}
		s.data[key] = newEntry
		return newEntry
	}

	// New key: version 1, createdAt = updatedAt = now
	newEntry := Entry{
		Value:     value,
		CreatedAt: n,
		UpdatedAt: n,
		Version:   1,
	}
	s.data[key] = newEntry
	return newEntry
}

// SET THE KEY IN STORE WITH TTL NOW
func (s *Store) SetWithTTL(
	key string,
	value string,
	ttl time.Duration,
) Entry {
	n := s.Clock.Now()
	if ttl <= 0 {
		ent := s.Set(key, value)
		return ent
	} else {
		if existing, ok := s.data[key]; ok {
			// Existing key: bump version, update time, keep createdAt
			newEntry := Entry{
				Value:     value,
				CreatedAt: existing.CreatedAt,
				UpdatedAt: n,
				Version:   existing.Version + 1,
				ExpiresAt: n.Add(ttl),
			}
			s.data[key] = newEntry
			return newEntry
		}
		newEntry := Entry{
			Value:     value,
			CreatedAt: n,
			UpdatedAt: n,
			Version:   1,
			ExpiresAt: n.Add(ttl),
		}
		s.data[key] = newEntry
		return newEntry
	}
}

// DELETE THE KEY FROM THE STORE
func (s *Store) Delete(key string) bool {
	if _, ok := s.data[key]; ok {
		delete(s.data, key)
		return true
	}
	return false
}

// LIST THE KEYS IN THE STORE
func (s *Store) Keys() []string {
	keys := []string{}
	for i := range s.data {
		keys = append(keys, i)
	}
	return keys
}

// SIZE OF THE DATASTORE (# OF KEYS)
func (s *Store) Size() int {
	count := len(s.data)
	return count
}

// COMPARE AND SET
func (s *Store) CAS(key string, expected string, newValue string) bool {
	if entry, ok := s.data[key]; ok {
		if entry.Value == expected {
			newEntry := Entry{
				Value:     newValue,
				CreatedAt: entry.CreatedAt,
				UpdatedAt: time.Now(),
				Version:   entry.Version + 1,
			}
			s.data[key] = newEntry
			return true
		}
		return false
	}
	return false
}
