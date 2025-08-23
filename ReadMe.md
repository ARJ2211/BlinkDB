<p align="center">
<img src="https://github.com/ARJ2211/BlinkDB/blob/main/assets/blink_logov2.jpg?raw=true" alt="logo" width="700" height="300"/>
</p>

# 🧠 BlinkDB — Versioned, Time-Traveling, In-Memory KV (TTL, CAS, Tombstones)

A compact, test-driven **Go** key–value store that keeps **full per-key history**, supports **TTL**, exposes **versioned CAS**, and answers **time-travel queries** (“what did this key look like at time _t_?”). Explicit deletes are recorded as **tombstones**, so time travel reflects removals.

![demo](https://raw.githubusercontent.com/ARJ2211/BlinkDB/5c499dd33948aa3b6cdb5817d96731929b7ffe3c/assets/blinkdb.gif)

> **Status**  
> ✅ Store layer (**1E**) complete: versions, TTL, CAS, append-only history, `GetWhen` snapshot semantics, tombstones.  
> ✅ HTTP API built: PUT / GET / GET?at / **GET (list keys)** / DELETE / CAS / SWEEP.  
> ✅ Store made goroutine-safe with `sync.RWMutex` (1G).  
> ✅ Background sweepers and persistence.
> ⏭ Next: persistence (WAL/snapshots or pluggable engines).

---

## Table of Contents

1. [Quickstart](#quickstart)
2. [Features & Guarantees](#features--guarantees)
3. [Run the Server](#run-the-server)
4. [Docker](#docker)
5. [HTTP API](#http-api)
   - [Conventions](#conventions)
   - [DTO Schemas](#dto-schemas)
   - [Endpoints](#endpoints)
6. [Semantics & Examples](#semantics--examples)
7. [Project Structure](#project-structure)
8. [Testing](#testing)
9. [Design Notes](#design-notes)
10. [Concurreny & Locks](#concurrency--locks)
11. [Observability](#observability)
12. [Roadmap](#roadmap)
13. [FAQ](#faq)

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

// 8) List current keys (order not guaranteed)
keys := s.Keys()
```

---

## Features & Guarantees

- **Versioned writes**: monotonically increasing `Version` per key; tombstones also bump version.
- **Time-travel** (`GetWhen`) with **snapshot semantics** and **delete barriers**.
- **TTL** per write; **lazy expiry** on `Get` + **manual sweep** for GC.
- **CAS by version**: `expectedVersion` → update or conflict (409). **TTL preserved** on success.
- **Tombstones** on explicit delete (historical evidence of removal).
- **List keys**: `GET /v1/kv` returns live keys (after lazy expiry & sweeps).
- **RFC3339 UTC** timestamps in API responses.
- **Pretty logging**: HTTP middleware logs method, path, status, duration, and metadata with ANSI colors.

---

## Run the Server

> The HTTP router is in `internal/api/http.go`; server bootstrap in `cmd/server/main.go`.

Start (typical):

```bash
go run ./cmd/server

```

Or choose a custom port

```bash
go run ./cmd/server --port 9000

```

Run with sweeper enabled (default true)

```bash
go run ./cmd/server --sweep-enabled false

```

Run with a dedicated sweeper interval (default 30s)

```bash
go run ./cmd/server --sweep-interval 5s

```

Then hit (default router prefix):

```
GET    /v1/kv
PUT    /v1/kv/{key}
GET    /v1/kv/{key}
GET    /v1/kv/{key}?at=<RFC3339>
POST   /v1/kv/{key}:cas
DELETE /v1/kv/{key}
POST   /v1/admin/sweep
GET    /v1/admin/history/{key}
```

---

## Docker

BlinkDB ships with a Dockerfile and can be run in a container easily.

### Build the image

```bash
docker build -t blinkdb:1.0.0 .
```

### Run the container

```bash
docker run --rm -p 8080:8080 blinkdb:1.0.0
```

This will start BlinkDB on port 8080.

### Using docker-compose

For convenience, you can also use docker-compose:

```yaml
version: "3.9"
services:
  blinkdb:
    image: indianbollulz/blinkdb:1.0.0
    build:
      context: .
      dockerfile: Dockerfile
    ports:
      - "8080:8080"
```

Start it with:

```bash
docker compose up --build
```

Once pushed to Docker Hub, anyone can pull and run:

```bash
docker pull indianbollulz/blinkdb:1.0.0
docker run -p 8080:8080 indianbollulz/blinkdb:1.0.0
```

---

## HTTP API

For more info click [here](https://github.com/ARJ2211/BlinkDB/blob/5c499dd33948aa3b6cdb5817d96731929b7ffe3c/docs/api.md)

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

// KeysResponse (GET /v1/kv)
{
  "keys": ["k1","k2"],
  "size": 2
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

#### GET `/v1/kv` — List live keys

- Returns keys currently present in the live map (after lazy expiry / prior sweeps).
- **200** with `{ "keys": [...], "size": <int> }`. Order is not guaranteed.

```bash
curl -s 'http://localhost:8080/v1/kv'
# {"keys":["k1","k2"],"size":2}
```

---

#### GET `/v1/admin/history/{key}` — Full append-only history

- Returns the **entire history** of `{key}` in chronological order (writes + tombstones).
- **Pure read**: does not lazy-expire or mutate state.
- **200** with `{ "key": "<key>", "history": [EntryDTO...] }`
- **404** if no history exists for the key (even if it’s currently absent).

**Example:**

```bash
curl -s 'http://localhost:8080/v1/admin/history/user:1'
```

Response example:

```json
{
  "key": "user:1",
  "history": [
    {
      "key": "user:1",
      "value": "Alice",
      "version": 1,
      "createdAt": "2025-08-19T12:00:00Z",
      "updatedAt": "2025-08-19T12:00:00Z",
      "deleted": false
    },
    {
      "key": "user:1",
      "value": "Alice*",
      "version": 2,
      "createdAt": "2025-08-19T12:00:00Z",
      "updatedAt": "2025-08-19T12:03:00Z",
      "expiresAt": "2025-08-19T12:05:00Z",
      "deleted": false
    },
    {
      "key": "user:1",
      "version": 3,
      "createdAt": "2025-08-19T12:00:00Z",
      "updatedAt": "2025-08-19T12:06:00Z",
      "deleted": true
    }
  ]
}
```

---

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
│   │   ├── handlers.go       # PUT/GET/GET?at/CAS/DELETE/SWEEP/GET list keys
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

## Concurrency & Locks

Starting with milestone **1G**, the store is fully goroutine-safe.

### How it works

- **RWMutex** wraps all access:
  - **Readers** (`Get`, `Keys`, `GetHistory`, `GetWhen`) use `RLock`.
  - **Writers** (`Set`, `SetWithTTL`, `Delete`, `CASVersion`, `SweepExpired`) use `Lock`.
- **Get** has a special upgrade path:
  - Reads under `RLock`.
  - If expired, it releases `RLock` → takes `Lock` → re-checks expiry before deleting.
- **GetWhen** copies the history slice under `RLock` and searches it after unlocking, ensuring a consistent snapshot.
- **CAS** increments version and appends to history atomically under `Lock`.
- **Delete** appends exactly one tombstone under `Lock`.
- **SweepExpired** deletes expired keys under `Lock` but does not append tombstones.

### Why this matters

- Multiple HTTP requests already run concurrently (each handler is a goroutine).
- Locks prevent map read/write panics and keep history/versions strictly ordered.
- Readers don’t block each other, but writers serialize correctly.

**Run tests with the race detector to validate:**

```bash
go test -race ./...
```

---

## Observability

### Logging

BlinkDB ships with **pretty, colorized HTTP logging middleware** (`observability/logging.go`):

- Color-coded status codes (green 2xx, yellow 4xx, red 5xx).
- Methods colored by verb (GET cyan, POST blue, PUT magenta, DELETE red).
- Bold paths, dimmed metadata (remote, UA, timestamp).
- Works as `http.Handler` middleware.

Sweeper logs also use a human-friendly format with colored removed counts and durations.

### Health & Metrics

- `internal/observability/health.go`: liveness/readiness endpoints.
- `internal/observability/metrics.go`: counters and gauges (planned Prometheus support).

---

## Roadmap

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
