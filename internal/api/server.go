package api

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/ARJ2211/blinkdb/internal/store"
)

type Server struct {
	S *store.Store

	// sweep stats
	sweepTotal atomic.Int64 // total keys removed by sweeps
	sweepLast  atomic.Value // stores time.Time (UTC)
}

// existing constructor
func NewServer(st *store.Store) *Server {
	return &Server{S: st}
}

// called by the sweeper goroutine after each sweep
func (s *Server) RecordSweep(removed int) {
	if removed <= 0 {
		return
	}
	s.sweepTotal.Add(int64(removed))
	s.sweepLast.Store(time.Now().UTC())
}

// handler: GET /v1/admin/sweep-stats
func (s *Server) HandleSweepStats(w http.ResponseWriter, r *http.Request) {
	var last string
	if v := s.sweepLast.Load(); v != nil {
		last = v.(time.Time).Format(time.RFC3339)
	}

	resp := map[string]any{
		"totalSwept":  s.sweepTotal.Load(),
		"lastSweepAt": last, // "" if never swept
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// (optional) expose the store if main needs it
func (s *Server) Store() *store.Store { return s.S }
