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

func TestCAS_PreservesTTL_OnSuccess(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0).UTC()
	s, fc := newTestStoreAt(t, t0)

	eA := s.SetWithTTL("k", "A", 2*time.Minute)
	if eA.ExpiresAt.IsZero() {
		t.Fatalf("precondition: expected non-zero ExpiresAt")
	}
	exp := eA.ExpiresAt

	// ensure UpdatedAt changes
	advance(fc, time.Nanosecond) // <-- add this line

	if ok := s.CAS("k", "A", "B"); !ok {
		t.Fatalf("CAS should succeed")
	}
	eB, ok := s.Get("k")
	if !ok {
		t.Fatalf("expected key after CAS")
	}
	if !eB.ExpiresAt.Equal(exp) {
		t.Fatalf("CAS should preserve TTL: want %v, got %v", exp, eB.ExpiresAt)
	}
	if !eB.UpdatedAt.After(eA.UpdatedAt) {
		t.Fatalf("UpdatedAt should advance")
	}
}

func TestCAS_RespectsExpiry(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0).UTC()
	s, fc := newTestStoreAt(t, t0)

	s.SetWithTTL("k", "A", 30*time.Second)
	advance(fc, 30*time.Second) // at expiry boundary

	// CAS should fail and key should be removed (lazy delete semantics)
	if ok := s.CAS("k", "A", "B"); ok {
		t.Fatalf("CAS should fail when entry is expired")
	}
	if _, ok := s.Get("k"); ok {
		t.Fatalf("expected key to be gone after CAS against expired entry")
	}
}

func TestCAS_NoTTL_PreservesNone(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0).UTC()
	s, _ := newTestStoreAt(t, t0)

	eA := s.Set("k", "A")
	if !eA.ExpiresAt.IsZero() {
		t.Fatalf("precondition: Set without TTL should have zero ExpiresAt")
	}

	if ok := s.CAS("k", "A", "B"); !ok {
		t.Fatalf("CAS should succeed")
	}
	eB, ok := s.Get("k")
	if !ok {
		t.Fatalf("expected key after CAS")
	}
	if !eB.ExpiresAt.IsZero() {
		t.Fatalf("CAS should preserve 'no TTL' (zero ExpiresAt), got %v", eB.ExpiresAt)
	}
}

func TestHistory_AppendsOnSetAndCAS(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0).UTC()
	s, fc := newTestStoreAt(t, t0)

	e1 := s.Set("k", "A")
	if got := len(s.history["k"]); got != 1 {
		t.Fatalf("history length after first Set = %d, want 1", got)
	}
	if s.history["k"][0].Version != 1 || s.history["k"][0].Value != "A" {
		t.Fatalf("history[0] mismatch: %+v", s.history["k"][0])
	}
	if !s.history["k"][0].UpdatedAt.Equal(t0) {
		t.Fatalf("history[0].UpdatedAt = %v, want %v", s.history["k"][0].UpdatedAt, t0)
	}

	advance(fc, time.Minute)
	e2 := s.Set("k", "B")
	if got := len(s.history["k"]); got != 2 {
		t.Fatalf("history length after second Set = %d, want 2", got)
	}
	if s.history["k"][1].Version != 2 || s.history["k"][1].Value != "B" {
		t.Fatalf("history[1] mismatch: %+v", s.history["k"][1])
	}
	if !s.history["k"][1].UpdatedAt.After(s.history["k"][0].UpdatedAt) {
		t.Fatalf("history timestamps not increasing")
	}

	advance(fc, time.Minute)
	if ok := s.CAS("k", "B", "C"); !ok {
		t.Fatalf("CAS should succeed")
	}
	if got := len(s.history["k"]); got != 3 {
		t.Fatalf("history length after CAS = %d, want 3", got)
	}
	if s.history["k"][2].Version != 3 || s.history["k"][2].Value != "C" {
		t.Fatalf("history[2] mismatch: %+v", s.history["k"][2])
	}

	// sanity: Get still reflects latest from data and matches last history entry
	cur, ok := s.Get("k")
	if !ok {
		t.Fatalf("expected key present")
	}
	last := s.history["k"][2]
	if cur.Value != last.Value || cur.Version != last.Version || !cur.UpdatedAt.Equal(last.UpdatedAt) {
		t.Fatalf("Get != last history: got %+v, last %+v", cur, last)
	}

	// also sanity: versions progressed 1->2->3
	if e1.Version != 1 || e2.Version != 2 || cur.Version != 3 {
		t.Fatalf("versions not 1,2,3: e1=%d e2=%d cur=%d", e1.Version, e2.Version, cur.Version)
	}
}

func TestHistory_TTL_RecordedAndClearedCorrectly(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0).UTC()
	s, fc := newTestStoreAt(t, t0)

	// Set with TTL should record non-zero ExpiresAt in history
	eA := s.SetWithTTL("ttl", "A", 2*time.Minute)
	if got := len(s.history["ttl"]); got != 1 {
		t.Fatalf("history length after SetWithTTL = %d, want 1", got)
	}
	if s.history["ttl"][0].ExpiresAt.IsZero() {
		t.Fatalf("history[0].ExpiresAt should be non-zero")
	}
	if !s.history["ttl"][0].ExpiresAt.Equal(eA.ExpiresAt) {
		t.Fatalf("history[0].ExpiresAt mismatch: %v vs %v", s.history["ttl"][0].ExpiresAt, eA.ExpiresAt)
	}

	// A subsequent Set (no TTL) should append an entry with zero ExpiresAt
	advance(fc, time.Minute)
	_ = s.Set("ttl", "B")
	if got := len(s.history["ttl"]); got != 2 {
		t.Fatalf("history length after Set = %d, want 2", got)
	}
	if !s.history["ttl"][1].ExpiresAt.IsZero() {
		t.Fatalf("history[1].ExpiresAt should be zero for Set without TTL, got %v", s.history["ttl"][1].ExpiresAt)
	}
}

func TestHistory_NoAppendOnCASFail(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0).UTC()
	s, _ := newTestStoreAt(t, t0)

	_ = s.Set("x", "A")
	before := len(s.history["x"])

	// wrong expected, should fail and not append
	if ok := s.CAS("x", "wrong", "B"); ok {
		t.Fatalf("CAS should fail with wrong expected")
	}
	after := len(s.history["x"])
	if after != before {
		t.Fatalf("history changed on CAS fail: before=%d after=%d", before, after)
	}
}

func TestGet_LazyDelete_DoesNotTouchHistory(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0).UTC()
	s, fc := newTestStoreAt(t, t0)

	// write with TTL (history should get 1 entry)
	_ = s.SetWithTTL("k", "A", 3*time.Second)
	if got := len(s.history["k"]); got != 1 {
		t.Fatalf("precondition: history length = %d, want 1", got)
	}

	// advance to expiry boundary, first Get should lazily delete from s.data
	advance(fc, 3*time.Second)
	if _, ok := s.Get("k"); ok {
		t.Fatalf("expected not found at expiry boundary (lazy delete)")
	}

	// history must remain untouched
	if got := len(s.history["k"]); got != 1 {
		t.Fatalf("lazy delete must not change history; got %d, want 1", got)
	}
}

func TestGet_AfterLazyDelete_SetRecreatesCleanly(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0).UTC()
	s, fc := newTestStoreAt(t, t0)

	// write with short TTL and expire it
	_ = s.SetWithTTL("k", "A", 1*time.Second)
	advance(fc, 1*time.Second)

	// trigger lazy delete
	if _, ok := s.Get("k"); ok {
		t.Fatalf("expected not found at expiry boundary (lazy delete)")
	}

	// a normal Set should recreate the key with zero ExpiresAt
	e := s.Set("k", "B")
	if !e.ExpiresAt.IsZero() {
		t.Fatalf("Set should clear expiry; got ExpiresAt=%v", e.ExpiresAt)
	}

	// Get returns fresh value B
	g, ok := s.Get("k")
	if !ok {
		t.Fatalf("expected key present after Set")
	}
	if g.Value != "B" {
		t.Fatalf("expected value B, got %q", g.Value)
	}
	if !g.ExpiresAt.IsZero() {
		t.Fatalf("expected zero ExpiresAt on recreated entry, got %v", g.ExpiresAt)
	}
}

func TestGetWhen_BasicHistory(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	s, fc := newTestStoreAt(t, t0)

	eA := s.Set("k", "A") // 12:00
	advance(fc, 5*time.Minute)
	eB := s.Set("k", "B") // 12:05
	advance(fc, 5*time.Minute)
	eC := s.Set("k", "C") // 12:10

	// before first write
	if _, ok := s.GetWhen("k", t0.Add(-time.Minute)); ok {
		t.Fatalf("expected not found before first write")
	}

	// exactly at A
	v, ok := s.GetWhen("k", eA.UpdatedAt)
	if !ok || v.Value != "A" || v.Version != 1 {
		t.Fatalf("expected A@v1 at A time, got (%q,v%d,ok=%v)", v.Value, v.Version, ok)
	}

	// between B and C (12:07)
	v, ok = s.GetWhen("k", eB.UpdatedAt.Add(2*time.Minute))
	if !ok || v.Value != "B" || v.Version != 2 {
		t.Fatalf("expected B@v2 at 12:07, got (%q,v%d,ok=%v)", v.Value, v.Version, ok)
	}

	// long after C
	v, ok = s.GetWhen("k", eC.UpdatedAt.Add(10*time.Hour))
	if !ok || v.Value != "C" || v.Version != 3 {
		t.Fatalf("expected C@v3 long after C, got (%q,v%d,ok=%v)", v.Value, v.Version, ok)
	}
}

func TestGetWhen_RespectsTTLAtTimeT(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	s, fc := newTestStoreAt(t, t0)

	// A at 12:00, TTL 3m (expires 12:03)
	eA := s.SetWithTTL("k", "A", 3*time.Minute)
	if eA.ExpiresAt.IsZero() {
		t.Fatalf("precondition: expected non-zero ExpiresAt for A")
	}

	// Before expiry → A
	v, ok := s.GetWhen("k", t0.Add(2*time.Minute)) // 12:02
	if !ok || v.Value != "A" {
		t.Fatalf("expected A at 12:02, got (%q, ok=%v)", v.Value, ok)
	}

	// At expiry boundary → not found
	if _, ok := s.GetWhen("k", t0.Add(3*time.Minute)); ok { // 12:03
		t.Fatalf("expected not found at exact expiry")
	}

	// After expiry but before B → not found
	if _, ok := s.GetWhen("k", t0.Add(4*time.Minute)); ok { // 12:04
		t.Fatalf("expected not found between A expiry and B write")
	}

	// Write B at 12:05 (no TTL)
	advance(fc, 5*time.Minute) // move clock to ~12:05
	eB := s.Set("k", "B")

	// After B → B
	v, ok = s.GetWhen("k", eB.UpdatedAt.Add(time.Minute)) // 12:06
	if !ok || v.Value != "B" {
		t.Fatalf("expected B at 12:06, got (%q, ok=%v)", v.Value, ok)
	}
}

func TestGetWhen_DoesNotMutateState(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	s, fc := newTestStoreAt(t, t0)

	_ = s.Set("k", "A")
	advance(fc, time.Minute)
	_ = s.Set("k", "B")

	// capture state
	beforeHist := len(s.history["k"])
	beforeLive, liveOK := s.Get("k")

	// perform time-travel reads at different times
	_, _ = s.GetWhen("k", t0.Add(30*time.Second)) // should be A
	_, _ = s.GetWhen("k", t0.Add(2*time.Minute))  // should be B

	// ensure history unchanged
	if got := len(s.history["k"]); got != beforeHist {
		t.Fatalf("GetWhen must not mutate history; before=%d after=%d", beforeHist, got)
	}
	// ensure live map unchanged
	afterLive, afterOK := s.Get("k")
	if liveOK != afterOK || beforeLive != afterLive {
		t.Fatalf("GetWhen must not mutate live data; before=%+v ok=%v, after=%+v ok=%v",
			beforeLive, liveOK, afterLive, afterOK)
	}
}

func TestGetWhen_TiesByTimestamp_UsesLatestAppended(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	s, _ := newTestStoreAt(t, t0)

	// Two writes at identical UpdatedAt (no clock advance)
	e1 := s.Set("k", "A") // v1 @ t0
	e2 := s.Set("k", "B") // v2 @ t0 (same UpdatedAt as e1)

	// At exactly t0, should pick the last appended version (B@v2)
	v, ok := s.GetWhen("k", e1.UpdatedAt)
	if !ok {
		t.Fatalf("expected a value at exact timestamp")
	}
	if v.Value != "B" || v.Version != 2 {
		t.Fatalf("expected latest appended at same timestamp (B,v2), got (%q,v%d)", v.Value, v.Version)
	}

	// Sanity: at a much later time, still B (latest as of then)
	v, ok = s.GetWhen("k", e2.UpdatedAt.Add(time.Minute))
	if !ok || v.Value != "B" {
		t.Fatalf("expected B after, got (%q, ok=%v)", v.Value, ok)
	}
}

func TestGetWhen_MissingKey(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	s, _ := newTestStoreAt(t, t0)
	if _, ok := s.GetWhen("missing", t0); ok {
		t.Fatalf("expected missing key to return ok=false")
	}
}
