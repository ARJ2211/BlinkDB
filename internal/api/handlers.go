package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/ARJ2211/blinkdb/internal/store"
)

// ---------- PUT /v1/kv/{key} ----------

func (srv *Server) PutValue(w http.ResponseWriter, r *http.Request) {
	key, ok := getKey(r)
	if !ok || key == "" {
		writeError(w, http.StatusBadRequest, "missing key")
		return
	}

	// Decode request
	var req PutValueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	if req.Value == "" {
		writeError(w, http.StatusBadRequest, "value is required")
		return
	}

	// Did the key exist before? Grab it so we can preserve TTL if needed.
	old, existed := srv.S.Get(key)
	now := srv.S.Clock.Now()

	// TTL rules
	if req.ClearTTL {
		ent := srv.S.Set(key, req.Value)
		writeEntryDTO(w, existed, key, ent)
		return
	}

	if req.TTLSeconds > 0 && req.ExpiresAt != "" {
		writeError(w, http.StatusBadRequest, "ttlSeconds and expiresAt are mutually exclusive")
		return
	}

	if req.TTLSeconds > 0 {
		ent := srv.S.SetWithTTL(key, req.Value, time.Duration(req.TTLSeconds)*time.Second)
		writeEntryDTO(w, existed, key, ent)
		return
	}

	if req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "expiresAt must be RFC3339")
			return
		}
		d := t.Sub(now)
		if d <= 0 {
			writeError(w, http.StatusBadRequest, "expiresAt must be in the future")
			return
		}
		ent := srv.S.SetWithTTL(key, req.Value, d)
		writeEntryDTO(w, existed, key, ent)
		return
	}

	// No TTL fields provided:
	// - If NEW key  -> Set (no TTL)
	// - If EXISTING -> keep prior TTL (if any)
	if existed && !old.ExpiresAt.IsZero() && (old.ExpiresAt.After(now)) {
		remaining := old.ExpiresAt.Sub(now)
		ent := srv.S.SetWithTTL(key, req.Value, remaining)
		writeEntryDTO(w, existed, key, ent)
		return
	}

	ent := srv.S.Set(key, req.Value)
	writeEntryDTO(w, existed, key, ent)
}

// Helper: map store.Entry → EntryDTO and write JSON with 201/200
func writeEntryDTO(w http.ResponseWriter, existed bool, key string, ent store.Entry) {
	dto := EntryDTO{
		Key:       key,
		Value:     ent.Value,
		Version:   ent.Version,
		CreatedAt: ent.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: ent.UpdatedAt.UTC().Format(time.RFC3339),
		Deleted:   ent.Deleted,
	}
	if !ent.ExpiresAt.IsZero() {
		dto.ExpiresAt = ent.ExpiresAt.UTC().Format(time.RFC3339)
	}
	status := http.StatusCreated
	if existed {
		status = http.StatusOK
	}
	writeJSON(w, status, dto)
}

// ---------- Stubs for the rest (we’ll fill later) ----------

func (srv *Server) GetValue(w http.ResponseWriter, r *http.Request) {
	key, ok := getKey(r)
	if !ok || key == "" {
		writeError(w, http.StatusBadRequest, "missing key")
		return
	}

	e, found := srv.S.Get(key)
	if !found {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	dto := EntryDTO{
		Key:       key,
		Value:     e.Value,
		Version:   e.Version,
		CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: e.UpdatedAt.UTC().Format(time.RFC3339),
		Deleted:   e.Deleted,
	}
	if !e.ExpiresAt.IsZero() {
		dto.ExpiresAt = e.ExpiresAt.UTC().Format(time.RFC3339)
	}

	writeJSON(w, http.StatusOK, dto)
}

// CASValue: POST /v1/kv/{key}:cas
// Body: { "expectedVersion": <int>, "value": "<string>" }
// Behavior:
//   - 200 on success (TTL preserved)
//   - 404 if key missing/expired/tombstoned
//   - 409 if expectedVersion != current version
func (srv *Server) CASValue(w http.ResponseWriter, r *http.Request) {
	key, ok := getKey(r)
	if !ok || key == "" {
		writeError(w, http.StatusBadRequest, "missing key")
		return
	}

	var req CASRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	if req.Value == "" {
		writeError(w, http.StatusBadRequest, "value is required")
		return
	}

	// quick existence check (treat tombstone/expired as not found)
	if _, found := srv.S.Get(key); !found {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	updated, ok := srv.S.CASVersion(key, req.ExpectedVersion, req.Value)
	if !ok {
		// Distinguish conflict (version mismatch) vs "became not found" (expired/raced)
		if _, still := srv.S.Get(key); still {
			writeError(w, http.StatusConflict, "version conflict")
		} else {
			writeError(w, http.StatusNotFound, "not found")
		}
		return
	}

	// success → 200, TTL preserved by store
	dto := EntryDTO{
		Key:       key,
		Value:     updated.Value,
		Version:   updated.Version,
		CreatedAt: updated.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: updated.UpdatedAt.UTC().Format(time.RFC3339),
		Deleted:   updated.Deleted,
	}
	if !updated.ExpiresAt.IsZero() {
		dto.ExpiresAt = updated.ExpiresAt.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, dto)
}

func (srv *Server) DeleteValue(w http.ResponseWriter, r *http.Request) {
	key, ok := getKey(r)
	if !ok || key == "" {
		writeError(w, http.StatusBadRequest, "missing key")
		return
	}

	cur, found := srv.S.Get(key)
	if !found {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	// Perform delete in the store (idempotent: returns false if already gone)
	if ok := srv.S.Delete(key); !ok {
		// if store says false, treat as not found at API level
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	now := srv.S.Clock.Now().UTC()

	// Build tombstone view for response (does not include value)
	dto := EntryDTO{
		Key:       key,
		Version:   cur.Version + 1,
		CreatedAt: cur.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: now.Format(time.RFC3339),
		Deleted:   true,
		// ExpiresAt omitted by omitempty
	}

	writeJSON(w, http.StatusOK, DeleteResponse{Entry: dto})
}

// POST /v1/admin/sweep
// Request: { "before": "RFC3339" }  -> NOT supported (return 400)
// Behavior: sweep at "now" using store.SweepExpired(); returns how many and which keys were removed.
// Response: { "swept": <int>, "keys": ["k1", "k2", ...] }
func (srv *Server) SweepExpired(w http.ResponseWriter, r *http.Request) {
	var req SweepRequest
	if r.Body != nil {
		// Accept empty body (EOF). Real JSON errors -> 400.
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "bad JSON body")
			return
		}
	}

	// v1: we only support sweeping "now" (store.SweepExpired); explicit 'before' is not supported.
	if req.Before != "" {
		writeError(w, http.StatusBadRequest, "before is not supported")
		return
	}

	// Keys before sweep
	before := srv.S.Keys()
	beforeSet := make(map[string]struct{}, len(before))
	for _, k := range before {
		beforeSet[k] = struct{}{}
	}

	// Perform sweep at now (uses store clock)
	n := srv.S.SweepExpired()

	// Keys after sweep
	after := srv.S.Keys()
	afterSet := make(map[string]struct{}, len(after))
	for _, k := range after {
		afterSet[k] = struct{}{}
	}

	// Compute removed = before \ after
	removed := make([]string, 0, n)
	for k := range beforeSet {
		if _, still := afterSet[k]; !still {
			removed = append(removed, k)
		}
	}

	writeJSON(w, http.StatusOK, SweepResponse{Swept: n, Keys: removed})
}
