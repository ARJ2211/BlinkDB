// internal/store/store_1a_test.go
// 1A acceptance checks
//
// A) First write creates version 1
// B) Second write bumps version and updatedAt
// C) Get missing key
// D) Delete existing key
// E) Delete missing key is idempotent
// F) Size reflects live keys
// G) Keys has exactly current keys; order not asserted

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

// FakeClock lets tests control time deterministically.
type FakeClock struct {
	now time.Time
}

func (fc *FakeClock) Now() time.Time          { return fc.now }
func (fc *FakeClock) Advance(d time.Duration) { fc.now = fc.now.Add(d) }

func newTestStoreAt(t *testing.T, t0 time.Time) (*Store, *FakeClock) {
	t.Helper()
	fc := &FakeClock{now: t0}
	s := NewStoreWithClock(fc)
	return s, fc
}

func advance(fc *FakeClock, d time.Duration) {
	fc.Advance(d)
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
	s, fc := newTestStoreAt(t, t0)

	s.SetWithTTL("k", "A", 10*time.Second) // expires at t0+10s

	advance(fc, 9*time.Second) // before expiry
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
	s, fc := newTestStoreAt(t, t0)

	s.SetWithTTL("k", "A", 3*time.Second) // expires at t0+3s

	advance(fc, 3*time.Second) // at expiry boundary

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
	s, fc := newTestStoreAt(t, t0)

	s.SetWithTTL("k", "A", 1*time.Second)

	advance(fc, 2*time.Second) // now expired
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

func TestSweepExpired_RemovesOnlyExpired(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0).UTC()
	s, fc := newTestStoreAt(t, t0)

	// Set two keys, one short TTL, one long TTL
	s.SetWithTTL("short", "A", 2*time.Second) // expires at t0+2s
	s.SetWithTTL("long", "B", 20*time.Second) // expires at t0+20s

	// Advance just past the short expiry
	advance(fc, 3*time.Second)

	removed := s.SweepExpired()
	if removed != 1 {
		t.Fatalf("expected 1 key removed, got %d", removed)
	}

	// short should be gone
	if _, ok := s.Get("short"); ok {
		t.Fatalf("expected 'short' to be deleted after sweep")
	}

	// long should still be present
	if e, ok := s.Get("long"); !ok || e.Value != "B" {
		t.Fatalf("expected 'long' to still be present")
	}

	// Second sweep should remove nothing
	removed2 := s.SweepExpired()
	if removed2 != 0 {
		t.Fatalf("expected 0 keys removed on second sweep, got %d", removed2)
	}
}

func TestNewStore_InitializesMaps(t *testing.T) {
	s := NewStore()
	if s == nil {
		t.Fatal("NewStore returned nil")
	}
	if s.data == nil {
		t.Fatal("data map is nil")
	}
	if s.history == nil {
		t.Fatal("history map is nil")
	}
	if got := len(s.data); got != 0 {
		t.Fatalf("expected data to be empty, got %d", got)
	}
	if got := len(s.history); got != 0 {
		t.Fatalf("expected history to be empty, got %d", got)
	}
}

func TestNewStore_ClockIsSet(t *testing.T) {
	s := NewStore()
	now := s.Clock.Now()
	if now.IsZero() {
		t.Fatal("Clock.Now returned zero time")
	}
	// sanity: Now should be close to wall clock (smoke check)
	if time.Since(now) > 5*time.Second {
		t.Fatalf("Clock.Now seems off; now=%v, time.Now()=%v", now, time.Now())
	}
}

func TestSet_UsesClock(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	fc := &FakeClock{now: t0}
	s := NewStoreWithClock(fc)

	e1 := s.Set("k", "A")
	if !e1.UpdatedAt.Equal(t0) {
		t.Errorf("expected UpdatedAt=%v, got %v", t0, e1.UpdatedAt)
	}

	// Fast forward 5 minutes instantly (no sleep!)
	fc.Advance(5 * time.Minute)
	e2 := s.Set("k", "B")
	if !e2.UpdatedAt.Equal(t0.Add(5 * time.Minute)) {
		t.Errorf("expected UpdatedAt=%v, got %v", t0.Add(5*time.Minute), e2.UpdatedAt)
	}
}
