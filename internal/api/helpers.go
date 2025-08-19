package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}

func methodNotAllowed(w http.ResponseWriter, allow []string) {
	w.Header().Set("Allow", strings.Join(allow, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

// getKey extracts the {key} from paths like /v1/kv/{key} or /v1/kv/{key}:cas.
func getKey(r *http.Request) (string, bool) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/kv/")
	if rest == "" {
		return "", false
	}
	// strip optional :cas suffix for the CAS route
	rest = strings.TrimSuffix(rest, ":cas")
	return rest, true
}

// hasAll reports whether 'have' contains every key in 'want' (order ignored).
func hasAll(have []string, want ...string) bool {
	set := make(map[string]struct{}, len(have))
	for _, k := range have {
		set[k] = struct{}{}
	}
	for _, w := range want {
		if _, ok := set[w]; !ok {
			return false
		}
	}
	return true
}
