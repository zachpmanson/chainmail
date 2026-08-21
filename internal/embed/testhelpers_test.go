package embed

import (
	"encoding/json"
	"net/http"
	"strings"
)

func decodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }
