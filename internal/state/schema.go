package state

import "time"

// WorkspaceState represents the state of overlays applied to a workspace.
// This is the authoritative record of what monodev has modified in a workspace.
type WorkspaceState struct {
	// Repo is the fingerprint of the git repository
	Repo string `json:"repo"`

	// WorkspacePath is the relative path from repo root to the workspace
	WorkspacePath string `json:"workspacePath"`

	// Applied indicates whether overlays are currently applied
	Applied bool `json:"applied"`

	// Mode is the overlay mode ("symlink" or "copy")
	Mode string `json:"mode"`

	// Stack is the ordered list of stores applied (excluding active store)
	Stack []string `json:"stack"`

	// ActiveStore is the store that was active when overlays were applied
	ActiveStore string `json:"activeStore"`

	// Paths maps destination paths to their ownership information
	Paths map[string]PathOwnership `json:"paths"`
}

// PathOwnership describes which store owns a specific path and how it was applied.
type PathOwnership struct {
	// Store is the ID of the store that contributed this path
	Store string `json:"store"`

	// Type is how the path was applied ("symlink" or "copy")
	Type string `json:"type"`

	// Timestamp is when the path was applied
	Timestamp time.Time `json:"timestamp"`

	// Checksum is the hash of the file (only used in copy mode)
	Checksum string `json:"checksum,omitempty"`
}

// RepoState represents repository-level state (stack and active store).
// This persists the store stack and active store selection per repository.
type RepoState struct {
	// Fingerprint is the repository fingerprint
	Fingerprint string `json:"fingerprint"`

	// Stack is the ordered list of stores to apply
	Stack []string `json:"stack"`

	// ActiveStore is the currently selected store
	ActiveStore string `json:"activeStore"`
}

// NewWorkspaceState creates a new empty WorkspaceState.
func NewWorkspaceState(repo, workspacePath, mode string) *WorkspaceState {
	return &WorkspaceState{
		Repo:          repo,
		WorkspacePath: workspacePath,
		Applied:       false,
		Mode:          mode,
		Stack:         []string{},
		ActiveStore:   "",
		Paths:         make(map[string]PathOwnership),
	}
}

// NewRepoState creates a new empty RepoState.
func NewRepoState(fingerprint string) *RepoState {
	return &RepoState{
		Fingerprint: fingerprint,
		Stack:       []string{},
		ActiveStore: "",
	}
}
