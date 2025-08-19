package api

import (
	"encoding/json"
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

func (srv *Server) CASValue(w http.ResponseWriter, r *http.Request) {
	if _, ok := getKey(r); !ok {
		writeError(w, http.StatusBadRequest, "missing key")
		return
	}
	writeError(w, http.StatusNotImplemented, "not implemented")
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

func (srv *Server) SweepExpired(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}
