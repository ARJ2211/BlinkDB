// cmd/server/main.go
package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/ARJ2211/blinkdb/internal/api"
	"github.com/ARJ2211/blinkdb/internal/observability"
	"github.com/ARJ2211/blinkdb/internal/store"
)

const banner = `
 /$$$$$$$  /$$ /$$           /$$             /$$$$$$$  /$$$$$$$ 
| $$__  $$| $$|__/          | $$            | $$__  $$| $$__  $$
| $$  \ $$| $$ /$$ /$$$$$$$ | $$   /$$      | $$  \ $$| $$  \ $$
| $$$$$$$ | $$| $$| $$__  $$| $$  /$$/      | $$  | $$| $$$$$$$ 
| $$__  $$| $$| $$| $$  \ $$| $$$$$$/       | $$  | $$| $$__  $$
| $$  \ $$| $$| $$| $$  | $$| $$_  $$       | $$  | $$| $$  \ $$
| $$$$$$$/| $$| $$| $$  | $$| $$ \  $$      | $$$$$$$/| $$$$$$$/
|_______/ |__/|__/|__/  |__/|__/  \__/      |_______/ |_______/ 


`

// --- ANSI styling ---
const (
	ansiClearScreen     = "\x1b[2J"
	ansiClearScrollback = "\x1b[3J"
	ansiCursorHome      = "\x1b[H"

	ansiDim   = "\x1b[2m"
	ansiBold  = "\x1b[1m"
	ansiGreen = "\x1b[32m"
	ansiCyan  = "\x1b[36m"
	ansiReset = "\x1b[0m"
)

func clearTerminal() {
	fmt.Print(ansiClearScreen, ansiClearScrollback, ansiCursorHome)
}

func printBanner(addr string) {
	fmt.Print(banner)
	fmt.Printf("%sBlinkDB listening on %shttp://localhost%s%s\n", ansiBold, ansiGreen, addr, ansiReset)

	fmt.Println(ansiDim + "\nEndpoints:" + ansiReset)

	fmt.Println("  GET    /v1/kv")
	fmt.Println("         -> List current (non-expired) keys.")
	fmt.Println("         -> Response: {\"keys\":[...],\"size\":N}")
	fmt.Println("         -> Example:", ansiCyan, "curl -s http://localhost"+addr+"/v1/kv", ansiReset)

	fmt.Println("  PUT    /v1/kv/{key}")
	fmt.Println("         -> Create/update value. TTL rules:")
	fmt.Println("             clearTTL=true  → clear TTL")
	fmt.Println("             ttlSeconds>0   → set relative TTL")
	fmt.Println("             expiresAt RFC3339 (future) → set absolute TTL")
	fmt.Println("             none (on existing key) → preserve current TTL")
	fmt.Println("         -> Status: 201 on create, 200 on update; 400 on bad JSON/TTL.")
	fmt.Println("         -> Example:", ansiCyan, `curl -s -X PUT http://localhost`+addr+`/v1/kv/user:1 -H 'Content-Type: application/json' -d '{"value":"Alice","ttlSeconds":120}'`, ansiReset)

	fmt.Println("  GET    /v1/kv/{key}         (?at=RFC3339)")
	fmt.Println("         -> Read latest, or as-of time with ?at. 404 if missing/expired/tombstoned.")
	fmt.Println("         -> Example (now):", ansiCyan, "curl -s http://localhost"+addr+"/v1/kv/user:1", ansiReset)
	fmt.Println("         -> Example (time travel):", ansiCyan, "curl -s 'http://localhost"+addr+"/v1/kv/user:1?at=2025-08-19T12:05:00Z'", ansiReset)

	fmt.Println("  POST   /v1/kv/{key}:cas")
	fmt.Println("         -> Compare-and-swap by version (TTL preserved on success).")
	fmt.Println("         -> Status: 200 on success, 409 on version mismatch, 404 if key missing/expired.")
	fmt.Println("         -> Body: {\"expectedVersion\":<int>, \"value\":\"<string>\"}")
	fmt.Println("         -> Example:", ansiCyan, `curl -s -X POST http://localhost`+addr+`/v1/kv/user:1:cas -H 'Content-Type: application/json' -d '{"expectedVersion":2,"value":"Alice++"}'`, ansiReset)

	fmt.Println("  DELETE /v1/kv/{key}")
	fmt.Println("         -> Write tombstone (version++). 200 with tombstone view; 404 if already gone.")
	fmt.Println("         -> Example:", ansiCyan, "curl -s -X DELETE http://localhost"+addr+"/v1/kv/user:1", ansiReset)

	fmt.Println("  POST   /v1/admin/sweep")
	fmt.Println("         -> GC expired keys at 'now' (no tombstones). 400 if body contains unsupported 'before'.")
	fmt.Println("         -> Response: {\"swept\":N, \"keys\":[...]} ")
	fmt.Println("         -> Example:", ansiCyan, "curl -s -X POST http://localhost"+addr+"/v1/admin/sweep", ansiReset)

	fmt.Println(ansiDim + "\nNotes:")
	fmt.Println("  • All timestamps are RFC3339 UTC.  • Errors use {\"error\":\"...\"}.")
	fmt.Println("  • GET returns 404 when TTL has expired (lazy delete).  • Order of /v1/kv keys not guaranteed." + ansiReset)
	fmt.Println()
}

func main() {
	clearTerminal()
	addr := ":8080"
	printBanner(addr)

	// Store + API
	st := store.NewStore()
	srv := api.NewServer(st)
	router := api.NewRouter(srv)

	// Health endpoint (kept outside middleware chain)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Middleware chain
	var h http.Handler = router
	h = observability.Recoverer(nil)(h)     // panic catcher
	h = observability.PrettyHTTPLogger()(h) // colorized human logs

	mux.Handle("/", h)

	fmt.Println("BlinkDB listening on", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Fprintln(os.Stderr, "server error:", err)
		os.Exit(1)
	}
}
