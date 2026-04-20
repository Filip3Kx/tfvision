package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gorilla/mux"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ---------------------------------------------------------------------------
// Test database setup
// ---------------------------------------------------------------------------

func setupTestDB(t *testing.T) {
	t.Helper()
	testDB, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open in-memory SQLite: %v", err)
	}
	testDB.AutoMigrate(&Organization{}, &Workspace{}, &StateVersion{}, &CLIRun{}, &ProviderSelection{})
	db = testDB
	t.Cleanup(func() {
		sqlDB, _ := testDB.DB()
		sqlDB.Close()
	})
}

// ---------------------------------------------------------------------------
// Pure function tests (no database required)
// ---------------------------------------------------------------------------

func TestNormalizeRunStatus(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"planned", RunStatusPlanned},
		{"PLANNED", RunStatusPlanned},
		{"  applied  ", RunStatusApplied},
		{"error", RunStatusError},
		{"ERROR", RunStatusError},
		{"unknown", ""},
		{"", ""},
		{"running", ""},
	}
	for _, tc := range cases {
		got := normalizeRunStatus(tc.input)
		if got != tc.want {
			t.Errorf("normalizeRunStatus(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeProviderSource(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{`provider["registry.terraform.io/hashicorp/aws"]`, "registry.terraform.io/hashicorp/aws"},
		{"registry.terraform.io/hashicorp/aws", "registry.terraform.io/hashicorp/aws"},
		{"", "unknown"},
	}
	for _, tc := range cases {
		got := normalizeProviderSource(tc.input)
		if got != tc.want {
			t.Errorf("normalizeProviderSource(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestQualifyAddress(t *testing.T) {
	cases := []struct {
		modulePath string
		address    string
		want       string
	}{
		{"root", "aws_instance.web", "aws_instance.web"},
		{"", "aws_instance.web", "aws_instance.web"},
		{"module.vpc", "aws_subnet.private", "module.vpc.aws_subnet.private"},
		{"module.vpc", "module.inner.aws_sg.x", "module.inner.aws_sg.x"},
		{"module.vpc", "data.aws_ami.ubuntu", "module.vpc.data.aws_ami.ubuntu"},
		{"module.vpc", "", ""},
	}
	for _, tc := range cases {
		got := qualifyAddress(tc.modulePath, tc.address)
		if got != tc.want {
			t.Errorf("qualifyAddress(%q, %q) = %q, want %q", tc.modulePath, tc.address, got, tc.want)
		}
	}
}

func TestModulePathFromAddress(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"aws_instance.web", "root"},
		{"module.vpc.aws_subnet.private", "module.vpc"},
		{"module.vpc.module.sub.aws_sg.x", "module.vpc.module.sub"},
		{"", "root"},
	}
	for _, tc := range cases {
		got := modulePathFromAddress(tc.input)
		if got != tc.want {
			t.Errorf("modulePathFromAddress(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestCanonicalizeWorkspaceID(t *testing.T) {
	cases := []struct {
		input  string
		wantID string
		wantNm string
	}{
		{"my-workspace", "ws-my-workspace", "my-workspace"},
		{"ws-my-workspace", "ws-my-workspace", "my-workspace"},
		{"ws-", "ws-", "ws-"},
	}
	for _, tc := range cases {
		gotID, gotNm := canonicalizeWorkspaceID(tc.input)
		if gotID != tc.wantID || gotNm != tc.wantNm {
			t.Errorf("canonicalizeWorkspaceID(%q) = (%q, %q), want (%q, %q)",
				tc.input, gotID, gotNm, tc.wantID, tc.wantNm)
		}
	}
}

func TestExtractResources_LegacyState(t *testing.T) {
	raw := []byte(`{
		"terraform_version": "1.5.0",
		"serial": 3,
		"lineage": "abc-123",
		"resources": [
			{
				"mode": "managed",
				"type": "aws_instance",
				"name": "web",
				"provider": "provider[\"registry.terraform.io/hashicorp/aws\"]",
				"instances": [{"attributes": {"id": "i-1234"}}]
			}
		]
	}`)

	resources := extractResources(raw)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	res := resources[0]
	if res.Type != "aws_instance" {
		t.Errorf("type = %q, want aws_instance", res.Type)
	}
	if res.ProviderSource != "registry.terraform.io/hashicorp/aws" {
		t.Errorf("provider_source = %q, want registry.terraform.io/hashicorp/aws", res.ProviderSource)
	}
}

func TestParseRawStateMetadata(t *testing.T) {
	raw := []byte(`{"terraform_version":"1.5.0","serial":7,"lineage":"test-lineage"}`)
	tfv, serial, lineage := parseRawStateMetadata(raw)
	if tfv != "1.5.0" || serial != 7 || lineage != "test-lineage" {
		t.Errorf("parseRawStateMetadata returned (%q, %d, %q)", tfv, serial, lineage)
	}
}

func TestParseRawStateMetadata_Invalid(t *testing.T) {
	// Must not panic on invalid JSON; return zero values.
	tfv, serial, lineage := parseRawStateMetadata([]byte("not-json"))
	if tfv != "" || serial != 0 || lineage != "" {
		t.Errorf("expected zero values for invalid JSON, got (%q, %d, %q)", tfv, serial, lineage)
	}
}

func TestNewID(t *testing.T) {
	id := newID("sv")
	if !strings.HasPrefix(id, "sv-") {
		t.Errorf("newID(%q) = %q, missing prefix", "sv", id)
	}
	if id2 := newID("sv"); id == id2 {
		t.Errorf("newID produced identical IDs: %q", id)
	}
}

// ---------------------------------------------------------------------------
// Handler contract tests (require in-memory SQLite)
// ---------------------------------------------------------------------------

func TestHandlePing(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v2/ping", nil)
	rr := httptest.NewRecorder()
	handlePing(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("ping returned %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != contentTypeJSONAPI {
		t.Errorf("Content-Type = %q, want %q", ct, contentTypeJSONAPI)
	}
	if rr.Header().Get("X-TFE-Version") == "" {
		t.Error("X-TFE-Version header missing")
	}
}

func TestHandleHealth(t *testing.T) {
	setupTestDB(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	handleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("health returned %d, want 200", rr.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("health body status = %v, want ok", body["status"])
	}
}

func TestHandleServiceDiscovery(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/.well-known/terraform.json", nil)
	rr := httptest.NewRecorder()
	handleServiceDiscovery(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("service discovery returned %d, want 200", rr.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&body)
	if _, ok := body["tfe.v2"]; !ok {
		t.Error("service discovery missing tfe.v2 key")
	}
}

func TestHandleCreateOrganization(t *testing.T) {
	setupTestDB(t)

	body := `{"data":{"id":"test-org","attributes":{"name":"Test Org"}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/organizations", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handleCreateOrganization(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("create org returned %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	data := resp["data"].(map[string]interface{})
	if data["id"] != "test-org" {
		t.Errorf("org id = %v, want test-org", data["id"])
	}
}

func TestHandleCreateOrganization_MissingName(t *testing.T) {
	setupTestDB(t)

	body := `{"data":{"attributes":{}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/organizations", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handleCreateOrganization(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestHandleGetOrCreateWorkspace(t *testing.T) {
	setupTestDB(t)

	db.Create(&Organization{ID: "my-org", Name: "my-org"})

	req := httptest.NewRequest(http.MethodGet, "/api/v2/organizations/my-org/workspaces/prod", nil)
	req = mux.SetURLVars(req, map[string]string{"org": "my-org", "workspace": "prod"})
	rr := httptest.NewRecorder()
	handleGetOrCreateWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("get-or-create workspace returned %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	data := resp["data"].(map[string]interface{})
	if data["id"] != "ws-prod" {
		t.Errorf("workspace id = %v, want ws-prod", data["id"])
	}
	attrs := data["attributes"].(map[string]interface{})
	if attrs["name"] != "prod" {
		t.Errorf("workspace name = %v, want prod", attrs["name"])
	}
}

func TestHandleGetOrCreateWorkspace_Idempotent(t *testing.T) {
	setupTestDB(t)

	db.Create(&Organization{ID: "acme", Name: "acme"})

	call := func() string {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/organizations/acme/workspaces/staging", nil)
		req = mux.SetURLVars(req, map[string]string{"org": "acme", "workspace": "staging"})
		rr := httptest.NewRecorder()
		handleGetOrCreateWorkspace(rr, req)
		var resp map[string]interface{}
		json.NewDecoder(rr.Body).Decode(&resp)
		return resp["data"].(map[string]interface{})["id"].(string)
	}

	id1 := call()
	id2 := call()
	if id1 != id2 {
		t.Errorf("second call returned different id: %q vs %q", id1, id2)
	}
	if id1 != "ws-staging" {
		t.Errorf("workspace id = %q, want ws-staging", id1)
	}
}

func TestHandleLockAndUnlock(t *testing.T) {
	setupTestDB(t)

	db.Create(&Workspace{ID: "ws-abc", Name: "abc", OrganizationID: "org"})

	// Lock.
	req := httptest.NewRequest(http.MethodPost, "/api/v2/workspaces/ws-abc/actions/lock", strings.NewReader(`{}`))
	req = mux.SetURLVars(req, map[string]string{"workspace_id": "ws-abc"})
	rr := httptest.NewRecorder()
	handleLockWorkspace(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("lock returned %d: %s", rr.Code, rr.Body.String())
	}

	// Lock again → conflict.
	req = httptest.NewRequest(http.MethodPost, "/api/v2/workspaces/ws-abc/actions/lock", strings.NewReader(`{}`))
	req = mux.SetURLVars(req, map[string]string{"workspace_id": "ws-abc"})
	rr = httptest.NewRecorder()
	handleLockWorkspace(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 on second lock, got %d", rr.Code)
	}

	// Unlock.
	req = httptest.NewRequest(http.MethodPost, "/api/v2/workspaces/ws-abc/actions/unlock", nil)
	req = mux.SetURLVars(req, map[string]string{"workspace_id": "ws-abc"})
	rr = httptest.NewRecorder()
	handleUnlockWorkspace(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unlock returned %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleCreateAndGetStateVersion(t *testing.T) {
	setupTestDB(t)

	db.Create(&Workspace{ID: "ws-test", Name: "test", OrganizationID: "org"})

	// Create state version.
	body := `{"data":{"attributes":{"serial":1,"lineage":"lin-abc"}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/workspaces/ws-test/state-versions", strings.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"workspace_id": "ws-test"})
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	handleCreateStateVersion(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("create state version returned %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	data := resp["data"].(map[string]interface{})
	svID := data["id"].(string)
	if !strings.HasPrefix(svID, "sv-") {
		t.Errorf("state version id %q does not have sv- prefix", svID)
	}
	attrs := data["attributes"].(map[string]interface{})
	if attrs["status"] != StateVersionStatusPending {
		t.Errorf("status = %v, want pending", attrs["status"])
	}

	// Get state version.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v2/state-versions/"+svID, nil)
	req2 = mux.SetURLVars(req2, map[string]string{"id": svID})
	req2.Host = "localhost"
	rr2 := httptest.NewRecorder()
	handleGetStateVersion(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("get state version returned %d", rr2.Code)
	}
}

func TestHandleUploadAndDownloadState(t *testing.T) {
	setupTestDB(t)

	rawState := []byte(`{"terraform_version":"1.5.0","serial":1,"lineage":"test-lin"}`)
	sv := StateVersion{ID: "sv-upload-test", WorkspaceID: "ws-x", Serial: 1, UploadComplete: false}
	db.Create(&sv)

	// Upload.
	req := httptest.NewRequest(http.MethodPut, "/internal/state/sv-upload-test/upload", strings.NewReader(string(rawState)))
	req = mux.SetURLVars(req, map[string]string{"sv_id": "sv-upload-test"})
	rr := httptest.NewRecorder()
	handleUploadState(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("upload returned %d: %s", rr.Code, rr.Body.String())
	}

	// Download.
	req2 := httptest.NewRequest(http.MethodGet, "/internal/state/sv-upload-test", nil)
	req2 = mux.SetURLVars(req2, map[string]string{"sv_id": "sv-upload-test"})
	rr2 := httptest.NewRecorder()
	handleDownloadState(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("download returned %d", rr2.Code)
	}
	if rr2.Body.String() != string(rawState) {
		t.Errorf("downloaded state does not match uploaded state")
	}
}

func TestHandleCLIRuns(t *testing.T) {
	setupTestDB(t)

	db.Create(&Workspace{ID: "ws-runs", Name: "runs", OrganizationID: "org"})

	// Create run.
	body := `{"command":"plan","status":"planned","message":"dry run"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/workspaces/ws-runs/cli-runs", strings.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"workspace_id": "ws-runs"})
	rr := httptest.NewRecorder()
	handleCLIRuns(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create run returned %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	runID := resp["data"].(map[string]interface{})["id"].(string)

	// List runs.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v2/workspaces/ws-runs/cli-runs", nil)
	req2 = mux.SetURLVars(req2, map[string]string{"workspace_id": "ws-runs"})
	rr2 := httptest.NewRecorder()
	handleCLIRuns(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("list runs returned %d", rr2.Code)
	}
	var listResp map[string]interface{}
	json.NewDecoder(rr2.Body).Decode(&listResp)
	runs := listResp["data"].([]interface{})
	if len(runs) != 1 {
		t.Errorf("expected 1 run, got %d", len(runs))
	}

	// Get specific run.
	req3 := httptest.NewRequest(http.MethodGet, "/api/v2/cli-runs/"+runID, nil)
	req3 = mux.SetURLVars(req3, map[string]string{"run_id": runID})
	rr3 := httptest.NewRecorder()
	handleGetCLIRun(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("get run returned %d", rr3.Code)
	}
}

func TestHandleCLIRuns_InvalidStatus(t *testing.T) {
	setupTestDB(t)

	db.Create(&Workspace{ID: "ws-bad", Name: "bad", OrganizationID: "org"})

	body := `{"command":"plan","status":"running"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/workspaces/ws-bad/cli-runs", strings.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"workspace_id": "ws-bad"})
	rr := httptest.NewRecorder()
	handleCLIRuns(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid status, got %d", rr.Code)
	}
}

func TestHandleStateSyncFullMode(t *testing.T) {
	setupTestDB(t)

	db.Create(&Workspace{ID: "ws-sync", Name: "sync", OrganizationID: "org"})

	rawState := `{"terraform_version":"1.5.0","serial":1,"lineage":"lin-123"}`
	encoded := base64.StdEncoding.EncodeToString([]byte(rawState))

	body := `{"raw_state_base64":"` + encoded + `","providers":[{"source":"registry.terraform.io/hashicorp/aws","version":"5.0.0"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/workspaces/ws-sync/state-sync", strings.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"workspace_id": "ws-sync"})
	rr := httptest.NewRecorder()
	handleStateSync(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("state sync returned %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleStateSyncEnrichMode(t *testing.T) {
	setupTestDB(t)

	rawState := []byte(`{"terraform_version":"1.5.0","serial":1,"lineage":"lin-abc"}`)
	sv := StateVersion{ID: "sv-enrich", WorkspaceID: "ws-enrich", Serial: 1, Lineage: "lin-abc", RawState: rawState, UploadComplete: true}
	db.Create(&sv)

	body := `{"providers":[{"source":"registry.terraform.io/hashicorp/aws","version":"5.0.0"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/workspaces/ws-enrich/state-sync", strings.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"workspace_id": "ws-enrich"})
	rr := httptest.NewRecorder()
	handleStateSync(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("enrich sync returned %d: %s", rr.Code, rr.Body.String())
	}

	// Ensure provider was saved.
	var count int64
	db.Model(&ProviderSelection{}).Where("state_version_id = ?", "sv-enrich").Count(&count)
	if count != 1 {
		t.Errorf("expected 1 provider selection, got %d", count)
	}

	// Call again – upsert must keep count at 1.
	req2 := httptest.NewRequest(http.MethodPost, "/api/v2/workspaces/ws-enrich/state-sync", strings.NewReader(body))
	req2 = mux.SetURLVars(req2, map[string]string{"workspace_id": "ws-enrich"})
	rr2 := httptest.NewRecorder()
	handleStateSync(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("second enrich sync returned %d", rr2.Code)
	}

	db.Model(&ProviderSelection{}).Where("state_version_id = ?", "sv-enrich").Count(&count)
	if count != 1 {
		t.Errorf("expected 1 provider selection after upsert, got %d", count)
	}
}

