package main

import (
	"encoding/json"
	"net/http"
	"strconv"
)

const (
	contentTypeJSONAPI = "application/vnd.api+json"
	contentTypeJSON    = "application/json"
)

// writeJSONAPI writes a TFC-compatible JSON:API response with the given status code.
func writeJSONAPI(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", contentTypeJSONAPI)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSON writes a plain JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON:API-shaped error response.
func writeError(w http.ResponseWriter, status int, code, title string) {
	writeJSONAPI(w, status, map[string]interface{}{
		"errors": []map[string]interface{}{
			{
				"status": strconv.Itoa(status),
				"code":   code,
				"title":  title,
			},
		},
	})
}

func writeNotFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "not_found", "Not Found")
}

func writeInternalError(w http.ResponseWriter) {
	writeError(w, http.StatusInternalServerError, "internal_error", "Internal Server Error")
}

func writeBadRequest(w http.ResponseWriter, title string) {
	writeError(w, http.StatusBadRequest, "bad_request", title)
}

func writeConflict(w http.ResponseWriter, title string) {
	writeError(w, http.StatusConflict, "conflict", title)
}

// writeTFEHeaders sets the standard TFE/TFC compatibility headers on a response.
func writeTFEHeaders(w http.ResponseWriter) {
	w.Header().Set("X-TFE-Version", "v202501-1")
	w.Header().Set("X-Terraform-Enterprise-Version", "v202501-1")
	w.Header().Set("TFP-API-Version", "2.5")
	w.Header().Set("TFP-AppName", "Terraform Enterprise")
}
