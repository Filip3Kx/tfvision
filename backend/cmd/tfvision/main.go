package main

import (
	"bufio"
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
	"strings"
	"time"
)

// httpClient is the shared HTTP client used for all API requests.
// A 30-second timeout guards against hung connections and ensures the CLI
// does not block indefinitely when the server is unreachable.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// ---------------------------------------------------------------------------
// Domain types
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
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
		fatalf("unknown command %q\n\nRun tfvision with no arguments for usage.", os.Args[1])
	}
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

func runSyncState(args []string) error {
	fs := flag.NewFlagSet("sync-state", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	host := fs.String("host", envOr("TFVISION_HOST", "http://localhost:8080"), "TFVision base URL")
	organization := fs.String("organization", envOr("TFVISION_ORGANIZATION", ""), "organization name")
	workspace := fs.String("workspace", envOr("TFVISION_WORKSPACE", ""), "workspace name")
	stateFile := fs.String("state-file", envOr("TFVISION_STATE_FILE", ""), "path to .tfstate file; omit to enrich the latest state with provider versions only")
	lockFile := fs.String("lock-file", envOr("TFVISION_LOCK_FILE", ".terraform.lock.hcl"), "path to .terraform.lock.hcl")
	serial := fs.Int("serial", 0, "override state serial (optional)")
	lineage := fs.String("lineage", "", "override state lineage (optional)")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w\n\nUsage: tfvision sync-state --organization ORG --workspace WS [--lock-file .terraform.lock.hcl] [--state-file terraform.tfstate]", err)
	}

	if *organization == "" {
		return errors.New("--organization is required")
	}
	if *workspace == "" {
		return errors.New("--workspace is required")
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

	fmt.Printf("synced state version %s for workspace %s (serial=%d, providers=%d)\n",
		resp.Data.ID, resp.Data.WorkspaceID, resp.Data.Serial, resp.Data.ProviderCount)
	return nil
}

func runPullTFLogs(args []string) error {
	fs := flag.NewFlagSet("pull-tf-logs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	host := fs.String("host", envOr("TFVISION_HOST", "http://localhost:8080"), "TFVision base URL")
	organization := fs.String("organization", envOr("TFVISION_ORGANIZATION", ""), "organization name")
	workspace := fs.String("workspace", envOr("TFVISION_WORKSPACE", ""), "workspace name")
	command := fs.String("command", "", "terraform command (e.g. init, plan, apply)")
	status := fs.String("status", "", "run status: planned|applied|error")
	message := fs.String("message", "", "human-readable message shown in the UI")
	logFile := fs.String("log-file", "", "path to captured terraform log output")
	readStdin := fs.Bool("stdin", false, "read log body from stdin (mutually exclusive with --log-file)")
	stateVersionID := fs.String("state-version-id", "", "optional related state version id")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w\n\nUsage: tfvision pull-tf-logs --organization ORG --workspace WS --command plan --status planned [--log-file plan.log|--stdin]", err)
	}

	if *organization == "" {
		return errors.New("--organization is required")
	}
	if *workspace == "" {
		return errors.New("--workspace is required")
	}
	if *command == "" {
		return errors.New("--command is required")
	}
	if *status == "" {
		return errors.New("--status is required (planned|applied|error)")
	}
	if !isValidRunStatus(*status) {
		return fmt.Errorf("--status %q is not valid; must be one of: planned, applied, error", *status)
	}
	if *logFile != "" && *readStdin {
		return errors.New("--log-file and --stdin are mutually exclusive")
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

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func isValidRunStatus(s string) bool {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "planned", "applied", "error":
		return true
	}
	return false
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

// parseLockFile extracts provider source/version pairs from a .terraform.lock.hcl
// file using a line-by-line parser.  This is more resilient than a single
// multiline regex and correctly handles nested braces, blank lines, and
// provider blocks that span many lines.
func parseLockFile(path string) ([]providerVersion, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var providers []providerVersion
	var currentSource string
	var currentVersion string
	depth := 0

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Detect provider block opening: provider "source" {
		if depth == 0 && strings.HasPrefix(line, "provider") && strings.Contains(line, `"`) {
			currentSource = extractQuotedValue(line)
			currentVersion = ""
			if strings.HasSuffix(line, "{") {
				depth = 1
			}
			continue
		}

		if depth > 0 {
			// Track brace depth.
			depth += strings.Count(line, "{")
			depth -= strings.Count(line, "}")

			// Extract version assignment inside the block.
			if currentVersion == "" && strings.HasPrefix(line, "version") && strings.Contains(line, "=") {
				currentVersion = extractQuotedValue(line)
			}

			// Block closed.
			if depth == 0 && currentSource != "" {
				if currentVersion == "" {
					currentVersion = "unknown"
				}
				providers = append(providers, providerVersion{Source: currentSource, Version: currentVersion})
				currentSource = ""
				currentVersion = ""
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan lock file: %w", err)
	}
	return providers, nil
}

// extractQuotedValue returns the first double-quoted string found in line.
func extractQuotedValue(line string) string {
	start := strings.Index(line, `"`)
	if start < 0 {
		return ""
	}
	end := strings.Index(line[start+1:], `"`)
	if end < 0 {
		return ""
	}
	return line[start+1 : start+1+end]
}

func resolveWorkspaceID(host string, organization string, workspace string) (string, error) {
	var resp workspaceResponse
	endpoint := joinURL(host, fmt.Sprintf("/api/v2/organizations/%s/workspaces/%s",
		url.PathEscape(organization), url.PathEscape(workspace)))
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
			return fmt.Errorf("marshal request: %w", err)
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", method, endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func joinURL(base string, path string) string {
	return strings.TrimRight(base, "/") + path
}

func envOr(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: tfvision <command> [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  sync-state     Sync a Terraform state file and provider versions to TFVision.")
	fmt.Fprintln(os.Stderr, "  pull-tf-logs   Store Terraform run logs in TFVision.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Examples:")
	fmt.Fprintln(os.Stderr, "  tfvision sync-state --organization acme --workspace prod --state-file terraform.tfstate")
	fmt.Fprintln(os.Stderr, "  tfvision pull-tf-logs --organization acme --workspace prod --command plan --status planned --stdin")
}

