package store

import (
	"sync"
	"time"
)

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

// Store holds live key->entry mappings plus an append-only history log.
// Invariants:
//   - s.data stores only the *latest* live version for each key.
//   - s.history[key] is append-only, time-ordered by UpdatedAt, and includes
//     every successful Set/SetWithTTL/CAS (whether or not the key later expired).
//   - Concurrency: not safe for concurrent use. Mutex comes in milestone 1D.
type Store struct {
	mu      sync.RWMutex       // lock the concurrent bits
	data    map[string]Entry   // current live snapshot per key
	history map[string][]Entry // append-only log of versions per key
	Clock   Clock              // time source (real or fake)
}

// makeTombstone creates a history tombstone from a live entry.
func makeTombstone(prev Entry, now time.Time) Entry {
	return Entry{
		// Value is ignored for tombstones; keep empty.
		CreatedAt: prev.CreatedAt, // policy: carry lineage
		UpdatedAt: now,
		Version:   prev.Version + 1,
		ExpiresAt: time.Time{}, // must be zero
		Deleted:   true,
	}
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

// nextVersionFromHistory returns the next version number if the key has history,
// otherwise 1 for a brand-new key (no prior lineage).
func (s *Store) nextVersionFromHistory(key string) int64 {
	h := s.history[key]
	if len(h) == 0 {
		return 1
	}
	return h[len(h)-1].Version + 1
}

// Get returns the latest live entry for a key, if present.
//
// - If key missing: returns (zero, false).
// - If entry has no expiry (ExpiresAt.IsZero): return it.
// - If expired (ExpiresAt <= now): delete from s.data and return (zero, false).
// - Otherwise: return it.
// Note: Get never reads or mutates s.history. Historical versions remain.
func (s *Store) Get(key string) (Entry, bool) {
	now := s.Clock.Now()

	s.mu.RLock()
	e, ok := s.data[key]
	if !ok {
		s.mu.RUnlock()
		return Entry{}, false
	}
	// visible if no TTL or not expired yet
	if e.ExpiresAt.IsZero() || now.Before(e.ExpiresAt) {
		s.mu.RUnlock()
		return e, true
	}
	s.mu.RUnlock()

	// Looks expired: take write lock, re-check, and possibly delete.
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok = s.data[key]
	if !ok {
		return Entry{}, false
	}
	now = s.Clock.Now()
	if !e.ExpiresAt.IsZero() && (now.Equal(e.ExpiresAt) || now.After(e.ExpiresAt)) {
		delete(s.data, key)
		return Entry{}, false
	}
	return e, true
}

// GetHistory returns the full, append-only history for key in chronological
// write order (UpdatedAt ascending). The history includes all successful
// Set/SetWithTTL/CAS writes (Deleted=false) and explicit deletes as tombstones
// (Deleted=true, ExpiresAt=zero).
func (s *Store) GetHistory(key string) ([]Entry, bool) {
	s.mu.RLock()
	hist, ok := s.history[key]
	if !ok || len(hist) == 0 {
		s.mu.RUnlock()
		return nil, false
	}
	// defensive copy
	out := make([]Entry, len(hist))
	copy(out, hist)
	s.mu.RUnlock()
	return out, true
}

// Set writes a new value for a key without a TTL.
// - New key: version = nextVersionFromHistory(key), createdAt=updatedAt=now.
// - Existing key: version++, createdAt unchanged, updatedAt=now.
// Side effects:
// - Updates s.data[key].
// - Appends the new version to s.history[key].
func (s *Store) Set(key string, value string) Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.Clock.Now()
	if existing, ok := s.data[key]; ok {
		newEntry := Entry{
			Value:     value,
			CreatedAt: existing.CreatedAt,
			UpdatedAt: n,
			Version:   existing.Version + 1,
			ExpiresAt: time.Time{}, // Set clears TTL
			Deleted:   false,
		}
		s.data[key] = newEntry
		s.history[key] = append(s.history[key], newEntry)
		return newEntry
	}
	newEntry := Entry{
		Value:     value,
		CreatedAt: n, // new lineage after missing/tombstone
		UpdatedAt: n,
		Version:   s.nextVersionFromHistory(key),
		ExpiresAt: time.Time{}, // no TTL
		Deleted:   false,
	}
	s.data[key] = newEntry
	s.history[key] = append(s.history[key], newEntry)
	return newEntry
}

// SetWithTTL writes a value with a TTL (time-to-live).
// - ttl <= 0 -> behaves like Set (no expiry).
// - ttl > 0 -> entry expires at now+ttl.
// Versioning: same rules as Set.
// Side effects:
// - Updates s.data[key].
// - Appends the new version (with ExpiresAt set/cleared) to s.history[key].
func (s *Store) SetWithTTL(
	key string,
	value string,
	ttl time.Duration) Entry {
	n := s.Clock.Now()
	if ttl <= 0 {
		return s.Set(key, value)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.data[key]; ok {
		newEntry := Entry{
			Value:     value,
			CreatedAt: existing.CreatedAt,
			UpdatedAt: n,
			Version:   existing.Version + 1,
			ExpiresAt: n.Add(ttl),
			Deleted:   false,
		}
		s.data[key] = newEntry
		s.history[key] = append(s.history[key], newEntry)
		return newEntry
	}
	newEntry := Entry{
		Value:     value,
		CreatedAt: n,
		UpdatedAt: n,
		Version:   s.nextVersionFromHistory(key),
		ExpiresAt: n.Add(ttl),
		Deleted:   false,
	}
	s.data[key] = newEntry
	s.history[key] = append(s.history[key], newEntry)
	return newEntry
}

// Delete removes a key and appends a tombstone in history if the key exists.
// Returns true if the key was present (and a tombstone written), false otherwise.
// Note: history is not pruned; tombstones and older versions remain.
func (s *Store) Delete(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.data[key]
	if !ok {
		return false
	}
	now := s.Clock.Now()
	tomb := makeTombstone(entry, now)
	delete(s.data, key)
	s.history[key] = append(s.history[key], tomb)
	return true
}

// Keys returns the set of live keys in s.data.
// Order is undefined.
func (s *Store) Keys() []string {
	s.mu.RLock()
	keys := []string{}
	for k := range s.data {
		keys = append(keys, k)
	}
	s.mu.RUnlock()
	return keys
}

// Size returns the number of live keys in s.data.
func (s *Store) Size() int {
	s.mu.RLock()
	n := len(s.data)
	s.mu.RUnlock()
	return n
}

// CASVersion updates key to newValue only if the current live version matches expectedVersion.
// Returns the updated entry and true on success; zero Entry and false otherwise.
// Policy: preserves existing TTL (ExpiresAt), does not modify CreatedAt, bumps Version and UpdatedAt.
func (s *Store) CASVersion(key string, expectedVersion int64, newValue string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := s.Clock.Now()

	cur, ok := s.data[key]
	if !ok {
		return Entry{}, false
	}
	// Treat tombstoned as not found
	if cur.Deleted {
		return Entry{}, false
	}
	// Reject if expired at or before now
	if !cur.ExpiresAt.IsZero() && (cur.ExpiresAt.Before(n) || cur.ExpiresAt.Equal(n)) {
		return Entry{}, false
	}
	// Version check
	if cur.Version != expectedVersion {
		return Entry{}, false
	}

	updated := Entry{
		Value:     newValue,
		CreatedAt: cur.CreatedAt,
		UpdatedAt: n,
		Version:   cur.Version + 1,
		ExpiresAt: cur.ExpiresAt, // preserve TTL
		Deleted:   false,
	}

	s.data[key] = updated
	s.history[key] = append(s.history[key], updated)
	return updated, true
}

// SweepExpired scans s.data and deletes all entries whose ExpiresAt <= now.
// Returns the count of keys removed.
// Note: history is not pruned (versions remain).
func (s *Store) SweepExpired() int {
	s.mu.Lock()
	defer s.mu.Unlock()
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

// GetWhen returns the snapshot value for key as of time t.
//
// Snapshot semantics:
//   - Reads from history[key] only (append-only, ordered by UpdatedAt).
//   - Returns the most recent Entry with UpdatedAt <= t that was *alive at t*:
//     AliveAt(t) := ExpiresAt.IsZero() || ExpiresAt.After(t)
//     (Note: if ExpiresAt == t the entry is NOT visible at t.)
//   - Tombstones are barriers: if a Deleted==true entry has UpdatedAt <= t,
//     older values are not visible for that t.
//   - Pure read: does not mutate s.data or s.history.
//
// Search strategy (brief):
//   - Binary search for the first index i with ents[i].UpdatedAt > t.
//   - Start at i-1 (latest <= t) and scan left until a non-deleted entry
//     alive at t is found; if a tombstone with UpdatedAt <= t is encountered,
//     return not found.
//   - Complexity: O(log N + K).
func (s *Store) GetWhen(key string, t time.Time) (Entry, bool) {
	s.mu.RLock()
	ents, ok := s.history[key]
	if !ok || len(ents) == 0 {
		s.mu.RUnlock()
		return Entry{}, false
	}
	cp := make([]Entry, len(ents))
	copy(cp, ents)
	s.mu.RUnlock()

	// Binary search for the first index with UpdatedAt > t (upper bound of t).
	l, r := 0, len(cp) // search space is [l, r)
	for l < r {
		mid := l + (r-l)/2
		if cp[mid].UpdatedAt.After(t) {
			r = mid // answer is in [l, mid)
		} else {
			l = mid + 1 // answer is in (mid, r)
		}
	}
	idx := l

	// If idx == 0, all entries have UpdatedAt > t -> nothing existed by time t.
	if idx == 0 {
		return Entry{}, false
	}

	// Walk left from the candidate (idx-1) to honor tombstones and TTL at time t.
	for i := idx - 1; i >= 0; i-- {
		e := cp[i]

		// Tombstone barrier: if the delete happened at/ before t, nothing older is visible.
		if e.Deleted && (e.UpdatedAt.Before(t) || e.UpdatedAt.Equal(t)) {
			return Entry{}, false
		}

		// Live entry: visible at t iff no TTL or expiry strictly after t.
		if !e.Deleted && (e.ExpiresAt.IsZero() || e.ExpiresAt.After(t)) {
			return e, true
		}

		// Else: either expired-by-t or (rare) a tombstone after t; continue left.
	}

	return Entry{}, false
}
