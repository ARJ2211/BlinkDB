package api

// EntryDTO is the external shape returned to clients.
// All timestamps are RFC3339 UTC strings.
type EntryDTO struct {
	Key       string `json:"key"`
	Value     string `json:"value,omitempty"` // omitted for tombstones
	Version   int64  `json:"version"`
	CreatedAt string `json:"createdAt"`           // RFC3339 UTC
	UpdatedAt string `json:"updatedAt"`           // RFC3339 UTC
	ExpiresAt string `json:"expiresAt,omitempty"` // RFC3339 UTC, optional
	Deleted   bool   `json:"deleted"`             // true for tombstones
}

// PutValueRequest is the body for PUT /v1/kv/{key}.
type PutValueRequest struct {
	Value      string `json:"value"`                // required
	TTLSeconds int    `json:"ttlSeconds,omitempty"` // optional, >=1
	ExpiresAt  string `json:"expiresAt,omitempty"`  // optional RFC3339; mutually exclusive with TTLSeconds
	ClearTTL   bool   `json:"clearTTL,omitempty"`   // optional; if true, ignore TTLSeconds/ExpiresAt and clear TTL
}

// CASRequest is the body for POST /v1/kv/{key}:cas.
// CAS preserves any existing TTL; clients do not send TTL here.
type CASRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"` // required
	Value           string `json:"value"`           // required
}

// DeleteResponse is returned by DELETE /v1/kv/{key}.
type DeleteResponse struct {
	Entry EntryDTO `json:"entry"`
}

// KeysResponse is returned by GET /v1/kv (no key).
type KeysResponse struct {
	Keys []string `json:"keys"`
	Size int      `json:"size"`
}

// SweepRequest is the body for POST /v1/admin/sweep.
type SweepRequest struct {
	Before string `json:"before,omitempty"` // optional RFC3339; default server "now"
}

// SweepResponse is returned by POST /v1/admin/sweep.
type SweepResponse struct {
	Swept int      `json:"swept"` // number of keys affected
	Keys  []string `json:"keys"`  // list of affected keys
}

// ErrorResponse is a minimal error envelope for all error cases.
type ErrorResponse struct {
	Error string `json:"error"`
}
