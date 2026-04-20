package main

import (
	"context"
	"log"
	"net/http"
	"strings"
)

type contextKey string

const correlationIDKey contextKey = "correlation_id"

// requestLoggingMiddleware logs every incoming request with a unique correlation
// ID.  Download requests for state blobs are intentionally suppressed to keep
// logs readable under continuous polling.
func requestLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		correlationID := newID("req")
		ctx := context.WithValue(r.Context(), correlationIDKey, correlationID)
		w.Header().Set("X-Correlation-ID", correlationID)

		if !strings.HasPrefix(r.URL.Path, "/internal/state/") || r.Method != http.MethodGet {
			log.Printf("[%s] %s %s", correlationID, r.Method, r.URL.Path)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
