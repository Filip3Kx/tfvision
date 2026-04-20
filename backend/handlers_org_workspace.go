package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

func handlePing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.api+json")
	w.Header().Set("X-TFE-Version", "v202501-1")
	w.Header().Set("X-Terraform-Enterprise-Version", "v202501-1")
	w.Header().Set("TFP-API-Version", "2.5")
	w.Header().Set("TFP-AppName", "Terraform Enterprise")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"meta": map[string]interface{}{"status": "ok"},
	})
}

func handleListOrganizations(w http.ResponseWriter, r *http.Request) {
	var orgs []Organization
	db.Find(&orgs)
	data := make([]interface{}, 0, len(orgs))
	for _, org := range orgs {
		data = append(data, map[string]interface{}{
			"id":   org.ID,
			"type": "organizations",
			"attributes": map[string]interface{}{
				"name": org.Name,
			},
		})
	}
	w.Header().Set("Content-Type", "application/vnd.api+json")
	json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
}

func handleCreateOrganization(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				Name string `json:"name"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	id := p.Data.ID
	if id == "" {
		id = p.Data.Attributes.Name
	}
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	org := Organization{ID: id, Name: p.Data.Attributes.Name, CreatedAt: time.Now()}
	if org.Name == "" {
		org.Name = id
	}
	if err := db.Create(&org).Error; err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.api+json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": map[string]interface{}{
			"id":   org.ID,
			"type": "organizations",
			"attributes": map[string]interface{}{
				"name": org.Name,
			},
		},
	})
}

func handleGetOrganization(w http.ResponseWriter, r *http.Request) {
	orgName := mux.Vars(r)["org"]
	w.Header().Set("Content-Type", "application/vnd.api+json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": map[string]interface{}{
			"id":   orgName,
			"type": "organizations",
			"attributes": map[string]interface{}{
				"name": orgName,
				"permissions": map[string]interface{}{
					"can-create-workspaces": true,
				},
			},
		},
	})
}

func handleGetEntitlementSet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.api+json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": map[string]interface{}{
			"id":   "ent-state-only",
			"type": "entitlement-sets",
			"attributes": map[string]interface{}{
				"operations":    false,
				"state-storage": true,
			},
		},
	})
}

func handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["org"]
	var workspaces []Workspace
	db.Where("organization_id = ?", orgID).Find(&workspaces)

	workspaceByID := make(map[string]Workspace, len(workspaces))
	for _, ws := range workspaces {
		workspaceByID[ws.ID] = ws
	}

	data := make([]interface{}, 0, len(workspaces))
	for _, ws := range workspaces {
		if strings.HasPrefix(ws.ID, "ws-ws-") {
			canonicalID := strings.TrimPrefix(ws.ID, "ws-")
			if _, ok := workspaceByID[canonicalID]; ok {
				continue
			}
		}

		tfVersion := ws.TerraformVersion
		if tfVersion == "" {
			tfVersion = defaultWorkspaceTerraformVersion
		}
		data = append(data, map[string]interface{}{
			"id":   ws.ID,
			"type": "workspaces",
			"attributes": map[string]interface{}{
				"name":              ws.Name,
				"execution-mode":    "local",
				"terraform-version": tfVersion,
			},
		})
	}

	w.Header().Set("Content-Type", "application/vnd.api+json")
	json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
}

func handleGetOrCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	orgName := vars["org"]
	wsName := vars["workspace"]

	var org Organization
	if err := db.Where("id = ?", orgName).First(&org).Error; err != nil {
		org = Organization{ID: orgName, Name: orgName, CreatedAt: time.Now()}
		db.Create(&org)
	}

	var ws Workspace
	if err := db.Where("id = ? AND organization_id = ?", wsName, orgName).First(&ws).Error; err != nil {
		if err := db.Where("name = ? AND organization_id = ?", wsName, orgName).First(&ws).Error; err != nil {
			normalizedName := wsName
			workspaceID := "ws-" + wsName
			if strings.HasPrefix(wsName, "ws-") {
				workspaceID = wsName
				trimmed := strings.TrimPrefix(wsName, "ws-")
				if trimmed != "" {
					normalizedName = trimmed
				}
			}
			ws = Workspace{
				ID:               workspaceID,
				Name:             normalizedName,
				OrganizationID:   orgName,
				CreatedAt:        time.Now(),
				TerraformVersion: defaultWorkspaceTerraformVersion,
			}
			db.Create(&ws)
		}
	}

	tfVersion := ws.TerraformVersion
	if tfVersion == "" {
		tfVersion = defaultWorkspaceTerraformVersion
	}

	w.Header().Set("Content-Type", "application/vnd.api+json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": map[string]interface{}{
			"id":   ws.ID,
			"type": "workspaces",
			"attributes": map[string]interface{}{
				"name":              ws.Name,
				"execution-mode":    "local",
				"terraform-version": tfVersion,
				"locked":            ws.Locked,
				"can-queue-run":     false,
				"permissions": map[string]interface{}{
					"can-update":     true,
					"can-read-state": true,
					"can-queue-run":  false,
					"can-lock":       true,
					"can-unlock":     true,
				},
			},
			"relationships": map[string]interface{}{
				"organization": map[string]interface{}{"data": map[string]interface{}{"id": orgName, "type": "organizations"}},
			},
			"links": map[string]interface{}{"self": "/api/v2/workspaces/" + ws.ID},
		},
	})
}

func handleLockWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspace_id"]
	var ws Workspace
	if err := db.Where("id = ?", workspaceID).First(&ws).Error; err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if ws.Locked {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]interface{}{{
				"status": "409",
				"title":  "workspace already locked",
			}},
		})
		return
	}

	var payload struct {
		Data struct {
			Attributes struct {
				Reason string `json:"reason"`
			} `json:"attributes"`
		} `json:"data"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)

	ws.Locked = true
	ws.LockID = fmt.Sprintf("lock-%d", time.Now().UnixNano())
	ws.LockInfo = payload.Data.Attributes.Reason
	db.Save(&ws)

	w.Header().Set("Content-Type", "application/vnd.api+json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": map[string]interface{}{
			"id":   ws.ID,
			"type": "workspaces",
			"attributes": map[string]interface{}{
				"locked": ws.Locked,
			},
		},
	})
}

func handleUnlockWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspace_id"]
	var ws Workspace
	if err := db.Where("id = ?", workspaceID).First(&ws).Error; err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	ws.Locked = false
	ws.LockID = ""
	ws.LockInfo = ""
	db.Save(&ws)

	w.Header().Set("Content-Type", "application/vnd.api+json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": map[string]interface{}{
			"id":   ws.ID,
			"type": "workspaces",
			"attributes": map[string]interface{}{
				"locked": ws.Locked,
			},
		},
	})
}
