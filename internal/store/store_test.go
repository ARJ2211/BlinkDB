// 1A acceptance checks (no code yet)
//
// A) First write creates version 1
// - Set("u1", "A")
// - Get("u1") => value="A", version=1
// - createdAt == updatedAt (roughly equal, non-zero)
//
// B) Second write bumps version and updatedAt
// - Set("u1", "B")
// - Get("u1") => value="B", version=2
// - createdAt unchanged; updatedAt > previous updatedAt
//
// C) Get missing key
// - Get("missing") => found=false
//
// D) Delete existing key
// - Delete("u1") => true
// - Get("u1") => found=false
//
// E) Delete missing key is idempotent
// - Delete("u1") => false
//
// F) Size reflects live keys
// - After inserting k1,k2 and deleting k1 => Size()==1
//
// G) (Optional) Keys has exactly current keys; order not asserted
// - Insert k1,k2; Keys() contains both; no duplicates

package store

import (
	"testing"
	"time"
)

// --- helpers ---

func mustNonZero(t *testing.T, tm time.Time, name string) {
	t.Helper()
	if tm.IsZero() {
		t.Fatalf("%s should be non-zero", name)
	}
}

func mustAfter(t *testing.T, newer, older time.Time, ctx string) {
	t.Helper()
	if !newer.After(older) {
		t.Fatalf("%s: expected %v to be after %v", ctx, newer, older)
	}
}

func containsAll(have []string, want []string) bool {
	set := make(map[string]struct{}, len(have))
	for _, k := range have {
		set[k] = struct{}{}
	}
	for _, w := range want {
		if _, ok := set[w]; !ok {
			return false
		}
	}
	return true
}

func newTestStoreWithClock(t *testing.T, t0 time.Time) (*Store, *time.Time) {
	t.Helper()
	s := NewStore()
	// We chose to keep Clock public in this project
	current := t0
	s.Clock.Now = func() time.Time { return current }
	return s, &current
}

func advance(current *time.Time, d time.Duration) {
	*current = current.Add(d)
}

// --- tests ---

func TestSetAndGet_NewAndUpdate(t *testing.T) {
	s := NewStore()

	e1 := s.Set("u1", "A")
	if e1.Value != "A" {
		t.Fatalf("expected A, got %q", e1.Value)
	}
	if e1.Version != 1 {
		t.Fatalf("expected version 1, got %d", e1.Version)
	}
	mustNonZero(t, e1.CreatedAt, "CreatedAt (e1)")
	mustNonZero(t, e1.UpdatedAt, "UpdatedAt (e1)")
	// first write: CreatedAt == UpdatedAt (allow tiny skew)
	if !e1.CreatedAt.Equal(e1.UpdatedAt) {
		t.Fatalf("first write should have CreatedAt == UpdatedAt; got %v vs %v", e1.CreatedAt, e1.UpdatedAt)
	}

	prevCreated := e1.CreatedAt
	prevUpdated := e1.UpdatedAt

	// update same key
	e2 := s.Set("u1", "B")
	if e2.Value != "B" {
		t.Fatalf("expected B, got %q", e2.Value)
	}
	if e2.Version != 2 {
		t.Fatalf("expected version 2, got %d", e2.Version)
	}
	if !e2.CreatedAt.Equal(prevCreated) {
		t.Fatalf("CreatedAt must remain unchanged")
	}
	mustAfter(t, e2.UpdatedAt, prevUpdated, "UpdatedAt should increase on update")

	// Get reflects latest
	e, ok := s.Get("u1")
	if !ok {
		t.Fatalf("expected to find u1")
	}
	if e.Value != "B" || e.Version != 2 {
		t.Fatalf("Get mismatch: got (%q, v%d)", e.Value, e.Version)
	}
}

func TestGet_Missing(t *testing.T) {
	s := NewStore()
	if _, ok := s.Get("missing"); ok {
		t.Fatalf("expected missing key to return ok=false")
	}
}

func TestDelete_Idempotent(t *testing.T) {
	s := NewStore()
	s.Set("u1", "A")

	if ok := s.Delete("u1"); !ok {
		t.Fatalf("expected delete u1 == true")
	}
	if _, ok := s.Get("u1"); ok {
		t.Fatalf("u1 should be gone after delete")
	}
	if ok := s.Delete("u1"); ok {
		t.Fatalf("second delete should be false (idempotent)")
	}
	if ok := s.Delete("nope"); ok {
		t.Fatalf("delete on missing should be false")
	}
}

func TestSize_CountsLiveKeys(t *testing.T) {
	s := NewStore()
	if s.Size() != 0 {
		t.Fatalf("new store size should be 0, got %d", s.Size())
	}
	s.Set("a", "1")
	s.Set("b", "2")
	if s.Size() != 2 {
		t.Fatalf("expected size 2, got %d", s.Size())
	}
	s.Delete("a")
	if s.Size() != 1 {
		t.Fatalf("expected size 1 after delete, got %d", s.Size())
	}
}

func TestKeys_ContentsIgnoreOrder(t *testing.T) {
	s := NewStore()
	s.Set("k1", "x")
	s.Set("k2", "y")

	ks := s.Keys()
	if len(ks) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(ks))
	}
	if !containsAll(ks, []string{"k1", "k2"}) {
		t.Fatalf("keys should contain k1 and k2, got %v", ks)
	}

	s.Delete("k1")
	ks = s.Keys()
	if len(ks) != 1 {
		t.Fatalf("expected 1 key after delete, got %d", len(ks))
	}
	if !containsAll(ks, []string{"k2"}) {
		t.Fatalf("keys should contain only k2, got %v", ks)
	}
}

func TestCAS_SuccessAndFail(t *testing.T) {
	s := NewStore()
	s.Set("u1", "A")

	// success
	if ok := s.CAS("u1", "A", "B"); !ok {
		t.Fatalf("CAS should succeed when expected matches")
	}
	e, ok := s.Get("u1")
	if !ok {
		t.Fatalf("u1 should exist after CAS")
	}
	if e.Value != "B" || e.Version != 2 {
		t.Fatalf("after CAS expected (B, v2), got (%q, v%d)", e.Value, e.Version)
	}
	prevUpdated := e.UpdatedAt

	// failure (expected mismatch)
	if ok := s.CAS("u1", "A", "C"); ok {
		t.Fatalf("CAS should fail when expected mismatches")
	}
	e2, _ := s.Get("u1")
	if e2.Value != "B" || e2.Version != 2 {
		t.Fatalf("on CAS fail, entry must be unchanged; got (%q, v%d)", e2.Value, e2.Version)
	}
	// UpdatedAt should not change on CAS fail
	if !e2.UpdatedAt.Equal(prevUpdated) {
		t.Fatalf("UpdatedAt changed on CAS fail")
	}

	// missing key
	if ok := s.CAS("missing", "X", "Y"); ok {
		t.Fatalf("CAS on missing key should return false")
	}
}

func TestGet_NotExpired_ReturnsEntry(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0).UTC()
	s, now := newTestStoreWithClock(t, t0)

	s.SetWithTTL("k", "A", 10*time.Second) // expires at t0+10s

	advance(now, 9*time.Second) // before expiry
	e, ok := s.Get("k")
	if !ok {
		t.Fatalf("expected key present before expiry")
	}
	if e.Value != "A" {
		t.Fatalf("expected value A, got %q", e.Value)
	}
	// still present on second Get if not expired
	if _, ok2 := s.Get("k"); !ok2 {
		t.Fatalf("expected key still present before expiry on second Get")
	}
}

func TestGet_Expired_IsDeletedAndNotFound(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0).UTC()
	s, now := newTestStoreWithClock(t, t0)

	s.SetWithTTL("k", "A", 3*time.Second) // expires at t0+3s

	advance(now, 3*time.Second) // at expiry boundary

	// First Get should lazily delete and report not found
	if _, ok := s.Get("k"); ok {
		t.Fatalf("expected not found at expiry boundary (lazy delete)")
	}

	// Subsequent Get should also be not found (it was deleted)
	if _, ok := s.Get("k"); ok {
		t.Fatalf("expected not found after lazy delete")
	}
}

func TestGet_OverwriteExpiredKey_WorksNormally(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0).UTC()
	s, now := newTestStoreWithClock(t, t0)

	s.SetWithTTL("k", "A", 1*time.Second)

	advance(now, 2*time.Second) // now expired
	if _, ok := s.Get("k"); ok {
		t.Fatalf("expected not found after expiry and lazy delete")
	}

	// Overwrite with no TTL should clear expiry and be present
	s.Set("k", "B")
	e, ok := s.Get("k")
	if !ok {
		t.Fatalf("expected key present after Set without TTL")
	}
	if e.Value != "B" {
		t.Fatalf("expected value B, got %q", e.Value)
	}
	if !e.ExpiresAt.IsZero() {
		t.Fatalf("expected expiry cleared on Set without TTL")
	}
}
