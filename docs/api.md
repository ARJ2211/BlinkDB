# BlinkDB HTTP API — Complete Guide

> **Version:** v1 \
> **Last updated:** 2025-08-20 \
> **Scope:** All public HTTP endpoints exposed by BlinkDB, request/response shapes (DTOs), semantics, edge cases, complexities, and examples.

BlinkDB is a **time-travel caching KV store in Go**. The API lets clients:

- Create/update keys (with optional TTL), read current values, read **as-of** time (time travel), perform CAS by version, **delete with tombstones**, list current keys, **sweep** expired values, and **inspect full history**.
- All timestamps are **RFC3339 UTC** (e.g., `2025-08-19T12:05:00Z`).

---

## Contents

1. [Conventions](#conventions)
2. [DTOs (Schemas)](#dtos-schemas)
3. [Endpoint Matrix](#endpoint-matrix)
4. [Endpoints](#endpoints)
   - [GET /v1/kv — List live keys](#get-v1kv--list-live-keys)
   - [PUT /v1/kv/{key} — Create/Update with TTL rules](#put-v1kvkey--createupdate-with-ttl-rules)
   - [GET /v1/kv/{key} — Read current value](#get-v1kvkey--read-current-value)
   - [GET /v1/kv/{key}?at=RFC3339 — Time travel](#get-v1kvkeyatrfc3339--time-travel)
   - [POST /v1/kv/{key}:cas — Compare-and-Swap by version](#post-v1kvkeycas--compare-and-swap-by-version)
   - [DELETE /v1/kv/{key} — Delete with tombstone](#delete-v1kvkey--delete-with-tombstone)
   - [POST /v1/admin/sweep — GC expired “now”](#post-v1adminsweep--gc-expired-now)
   - [GET /v1/admin/history/{key} — Full append-only history](#get-v1adminhistorykey--full-append-only-history)
5. [Semantics & Guarantees](#semantics--guarantees)
6. [Complexity & Performance](#complexity--performance)
7. [Errors](#errors)
8. [Keys & URL Encoding](#keys--url-encoding)
9. [Observability](#observability)
10. [Testing the API](#testing-the-api)
11. [Change Log](#change-log)

---

## Conventions

- **Base path:** `/v1`
- **Content-Type:** `application/json` for requests with body and all responses.
- **Timestamps:** RFC3339 UTC only.
- **Status codes:** see [Errors](#errors).
- **Idempotency:** DELETE is idempotent at the store level; the HTTP API returns `404` when the key is already gone.
- **Versioning:** Path-based (`/v1/...`).

---

## DTOs (Schemas)

> These are **representations**, not formal JSON Schema. Fields marked “omitempty” are omitted when empty.

### EntryDTO (response)

```json
{
  "key": "k",
  "value": "v",
  "version": 3,
  "createdAt": "2025-08-19T12:00:00Z",
  "updatedAt": "2025-08-19T12:05:00Z",
  "expiresAt": "2025-08-19T12:10:00Z",
  "deleted": false
}
```

Notes:

- `value` and `expiresAt` are omitted for tombstones in actual responses.
- All times are RFC3339 UTC strings.

### PutValueRequest

```json
{
  "value": "Alice",
  "ttlSeconds": 120,
  "expiresAt": "2025-08-19T12:10:00Z",
  "clearTTL": false
}
```

### CASRequest

```json
{
  "expectedVersion": 2,
  "value": "Alice++"
}
```

### DeleteResponse

```json
{
  "entry": {
    "key": "k",
    "version": 4,
    "createdAt": "2025-08-19T12:00:00Z",
    "updatedAt": "2025-08-19T12:07:00Z",
    "deleted": true
  }
}
```

### KeysResponse

```json
{
  "keys": ["k1", "k2"],
  "size": 2
}
```

### SweepRequest

```json
{ "before": "2025-08-19T13:00:00Z" }
```

> **Note:** The `before` field is **not supported** in v1; the server will return **400** if provided.

### SweepResponse

```json
{
  "swept": 2,
  "keys": ["k1", "k2"]
}
```

### HistoryResponse

```json
{
  "key": "user:1",
  "history": []
}
```

> Each item in `history` is an EntryDTO; tombstones have `"deleted": true` and omit `value`/`expiresAt`.

### ErrorResponse

```json
{ "error": "message" }
```

---

## Endpoint Matrix

| Method | Path                      | Purpose                     |
| -----: | ------------------------- | --------------------------- |
|    GET | `/v1/kv`                  | List live keys              |
|    PUT | `/v1/kv/{key}`            | Create/Update (TTL rules)   |
|    GET | `/v1/kv/{key}`            | Read current value          |
|    GET | `/v1/kv/{key}?at=RFC3339` | Time-travel (as-of read)    |
|   POST | `/v1/kv/{key}:cas`        | Compare-and-Swap by version |
| DELETE | `/v1/kv/{key}`            | Delete with tombstone       |
|   POST | `/v1/admin/sweep`         | GC expired at “now”         |
|    GET | `/v1/admin/history/{key}` | Full append-only history    |

---

## Endpoints

### GET `/v1/kv` — List live keys

- Returns keys present in the **live map**. Keys with expired TTL may remain until accessed (lazy delete) or swept.
- **200** with `KeysResponse`. Order is not guaranteed.

**Example**

```bash
curl -s 'http://localhost:8080/v1/kv'
# {"keys":["k1","k2"],"size":2}
```

**Complexity:** O(N) over live keys.

---

### PUT `/v1/kv/{key}` — Create/Update with TTL rules

- Body: `PutValueRequest` (value required).
- **201** on create; **200** on update.
- TTL behavior:
  - `clearTTL=true` → **clear** TTL.
  - `ttlSeconds>0` → set **relative** TTL.
  - `expiresAt` (future) → set **absolute** TTL.
  - **No TTL fields**: if key exists with a non-expired TTL → **preserve** remaining TTL; otherwise write with **no TTL**.
- Errors: `400` for bad JSON, mutually exclusive TTL fields, or past `expiresAt`.

**Create (no TTL)**

```bash
curl -s -X PUT 'http://localhost:8080/v1/kv/user:1'   -H 'Content-Type: application/json'   -d '{"value":"Alice"}'
```

**Set relative TTL**

```bash
curl -s -X PUT 'http://localhost:8080/v1/kv/user:1'   -H 'Content-Type: application/json'   -d '{"value":"Alice","ttlSeconds":90}'
```

**Set absolute TTL**

```bash
curl -s -X PUT 'http://localhost:8080/v1/kv/user:1'   -H 'Content-Type: application/json'   -d '{"value":"Alice","expiresAt":"2025-08-19T12:10:00Z"}'
```

**Clear TTL**

```bash
curl -s -X PUT 'http://localhost:8080/v1/kv/user:1'   -H 'Content-Type: application/json'   -d '{"value":"Alice","clearTTL":true}'
```

**Complexity:** O(1) (map update + history append).

---

### GET `/v1/kv/{key}` — Read current value

- Returns `EntryDTO` when the key exists and is **not expired at now**.
- If TTL is expired (at now), GET lazily removes the entry from the live map and returns **404**.
- Errors: none (besides 404) for the now case.

**Example**

```bash
curl -s 'http://localhost:8080/v1/kv/user:1'
```

**Complexity:** O(1).

---

### GET `/v1/kv/{key}?at=RFC3339` — Time travel

- Returns the version **visible at `at`** (snapshot semantics): pick the most recent write with `UpdatedAt ≤ at` that is **alive at `at`** (`ExpiresAt > at` or no TTL).
- **Delete barrier**: any tombstone with `UpdatedAt ≤ at` hides older values.
- **200** with `EntryDTO`; **404** if none is visible at `at`; **400** for bad timestamp.

**Example**

```bash
curl -s 'http://localhost:8080/v1/kv/user:1?at=2025-08-19T12:05:00Z'
```

**Complexity:** O(log N + K) per key (binary-search + small left scan).

---

### POST `/v1/kv/{key}:cas` — Compare-and-Swap by version

- Body: `CASRequest { expectedVersion, value }`.
- Succeeds only when key exists now and `expectedVersion == currentVersion`.
- **TTL policy**: preserves existing TTL on success.
- **200** on success (returns updated `EntryDTO`), **409** on version mismatch, **404** if missing/expired/tombstoned, **400** on bad JSON or missing `value`.

**Example**

```bash
curl -s -X POST 'http://localhost:8080/v1/kv/user:1:cas'   -H 'Content-Type: application/json'   -d '{"expectedVersion":2,"value":"Alice++"}'
```

**Complexity:** O(1).

---

### DELETE `/v1/kv/{key}` — Delete with tombstone

- Writes a **tombstone** (Deleted=true, Version=prev+1) into history and removes the key from the live map.
- **200** with `DeleteResponse.entry` (tombstone view).
- **404** if already missing/expired (idempotent at API surface).

**Example**

```bash
curl -s -X DELETE 'http://localhost:8080/v1/kv/user:1'
```

**Complexity:** O(1).

---

### POST `/v1/admin/sweep` — GC expired “now”

- No body (or empty body). If a `before` field is provided, returns **400** (unsupported in v1).
- Runs `SweepExpired()` using the server’s clock; **does not create tombstones**.
- **200** with `SweepResponse {swept, keys}` — keys removed from live map.

**Example**

```bash
curl -s -X POST 'http://localhost:8080/v1/admin/sweep'
```

**Complexity:** O(N) over live keys.

---

### GET `/v1/admin/history/{key}` — Full append-only history

- Returns the full per-key history in chronological order: live writes (Deleted=false) **and** tombstones (Deleted=true).
- Pure read: **does not** lazy-expire or mutate state.
- **200** with `HistoryResponse`; **404** if the key has no recorded history.

**Example**

```bash
curl -s 'http://localhost:8080/v1/admin/history/user:1'
```

**Complexity:** O(M) where M is the number of historical entries for the key.

---

## Semantics & Guarantees

- **Versioning:** versions strictly increase per key; tombstones also bump version.
- **CreatedAt:** preserved across updates within a live lineage; after a tombstone, a new Set starts a new lineage.
- **TTL policy:** per write; CAS **preserves** existing TTL; PUT with no TTL fields **preserves** non-expired TTL, else clears.
- **Lazy expiry:** GET evicts entries whose `ExpiresAt ≤ now`. History remains unchanged.
- **Sweep:** a bulk GC at “now” that only affects the live map (no tombstones).
- **Time travel:** snapshot semantics; delete barriers; `ExpiresAt == at` is **not visible**.
- **Ties:** if several writes share the same `UpdatedAt`, append order wins; a same-timestamp delete beats a set.

---

## Complexity & Performance

| Operation / Endpoint               | Complexity (average) |
| ---------------------------------- | -------------------- |
| PUT / Set / SetWithTTL / CAS / Del | O(1)                 |
| GET (now)                          | O(1)                 |
| GET (as-of `?at=`)                 | O(log N + K)         |
| GET /v1/kv (list)                  | O(N)                 |
| POST /v1/admin/sweep               | O(N)                 |
| GET /v1/admin/history/{key}        | O(M)                 |

> N = number of live keys; M = history length for the key; K = skipped expired-by-`at` versions during scan.

---

## Errors

All errors use the envelope:

```json
{ "error": "<message>" }
```

**Common status codes**

| Code | When                                                                    |
| ---: | ----------------------------------------------------------------------- |
|  200 | Success (GET/PUT update/CAS/DELETE/SWEEP)                               |
|  201 | Created (PUT new key)                                                   |
|  400 | Bad JSON, invalid TTL combo, bad `at`/`expiresAt`, unsupported `before` |
|  404 | Key missing/expired/tombstoned; or no history                           |
|  409 | CAS version mismatch                                                    |

---

## Keys & URL Encoding

- Keys are path segments. Characters like spaces or `:` work as-is.
- Avoid raw `/` within keys; if needed, URL-encode (`/` → `%2F`).
- The history endpoint accepts URL-encoded keys and will unescape them server-side.

---

## Observability

- **Console logs:** pretty colorized logs per request (status, latency, bytes, UA).
- **Health:** `GET /healthz` responds `200 OK`.
- **Error envelope:** consistent JSON for errors across endpoints.

> In dev, enable pretty logs; in prod, consider JSON logs and external aggregation.

---

## Testing the API

- Table-driven tests in `internal/api/api_test.go`.
- Run:

```bash
go test ./internal/api -v
```

- End-to-end smoke (server):

```bash
go run ./cmd/server &
sleep 0.2
curl -s http://localhost:8080/healthz
```

---

## Change Log

- **v1.0** — Initial HTTP surface: PUT/GET/GET?at/CAS/DELETE, list keys, admin sweep, admin history.
