package main

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterHandlers(r *mux.Router) {
	r.Use(requestLoggingMiddleware)

	api := r.PathPrefix("/api/v2").Subrouter()
	registerAPIRoutes(api)
	registerInternalRoutes(r)
}

func registerAPIRoutes(api *mux.Router) {
	// Compatibility / discovery
	api.HandleFunc("/ping", handlePing).Methods(http.MethodGet)
	api.HandleFunc("/health", handleHealth).Methods(http.MethodGet)

	// Organizations
	api.HandleFunc("/organizations", handleListOrganizations).Methods(http.MethodGet)
	api.HandleFunc("/organizations", handleCreateOrganization).Methods(http.MethodPost)
	api.HandleFunc("/organizations/{org}", handleGetOrganization).Methods(http.MethodGet)
	api.HandleFunc("/organizations/{org}/entitlement-set", handleGetEntitlementSet).Methods(http.MethodGet)
	api.HandleFunc("/organizations/{org}/workspaces", handleListWorkspaces).Methods(http.MethodGet)
	api.HandleFunc("/organizations/{org}/workspaces/{workspace}", handleGetOrCreateWorkspace).Methods(http.MethodGet)

	// Workspace actions
	api.HandleFunc("/workspaces/{workspace_id}/actions/lock", handleLockWorkspace).Methods(http.MethodPost)
	api.HandleFunc("/workspaces/{workspace_id}/actions/unlock", handleUnlockWorkspace).Methods(http.MethodPost)

	// State versions (TFC protocol)
	api.HandleFunc("/workspaces/{workspace_id}/current-state-version", handleGetCurrentStateVersion).Methods(http.MethodGet)
	api.HandleFunc("/workspaces/{workspace_id}/state-versions", handleListStateVersions).Methods(http.MethodGet)
	api.HandleFunc("/workspaces/{workspace_id}/state-versions", handleCreateStateVersion).Methods(http.MethodPost)
	api.HandleFunc("/state-versions/{id}", handleGetStateVersion).Methods(http.MethodGet)
	api.HandleFunc("/workspaces/{workspace_id}/state-versions/{from_id}/compare/{to_id}", handleCompareStateVersions).Methods(http.MethodGet)

	// tfvision-specific analysis endpoints
	api.HandleFunc("/workspaces/{workspace_id}/resources", handleListWorkspaceResources).Methods(http.MethodGet)
	api.HandleFunc("/workspaces/{workspace_id}/state-summary", handleGetWorkspaceStateSummary).Methods(http.MethodGet)
	api.HandleFunc("/workspaces/{workspace_id}/state-versions/{state_version_id}/summary", handleGetStateVersionSummary).Methods(http.MethodGet)

	// tfvision CLI state sync
	api.HandleFunc("/workspaces/{workspace_id}/state-sync", handleStateSync).Methods(http.MethodPost)

	// CLI run tracking
	api.HandleFunc("/workspaces/{workspace_id}/cli-runs", handleCLIRuns).Methods(http.MethodGet, http.MethodPost)
	api.HandleFunc("/cli-runs/{run_id}", handleGetCLIRun).Methods(http.MethodGet)
}

func registerInternalRoutes(r *mux.Router) {
	r.HandleFunc("/health", handleHealth).Methods(http.MethodGet)
	r.HandleFunc("/internal/state/{sv_id}", handleDownloadState).Methods(http.MethodGet)
	r.HandleFunc("/internal/state/{sv_id}/upload", handleUploadState).Methods(http.MethodPut)
	r.HandleFunc("/internal/state/{sv_id}/upload-json", handleUploadStateJSON).Methods(http.MethodPut)
}

