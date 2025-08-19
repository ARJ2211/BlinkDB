package api

import (
	"encoding/json"
	"testing"
	"time"
)

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
