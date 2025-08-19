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
