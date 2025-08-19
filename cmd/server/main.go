package main

import (
	"fmt"
	"net/http"
	"time"
)

func main() {
	// demo: use the in-memory store
	// s := store.NewStore()

	// e1 := s.Set("u1", "A")
	// fmt.Println("Set u1=A ->", e1.Value(), e1.Version(), e1.CreatedAt(), e1.UpdatedAt())

	// e2 := s.Set("u1", "B")
	// fmt.Println("Set u1=B ->", e2.Value(), e2.Version(), e2.CreatedAt(), e2.UpdatedAt())

	// minimal HTTP server with health endpoint
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("%v /healthz -> OK\n", time.Now())
		fmt.Fprintln(w, "OK")
	})

	addr := ":8080"
	fmt.Println("BlinkDB server listening on", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		panic(err)
	}
}
