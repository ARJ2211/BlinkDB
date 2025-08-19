package api

import "github.com/ARJ2211/blinkdb/internal/store"

// Server holds dependencies for handlers (just the store for now).
type Server struct {
	S *store.Store
}

func NewServer(s *store.Store) *Server {
	return &Server{S: s}
}
