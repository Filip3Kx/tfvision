package main

import (
	"net/http"

	"gorm.io/gorm"
)

// handleHealth returns 200 OK when the server is up and the database is reachable,
// or 503 Service Unavailable when the database cannot be pinged.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := pingDB(db); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"status": "unhealthy",
			"error":  err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
	})
}

// pingDB executes a lightweight SQL statement to verify the database connection.
func pingDB(d *gorm.DB) error {
	sqlDB, err := d.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}
