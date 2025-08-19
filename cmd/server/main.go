package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

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

const (
	ansiClearScreen     = "\x1b[2J"
	ansiClearScrollback = "\x1b[3J"
	ansiCursorHome      = "\x1b[H"
	ansiDim             = "\x1b[2m"
	ansiGreen           = "\x1b[32m"
	ansiReset           = "\x1b[0m"
)

func clearTerminal() {
	fmt.Print(ansiClearScreen, ansiClearScrollback, ansiCursorHome)
}

func printBanner(addr string) {
	now := time.Now().UTC().Format(time.RFC3339)
	fmt.Println(banner)
	fmt.Printf("%sBlinkDB%s listening on %s%s%s   %sUTC:%s %s\n\n",
		ansiGreen, ansiReset, ansiGreen, addr, ansiReset, ansiDim, ansiReset, now,
	)
	fmt.Println(ansiDim + "Endpoints:")
	fmt.Println("  GET    /v1/kv")
	fmt.Println("  PUT    /v1/kv/{key}")
	fmt.Println("  GET    /v1/kv/{key}         (?at=RFC3339)")
	fmt.Println("  POST   /v1/kv/{key}:cas")
	fmt.Println("  DELETE /v1/kv/{key}")
	fmt.Println("  POST   /v1/admin/sweep" + ansiReset)
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

	// Health (kept separate from middleware chain)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Middleware chain:
	var h http.Handler = router
	h = observability.Recoverer(nil)(h) // panic catcher
	h = observability.PrettyHTTPLoggerWithClock(
		st.Clock.Now)(h) // pretty logs

	mux.Handle("/", h)

	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Fprintln(os.Stderr, "server error:", err)
		os.Exit(1)
	}
}
