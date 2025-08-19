package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/ARJ2211/blinkdb/internal/store"
)

//
// HTTP handlers for the versioned KV API.
//
// Conventions/Policies implemented here:
//
// • Timestamps: all serialized timestamps are RFC3339 in UTC.
// • Status codes:
//   - 201 Created  → new key created by PUT
//   - 200 OK       → successful GET/PUT(update)/DELETE/CAS/SWEEP
//   - 400 BadRequest → invalid JSON or invalid inputs (e.g., bad RFC3339, illegal TTL combo)
//   - 404 NotFound → key not present *at the requested time* (missing/expired/tombstoned)
//   - 409 Conflict → CAS version mismatch
//
// • TTL semantics:
//   - PUT with neither ttlSeconds nor expiresAt:
//       * If key is NEW    → create without TTL
//       * If key EXISTS    → preserve existing TTL (if still in the future)
//   - PUT with clearTTL: remove TTL regardless of prior state
//   - PUT with ttlSeconds: set relative TTL
//   - PUT with expiresAt: set absolute TTL (must be strictly in the future)
//   - CAS always preserves existing TTL on success.
// • Delete semantics:
//   - DELETE writes a tombstone (version++, Deleted=true) via the store.
// • Sweep semantics:
//   - /v1/admin/sweep performs GC at “now”: removes expired keys from the live map.
//   - Sweep does NOT write tombstones.
//   - Request field `before` is not supported (returns 400).
//

// ---------- PUT /v1/kv/{key} ----------

// PutValue upserts a value for {key} with TTL options.
// Behavior summary:
//   - 201 when creating a new key; 200 when updating an existing key.
//   - If no TTL fields are provided and the key already exists, we preserve its TTL
//     by computing the remaining duration and calling SetWithTTL.
//   - TTL selection precedence:
//     clearTTL → strip TTL
//     ttlSeconds → set relative TTL
//     expiresAt  → parse RFC3339, must be in the future; set absolute TTL
//     none       → new key (no TTL) / existing key (preserve TTL)
func (srv *Server) PutValue(w http.ResponseWriter, r *http.Request) {
	key, ok := getKey(r)
	if !ok || key == "" {
		writeError(w, http.StatusBadRequest, "missing key")
		return
	}

	// Decode body
	var req PutValueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	if req.Value == "" {
		writeError(w, http.StatusBadRequest, "value is required")
		return
	}

	// Check if key exists to choose status code and potentially preserve TTL.
	old, existed := srv.S.Get(key)
	now := srv.S.Clock.Now()

	// TTL rule: explicit clear beats everything else.
	if req.ClearTTL {
		ent := srv.S.Set(key, req.Value)
		writeEntryDTO(w, existed, key, ent)
		return
	}

	// TTL rule: cannot specify both relative and absolute expiry.
	if req.TTLSeconds > 0 && req.ExpiresAt != "" {
		writeError(w, http.StatusBadRequest, "ttlSeconds and expiresAt are mutually exclusive")
		return
	}

	// TTL rule: relative TTL
	if req.TTLSeconds > 0 {
		ent := srv.S.SetWithTTL(key, req.Value, time.Duration(req.TTLSeconds)*time.Second)
		writeEntryDTO(w, existed, key, ent)
		return
	}

	// TTL rule: absolute TTL
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

	// No TTL fields:
	// • New key  → create without TTL
	// • Existing → preserve existing (unexpired) TTL
	if existed && !old.ExpiresAt.IsZero() && old.ExpiresAt.After(now) {
		remaining := old.ExpiresAt.Sub(now)
		ent := srv.S.SetWithTTL(key, req.Value, remaining)
		writeEntryDTO(w, existed, key, ent)
		return
	}

	// Plain set (no TTL).
	ent := srv.S.Set(key, req.Value)
	writeEntryDTO(w, existed, key, ent)
}

// writeEntryDTO converts a store.Entry to an API EntryDTO and writes JSON.
// It also picks the correct status code (201 for new keys, 200 for updates).
// Note: ExpiresAt is omitted when zero, per json:",omitempty".
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

// ---------- GET /v1/kv/{key} (supports ?at=RFC3339) ----------

// GetValue returns the current value for {key}, or a historical value when
// the optional query parameter `at` is provided.
//
// • Without `at`: uses store.Get (respects lazy delete/expiry at "now").
// • With `at`: parses RFC3339 and uses store.GetWhen to time-travel read.
//   - 400 on bad time format
//   - 404 when the key is not visible at that instant (missing, expired by T, or tombstoned at/ before T).
func (srv *Server) GetValue(w http.ResponseWriter, r *http.Request) {
	key, ok := getKey(r)
	if !ok || key == "" {
		writeError(w, http.StatusBadRequest, "missing key")
		return
	}

	// Time-travel read: ?at=<RFC3339>
	if at := r.URL.Query().Get("at"); at != "" {
		t, err := time.Parse(time.RFC3339, at)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad 'at' time (RFC3339 required)")
			return
		}
		e, found := srv.S.GetWhen(key, t)
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
		return
	}

	// Normal "now" read.
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

// ---------- POST /v1/kv/{key}:cas ----------
//
// CASValue performs a Compare-And-Swap using the current version.
// Request: { "expectedVersion": <int>, "value": "<string>" }
// Responses:
//   - 200 OK     → CAS succeeded (value updated, version++); TTL preserved by the store
//   - 404 NotFound → key missing/expired/tombstoned at "now"
//   - 409 Conflict → version mismatch (key still present)
//   - 400 BadRequest → invalid JSON / missing value
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

	// Quick existence check so we can distinguish 404 vs 409 later.
	if _, found := srv.S.Get(key); !found {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	updated, ok := srv.S.CASVersion(key, req.ExpectedVersion, req.Value)
	if !ok {
		// If the key still exists, it's a version conflict (409).
		// If it disappeared (expired/lazy deleted) in between, return 404.
		if _, still := srv.S.Get(key); still {
			writeError(w, http.StatusConflict, "version conflict")
		} else {
			writeError(w, http.StatusNotFound, "not found")
		}
		return
	}

	// Success: map store.Entry → DTO (TTL preserved by store policy).
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

// ---------- DELETE /v1/kv/{key} ----------
//
// DeleteValue removes the key and appends a tombstone in history.
// Store.Delete handles writing the tombstone; we return a tombstone "view":
//   - version = previous version + 1
//   - deleted = true
//   - createdAt = original CreatedAt
//   - updatedAt = now
//
// ExpiresAt is omitted (tombstones do not carry TTL).
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

	// Store.Delete is idempotent and returns false if already gone.
	if ok := srv.S.Delete(key); !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	now := srv.S.Clock.Now().UTC()

	dto := EntryDTO{
		Key:       key,
		Version:   cur.Version + 1,
		CreatedAt: cur.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: now.Format(time.RFC3339),
		Deleted:   true, // tombstone
	}
	writeJSON(w, http.StatusOK, DeleteResponse{Entry: dto})
}

// ---------- POST /v1/admin/sweep ----------
//
// SweepExpired performs a one-shot GC at "now" (store clock) and removes any keys
// whose TTL has expired. This does NOT write tombstones and is distinct from DELETE.
//
// Request body:
//   - currently we do not support a "before" parameter; if provided, returns 400.
//
// Response body:
//
//	{ "swept": <int>, "keys": ["k1","k2", ...] } where keys are the ones removed.
//
// Implementation detail: to provide the names of removed keys, we compute a
// before/after set difference around the store’s SweepExpired() call.
func (srv *Server) SweepExpired(w http.ResponseWriter, r *http.Request) {
	var req SweepRequest
	if r.Body != nil {
		// Accept empty body (EOF); treat only real decoding errors as 400.
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "bad JSON body")
			return
		}
	}
	// v1: "before" is intentionally not supported to match store API.
	if req.Before != "" {
		writeError(w, http.StatusBadRequest, "before is not supported")
		return
	}

	// Record keys pre-sweep.
	before := srv.S.Keys()
	beforeSet := make(map[string]struct{}, len(before))
	for _, k := range before {
		beforeSet[k] = struct{}{}
	}

	// Perform the sweep at "now" (store.clock.Now()).
	n := srv.S.SweepExpired()

	// Record keys post-sweep.
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
