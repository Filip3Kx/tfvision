package main

import (
	"time"
)

type Organization struct {
	ID        string `gorm:"primary_key"`
	Name      string `gorm:"uniqueIndex"`
	CreatedAt time.Time
}

// Workspace represents a Terraform workspace inside an organisation.
// The Name column has a composite unique index scoped to the organisation so
// two different organisations can independently have a workspace named "prod".
// NOTE: If you are upgrading from an earlier schema that had a global uniqueIndex
// on Name, drop the old constraint manually before running the server.
type Workspace struct {
	ID             string `gorm:"primary_key"`
	Name           string `gorm:"uniqueIndex:idx_workspace_org_name"`
	OrganizationID string `gorm:"uniqueIndex:idx_workspace_org_name;index"`
	CreatedAt      time.Time

	TerraformVersion string

	// A workspace can be locked to prevent concurrent state operations.
	Locked   bool
	LockID   string
	LockInfo string // json lock info
}

type StateVersion struct {
	ID             string `gorm:"primary_key"`
	WorkspaceID    string `gorm:"index"`
	Serial         int
	Lineage        string
	RawState       []byte // binary .tfstate
	UploadComplete bool
	CreatedAt      time.Time
}

type CLIRun struct {
	ID             string `gorm:"primary_key"`
	WorkspaceID    string `gorm:"index"`
	Command        string
	Status         string
	Message        string
	LogBody        string `gorm:"type:text"`
	StateVersionID string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CompletedAt    *time.Time
}

// ProviderSelection stores the provider version pinned for a given state version.
// The composite unique index on (state_version_id, source) prevents duplicate
// provider entries for the same source when sync-state is called more than once.
type ProviderSelection struct {
	ID             string `gorm:"primary_key"`
	StateVersionID string `gorm:"uniqueIndex:idx_provider_sv_source;index"`
	Source         string `gorm:"uniqueIndex:idx_provider_sv_source"`
	Version        string
	CreatedAt      time.Time
}

