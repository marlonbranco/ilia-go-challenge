package http

import (
	"encoding/json"
	"net/http"
)

func writeJSON(response http.ResponseWriter, status int, v any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	json.NewEncoder(response).Encode(v)
}
