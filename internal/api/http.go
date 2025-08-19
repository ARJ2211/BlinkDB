package api

import (
	"net/http"
	"strings"
)

// NewRouter wires all routes. Pass the Server so handlers can use the store.
func NewRouter(srv *Server) http.Handler {
	mux := http.NewServeMux()

	// KV routes
	mux.HandleFunc("/v1/kv/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, ":cas") {
			if r.Method != http.MethodPost {
				methodNotAllowed(w, []string{http.MethodPost})
				return
			}
			srv.CASValue(w, r)
			return
		}

		switch r.Method {
		case http.MethodPut:
			srv.PutValue(w, r)
		case http.MethodGet:
			srv.GetValue(w, r)
		case http.MethodDelete:
			srv.DeleteValue(w, r)
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
		srv.SweepExpired(w, r)
	})

	return mux
}
