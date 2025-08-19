package api

import (
	"net/http"
	"strings"
)

// NewRouter wires all routes. Use this in cmd/server/main.go.
func NewRouter() http.Handler {
	mux := http.NewServeMux()

	// KV routes
	mux.HandleFunc("/v1/kv/", func(w http.ResponseWriter, r *http.Request) {
		// Route by method and suffix
		path := r.URL.Path
		if strings.HasSuffix(path, ":cas") {
			if r.Method != http.MethodPost {
				methodNotAllowed(w, []string{http.MethodPost})
				return
			}
			CASValue(w, r)
			return
		}

		switch r.Method {
		case http.MethodPut:
			PutValue(w, r)
		case http.MethodGet:
			GetValue(w, r)
		case http.MethodDelete:
			DeleteValue(w, r)
		default:
			methodNotAllowed(w, []string{http.MethodPut, http.MethodGet, http.MethodDelete})
		}
	})

	// Admin routes
	mux.HandleFunc("/v1/admin/sweep", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, []string{http.MethodPost})
			return
		}
		SweepExpired(w, r)
	})

	return mux
}
