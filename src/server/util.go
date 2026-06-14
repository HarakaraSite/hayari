package server

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, err error, code int) {
	http.Error(w, err.Error(), code)
}
