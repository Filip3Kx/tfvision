package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/gorilla/mux"
	"gorm.io/gorm"
)

func handleGetCurrentStateVersion(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspace_id"]
	sv, found := latestUploadedState(workspaceID)
	if !found {
		writeNotFound(w)
		return
	}
	writeJSONAPI(w, http.StatusOK, map[string]interface{}{
		"data": stateVersionResource(sv, r.Host),
	})
}

func handleListStateVersions(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspace_id"]
	var svs []StateVersion
	if err := db.Where("workspace_id = ? AND upload_complete = ?", workspaceID, true).
		Order("created_at desc, serial desc").
		Find(&svs).Error; err != nil {
		writeInternalError(w)
		return
	}

	data := make([]interface{}, 0, len(svs))
	for _, sv := range svs {
		data = append(data, stateVersionResource(sv, r.Host))
	}
	writeJSONAPI(w, http.StatusOK, map[string]interface{}{"data": data})
}

func handleCreateStateVersion(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspace_id"]

	var payload stateVersionCreatePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeBadRequest(w, fmt.Sprintf("invalid request body: %s", err.Error()))
		return
	}

	serial := payload.Data.Attributes.Serial
	if serial == 0 {
		serial = nextSerial(workspaceID)
	}

	lineage := payload.Data.Attributes.Lineage
	if lineage == "" {
		lineage = resolveLineage(workspaceID, "")
	}

	sv := StateVersion{
		ID:             newID("sv"),
		WorkspaceID:    workspaceID,
		Serial:         serial,
		Lineage:        lineage,
		UploadComplete: false,
		CreatedAt:      time.Now(),
	}
	if err := db.Create(&sv).Error; err != nil {
		writeInternalError(w)
		return
	}

	writeJSONAPI(w, http.StatusCreated, map[string]interface{}{
		"data": map[string]interface{}{
			"id":   sv.ID,
			"type": "state-versions",
			"attributes": map[string]interface{}{
				"serial":                       sv.Serial,
				"lineage":                      sv.Lineage,
				"status":                       StateVersionStatusPending,
				"hosted-state-upload-url":      fmt.Sprintf("https://%s/internal/state/%s/upload", r.Host, sv.ID),
				"hosted-state-download-url":    fmt.Sprintf("https://%s/internal/state/%s", r.Host, sv.ID),
				"hosted-json-state-upload-url": fmt.Sprintf("https://%s/internal/state/%s/upload-json", r.Host, sv.ID),
			},
		},
	})
}

func handleGetStateVersion(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var sv StateVersion
	if err := db.Where("id = ?", id).First(&sv).Error; err != nil {
		writeNotFound(w)
		return
	}

	status := StateVersionStatusPending
	if sv.UploadComplete {
		status = StateVersionStatusUploaded
	}

	writeJSONAPI(w, http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{
			"id":   sv.ID,
			"type": "state-versions",
			"attributes": map[string]interface{}{
				"serial":                    sv.Serial,
				"lineage":                   sv.Lineage,
				"status":                    status,
				"created-at":                sv.CreatedAt.Format(time.RFC3339),
				"hosted-state-download-url": fmt.Sprintf("https://%s/internal/state/%s", r.Host, sv.ID),
			},
		},
	})
}

func handleCompareStateVersions(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workspaceID := vars["workspace_id"]
	fromID := vars["from_id"]
	toID := vars["to_id"]

	fromState, okFrom := loadUploadedStateVersion(workspaceID, fromID)
	toState, okTo := loadUploadedStateVersion(workspaceID, toID)
	if !okFrom || !okTo {
		writeNotFound(w)
		return
	}

	fromMap := digestResources(fromState)
	toMap := digestResources(toState)

	added := make([]string, 0)
	removed := make([]string, 0)
	changed := make([]string, 0)

	for addr, digest := range toMap {
		if prev, ok := fromMap[addr]; !ok {
			added = append(added, addr)
		} else if prev.Hash != digest.Hash {
			changed = append(changed, addr)
		}
	}
	for addr := range fromMap {
		if _, ok := toMap[addr]; !ok {
			removed = append(removed, addr)
		}
	}

	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(changed)

	var rawBefore interface{}
	var rawAfter interface{}
	_ = json.Unmarshal(fromState.RawState, &rawBefore)
	_ = json.Unmarshal(toState.RawState, &rawAfter)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{
			"type": "state-version-diff",
			"id":   fmt.Sprintf("%s-%s", fromID, toID),
			"attributes": map[string]interface{}{
				"from": map[string]interface{}{"id": fromID, "serial": fromState.Serial},
				"to":   map[string]interface{}{"id": toID, "serial": toState.Serial},
				"summary": map[string]interface{}{
					"added":   len(added),
					"removed": len(removed),
					"changed": len(changed),
				},
				"added":   added,
				"removed": removed,
				"changed": changed,
				"raw": map[string]interface{}{
					"before": rawBefore,
					"after":  rawAfter,
				},
			},
		},
	})
}

// handleStateSync is the tfvision-specific endpoint used by the CLI to
// synchronise state and/or provider metadata.  Two modes are supported:
//
//  1. Full sync: raw_state_base64 is present → persist a new state version and
//     its provider selections atomically inside a single transaction.
//  2. Enrich-only: raw_state_base64 is absent → attach provider versions to the
//     latest existing state version.
func handleStateSync(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspace_id"]

	r.Body = http.MaxBytesReader(w, r.Body, maxStateBodyBytes)
	var payload stateSyncPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeBadRequest(w, "invalid request body")
		return
	}

	if payload.RawStateBase64 == "" {
		// Enrich-only mode: update provider versions on the latest state.
		sv, found := latestUploadedState(workspaceID)
		if !found {
			writeNotFound(w)
			return
		}
		if err := upsertProviders(db, sv.ID, payload.Providers); err != nil {
			writeInternalError(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"data": map[string]interface{}{
				"id":             sv.ID,
				"workspace_id":   sv.WorkspaceID,
				"serial":         sv.Serial,
				"lineage":        sv.Lineage,
				"provider_count": len(payload.Providers),
				"enriched":       true,
			},
		})
		return
	}

	// Full sync mode: decode the state, resolve serial/lineage, and persist
	// the state version together with its provider selections in a transaction.
	rawState, err := base64.StdEncoding.DecodeString(payload.RawStateBase64)
	if err != nil {
		writeBadRequest(w, "invalid base64 in raw_state_base64")
		return
	}

	_, parsedSerial, parsedLineage := parseRawStateMetadata(rawState)

	serial := payload.Serial
	if serial == 0 {
		if parsedSerial > 0 {
			serial = parsedSerial
		} else {
			serial = nextSerial(workspaceID)
		}
	}

	lineage := payload.Lineage
	if lineage == "" {
		lineage = resolveLineage(workspaceID, parsedLineage)
	}

	sv := StateVersion{
		ID:             newID("sv"),
		WorkspaceID:    workspaceID,
		Serial:         serial,
		Lineage:        lineage,
		RawState:       rawState,
		UploadComplete: true,
		CreatedAt:      time.Now(),
	}

	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&sv).Error; err != nil {
			return err
		}
		return upsertProviders(tx, sv.ID, payload.Providers)
	}); err != nil {
		writeInternalError(w)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"data": map[string]interface{}{
			"id":             sv.ID,
			"workspace_id":   sv.WorkspaceID,
			"serial":         sv.Serial,
			"lineage":        sv.Lineage,
			"provider_count": len(payload.Providers),
		},
	})
}

// ---------------------------------------------------------------------------
// Response shape helper
// ---------------------------------------------------------------------------

func stateVersionResource(sv StateVersion, host string) map[string]interface{} {
	return map[string]interface{}{
		"id":   sv.ID,
		"type": "state-versions",
		"attributes": map[string]interface{}{
			"serial":                    sv.Serial,
			"lineage":                   sv.Lineage,
			"created-at":                sv.CreatedAt.Format(time.RFC3339),
			"hosted-state-download-url": fmt.Sprintf("https://%s/internal/state/%s", host, sv.ID),
		},
	}
}

