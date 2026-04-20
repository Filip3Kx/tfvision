package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLockFile(t *testing.T) {
	content := `
# This file is maintained automatically by "terraform init".
# Manual edits may be lost in future updates.

provider "registry.terraform.io/hashicorp/aws" {
  version     = "5.0.0"
  constraints = "~> 5.0"
  hashes = [
    "h1:abcdef",
  ]
}

provider "registry.terraform.io/hashicorp/random" {
  version     = "3.5.1"
  constraints = ">= 3.0.0"
  hashes = [
    "h1:xyz",
  ]
}
`
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".terraform.lock.hcl")
	if err := os.WriteFile(lockPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	providers, err := parseLockFile(lockPath)
	if err != nil {
		t.Fatalf("parseLockFile returned error: %v", err)
	}
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(providers))
	}

	bySource := map[string]string{}
	for _, p := range providers {
		bySource[p.Source] = p.Version
	}

	if v := bySource["registry.terraform.io/hashicorp/aws"]; v != "5.0.0" {
		t.Errorf("aws version = %q, want 5.0.0", v)
	}
	if v := bySource["registry.terraform.io/hashicorp/random"]; v != "3.5.1" {
		t.Errorf("random version = %q, want 3.5.1", v)
	}
}

func TestParseLockFile_Empty(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".terraform.lock.hcl")
	os.WriteFile(lockPath, []byte("# empty\n"), 0644)

	providers, err := parseLockFile(lockPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(providers) != 0 {
		t.Errorf("expected 0 providers, got %d", len(providers))
	}
}

func TestParseLockFile_NotExist(t *testing.T) {
	_, err := parseLockFile("/nonexistent/.terraform.lock.hcl")
	if !os.IsNotExist(err) {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}
}

func TestParseLockFile_NoVersion(t *testing.T) {
	content := `
provider "registry.terraform.io/hashicorp/null" {
  hashes = ["h1:abc"]
}
`
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".terraform.lock.hcl")
	os.WriteFile(lockPath, []byte(content), 0644)

	providers, err := parseLockFile(lockPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(providers))
	}
	if providers[0].Version != "unknown" {
		t.Errorf("version = %q, want unknown", providers[0].Version)
	}
}

func TestIsValidRunStatus(t *testing.T) {
	cases := []struct {
		input string
		valid bool
	}{
		{"planned", true},
		{"applied", true},
		{"error", true},
		{"PLANNED", true},
		{"running", false},
		{"", false},
		{"unknown", false},
	}
	for _, tc := range cases {
		got := isValidRunStatus(tc.input)
		if got != tc.valid {
			t.Errorf("isValidRunStatus(%q) = %v, want %v", tc.input, got, tc.valid)
		}
	}
}

func TestExtractQuotedValue(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{`provider "registry.terraform.io/hashicorp/aws" {`, "registry.terraform.io/hashicorp/aws"},
		{`version = "5.0.0"`, "5.0.0"},
		{"no quotes here", ""},
		{`"only start`, ""},
	}
	for _, tc := range cases {
		got := extractQuotedValue(tc.input)
		if got != tc.want {
			t.Errorf("extractQuotedValue(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestJoinURL(t *testing.T) {
	cases := []struct {
		base string
		path string
		want string
	}{
		{"http://localhost:8080", "/api/v2/ping", "http://localhost:8080/api/v2/ping"},
		{"http://localhost:8080/", "/api/v2/ping", "http://localhost:8080/api/v2/ping"},
	}
	for _, tc := range cases {
		got := joinURL(tc.base, tc.path)
		if got != tc.want {
			t.Errorf("joinURL(%q, %q) = %q, want %q", tc.base, tc.path, got, tc.want)
		}
	}
}
