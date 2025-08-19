# 🧠 BlinkDB — Versioned, Time-Traveling, In-Memory KV (TTL, CAS, Tombstones)

A compact, test-driven **Go** key–value store that keeps **full per-key history**, supports **TTL**, exposes **versioned CAS**, and answers **time-travel queries** (“what did this key look like at time _t_?”). Explicit deletes are recorded as **tombstones**, so time travel reflects removals.

> **Status**  
> ✅ Store layer (**1E**) complete: versions, TTL, CAS, append-only history, `GetWhen` snapshot semantics, tombstones.  
> ✅ HTTP API built: PUT / GET / GET?at / DELETE / CAS / SWEEP.  
> ⏭ Next (1D): add `RWMutex` for concurrency.

---

## Table of Contents

1. [Quickstart](#quickstart)
2. [Features & Guarantees](#features--guarantees)
3. [Run the Server](#run-the-server)
4. [HTTP API](#http-api)
   - [Conventions](#conventions)
   - [DTO Schemas](#dto-schemas)
   - [Endpoints](#endpoints)
5. [Semantics & Examples](#semantics--examples)
6. [Project Structure](#project-structure)
7. [Testing](#testing)
8. [Design Notes](#design-notes)
9. [Roadmap](#roadmap)
10. [FAQ](#faq)

---

## Quickstart

```bash
git clone <your-fork-or-repo>
cd blinkdb
go mod tidy
go test ./...
```

**Store usage (in code):**

```go
s := store.NewStore()

// 1) Create
e1 := s.Set("user:1", "Alice") // v1

// 2) Update with TTL (expires in 2 minutes)
e2 := s.SetWithTTL("user:1", "Alice*", 2*time.Minute) // v2

// 3) CAS by version (preserves TTL)
ok, e3 := s.CASVersion("user:1", e2.Version, "Alice ✅") // v3 if ok

// 4) Read latest (lazy-deletes expired)
cur, ok := s.Get("user:1")

// 5) Time travel (snapshot semantics)
t := e2.UpdatedAt.Add(30 * time.Second)
past, ok := s.GetWhen("user:1", t)

// 6) Delete (tombstone, version++)
s.Delete("user:1")

// 7) Sweep expired (GC at “now”)
n := s.SweepExpired()
```

---

## Features & Guarantees

- **Versioned writes**: monotonically increasing `Version` per key; tombstones also bump version.
- **Time-travel** (`GetWhen`) with **snapshot semantics** and **delete barriers**.
- **TTL** per write; **lazy expiry** on `Get` + **manual sweep** for GC.
- **CAS by version**: `expectedVersion` → update or conflict (409). **TTL preserved** on success.
- **Tombstones** on explicit delete (historical evidence of removal).
- **RFC3339 UTC** timestamps in API responses.

---

## Run the Server

> The HTTP router is in `internal/api/http.go`; server bootstrap in `cmd/server/main.go`.

Start (typical):

```bash
go run ./cmd/server
```

Then hit (default router prefix):

```
PUT    /v1/kv/{key}
GET    /v1/kv/{key}
GET    /v1/kv/{key}?at=<RFC3339>
POST   /v1/kv/{key}:cas
DELETE /v1/kv/{key}
POST   /v1/admin/sweep
```

---

## HTTP API

### Conventions

- **Content-Type**: `application/json` for all requests with a body and all responses.
- **Timestamps**: RFC3339, always UTC (e.g., `2025-08-19T12:05:00Z`).
- **Errors**: JSON envelope `{ "error": "<message>" }`.
- **Status codes**:
  - `201 Created` – new key via PUT
  - `200 OK` – success (GET/PUT update/DELETE/CAS/SWEEP)
  - `400 Bad Request` – invalid JSON, bad TTL combo, bad `at`/`expiresAt`, or unsupported `before`
  - `404 Not Found` – key missing/expired/tombstoned (at the time of the request)
  - `409 Conflict` – CAS version mismatch

### DTO Schemas

```json
// EntryDTO (response)
{
  "key": "k",
  "value": "v",              // omitted for tombstones
  "version": 3,
  "createdAt": "2025-08-19T12:00:00Z",
  "updatedAt": "2025-08-19T12:05:00Z",
  "expiresAt": "2025-08-19T12:10:00Z", // omitted if no TTL
  "deleted": false
}

// PutValueRequest
{
  "value": "Alice",
  "ttlSeconds": 120,         // optional; mutually exclusive with expiresAt
  "expiresAt": "2025-08-19T12:10:00Z", // optional; mutually exclusive with ttlSeconds; must be in future
  "clearTTL": false          // optional; if true, ignores ttlSeconds/expiresAt and clears TTL
}

// CASRequest
{
  "expectedVersion": 2,
  "value": "Alice++"
}

// DeleteResponse
{
  "entry": {
    "key": "k",
    "version": 4,
    "createdAt": "2025-08-19T12:00:00Z",
    "updatedAt": "2025-08-19T12:07:00Z",
    "deleted": true
  }
}

// SweepRequest (NOTE: "before" is NOT supported; will 400 if provided)
{ "before": "2025-08-19T13:00:00Z" }

// SweepResponse
{
  "swept": 2,
  "keys": ["k1","k2"]
}

// ErrorResponse
{ "error": "not found" }
```

### Endpoints

#### PUT `/v1/kv/{key}` — Create/Update with TTL rules

- **Body**: `PutValueRequest`
- **201** on create; **200** on update.
- **TTL policy**:
  - `clearTTL=true` → strip TTL
  - `ttlSeconds` → set relative TTL
  - `expiresAt` → set absolute TTL (future only)
  - **none** → if key exists, **preserve existing TTL**; otherwise no TTL
- **Errors**: `400` for bad JSON, invalid TTL combo, or past `expiresAt`.

**Create:**

```bash
curl -s -X PUT 'http://localhost:8080/v1/kv/user:1'   -H 'Content-Type: application/json'   -d '{"value":"Alice"}'
# 201 Created
```

**Update preserving TTL:**

```bash
# assume user:1 currently has a TTL
curl -s -X PUT 'http://localhost:8080/v1/kv/user:1'   -H 'Content-Type: application/json'   -d '{"value":"Alice*"}'
# 200 OK; expiresAt unchanged
```

**Set TTL (relative):**

```bash
curl -s -X PUT 'http://localhost:8080/v1/kv/user:1'   -H 'Content-Type: application/json'   -d '{"value":"Alice","ttlSeconds":90}'
```

**Set TTL (absolute):**

```bash
curl -s -X PUT 'http://localhost:8080/v1/kv/user:1'   -H 'Content-Type: application/json'   -d '{"value":"Alice","expiresAt":"2025-08-19T12:10:00Z"}'
```

**Clear TTL:**

```bash
curl -s -X PUT 'http://localhost:8080/v1/kv/user:1'   -H 'Content-Type: application/json'   -d '{"value":"Alice","clearTTL":true}'
```

---

#### GET `/v1/kv/{key}` — Read current value

- **200** with `EntryDTO` if present and not expired; **404** if missing/expired/tombstoned.

```bash
curl -s 'http://localhost:8080/v1/kv/user:1'
```

#### GET `/v1/kv/{key}?at=<RFC3339>` — Time-travel read

- **200** with `EntryDTO` for the version **visible at `at`**.
- **404** if no version is alive at `at` (including delete barrier).
- **400** for bad time format.

```bash
curl -s 'http://localhost:8080/v1/kv/user:1?at=2025-08-19T12:05:00Z'
```

---

#### POST `/v1/kv/{key}:cas` — Compare-and-Swap (by version)

- **Body**: `CASRequest { expectedVersion, value }`
- **200** on success (version++, TTL preserved).
- **409** if `expectedVersion` doesn’t match current live version.
- **404** if the key is missing/expired/tombstoned at “now”.
- **400** on bad JSON or missing value.

```bash
curl -s -X POST 'http://localhost:8080/v1/kv/user:1:cas'   -H 'Content-Type: application/json'   -d '{"expectedVersion":2,"value":"Alice++"}'
```

---

#### DELETE `/v1/kv/{key}` — Delete with tombstone

- **200** with a **tombstone view** (Deleted=true, Version=prev+1).
- **404** if key is already missing/expired/tombstoned.

```bash
curl -s -X DELETE 'http://localhost:8080/v1/kv/user:1'
```

---

#### POST `/v1/admin/sweep` — GC expired keys (at “now”)

- **No body** (or empty body).
- Runs `SweepExpired()` using the store’s clock; **does not** write tombstones.
- Responds with `{swept, keys}` — keys removed by the sweep.
- **400** if a `before` field is provided (unsupported).

```bash
curl -s -X POST 'http://localhost:8080/v1/admin/sweep'
```

---

## Semantics & Examples

- **Lazy expiry on GET**: if `ExpiresAt ≤ now`, the key is evicted from the live map and GET returns `404`. History is untouched.
- **Sweep**: bulk GC for expired entries at **now**; returns how many and which keys were removed; **no tombstones**.
- **Time-travel (`?at=`)**:
  - Picks the most recent version with `UpdatedAt ≤ at` that’s **alive at `at`** (no TTL or `ExpiresAt > at`).
  - **Delete barrier**: a tombstone at/≤`at` hides earlier values.
  - Ties on `UpdatedAt` are resolved by append order; a same-timestamp delete wins over a set.

**Mini timeline (delete barrier):**

```
12:00  Set(k,"A")        -> v1
12:05  Delete(k)         -> v2 (tombstone)
12:07  Set(k,"B")        -> v3

GET k?at=12:04Z  -> "A"
GET k?at=12:05Z  -> 404 (delete at t)
GET k?at=12:06Z  -> 404
GET k?at=12:07Z  -> "B"
```

**TTL example:**

```
12:00  SetWithTTL(k,"A", 3m) -> v1, ExpiresAt=12:03
12:02  GET k?at=12:02Z       -> "A"
12:03  GET k?at=12:03Z       -> 404 (boundary not visible)
12:05  Set(k,"B")            -> v2, no TTL
12:06  GET k?at=12:06Z       -> "B"
```

---

## Project Structure

```
.
├── LICENSE
├── ReadMe.md
├── cmd
│   └── server
│       └── main.go
├── docs
│   ├── api.md
│   ├── design.md
│   └── perf.md
├── go.mod
├── internal
│   ├── api
│   │   ├── api_test.go       # API handler tests (table-driven)
│   │   ├── dto.go            # JSON DTOs (EntryDTO, PutValueRequest, etc.)
│   │   ├── handlers.go       # PUT/GET/GET?at/CAS/DELETE/SWEEP
│   │   └── http.go           # router wiring & server
│   ├── config
│   │   └── config.go
│   ├── observability
│   │   ├── health.go
│   │   ├── logging.go
│   │   └── metrics.go
│   └── store
│       ├── entry.go          # Entry type & helpers
│       ├── errors.go
│       ├── store.go          # Store logic, history, TTL, GetWhen, SweepExpired
│       └── store_test.go     # Store unit tests (acceptance, TTL, CAS, GetWhen)
├── main
└── scripts
```

---

## Testing

Run all tests:

```bash
go test ./...
```

API-only tests:

```bash
go test ./internal/api -v
```

(After 1D) Run with race detector:

```bash
go test -race ./...
```

---

## Design Notes

- **History is the source of truth** for time travel; the live map is a cache of “now.”
- **Snapshot semantics** make `GetWhen` predictable and auditable.
- **Tombstones** ensure deletes are visible in history and act as time-travel barriers.
- **CAS preserves TTL** by design (clearly tested & documented).
- **Lazy vs eager expiry**: we chose lazy expiry on reads for hot-path simplicity; `SweepExpired` provides explicit cleanup.

---

## Roadmap

- **1D**: Add `sync.RWMutex` to make the store goroutine-safe; audit lazy-delete path under locks; add concurrency tests.
- **Background sweeper**: optional goroutine to call `SweepExpired()` periodically.
- **History inspection**: paged history export / debug endpoints.
- **Persistence**: optional WAL/snapshots or pluggable engines (Bolt/Badger/Pebble).
- **Metrics**: hit/miss, expirations, CAS success rate, sweep counts; tracing with OpenTelemetry.

---

## FAQ

**Why does DELETE return a tombstone view?**  
So clients can observe the new version (version++), the delete time, and confirm lineage.

**Why does SWEEP not write tombstones?**  
Sweep is GC for expired entries, not a user-intent delete. It only affects the live map.

**What happens if TTL expires exactly at `at`?**  
Boundary is **not visible**: `ExpiresAt == at` → considered expired for `GetWhen`.

**Can I sweep at a specific `before` time?**  
Not in v1. The endpoint runs GC at “now”. If needed, add a `SweepExpiredBefore(t)` store API and wire it into HTTP later.

---

Happy hacking! If you change a behavior (e.g., CAS TTL policy), please update both tests and this README to keep them aligned.
