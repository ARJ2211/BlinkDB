package store

import "time"

// Clock is an abstraction of time so we can substitute a fake clock in tests.
// In production we use RealClock (which wraps time.Now).
type Clock interface {
	Now() time.Time
}

// RealClock implements Clock by returning the system wall clock.
type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now()
}

// Store holds live key→entry mappings plus an append-only history log.
// Invariants:
//   - s.data stores only the *latest* live version for each key.
//   - s.history[key] is append-only, time-ordered by UpdatedAt, and includes
//     every successful Set/SetWithTTL/CAS (whether or not the key later expired).
//   - Concurrency: not safe for concurrent use. Mutex comes in milestone 1D.
type Store struct {
	data    map[string]Entry   // current live snapshot per key
	history map[string][]Entry // append-only log of versions per key
	Clock   Clock              // time source (real or fake)
}

// NewStore constructs a Store with a RealClock (wall time).
func NewStore() *Store {
	return &Store{
		data:    make(map[string]Entry),
		history: make(map[string][]Entry),
		Clock:   RealClock{},
	}
}

// NewStoreWithClock constructs a Store with a caller-supplied Clock.
// Used in tests to inject a fake clock.
func NewStoreWithClock(c Clock) *Store {
	return &Store{
		data:    make(map[string]Entry),
		history: make(map[string][]Entry),
		Clock:   c,
	}
}

// Get returns the latest live entry for a key, if present.
//
// Policy: lazy-delete on expiry, history-agnostic.
// - If key missing: returns (zero, false).
// - If entry has no expiry (ExpiresAt.IsZero): return it.
// - If expired (ExpiresAt <= now): delete from s.data and return (zero, false).
// - Otherwise: return it.
// Note: Get never reads or mutates s.history. Historical versions remain.
func (s *Store) Get(key string) (Entry, bool) {
	n := s.Clock.Now()
	if entry, ok := s.data[key]; ok {
		if entry.ExpiresAt.IsZero() {
			return entry, true
		}
		if entry.ExpiresAt.Before(n) || entry.ExpiresAt.Equal(n) {
			delete(s.data, key) // lazy delete
			return Entry{}, false
		}
		return entry, true
	}
	return Entry{}, false
}

// Set writes a new value for a key without a TTL.
// - New key: version=1, createdAt=updatedAt=now.
// - Existing key: version++, createdAt unchanged, updatedAt=now.
// Side effects:
// - Updates s.data[key].
// - Appends the new version to s.history[key].
func (s *Store) Set(key string, value string) Entry {
	n := s.Clock.Now()
	if existing, ok := s.data[key]; ok {
		newEntry := Entry{
			Value:     value,
			CreatedAt: existing.CreatedAt,
			UpdatedAt: n,
			Version:   existing.Version + 1,
		}
		s.data[key] = newEntry
		s.history[key] = append(s.history[key], newEntry)
		return newEntry
	}
	newEntry := Entry{
		Value:     value,
		CreatedAt: n,
		UpdatedAt: n,
		Version:   1,
	}
	s.data[key] = newEntry
	s.history[key] = append(s.history[key], newEntry)
	return newEntry
}

// SetWithTTL writes a value with a TTL (time-to-live).
// - ttl <= 0 → behaves like Set (no expiry).
// - ttl > 0 → entry expires at now+ttl.
// Versioning: same rules as Set.
// Side effects:
// - Updates s.data[key].
// - Appends the new version (with ExpiresAt set/cleared) to s.history[key].
func (s *Store) SetWithTTL(key string, value string, ttl time.Duration) Entry {
	n := s.Clock.Now()
	if ttl <= 0 {
		return s.Set(key, value)
	}
	if existing, ok := s.data[key]; ok {
		newEntry := Entry{
			Value:     value,
			CreatedAt: existing.CreatedAt,
			UpdatedAt: n,
			Version:   existing.Version + 1,
			ExpiresAt: n.Add(ttl),
		}
		s.data[key] = newEntry
		s.history[key] = append(s.history[key], newEntry)
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
	s.history[key] = append(s.history[key], newEntry)
	return newEntry
}

// Delete removes a key from s.data (live snapshot).
// Returns true if the key was present, false otherwise.
// Note: history is not pruned; old versions remain.
func (s *Store) Delete(key string) bool {
	if _, ok := s.data[key]; ok {
		delete(s.data, key)
		return true
	}
	return false
}

// Keys returns the set of live keys in s.data.
// Order is undefined.
func (s *Store) Keys() []string {
	keys := []string{}
	for k := range s.data {
		keys = append(keys, k)
	}
	return keys
}

// Size returns the number of live keys in s.data.
func (s *Store) Size() int {
	return len(s.data)
}

// CAS (Compare-And-Set) updates a value only if the current value matches
// the expected string. Behavior:
//   - If key missing: return false.
//   - If expired: delete from s.data and return false (lazy delete semantics).
//   - If value != expected: return false.
//   - If value == expected: bump version, set UpdatedAt=now, preserve CreatedAt,
//     preserve ExpiresAt, write newValue.
//
// Side effects:
// - Updates s.data[key].
// - Appends the new version to s.history[key].
func (s *Store) CAS(key string, expected string, newValue string) bool {
	n := s.Clock.Now()
	entry, ok := s.data[key]
	if !ok {
		return false
	}
	if !entry.ExpiresAt.IsZero() && (entry.ExpiresAt.Before(n) || entry.ExpiresAt.Equal(n)) {
		delete(s.data, key) // lazy delete
		return false
	}
	if entry.Value != expected {
		return false
	}
	newEntry := Entry{
		Value:     newValue,
		CreatedAt: entry.CreatedAt,
		UpdatedAt: n,
		Version:   entry.Version + 1,
		ExpiresAt: entry.ExpiresAt,
	}
	s.data[key] = newEntry
	s.history[key] = append(s.history[key], newEntry)
	return true
}

// SweepExpired scans s.data and deletes all entries whose ExpiresAt <= now.
// Returns the count of keys removed.
// Note: history is not pruned (versions remain).
func (s *Store) SweepExpired() int {
	n := s.Clock.Now()
	removed := 0
	for key, entry := range s.data {
		if !entry.ExpiresAt.IsZero() && (entry.ExpiresAt.Before(n) || entry.ExpiresAt.Equal(n)) {
			delete(s.data, key)
			removed++
		}
	}
	return removed
}
