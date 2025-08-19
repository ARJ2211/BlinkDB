package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/ARJ2211/blinkdb/internal/api"
	"github.com/ARJ2211/blinkdb/internal/observability"
	"github.com/ARJ2211/blinkdb/internal/store"
)

func main() {
	// Store and API router
	st := store.NewStore()
	srv := api.NewServer(st)
	router := api.NewRouter(srv)

	// Structured JSON logs to stdout
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Compose middlewares: recover -> log -> router
	var h http.Handler = router
	h = observability.Recoverer(logger)(h)
	h = observability.HTTPLogger(logger)(h)

	mux := http.NewServeMux()
	mux.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	mux.Handle("/", h)

	addr := ":8080"
	fmt.Println("BlinkDB listening on", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		panic(err)
	}
}
