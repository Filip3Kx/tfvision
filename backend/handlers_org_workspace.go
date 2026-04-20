package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

func handlePing(w http.ResponseWriter, r *http.Request) {
	writeTFEHeaders(w)
	writeJSONAPI(w, http.StatusOK, map[string]interface{}{
		"meta": map[string]interface{}{"status": "ok"},
	})
}

func handleListOrganizations(w http.ResponseWriter, r *http.Request) {
	var orgs []Organization
	if err := db.Find(&orgs).Error; err != nil {
		writeInternalError(w)
		return
	}
	data := make([]interface{}, 0, len(orgs))
	for _, org := range orgs {
		data = append(data, orgResource(org))
	}
	writeJSONAPI(w, http.StatusOK, map[string]interface{}{"data": data})
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
		writeBadRequest(w, "invalid request body")
		return
	}

	id := p.Data.ID
	if id == "" {
		id = p.Data.Attributes.Name
	}
	if id == "" {
		writeBadRequest(w, "organization name is required")
		return
	}

	org := Organization{ID: id, Name: p.Data.Attributes.Name, CreatedAt: time.Now()}
	if org.Name == "" {
		org.Name = id
	}
	if err := db.Create(&org).Error; err != nil {
		writeInternalError(w)
		return
	}

	writeJSONAPI(w, http.StatusCreated, map[string]interface{}{"data": orgResource(org)})
}

func handleGetOrganization(w http.ResponseWriter, r *http.Request) {
	orgName := mux.Vars(r)["org"]
	writeJSONAPI(w, http.StatusOK, map[string]interface{}{
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
	writeJSONAPI(w, http.StatusOK, map[string]interface{}{
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
	if err := db.Where("organization_id = ?", orgID).Find(&workspaces).Error; err != nil {
		writeInternalError(w)
		return
	}

	data := make([]interface{}, 0, len(workspaces))
	for _, ws := range workspaces {
		data = append(data, workspaceListResource(ws))
	}
	writeJSONAPI(w, http.StatusOK, map[string]interface{}{"data": data})
}

// handleGetOrCreateWorkspace implements the Terraform Cloud workspace lookup
// endpoint.  When Terraform sends the workspace name (e.g. "my-workspace"), we
// look it up first by its canonical "ws-<name>" ID, then by name.  If neither
// matches we create it with a deterministic "ws-<name>" ID.
func handleGetOrCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	orgName := vars["org"]
	wsName := vars["workspace"]

	// Ensure the organisation exists, creating it on-the-fly if needed.
	var org Organization
	if err := db.Where("id = ?", orgName).First(&org).Error; err != nil {
		org = Organization{ID: orgName, Name: orgName, CreatedAt: time.Now()}
		if err := db.Create(&org).Error; err != nil {
			writeInternalError(w)
			return
		}
	}

	ws, err := findOrCreateWorkspace(orgName, wsName)
	if err != nil {
		writeInternalError(w)
		return
	}

	writeJSONAPI(w, http.StatusOK, map[string]interface{}{"data": workspaceDetailResource(ws, orgName)})
}

// findOrCreateWorkspace finds a workspace by the canonical ID or name, creating
// it if neither lookup succeeds.
func findOrCreateWorkspace(orgName, wsName string) (Workspace, error) {
	canonicalID, displayName := canonicalizeWorkspaceID(wsName)

	// 1. Look up by the canonical workspace ID.
	var ws Workspace
	if err := db.Where("id = ? AND organization_id = ?", canonicalID, orgName).First(&ws).Error; err == nil {
		return ws, nil
	}

	// 2. Look up by name (handles lookups where the caller already knows the
	//    canonical ID and passes it without the "ws-" prefix).
	if err := db.Where("name = ? AND organization_id = ?", displayName, orgName).First(&ws).Error; err == nil {
		return ws, nil
	}

	// 3. Create a new workspace with the canonical ID.
	ws = Workspace{
		ID:               canonicalID,
		Name:             displayName,
		OrganizationID:   orgName,
		CreatedAt:        time.Now(),
		TerraformVersion: defaultWorkspaceTerraformVersion,
	}
	if err := db.Create(&ws).Error; err != nil {
		return Workspace{}, err
	}
	return ws, nil
}

// canonicalizeWorkspaceID returns the canonical (ws-prefixed) ID and the
// human-readable display name from an incoming workspace identifier.
//
// Rules:
//   - If wsName already starts with "ws-", it is already the canonical ID;
//     the display name is the part after the prefix.
//   - Otherwise the canonical ID is "ws-" + wsName and the display name is
//     wsName unchanged.
func canonicalizeWorkspaceID(wsName string) (id, name string) {
	if strings.HasPrefix(wsName, "ws-") {
		trimmed := strings.TrimPrefix(wsName, "ws-")
		if trimmed == "" {
			// Pathological input: "ws-" with nothing after it – keep as-is.
			return wsName, wsName
		}
		return wsName, trimmed
	}
	return "ws-" + wsName, wsName
}

func handleLockWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspace_id"]
	var ws Workspace
	if err := db.Where("id = ?", workspaceID).First(&ws).Error; err != nil {
		writeNotFound(w)
		return
	}

	if ws.Locked {
		writeConflict(w, "workspace already locked")
		return
	}

	var payload struct {
		Data struct {
			Attributes struct {
				Reason string `json:"reason"`
			} `json:"attributes"`
		} `json:"data"`
	}
	// Decode is best-effort; an empty body is valid for a lock request.
	_ = json.NewDecoder(r.Body).Decode(&payload)

	ws.Locked = true
	ws.LockID = newID("lock")
	ws.LockInfo = payload.Data.Attributes.Reason
	if err := db.Save(&ws).Error; err != nil {
		writeInternalError(w)
		return
	}

	writeJSONAPI(w, http.StatusOK, map[string]interface{}{
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
		writeNotFound(w)
		return
	}

	ws.Locked = false
	ws.LockID = ""
	ws.LockInfo = ""
	if err := db.Save(&ws).Error; err != nil {
		writeInternalError(w)
		return
	}

	writeJSONAPI(w, http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{
			"id":   ws.ID,
			"type": "workspaces",
			"attributes": map[string]interface{}{
				"locked": ws.Locked,
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Response shape helpers
// ---------------------------------------------------------------------------

func orgResource(org Organization) map[string]interface{} {
	return map[string]interface{}{
		"id":   org.ID,
		"type": "organizations",
		"attributes": map[string]interface{}{
			"name": org.Name,
		},
	}
}

func workspaceListResource(ws Workspace) map[string]interface{} {
	tfVersion := ws.TerraformVersion
	if tfVersion == "" {
		tfVersion = defaultWorkspaceTerraformVersion
	}
	return map[string]interface{}{
		"id":   ws.ID,
		"type": "workspaces",
		"attributes": map[string]interface{}{
			"name":              ws.Name,
			"execution-mode":    ExecutionModeLocal,
			"terraform-version": tfVersion,
		},
	}
}

func workspaceDetailResource(ws Workspace, orgName string) map[string]interface{} {
	tfVersion := ws.TerraformVersion
	if tfVersion == "" {
		tfVersion = defaultWorkspaceTerraformVersion
	}
	return map[string]interface{}{
		"id":   ws.ID,
		"type": "workspaces",
		"attributes": map[string]interface{}{
			"name":              ws.Name,
			"execution-mode":    ExecutionModeLocal,
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
			"organization": map[string]interface{}{
				"data": map[string]interface{}{"id": orgName, "type": "organizations"},
			},
		},
		"links": map[string]interface{}{
			"self": "/api/v2/workspaces/" + ws.ID,
		},
	}
}

