package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestGET_Found_200(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	st, router := newTestHTTP(t, t0)

	st.Set("u1", "A")

	req := httptest.NewRequest(http.MethodGet, "/v1/kv/u1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var got EntryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Key != "u1" || got.Value != "A" || got.Version != 1 {
		t.Fatalf("bad dto: %+v", got)
	}
	if got.ExpiresAt != "" {
		t.Fatalf("did not expect expiresAt for Set without TTL; got %q", got.ExpiresAt)
	}
}

func TestGET_Missing_404(t *testing.T) {
	_, router := newTestHTTP(t, time.Now().UTC())

	req := httptest.NewRequest(http.MethodGet, "/v1/kv/missing", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
	var errBody ErrorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	if errBody.Error == "" {
		t.Fatalf("expected error message in body")
	}
}

func TestGET_Expired_404(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	st, router, fc := newTestHTTPWithClock(t, t0)

	// write with short TTL
	st.SetWithTTL("k", "A", 2*time.Second)

	// move to expiry boundary
	fc.now = fc.now.Add(2 * time.Second)

	req := httptest.NewRequest(http.MethodGet, "/v1/kv/k", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 when expired", rec.Code)
	}
}

func TestGET_Tombstoned_404(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	st, router := newTestHTTP(t, t0)

	st.Set("z", "A")
	ok := st.Delete("z")
	if !ok {
		t.Fatalf("precondition: delete should succeed")
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/kv/z", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 for tombstoned key", rec.Code)
	}
}
func TestDELETE_Existing_ReturnsTombstone_200(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	st, router := newTestHTTP(t, t0)

	e := st.Set("del", "A") // v1

	req := httptest.NewRequest(http.MethodDelete, "/v1/kv/del", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var dr DeleteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &dr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !dr.Entry.Deleted {
		t.Fatalf("expected deleted=true")
	}
	if dr.Entry.Key != "del" {
		t.Fatalf("expected key=del, got %q", dr.Entry.Key)
	}
	// version bumps by 1 across tombstone
	if dr.Entry.Version != e.Version+1 {
		t.Fatalf("expected version=%d, got %d", e.Version+1, dr.Entry.Version)
	}
	if dr.Entry.ExpiresAt != "" {
		t.Fatalf("tombstone should not have expiresAt")
	}

	// subsequent GET should be 404
	req2 := httptest.NewRequest(http.MethodGet, "/v1/kv/del", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("after delete, GET status=%d, want 404", rec2.Code)
	}
}

func TestDELETE_Missing_404(t *testing.T) {
	_, router := newTestHTTP(t, time.Now().UTC())

	req := httptest.NewRequest(http.MethodDelete, "/v1/kv/none", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestDELETE_SecondTime_404(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	st, router := newTestHTTP(t, t0)

	st.Set("z", "A")

	// first delete → 200
	req1 := httptest.NewRequest(http.MethodDelete, "/v1/kv/z", nil)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first delete status=%d, want 200", rec1.Code)
	}

	// second delete → 404 (store.Delete returns false)
	req2 := httptest.NewRequest(http.MethodDelete, "/v1/kv/z", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("second delete status=%d, want 404", rec2.Code)
	}
}

func TestCAS_Success_NoTTL_200(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	st, router := newTestHTTP(t, t0)

	e := st.Set("k", "A") // v1

	body := mustJSON(t, CASRequest{ExpectedVersion: e.Version, Value: "B"})
	req := httptest.NewRequest(http.MethodPost, "/v1/kv/k:cas", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var got EntryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Value != "B" || got.Version != e.Version+1 {
		t.Fatalf("want value=B, version=%d; got value=%q, version=%d", e.Version+1, got.Value, got.Version)
	}
	if got.ExpiresAt != "" {
		t.Fatalf("no TTL expected, got expiresAt=%q", got.ExpiresAt)
	}
}

func TestCAS_Conflict_409(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	st, router := newTestHTTP(t, t0)

	e := st.Set("k", "A") // v1

	// stale expectedVersion (v1), but first make it v2
	st.Set("k", "B")

	body := mustJSON(t, CASRequest{ExpectedVersion: e.Version, Value: "C"})
	req := httptest.NewRequest(http.MethodPost, "/v1/kv/k:cas", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d, want 409", rec.Code)
	}
}

func TestCAS_Missing_404(t *testing.T) {
	_, router := newTestHTTP(t, time.Now().UTC())

	body := mustJSON(t, CASRequest{ExpectedVersion: 1, Value: "X"})
	req := httptest.NewRequest(http.MethodPost, "/v1/kv/miss:cas", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestCAS_Tombstoned_404(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	st, router := newTestHTTP(t, t0)

	st.Set("z", "A")
	ok := st.Delete("z")
	if !ok {
		t.Fatalf("precondition: delete should succeed")
	}

	body := mustJSON(t, CASRequest{ExpectedVersion: 1, Value: "B"})
	req := httptest.NewRequest(http.MethodPost, "/v1/kv/z:cas", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestCAS_BadJSON_400(t *testing.T) {
	_, router := newTestHTTP(t, time.Now().UTC())

	req := httptest.NewRequest(http.MethodPost, "/v1/kv/x:cas", bytes.NewBufferString(`{"expectedVersion":"not-an-int","value":"B"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}
func TestSWEEP_ExpiredKeys_Tombstoned_200(t *testing.T) { // you can rename to _Removed_ if you want
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	st, router, fc := newTestHTTPWithClock(t, t0)

	// k1 @ +1s, k2 @ +2s, k3 no TTL
	st.SetWithTTL("k1", "A", 1*time.Second)
	st.SetWithTTL("k2", "B", 2*time.Second)
	st.Set("k3", "C")

	// make them expired "now"
	fc.now = fc.now.Add(2 * time.Second)

	// sweep at now (no body)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/sweep", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var got SweepResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Swept != 2 {
		t.Fatalf("swept=%d, want 2", got.Swept)
	}
	if !hasAll(got.Keys, "k1", "k2") {
		t.Fatalf("keys should include k1,k2; got %v", got.Keys)
	}

	// GET checks: k1,k2 gone; k3 present
	for _, k := range []string{"k1", "k2"} {
		reqG := httptest.NewRequest(http.MethodGet, "/v1/kv/"+k, nil)
		recG := httptest.NewRecorder()
		router.ServeHTTP(recG, reqG)
		if recG.Code != http.StatusNotFound {
			t.Fatalf("GET %s after sweep status=%d, want 404", k, recG.Code)
		}
	}
	reqG3 := httptest.NewRequest(http.MethodGet, "/v1/kv/k3", nil)
	recG3 := httptest.NewRecorder()
	router.ServeHTTP(recG3, reqG3)
	if recG3.Code != http.StatusOK {
		t.Fatalf("GET k3 after sweep status=%d, want 200", recG3.Code)
	}
}

func TestSWEEP_NoBody_DefaultNow_NoOp(t *testing.T) {
	// No keys expired at now => sweep 0
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	st, router := newTestHTTP(t, t0)

	st.SetWithTTL("far", "X", 1*time.Hour)

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/sweep", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var got SweepResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Swept != 0 || len(got.Keys) != 0 {
		t.Fatalf("expected no-op sweep, got %+v", got)
	}
}

func TestSWEEP_ExpiredKeys_Removed_200(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	st, router, fc := newTestHTTPWithClock(t, t0)

	// k1 expires at t0+1s, k2 at t0+2s, k3 no TTL
	st.SetWithTTL("k1", "A", 1*time.Second)
	st.SetWithTTL("k2", "B", 2*time.Second)
	st.Set("k3", "C")

	// move clock to t0+2s so k1 and k2 are expired "now"
	fc.now = fc.now.Add(2 * time.Second)

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/sweep", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var got SweepResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Swept != 2 {
		t.Fatalf("swept=%d, want 2", got.Swept)
	}
	if !hasAll(got.Keys, "k1", "k2") {
		t.Fatalf("keys should include k1,k2; got %v", got.Keys)
	}

	// confirm GET now returns 404 for k1 and k2; k3 still present
	for _, k := range []string{"k1", "k2"} {
		reqG := httptest.NewRequest(http.MethodGet, "/v1/kv/"+k, nil)
		recG := httptest.NewRecorder()
		router.ServeHTTP(recG, reqG)
		if recG.Code != http.StatusNotFound {
			t.Fatalf("GET %s after sweep status=%d, want 404", k, recG.Code)
		}
	}
	reqG3 := httptest.NewRequest(http.MethodGet, "/v1/kv/k3", nil)
	recG3 := httptest.NewRecorder()
	router.ServeHTTP(recG3, reqG3)
	if recG3.Code != http.StatusOK {
		t.Fatalf("GET k3 after sweep status=%d, want 200", recG3.Code)
	}
}

func TestSWEEP_Before_NotSupported_400(t *testing.T) {
	_, router := newTestHTTP(t, time.Now().UTC())

	body := mustJSON(t, SweepRequest{Before: time.Now().UTC().Format(time.RFC3339)})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/sweep", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestGET_At_BasicHistory(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	st, router, fc := newTestHTTPWithClock(t, t0)

	eA := st.Set("k", "A") // 12:00
	fc.now = fc.now.Add(5 * time.Minute)
	eB := st.Set("k", "B") // 12:05
	fc.now = fc.now.Add(5 * time.Minute)
	eC := st.Set("k", "C") // 12:10

	// before first write -> 404
	req := httptest.NewRequest(http.MethodGet, "/v1/kv/k?at="+t0.Add(-time.Minute).Format(time.RFC3339), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 before first write", rec.Code)
	}

	// exactly at A -> A
	req = httptest.NewRequest(http.MethodGet, "/v1/kv/k?at="+eA.UpdatedAt.UTC().Format(time.RFC3339), nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 at A", rec.Code)
	}
	var dto EntryDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)
	if dto.Value != "A" || dto.Version != 1 {
		t.Fatalf("want A@v1, got %+v", dto)
	}

	// between B and C (12:07) -> B
	at := eB.UpdatedAt.Add(2 * time.Minute).UTC().Format(time.RFC3339)
	req = httptest.NewRequest(http.MethodGet, "/v1/kv/k?at="+at, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 at 12:07", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)
	if dto.Value != "B" || dto.Version != 2 {
		t.Fatalf("want B@v2 at 12:07, got %+v", dto)
	}

	// long after C -> C
	at = eC.UpdatedAt.Add(10 * time.Hour).UTC().Format(time.RFC3339)
	req = httptest.NewRequest(http.MethodGet, "/v1/kv/k?at="+at, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 long after C", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)
	if dto.Value != "C" || dto.Version != 3 {
		t.Fatalf("want C@v3 long after, got %+v", dto)
	}
}

func TestGET_At_RespectsTTL(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	st, router, _ := newTestHTTPWithClock(t, t0)

	eA := st.SetWithTTL("ttl", "A", 3*time.Minute) // expires 12:03
	if eA.ExpiresAt.IsZero() {
		t.Fatalf("precondition: ExpiresAt should be set")
	}

	// 12:02 -> A
	at := t0.Add(2 * time.Minute).UTC().Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, "/v1/kv/ttl?at="+at, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 at 12:02", rec.Code)
	}

	// 12:03 (expiry boundary) -> 404
	at = t0.Add(3 * time.Minute).UTC().Format(time.RFC3339)
	req = httptest.NewRequest(http.MethodGet, "/v1/kv/ttl?at="+at, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 at expiry boundary", rec.Code)
	}

	// After expiry but before any new write -> 404
	at = t0.Add(4 * time.Minute).UTC().Format(time.RFC3339)
	req = httptest.NewRequest(http.MethodGet, "/v1/kv/ttl?at="+at, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 after expiry before next write", rec.Code)
	}

	// Write B at 12:05 (no TTL) and query 12:06 -> B
	atB := t0.Add(5 * time.Minute).UTC()
	st.Clock.Now() // no-op, just to show we aren’t moving time here
	st.Set("ttl", "B")
	req = httptest.NewRequest(http.MethodGet, "/v1/kv/ttl?at="+atB.Add(time.Minute).Format(time.RFC3339), nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 after B", rec.Code)
	}
}

func TestGET_At_DeleteBarrier(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	st, router, fc := newTestHTTPWithClock(t, t0)

	st.Set("gone", "A")                  // 12:00
	fc.now = fc.now.Add(5 * time.Minute) // 12:05
	if ok := st.Delete("gone"); !ok {
		t.Fatalf("delete should succeed")
	}

	// 12:04 -> A
	req := httptest.NewRequest(http.MethodGet, "/v1/kv/gone?at="+t0.Add(4*time.Minute).Format(time.RFC3339), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 at 12:04", rec.Code)
	}

	// 12:05 (delete time) -> 404
	req = httptest.NewRequest(http.MethodGet, "/v1/kv/gone?at="+t0.Add(5*time.Minute).Format(time.RFC3339), nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 at delete time", rec.Code)
	}

	// 12:06 -> 404
	req = httptest.NewRequest(http.MethodGet, "/v1/kv/gone?at="+t0.Add(6*time.Minute).Format(time.RFC3339), nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 after delete", rec.Code)
	}
}

func TestGET_At_BadFormat_400(t *testing.T) {
	_, router := newTestHTTP(t, time.Now().UTC())

	req := httptest.NewRequest(http.MethodGet, "/v1/kv/k?at=not-a-time", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 for bad 'at'", rec.Code)
	}
}

func TestHistory_NotFound_404(t *testing.T) {
	_, router := newTestHTTP(t, time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC))

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/history/missing", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestHistory_FullAppendOnly_200(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	st, router := newTestHTTP(t, t0)

	// Build history: v1 Set, v2 SetWithTTL, v3 Delete, v4 Set (new lineage)
	st.Set("k", "A")
	st.SetWithTTL("k", "B", time.Minute)
	st.Delete("k")
	st.Set("k", "C")

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/history/k", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}

	var got HistoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Key != "k" {
		t.Fatalf("key=%q, want k", got.Key)
	}
	if len(got.History) != 4 {
		t.Fatalf("history length=%d, want 4", len(got.History))
	}

	// v1
	if got.History[0].Version != 1 || got.History[0].Value != "A" || got.History[0].Deleted {
		t.Fatalf("v1 mismatch: %+v", got.History[0])
	}
	// v2 (has TTL → expiresAt present)
	if got.History[1].Version != 2 || got.History[1].Value != "B" || got.History[1].Deleted {
		t.Fatalf("v2 mismatch: %+v", got.History[1])
	}
	if got.History[1].ExpiresAt == "" {
		t.Fatalf("v2 should include expiresAt (TTL), got empty")
	}
	// v3 tombstone (no value / expiresAt)
	if got.History[2].Version != 3 || !got.History[2].Deleted {
		t.Fatalf("v3 tombstone mismatch: %+v", got.History[2])
	}
	if got.History[2].Value != "" {
		t.Fatalf("tombstone should omit value, got %q", got.History[2].Value)
	}
	if got.History[2].ExpiresAt != "" {
		t.Fatalf("tombstone should omit expiresAt, got %q", got.History[2].ExpiresAt)
	}
	// v4
	if got.History[3].Version != 4 || got.History[3].Value != "C" || got.History[3].Deleted {
		t.Fatalf("v4 mismatch: %+v", got.History[3])
	}

	// Basic timestamp format checks (RFC3339)
	for i, e := range got.History {
		mustRFC3339(t, e.CreatedAt)
		mustRFC3339(t, e.UpdatedAt)
		if e.ExpiresAt != "" {
			mustRFC3339(t, e.ExpiresAt)
		}
		if e.Version <= 0 {
			t.Fatalf("entry %d has non-positive version: %d", i, e.Version)
		}
	}
}

func TestHistory_URLDecodedKey(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	st, router := newTestHTTP(t, t0)

	origKey := "a/b c" // slash + space
	// Write a couple of versions
	st.Set(origKey, "X")
	st.Set(origKey, "Y")

	// URL-encode the key in the path
	pathKey := url.PathEscape(origKey) // "a%2Fb%20c"
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/history/"+pathKey, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}

	var got HistoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Key != origKey {
		t.Fatalf("decoded key=%q, want %q", got.Key, origKey)
	}
	if len(got.History) != 2 {
		t.Fatalf("history length=%d, want 2", len(got.History))
	}
	if got.History[1].Value != "Y" {
		t.Fatalf("latest value=%q, want Y", got.History[1].Value)
	}
}

func TestHistory_OmitsExpiresAtWhenZero(t *testing.T) {
	t0 := time.Date(2025, 8, 19, 12, 0, 0, 0, time.UTC)
	st, router := newTestHTTP(t, t0)

	st.Set("noTTL", "A") // no TTL

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/history/noTTL", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var got HistoryResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.History) != 1 {
		t.Fatalf("history length=%d, want 1", len(got.History))
	}
	if got.History[0].ExpiresAt != "" {
		t.Fatalf("expiresAt should be omitted when zero, got %q", got.History[0].ExpiresAt)
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

func newTestHTTPWithClock(t *testing.T, t0 time.Time) (*store.Store, http.Handler, *fakeClock) {
	t.Helper()
	fc := &fakeClock{now: t0}
	st := store.NewStoreWithClock(fc)
	srv := NewServer(st)
	return st, NewRouter(srv), fc
}

func mustRFC3339(t *testing.T, s string) {
	t.Helper()
	if s == "" {
		t.Fatalf("timestamp must be non-empty RFC3339")
	}
	if _, err := time.Parse(time.RFC3339, s); err != nil {
		t.Fatalf("bad RFC3339 time %q: %v", s, err)
	}
}
