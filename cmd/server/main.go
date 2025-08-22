// cmd/server/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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

func runSweeper(ctx context.Context, st *store.Store, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[sweeper] stopping (context canceled)")
			return
		case <-t.C:
			start := time.Now().UTC()
			n := st.SweepExpired()
			observability.LogSweeper(every, n, time.Since(start))
		}
	}
}

func clearTerminal() {
	fmt.Print(ansiClearScreen, ansiClearScrollback, ansiCursorHome)
}

func printMiniBanner(addr string) {
	fmt.Print(banner)
	fmt.Printf("%sBlinkDB%s listening at %shttp://localhost%s%s\n\n",
		ansiBold, ansiReset, ansiGreen, addr, ansiReset)

	fmt.Println(ansiDim + "Endpoints:" + ansiReset)
	fmt.Println("  GET    /v1/kv                     # list live keys")
	fmt.Println("  PUT    /v1/kv/{key}               # create/update (TTL rules)")
	fmt.Println("  GET    /v1/kv/{key}[?at=RFC3339]  # read now / time-travel")
	fmt.Println("  POST   /v1/kv/{key}:cas           # CAS by version (preserves TTL)")
	fmt.Println("  DELETE /v1/kv/{key}               # tombstone delete")
	fmt.Println("  POST   /v1/admin/sweep            # GC expired (no tombstones)")

	// 2 quick examples
	fmt.Println(ansiDim + "\nExamples:" + ansiReset)
	fmt.Println("  ", ansiCyan, "curl -s -X PUT http://localhost"+addr+"/v1/kv/user:1",
		"-H 'Content-Type: application/json' -d '{\"value\":\"Alice\",\"ttlSeconds\":60}'", ansiReset)
	fmt.Println("  ", ansiCyan, "curl -s 'http://localhost"+addr+"/v1/kv/user:1?at=2025-08-19T12:05:00Z'", ansiReset)

	// Link to docs (repo root ReadMe)
	fmt.Println(ansiDim + "\nDocs: ReadMe.md (https://github.com/ARJ2211/BlinkDB) → HTTP API, DTOs, semantics." + ansiReset)
	fmt.Println()
}

func main() {
	clearTerminal()
	log.SetFlags(log.LstdFlags | log.LUTC)
	log.SetOutput(os.Stdout)

	// flags
	port := flag.String("port", "8080", "Port to run the server on")
	sweepEnabled := flag.Bool("sweep-enabled", true, "Run background sweeper")
	sweepEvery := flag.Duration("sweep-interval", 30*time.Second, "How often to sweep expired keys")
	flag.Parse()

	addr := ":" + *port
	printMiniBanner(addr)

	// root context with cancel on SIGINT/SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// store + api
	st := store.NewStore()
	srv := api.NewServer(st)
	router := api.NewRouter(srv)

	// mux and middleware
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	var h http.Handler = router
	h = observability.Recoverer(nil)(h)     // panic catcher
	h = observability.PrettyHTTPLogger()(h) // human-friendly logs
	mux.Handle("/", h)

	// http server with graceful shutdown
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// start sweeper if enabled
	if *sweepEnabled && *sweepEvery > 0 {
		go runSweeper(ctx, st, *sweepEvery)
	}

	// start http in a goroutine
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe()
	}()

	// wait for either server error or signal
	select {
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, "server error:", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		// shutdown
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shCtx)
	}
}
