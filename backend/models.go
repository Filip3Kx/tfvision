package main

import (
	"time"
)

type Organization struct {
	ID        string `gorm:"primary_key"`
	Name      string `gorm:"uniqueIndex"`
	CreatedAt time.Time
}

type Workspace struct {
	ID             string `gorm:"primary_key"`
	Name           string `gorm:"uniqueIndex"`
	OrganizationID string
	CreatedAt      time.Time

	TerraformVersion string

	// A workspace can be locked to prevent concurrent state operations.
	Locked   bool
	LockID   string
	LockInfo string // json lock info
}

type StateVersion struct {
	ID             string `gorm:"primary_key"`
	WorkspaceID    string
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

type ProviderSelection struct {
	ID             string `gorm:"primary_key"`
	StateVersionID string `gorm:"index"`
	Source         string
	Version        string
	CreatedAt      time.Time
}
