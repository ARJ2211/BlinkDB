package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ARJ2211/blinkdb/internal/store"
)

func TestPUT_New_NoTTL_201(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	_, router := newTestHTTP(t, t0)

	body := mustJSON(t, PutValueRequest{Value: "A"})
	req := httptest.NewRequest(http.MethodPut, "/v1/kv/u1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201", rec.Code)
	}
	var got EntryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Key != "u1" || got.Value != "A" || got.Version != 1 {
		t.Fatalf("bad dto: %+v", got)
	}
	if got.ExpiresAt != "" {
		t.Fatalf("unexpected expiresAt: %q", got.ExpiresAt)
	}
}

func TestPUT_Update_KeepExistingTTL_200(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	st, router := newTestHTTP(t, t0)

	// seed with TTL 60s
	st.SetWithTTL("u1", "old", time.Minute)
	// update without TTL fields → keep same expiry
	body := mustJSON(t, PutValueRequest{Value: "new"})
	req := httptest.NewRequest(http.MethodPut, "/v1/kv/u1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var got EntryDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Value != "new" {
		t.Fatalf("want value=new, got %q", got.Value)
	}
	if got.ExpiresAt == "" {
		t.Fatalf("expected expiresAt preserved, got empty")
	}
}

func TestPUT_WithTTLSeconds_SetsExpiry(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	_, router := newTestHTTP(t, t0)

	body := mustJSON(t, PutValueRequest{Value: "A", TTLSeconds: 90})
	req := httptest.NewRequest(http.MethodPut, "/v1/kv/k", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201", rec.Code)
	}
	var got EntryDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.ExpiresAt == "" {
		t.Fatalf("expected expiresAt set, got empty")
	}
}

func TestPUT_WithExpiresAt_SetsExactExpiry(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	_, router := newTestHTTP(t, t0)

	exp := time.Date(2025, 8, 19, 13, 0, 0, 0, time.UTC).Format(time.RFC3339)
	body := mustJSON(t, PutValueRequest{Value: "A", ExpiresAt: exp})
	req := httptest.NewRequest(http.MethodPut, "/v1/kv/k", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201", rec.Code)
	}
	var got EntryDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.ExpiresAt != exp {
		t.Fatalf("expected expiresAt=%s, got %s", exp, got.ExpiresAt)
	}
}

func TestPUT_ClearTTL_RemovesExpiry(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	st, router := newTestHTTP(t, t0)

	st.SetWithTTL("k", "A", time.Minute) // seed with TTL
	body := mustJSON(t, PutValueRequest{Value: "B", ClearTTL: true})
	req := httptest.NewRequest(http.MethodPut, "/v1/kv/k", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var got EntryDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.ExpiresAt != "" {
		t.Fatalf("expected no expiresAt after clearTTL, got %q", got.ExpiresAt)
	}
}

func TestPUT_BadJSON_400(t *testing.T) {
	_, router := newTestHTTP(t, time.Now().UTC())
	req := httptest.NewRequest(http.MethodPut, "/v1/kv/k", bytes.NewBufferString(`{"value":123}`)) // value should be string
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestPUT_BadTTLCombo_400(t *testing.T) {
	_, router := newTestHTTP(t, time.Now().UTC())
	// both ttlSeconds and expiresAt set + clearTTL false → invalid combo
	payload := `{"value":"x","ttlSeconds":10,"expiresAt":"2025-08-19T13:00:00Z"}`
	req := httptest.NewRequest(http.MethodPut, "/v1/kv/k", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestEntryDTO_JSON_OmitsEmpty(t *testing.T) {
	e := EntryDTO{
		Key:       "u1",
		Version:   1,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		// Value empty, ExpiresAt empty → should be omitted
		Deleted: true,
	}

	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	out := string(b)
	if out == "" {
		t.Fatal("expected non-empty JSON")
	}
	if contains := `"value"`; containsIn(out, contains) {
		t.Fatalf("expected %q to be omitted, got %s", contains, out)
	}
	if contains := `"expiresAt"`; containsIn(out, contains) {
		t.Fatalf("expected %q to be omitted, got %s", contains, out)
	}
}

func TestPutValueRequest_JSON_RoundTrip(t *testing.T) {
	input := `{"value":"Alice","ttlSeconds":60}`
	var req PutValueRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if req.Value != "Alice" || req.TTLSeconds != 60 {
		t.Fatalf("unexpected values after unmarshal: %+v", req)
	}

	// Marshal back — ClearTTL and ExpiresAt are zero so they should be omitted
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	out := string(b)
	if !containsIn(out, `"value"`) || !containsIn(out, `"ttlSeconds"`) {
		t.Fatalf("expected value and ttlSeconds in JSON, got %s", out)
	}
	if containsIn(out, `"expiresAt"`) || containsIn(out, `"clearTTL"`) {
		t.Fatalf("did not expect expiresAt/clearTTL, got %s", out)
	}
}

// --- helpers ---

func containsIn(s, substr string) bool {
	return len(s) > 0 && (len(substr) > 0 && (stringIndex(s, substr) >= 0))
}

// minimal index check to avoid importing strings
func stringIndex(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// helper to make a server + router with a fixed clock
type fakeClock struct{ now time.Time }

func (fc *fakeClock) Now() time.Time { return fc.now }
func newTestHTTP(t *testing.T, t0 time.Time) (*store.Store, http.Handler) {
	t.Helper()
	fc := &fakeClock{now: t0}
	st := store.NewStoreWithClock(fc)
	srv := NewServer(st)
	return st, NewRouter(srv)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
