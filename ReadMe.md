# 🧠 BlinkDB — Versioned, Time‑Traveling, In‑Memory KV (with TTL & Tombstones)

A compact, test‑driven **Go** key–value store that keeps **full per‑key history**, supports **TTL**, exposes **atomic CAS**, and answers **time‑travel queries** (“what did this key look like at time _t_?”). Explicit deletes are recorded as **tombstones**, so time travel reflects removals.

> **Status:** Milestone **1E** (store layer) is complete — versions, TTL, CAS, append‑only history, `GetWhen` with **snapshot semantics**, and **tombstones** ✅  
> Next steps are an API layer (HTTP/JSON) and concurrency (mutex) in **1D**.

---

## Table of Contents

1. [Quickstart](#quickstart)
2. [Core Ideas](#core-ideas)
3. [API Surface](#api-surface)
4. [Data Model & Invariants](#data-model--invariants)
5. [Semantics (with Examples)](#semantics-with-examples)
   - [Set / SetWithTTL](#set--setwithttl)
   - [Get (lazy delete)](#get-lazy-delete)
   - [CAS (Compare-And-Set)](#cas-compare-and-set)
   - [Delete (tombstones)](#delete-tombstones)
   - [GetWhen (time travel)](#getwhen-time-travel)
6. [Complexity](#complexity)
7. [Thread Safety](#thread-safety)
8. [Testing](#testing)
9. [Design Decisions & Rationale](#design-decisions--rationale)
10. [Common Questions (FAQ)](#common-questions-faq)
11. [Extensibility & Roadmap](#extensibility--roadmap)
12. [Project Structure](#project-structure)
13. [Contributing](#contributing)

---

## Quickstart

```go
s := store.NewStore()

// 1) Plain set
e1 := s.Set("user:1", "Alice") // v1

// 2) Update with TTL (expires in 2 minutes)
e2 := s.SetWithTTL("user:1", "Alice*", 2*time.Minute) // v2

// 3) CAS: only write if current value == "Alice*"
ok := s.CAS("user:1", "Alice*", "Alice ✅") // v3 if ok==true

// 4) Read latest (honors TTL lazily on read)
e, ok := s.Get("user:1")

// 5) Time travel: “as of” a timestamp (snapshot semantics)
t := e2.UpdatedAt.Add(30 * time.Second)
past, ok := s.GetWhen("user:1", t) // returns the version alive at time t

// 6) Delete (writes a tombstone into history)
s.Delete("user:1")

// 7) Time travel after delete → not found (delete is a barrier from that time on)
_, ok = s.GetWhen("user:1", time.Now())
```

---

## Core Ideas

- **Live map** (`s.data`): holds only the _current_ version for each key.
- **History** (`s.history[key]`): append‑only log of every successful write (Set / SetWithTTL / CAS) **and** delete (as a **tombstone**). It is **time‑ordered** by `UpdatedAt`.
- **Snapshot semantics** for time travel: `GetWhen(key, t)` returns the most recent version with `UpdatedAt ≤ t` that was **alive at t** (i.e., no TTL or `ExpiresAt > t`). **Tombstones are barriers**: once a delete is recorded at/ before `t`, older values are not visible for that `t`.

---

## API Surface

```go
// Construction
func NewStore() *Store
func NewStoreWithClock(c Clock) *Store // tests inject a fake clock

// Basic ops
func (s *Store) Set(key, value string) Entry
func (s *Store) SetWithTTL(key, value string, ttl time.Duration) Entry
func (s *Store) Get(key string) (Entry, bool)
func (s *Store) Delete(key string) bool
func (s *Store) CAS(key, expected, newValue string) bool

// Introspection
func (s *Store) Size() int
func (s *Store) Keys() []string

// Time & expiry
func (s *Store) SweepExpired() int                        // bulk remove expired from live map
func (s *Store) GetWhen(key string, t time.Time) (Entry, bool) // time travel
```

**Clock injection** (`Clock` interface) is used to make time deterministic in tests.

---

## Data Model & Invariants

`Entry` (per version):

- `Value string` — the stored value (ignored for tombstones).
- `CreatedAt time.Time` — creation time of this _lineage_ (never changes for a live lineage).
- `UpdatedAt time.Time` — write time of this version (monotonic per key).
- `ExpiresAt time.Time` — zero means no TTL; otherwise the version expires at `ExpiresAt`.
- `Version int64` — strictly increases by **1** on each write **or** tombstone.
- `Deleted bool` — `true` means **tombstone** (logical delete). Tombstones never live in `s.data`; they only appear in `s.history`.

Store invariants:

- `s.data[k]` holds **exactly one** live entry (or none) — the current snapshot.
- `s.history[k]` is **append‑only**, time‑ordered by `UpdatedAt`. It includes:
  - Every successful `Set`, `SetWithTTL`, `CAS` (with `Deleted=false`)
  - Every successful `Delete` as a **tombstone** (`Deleted=true`, `ExpiresAt=zero`)
- Versioning:
  - If key exists in `s.data`, new version = `existing.Version + 1`
  - If key absent but history exists, new version = `last(history[k]).Version + 1`
  - If no history, new version = `1`

---

## Semantics (with Examples)

### Set / SetWithTTL

- **Set**:

  - New key → `Version=nextFromHistory`, `CreatedAt=UpdatedAt=now`, `ExpiresAt=zero`.
  - Existing key → `Version++`, `CreatedAt` preserved, `UpdatedAt=now`, `ExpiresAt=zero` (clears TTL).
  - Appends to history (`Deleted=false`) and updates live map.

- **SetWithTTL**:
  - Same version rules, but `ExpiresAt = now + ttl` (for `ttl > 0`).
  - If `ttl <= 0`, it behaves like `Set`.

**Example (TTL then Set):**

```
12:00 SetWithTTL("k","A", 3m)   -> v1, ExpiresAt=12:03
12:05 Set("k","B")              -> v2, ExpiresAt=zero
```

At 12:02 → `GetWhen("k", 12:02) == "A"`  
At 12:04 → `GetWhen("k", 12:04) == not found` (A expired, B not yet)  
At 12:06 → `GetWhen("k", 12:06) == "B"`

---

### Get (lazy delete)

- Returns current live value if:
  - No TTL, **or**
  - TTL exists and `ExpiresAt > now`
- If `ExpiresAt ≤ now` → removes it from `s.data` and returns `not found`.
- Does **not** touch history.

---

### CAS (Compare-And-Set)

- Succeeds only if key exists, not expired **now**, and `current.Value == expected`.
- On success:
  - `Version++`, `UpdatedAt=now`, `CreatedAt` preserved.
  - **TTL policy**: preserves existing TTL on CAS.
  - Append to history, update live map.
- On failure → no mutation.

---

### Delete (tombstones)

- If key **present**:

  - Remove it from `s.data`.
  - Append a **tombstone** to `history[key]`:
    - `Deleted=true`, `ExpiresAt=zero`,
    - `Version = prev.Version + 1`,
    - `UpdatedAt = now`,
    - `CreatedAt` carries lineage (chosen policy).
  - Return `true`.

- If key **missing** → return `false` (no new tombstone).

**Idempotence:** repeated `Delete("k")` calls without an intervening `Set` append **only one** tombstone (the first call) and return `false` thereafter.

---

### GetWhen (time travel)

> “What did `key` look like **at time `t`**?”

**Snapshot semantics:**

- Pick the most recent version with `UpdatedAt ≤ t` that’s **alive at `t`**:
  - Alive at `t` iff `ExpiresAt == zero` **or** `ExpiresAt > t`.
  - If `ExpiresAt == t`, it’s **not** visible.
- **Tombstone barrier:** if a `Deleted==true` entry has `UpdatedAt ≤ t`, older versions are **not** visible at/after that instant.

**Search strategy:**  
Binary search (upper bound of `t`) over `history[key]` by `UpdatedAt`, then scan left:

- If you hit a tombstone `≤ t` → **stop** → not found.
- Else, return first live version whose TTL is valid at `t`.
- Complexity: `O(log N + K)`, where `K` is the number of skipped (expired‑by‑t) versions.

**Illustrative timeline:**

```
12:00  Set("k","A")                 -> v1
12:05  Delete("k")                  -> v2 (tombstone)
12:07  Set("k","B")                 -> v3
```

- 12:04 → `GetWhen("k", 12:04) == "A"`
- 12:05 → `GetWhen("k", 12:05) == not found` (tombstone at t)
- 12:06 → `GetWhen("k", 12:06) == not found` (barrier still applies)
- 12:07 → `GetWhen("k", 12:07) == "B"`
- 12:09 → `GetWhen("k", 12:09) == "B"`

**Same‑timestamp ties:** if multiple writes share the same `UpdatedAt`, append order wins. A `Delete` at the same timestamp as a `Set` makes `GetWhen(t)` return **not found** (delete wins).

---

## Complexity

- **Set / SetWithTTL / CAS / Delete**: amortized **O(1)** (map update + history append).
- **Get**: **O(1)** (lazy TTL check).
- **GetWhen**: **O(log N + K)** over `history[key]` (binary search + small left scan).
- **SweepExpired**: **O(#live keys)** in the live map.

---

## Thread Safety

- The current store is **not** goroutine‑safe (by design for milestone 1x).
- A mutex will arrive in “1D” to make operations atomic across readers/writers.
- Until then, use this store **single‑threaded** or add your own external synchronization.

---

## Testing

The project uses a deterministic **Clock** interface to avoid flakey timing tests. A `FakeClock` + `NewStoreWithClock` let tests control time precisely.

Tests cover:

- **1A acceptance**: Set/Get/Delete/Size/Keys basics, versioning & timestamps.
- **TTL behavior**: SetWithTTL, `Get` lazy delete, `SweepExpired`.
- **CAS**: success/failure, TTL preservation policy.
- **History**: append‑only, monotonic timestamps, versions.
- **GetWhen**: snapshot semantics across normal writes, TTL, ties.
- **Tombstones**: delete barrier in time travel, idempotent delete, recreate after delete, tombstone vs unexpired value.

Run everything:

```bash
go test ./...
```

> Tip: when asserting that `UpdatedAt` progresses, **advance the fake clock** between writes to avoid equal timestamps.

---

## Design Decisions & Rationale

- **History is source‑of‑truth for time travel.** We never reconstruct from `s.data`.
- **Snapshot semantics** for `GetWhen`: reflects _state_ at `t`, not just “what has ever happened.”
- **Tombstones** make deletes visible to time travel (and auditing).
- **Lazy delete** in `Get`: keeps hot reads fast; expired entries vanish on access. `SweepExpired` provides a bulk cleanup.
- **CAS TTL policy**: preserve existing TTL on CAS (choice is explicit in code & tests).
- **Version monotonicity across tombstones**: after `A@v1`, `Del@v2`, the next `Set` is `v3`, not `v1`.

---

## Common Questions (FAQ)

**Why is `CreatedAt` preserved on updates but “new” after a tombstoned delete?**  
After a delete, we treat a subsequent `Set` as a **new lineage** (fresh `CreatedAt`). This matches many stores’ semantics and simplifies audits.

**What if I want `GetWhen` to ignore deletes?**  
That would be “audit” semantics. You could add `GetWhenIgnoringDeletes` that skips the tombstone barrier check.

**Does `GetWhen` ever mutate state?**  
No. It is a pure read. It doesn’t lazy‑delete, and it never prunes history.

**How do equal timestamps behave?**  
Append order wins. At the same `UpdatedAt`, the later appended entry is considered “later.”

---

## Extensibility & Roadmap

What’s next (pick & choose):

1. **1D — Concurrency & Atomics**

   - Add a `sync.RWMutex` to `Store`.
   - Make all methods safe for concurrent use.
   - Consider write batching or atomic multi‑ops if needed.

2. **APIs (Service Layer)**

   - **HTTP/JSON** endpoints around the store:
     - `PUT /kv/{key}` with optional `ttl`
     - `GET /kv/{key}`
     - `DELETE /kv/{key}`
     - `POST /kv/{key}:cas` (`expected`, `newValue`)
     - `GET /kv/{key}:at?t=RFC3339`
     - Admin: `POST /maintenance/sweep-expired`
   - Validation, error mapping, idempotency, tracing.

3. **Background Sweeper**

   - Optional goroutine to periodically call `SweepExpired()`.

4. **History Inspection**

   - `ListHistory(key, limit, beforeTime)` for debugging.
   - Exporters (JSON/NDJSON) for audits.

5. **Compaction / Retention**

   - Cap history by count or time window.
   - Optional snapshotting to disk.

6. **Persistence**

   - Pluggable storage engine (boltDB, Badger, SQLite, Pebble).
   - WAL + checkpointing; recover on restart.

7. **Metrics & Tracing**
   - Counters for hits/misses, CAS success rate, sweeps, expirations.
   - OpenTelemetry spans for hot paths.

---

## Project Structure

```
/store
  store.go            # Store, Entry invariants, methods
  ..._test.go         # Test suites (acceptance, TTL, CAS, GetWhen, tombstones)
  README.md           # This file
```

(If you add an API layer, consider `/cmd/server` and `/internal/http` packages.)

---

## Contributing

- Keep behavior **documented and tested**. If you change a policy (e.g., CAS TTL), update both docs and tests.
- Prefer small, reviewable PRs (like the micro‑tasks you used here).
- If you add public APIs, include examples in this README.
