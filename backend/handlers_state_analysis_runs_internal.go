package main

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gorilla/mux"
)

func handleListWorkspaceResources(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspace_id"]
	sv, found := latestUploadedState(workspaceID)
	if !found {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"data": []interface{}{}})
		return
	}

	resources := applyProviderVersions(extractResources(sv.RawState), providerVersionMapForState(sv.ID))
	sort.Slice(resources, func(i, j int) bool {
		left := resources[i].ModulePath + "|" + resources[i].Address + "|" + resources[i].ID
		right := resources[j].ModulePath + "|" + resources[j].Address + "|" + resources[j].ID
		return left < right
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"data": resources})
}

func handleGetWorkspaceStateSummary(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspace_id"]
	sv, found := latestUploadedState(workspaceID)
	if !found {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"data": nil})
		return
	}

	summary := summarizeState(sv)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"data": summary})
}

func handleGetStateVersionSummary(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workspaceID := vars["workspace_id"]
	stateVersionID := vars["state_version_id"]

	sv, found := loadUploadedStateVersion(workspaceID, stateVersionID)
	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	summary := summarizeState(sv)
	var raw interface{}
	if err := json.Unmarshal(sv.RawState, &raw); err != nil {
		raw = map[string]interface{}{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": map[string]interface{}{
			"id":      sv.ID,
			"serial":  sv.Serial,
			"created": sv.CreatedAt,
			"summary": summary,
			"raw":     raw,
		},
	})
}

func handleCLIRuns(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspace_id"]
	if r.Method == http.MethodGet {
		var runs []CLIRun
		db.Where("workspace_id = ?", workspaceID).Order("created_at desc").Find(&runs)
		data := make([]map[string]interface{}, 0, len(runs))
		for _, run := range runs {
			data = append(data, cliRunResponse(run, false))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
		return
	}

	var payload cliRunPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	status := normalizeRunStatus(payload.Status)
	if status == "" || payload.Command == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	now := time.Now()
	run := CLIRun{
		ID:             "run-" + strconv.FormatInt(now.UnixNano(), 10),
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
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{"data": cliRunResponse(run, true)})
}

func handleGetCLIRun(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["run_id"]
	var run CLIRun
	if err := db.Where("id = ?", runID).First(&run).Error; err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"data": cliRunResponse(run, true)})
}

func handleDownloadState(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["sv_id"]
	var sv StateVersion
	if err := db.Where("id = ?", id).First(&sv).Error; err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if !sv.UploadComplete {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(sv.RawState)
}

func handleUploadState(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["sv_id"]
	var sv StateVersion
	if err := db.Where("id = ?", id).First(&sv).Error; err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	sv.RawState = raw
	sv.UploadComplete = true
	db.Save(&sv)
	w.WriteHeader(http.StatusOK)
}

func handleUploadStateJSON(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["sv_id"]
	var sv StateVersion
	if err := db.Where("id = ?", id).First(&sv).Error; err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	_, _ = io.Copy(io.Discard, r.Body)
	w.WriteHeader(http.StatusOK)
}
