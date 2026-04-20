package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"os"

	"github.com/gorilla/mux"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var db *gorm.DB

func main() {
	var err error
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "host=localhost user=tfvision password=tfvision dbname=tfvision port=5432 sslmode=disable"
	}
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
	}

	// Auto Migrate the schema
	db.AutoMigrate(&Organization{}, &Workspace{}, &StateVersion{}, &CLIRun{}, &ProviderSelection{})
	r := mux.NewRouter()

	// Service Discovery
	r.HandleFunc("/.well-known/terraform.json", handleServiceDiscovery).Methods("GET")

	RegisterHandlers(r)

	// Catch-all for inspecting incoming tfe requests
	r.NotFoundHandler = http.HandlerFunc(handleNotFound)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Starting server on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}

func handleServiceDiscovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tfe.v2":   "/api/v2/",
		"tfe.v2.1": "/api/v2/",
		"tfe.v2.2": "/api/v2/",
	})
}

func handleNotFound(w http.ResponseWriter, r *http.Request) {
	dump, _ := httputil.DumpRequest(r, true)
	fmt.Printf("--- UNHANDLED REQUEST ---\n%s\n-------------------------\n", string(dump))
	w.WriteHeader(http.StatusNotFound)
}
