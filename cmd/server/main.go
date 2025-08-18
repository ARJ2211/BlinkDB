package main

import (
	"fmt"
	"net/http"
	"time"
)

func main() {
	// 1) create a request router (a multiplexer)
	mux := http.NewServeMux()

	// 2) register one route for health checks
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		// Write plain text "ok" back to the client
		fmt.Printf("%v: /healthz -> ok\n", time.Now())
		fmt.Fprint(w, "OK!\n")
	})

	// 3) choose an address to listen on
	addr := ":8080"

	// 4) print a helpful startup message to your terminal
	fmt.Println("BlinkDB server listening on", addr)

	// 5) start the HTTP server (this blocks until the program stops or errors)
	if err := http.ListenAndServe(addr, mux); err != nil {
		// If the server fails to start (e.g., port in use), crash with the error
		panic(err)
	}
}
