package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/gorilla/mux"
)

func handleGetCurrentStateVersion(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspace_id"]
	sv, found := latestUploadedState(workspaceID)
	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.api+json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": map[string]interface{}{
			"id":   sv.ID,
			"type": "state-versions",
			"attributes": map[string]interface{}{
				"serial":                    sv.Serial,
				"lineage":                   sv.Lineage,
				"created-at":                sv.CreatedAt.Format(time.RFC3339),
				"hosted-state-download-url": fmt.Sprintf("https://%s/internal/state/%s", r.Host, sv.ID),
			},
		},
	})
}

func handleListStateVersions(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspace_id"]
	var svs []StateVersion
	db.Where("workspace_id = ? AND upload_complete = ?", workspaceID, true).Order("created_at desc, serial desc").Find(&svs)

	data := make([]interface{}, 0, len(svs))
	for _, sv := range svs {
		data = append(data, map[string]interface{}{
			"id":   sv.ID,
			"type": "state-versions",
			"attributes": map[string]interface{}{
				"serial":                    sv.Serial,
				"lineage":                   sv.Lineage,
				"created-at":                sv.CreatedAt.Format(time.RFC3339),
				"hosted-state-download-url": fmt.Sprintf("https://%s/internal/state/%s", r.Host, sv.ID),
			},
		})
	}

	w.Header().Set("Content-Type", "application/vnd.api+json")
	json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
}

func handleCreateStateVersion(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspace_id"]

	var payload stateVersionCreatePayload
	_ = json.NewDecoder(r.Body).Decode(&payload)

	serial := payload.Data.Attributes.Serial
	if serial == 0 {
		var latest StateVersion
		if err := db.Where("workspace_id = ?", workspaceID).Order("serial desc").First(&latest).Error; err == nil {
			serial = latest.Serial + 1
		} else {
			serial = 1
		}
	}

	lineage := payload.Data.Attributes.Lineage
	if lineage == "" {
		latest, found := latestUploadedState(workspaceID)
		if found && latest.Lineage != "" {
			lineage = latest.Lineage
		} else {
			lineage = fmt.Sprintf("lineage-%d", time.Now().UnixNano())
		}
	}

	sv := StateVersion{
		ID:             "sv-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		WorkspaceID:    workspaceID,
		Serial:         serial,
		Lineage:        lineage,
		UploadComplete: false,
		CreatedAt:      time.Now(),
	}
	db.Create(&sv)

	w.Header().Set("Content-Type", "application/vnd.api+json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": map[string]interface{}{
			"id":   sv.ID,
			"type": "state-versions",
			"attributes": map[string]interface{}{
				"serial":                       sv.Serial,
				"lineage":                      sv.Lineage,
				"status":                       "pending",
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
		w.WriteHeader(http.StatusNotFound)
		return
	}

	status := "pending"
	if sv.UploadComplete {
		status = "uploaded"
	}

	w.Header().Set("Content-Type", "application/vnd.api+json")
	json.NewEncoder(w).Encode(map[string]interface{}{
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
		w.WriteHeader(http.StatusNotFound)
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
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

func handleStateSync(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspace_id"]
	var payload stateSyncPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if payload.RawStateBase64 == "" {
		sv, found := latestUploadedState(workspaceID)
		if !found {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		for _, provider := range payload.Providers {
			if provider.Source == "" {
				continue
			}
			selection := ProviderSelection{
				ID:             fmt.Sprintf("ps-%d", time.Now().UnixNano()),
				StateVersionID: sv.ID,
				Source:         provider.Source,
				Version:        provider.Version,
				CreatedAt:      time.Now(),
			}
			db.Create(&selection)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
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

	rawState, err := base64.StdEncoding.DecodeString(payload.RawStateBase64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	_, parsedSerial, parsedLineage := parseRawStateMetadata(rawState)
	serial := payload.Serial
	if serial == 0 {
		if parsedSerial > 0 {
			serial = parsedSerial
		} else {
			var latest StateVersion
			if err := db.Where("workspace_id = ?", workspaceID).Order("serial desc").First(&latest).Error; err == nil {
				serial = latest.Serial + 1
			} else {
				serial = 1
			}
		}
	}

	lineage := payload.Lineage
	if lineage == "" {
		if parsedLineage != "" {
			lineage = parsedLineage
		} else {
			latest, found := latestUploadedState(workspaceID)
			if found && latest.Lineage != "" {
				lineage = latest.Lineage
			} else {
				lineage = fmt.Sprintf("lineage-%d", time.Now().UnixNano())
			}
		}
	}

	sv := StateVersion{
		ID:             "sv-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		WorkspaceID:    workspaceID,
		Serial:         serial,
		Lineage:        lineage,
		RawState:       rawState,
		UploadComplete: true,
		CreatedAt:      time.Now(),
	}
	if err := db.Create(&sv).Error; err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	for _, provider := range payload.Providers {
		if provider.Source == "" {
			continue
		}
		selection := ProviderSelection{
			ID:             fmt.Sprintf("ps-%d", time.Now().UnixNano()),
			StateVersionID: sv.ID,
			Source:         provider.Source,
			Version:        provider.Version,
			CreatedAt:      time.Now(),
		}
		db.Create(&selection)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": map[string]interface{}{
			"id":             sv.ID,
			"workspace_id":   sv.WorkspaceID,
			"serial":         sv.Serial,
			"lineage":        sv.Lineage,
			"provider_count": len(payload.Providers),
		},
	})
}
