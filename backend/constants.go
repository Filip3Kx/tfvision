package main

const (
	defaultWorkspaceTerraformVersion = "1.14.8"

	// Run status values accepted by the CLI and stored in the database.
	RunStatusPlanned = "planned"
	RunStatusApplied = "applied"
	RunStatusError   = "error"

	// State version lifecycle status values returned by the API.
	StateVersionStatusPending  = "pending"
	StateVersionStatusUploaded = "uploaded"

	// Workspace execution mode reported to the Terraform CLI.
	ExecutionModeLocal = "local"

	// Maximum accepted request body sizes to prevent unbounded memory allocation.
	maxStateBodyBytes = 128 * 1024 * 1024 // 128 MiB – generous limit for large states
	maxLogBodyBytes   = 16 * 1024 * 1024  // 16 MiB  – enough for any realistic Terraform log
)
