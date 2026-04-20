package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type providerVersion struct {
	Source  string `json:"source"`
	Version string `json:"version"`
}

type workspaceResponse struct {
	Data struct {
		ID string `json:"id"`
	} `json:"data"`
}

type stateSyncResponse struct {
	Data struct {
		ID            string `json:"id"`
		WorkspaceID   string `json:"workspace_id"`
		Serial        int    `json:"serial"`
		Lineage       string `json:"lineage"`
		ProviderCount int    `json:"provider_count"`
	} `json:"data"`
}

type runResponse struct {
	Data struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"data"`
}

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: tfvision <sync-state|pull-tf-logs> [flags]")
	}

	switch os.Args[1] {
	case "sync-state":
		if err := runSyncState(os.Args[2:]); err != nil {
			fatalf("sync-state failed: %v", err)
		}
	case "pull-tf-logs":
		if err := runPullTFLogs(os.Args[2:]); err != nil {
			fatalf("pull-tf-logs failed: %v", err)
		}
	default:
		fatalf("unknown command: %s", os.Args[1])
	}
}

func runSyncState(args []string) error {
	fs := flag.NewFlagSet("sync-state", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	host := fs.String("host", envOr("TFVISION_HOST", "http://localhost:8080"), "TFVision base URL")
	organization := fs.String("organization", envOr("TFVISION_ORGANIZATION", ""), "organization name")
	workspace := fs.String("workspace", envOr("TFVISION_WORKSPACE", ""), "workspace name")
	stateFile := fs.String("state-file", envOr("TFVISION_STATE_FILE", ""), "optional path to state file; if omitted, enriches the latest DB-backed state with provider versions")
	lockFile := fs.String("lock-file", envOr("TFVISION_LOCK_FILE", ".terraform.lock.hcl"), "path to .terraform.lock.hcl")
	serial := fs.Int("serial", 0, "override state serial")
	lineage := fs.String("lineage", "", "override state lineage")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *organization == "" || *workspace == "" {
		return errors.New("--organization and --workspace are required")
	}

	providers, err := parseLockFile(*lockFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("parse lock file: %w", err)
	}

	workspaceID, err := resolveWorkspaceID(*host, *organization, *workspace)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{
		"providers": providers,
	}
	if *stateFile != "" {
		rawState, err := os.ReadFile(*stateFile)
		if err != nil {
			return fmt.Errorf("read state file: %w", err)
		}
		payload["raw_state_base64"] = base64.StdEncoding.EncodeToString(rawState)
	}
	if *serial > 0 {
		payload["serial"] = *serial
	}
	if *lineage != "" {
		payload["lineage"] = *lineage
	}

	var resp stateSyncResponse
	if err := requestJSON(http.MethodPost, joinURL(*host, fmt.Sprintf("/api/v2/workspaces/%s/state-sync", url.PathEscape(workspaceID))), payload, &resp); err != nil {
		return err
	}

	fmt.Printf("synced state version %s for workspace %s (serial=%d, providers=%d)\n", resp.Data.ID, resp.Data.WorkspaceID, resp.Data.Serial, resp.Data.ProviderCount)
	return nil
}

func runPullTFLogs(args []string) error {
	fs := flag.NewFlagSet("pull-tf-logs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	host := fs.String("host", envOr("TFVISION_HOST", "http://localhost:8080"), "TFVision base URL")
	organization := fs.String("organization", envOr("TFVISION_ORGANIZATION", ""), "organization name")
	workspace := fs.String("workspace", envOr("TFVISION_WORKSPACE", ""), "workspace name")
	command := fs.String("command", "", "terraform command name (init/plan/apply)")
	status := fs.String("status", "", "run status: planned|applied|error")
	message := fs.String("message", "", "message shown in UI")
	logFile := fs.String("log-file", "", "path to captured terraform logs")
	readStdin := fs.Bool("stdin", false, "read log body from stdin")
	stateVersionID := fs.String("state-version-id", "", "optional related state version id")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *organization == "" || *workspace == "" || *command == "" || *status == "" {
		return errors.New("--organization, --workspace, --command, and --status are required")
	}

	logBody, err := loadLogBody(*logFile, *readStdin)
	if err != nil {
		return err
	}

	workspaceID, err := resolveWorkspaceID(*host, *organization, *workspace)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{
		"command":          *command,
		"status":           *status,
		"message":          *message,
		"log_body":         logBody,
		"state_version_id": *stateVersionID,
	}

	var resp runResponse
	if err := requestJSON(http.MethodPost, joinURL(*host, fmt.Sprintf("/api/v2/workspaces/%s/cli-runs", url.PathEscape(workspaceID))), payload, &resp); err != nil {
		return err
	}

	fmt.Printf("stored run %s with status %s\n", resp.Data.ID, resp.Data.Status)
	return nil
}

func loadLogBody(logFile string, readStdin bool) (string, error) {
	if logFile != "" {
		content, err := os.ReadFile(logFile)
		if err != nil {
			return "", fmt.Errorf("read log file: %w", err)
		}
		return string(content), nil
	}
	if readStdin {
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(content), nil
	}
	return "", nil
}

func parseLockFile(path string) ([]providerVersion, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	providerRe := regexp.MustCompile(`provider\s+"([^"]+)"\s+\{([\s\S]*?)\n\}`)
	versionRe := regexp.MustCompile(`version\s*=\s*"([^"]+)"`)
	matches := providerRe.FindAllStringSubmatch(string(content), -1)
	providers := make([]providerVersion, 0, len(matches))
	for _, match := range matches {
		versionMatch := versionRe.FindStringSubmatch(match[2])
		version := "unknown"
		if len(versionMatch) > 1 {
			version = versionMatch[1]
		}
		providers = append(providers, providerVersion{Source: match[1], Version: version})
	}
	return providers, nil
}

func resolveWorkspaceID(host string, organization string, workspace string) (string, error) {
	var resp workspaceResponse
	endpoint := joinURL(host, fmt.Sprintf("/api/v2/organizations/%s/workspaces/%s", url.PathEscape(organization), url.PathEscape(workspace)))
	if err := requestJSON(http.MethodGet, endpoint, nil, &resp); err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	if resp.Data.ID == "" {
		return "", errors.New("workspace response did not include an id")
	}
	return resp.Data.ID, nil
}

func requestJSON(method string, endpoint string, payload interface{}, out interface{}) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed: %s %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func joinURL(base string, path string) string {
	return strings.TrimRight(base, "/") + path
}

func envOr(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "tfvision commands:\n")
		fmt.Fprintf(os.Stderr, "  tfvision sync-state --organization ORG --workspace WS [--lock-file .terraform.lock.hcl] [--state-file terraform.tfstate]\n")
		fmt.Fprintf(os.Stderr, "  tfvision pull-tf-logs --organization ORG --workspace WS --command plan --status planned [--message TEXT] [--log-file plan.log|--stdin]\n")
	}
	_ = filepath.Separator
}
