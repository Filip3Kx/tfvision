package main

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/gorilla/mux"
)

func handleListWorkspaceResources(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspace_id"]
	sv, found := latestUploadedState(workspaceID)
	if !found {
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": []interface{}{}})
		return
	}

	resources := applyProviderVersions(extractResources(sv.RawState), providerVersionMapForState(sv.ID))
	sort.Slice(resources, func(i, j int) bool {
		left := resources[i].ModulePath + "|" + resources[i].Address + "|" + resources[i].ID
		right := resources[j].ModulePath + "|" + resources[j].Address + "|" + resources[j].ID
		return left < right
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": resources})
}

func handleGetWorkspaceStateSummary(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspace_id"]
	sv, found := latestUploadedState(workspaceID)
	if !found {
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": nil})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": summarizeState(sv)})
}

func handleGetStateVersionSummary(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workspaceID := vars["workspace_id"]
	stateVersionID := vars["state_version_id"]

	sv, found := loadUploadedStateVersion(workspaceID, stateVersionID)
	if !found {
		writeNotFound(w)
		return
	}

	var raw interface{}
	if err := json.Unmarshal(sv.RawState, &raw); err != nil {
		raw = map[string]interface{}{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{
			"id":      sv.ID,
			"serial":  sv.Serial,
			"created": sv.CreatedAt,
			"summary": summarizeState(sv),
			"raw":     raw,
		},
	})
}

func handleCLIRuns(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspace_id"]
	if r.Method == http.MethodGet {
		var runs []CLIRun
		if err := db.Where("workspace_id = ?", workspaceID).Order("created_at desc").Find(&runs).Error; err != nil {
			writeInternalError(w)
			return
		}
		data := make([]map[string]interface{}, 0, len(runs))
		for _, run := range runs {
			data = append(data, cliRunResponse(run, false))
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": data})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxLogBodyBytes)
	var payload cliRunPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeBadRequest(w, "invalid request body")
		return
	}

	status := normalizeRunStatus(payload.Status)
	if status == "" {
		writeBadRequest(w, "status must be one of: planned, applied, error")
		return
	}
	if payload.Command == "" {
		writeBadRequest(w, "command is required")
		return
	}

	now := time.Now()
	run := CLIRun{
		ID:             newID("run"),
		WorkspaceID:    workspaceID,
		Command:        payload.Command,
		Status:         status,
		Message:        payload.Message,
		LogBody:        payload.LogBody,
		StateVersionID: payload.StateVersionID,
		CreatedAt:      now,
		UpdatedAt:      now,
		CompletedAt:    &now,
	}

	if err := db.Create(&run).Error; err != nil {
		writeInternalError(w)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{"data": cliRunResponse(run, true)})
}

func handleGetCLIRun(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["run_id"]
	var run CLIRun
	if err := db.Where("id = ?", runID).First(&run).Error; err != nil {
		writeNotFound(w)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": cliRunResponse(run, true)})
}

func handleDownloadState(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["sv_id"]
	var sv StateVersion
	if err := db.Where("id = ?", id).First(&sv).Error; err != nil {
		writeNotFound(w)
		return
	}

	if !sv.UploadComplete {
		writeNotFound(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(sv.RawState)
}

func handleUploadState(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["sv_id"]
	var sv StateVersion
	if err := db.Where("id = ?", id).First(&sv).Error; err != nil {
		writeNotFound(w)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxStateBodyBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeBadRequest(w, "failed to read request body")
		return
	}

	sv.RawState = raw
	sv.UploadComplete = true
	if err := db.Save(&sv).Error; err != nil {
		writeInternalError(w)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleUploadStateJSON(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["sv_id"]
	var sv StateVersion
	if err := db.Where("id = ?", id).First(&sv).Error; err != nil {
		writeNotFound(w)
		return
	}
	_, _ = io.Copy(io.Discard, r.Body)
	w.WriteHeader(http.StatusOK)
}

