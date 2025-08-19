package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/ARJ2211/blinkdb/internal/store"
)

func showTime(t time.Time) string {
	ft := t.Format(time.UTC.String())
	return ft
}

func main() {
	s := store.NewStore()
	s.Set("key1", "value1")
	entry, ok := s.Get("key1")
	fmt.Println(entry.ExpiresAt, ok)
	fmt.Println(s.Clock.Now())
	//==============================================================
	// minimal HTTP server with health endpoint
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("%v /healthz -> OK\n", showTime(time.Now()))
		fmt.Fprintln(w, "OK")
	})

	addr := ":8080"
	fmt.Println("BlinkDB server listening on", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		panic(err)
	}
}
