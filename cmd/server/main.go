package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/ARJ2211/blinkdb/internal/store"
)

func showTime(t time.Time) string {
	ft := t.Format(time.ANSIC)
	return ft
}

func main() {
	// demo: use the in-memory store
	s := store.NewStore()

	e1 := s.Set("u1", "A")
	fmt.Println(
		"Set u1=A ->",
		e1.Value,
		e1.Version,
		showTime(e1.CreatedAt),
		showTime(e1.UpdatedAt),
	)

	e2 := s.Set("u1", "B")
	fmt.Println(
		"Set u1=B ->",
		e2.Value,
		e2.Version,
		showTime(e2.CreatedAt),
		showTime(e2.UpdatedAt),
	)
	if e, ok := s.Get("u1"); ok {
		fmt.Println("Get u1 ->", e.Value, e.Version)
	}

	if _, ok := s.Get("missing"); !ok {
		fmt.Println("Get missing -> not found")
	}

	if ok := s.Delete("u1"); ok {
		fmt.Println("Deleted u1")
	}
	if ok := s.Delete("TEMP"); !ok {
		fmt.Println("NOT FOUND")
	}

	allEntries := []store.Entry{
		s.Set("u1", "A"),
		s.Set("u2", "AB"),
		s.Set("u3", "AC"),
		s.Set("u4", "ABAS"),
		s.Set("u5", "D"),
		s.Set("u6", "AF"),
	}
	for i := range allEntries {
		fmt.Println("Created!", i)
	}

	for _, ent := range s.Keys() {
		fmt.Println(ent)
	}

	fmt.Println(s.CAS("u6", "AF", "AF_New"))
	fmt.Println(s.Get("u6"))

	fmt.Println("COUNT: ", s.Size())

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
