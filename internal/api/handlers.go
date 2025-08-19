package api

import "net/http"

func PutValue(w http.ResponseWriter, r *http.Request) {
	if _, ok := getKey(r); !ok {
		writeError(w, http.StatusBadRequest, "missing key")
		return
	}
	writeError(w, http.StatusNotImplemented, "not implemented")
}

func GetValue(w http.ResponseWriter, r *http.Request) {
	if _, ok := getKey(r); !ok {
		writeError(w, http.StatusBadRequest, "missing key")
		return
	}
	writeError(w, http.StatusNotImplemented, "not implemented")
}

func CASValue(w http.ResponseWriter, r *http.Request) {
	if _, ok := getKey(r); !ok {
		writeError(w, http.StatusBadRequest, "missing key")
		return
	}
	writeError(w, http.StatusNotImplemented, "not implemented")
}

func DeleteValue(w http.ResponseWriter, r *http.Request) {
	if _, ok := getKey(r); !ok {
		writeError(w, http.StatusBadRequest, "missing key")
		return
	}
	writeError(w, http.StatusNotImplemented, "not implemented")
}

func SweepExpired(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}
