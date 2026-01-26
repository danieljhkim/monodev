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

	// AppliedStores is the list of stores that have been applied
	AppliedStores []AppliedStore `json:"appliedStores"`

	// ActiveStore is the store that was active when overlays were applied
	ActiveStore string `json:"activeStore"`

	// Paths maps destination paths to their ownership information
	Paths map[string]PathOwnership `json:"paths"`
}

type AppliedStore struct {
	// Store is the ID of the store that has been applied
	Store string `json:"store"`

	// Mode is the overlay mode ("symlink" or "copy")
	Type string `json:"type"`
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

// NewWorkspaceState creates a new empty WorkspaceState.
func NewWorkspaceState(repo, workspacePath, mode string) *WorkspaceState {
	return &WorkspaceState{
		Repo:          repo,
		WorkspacePath: workspacePath,
		Applied:       false,
		Mode:          mode,
		Stack:         []string{},
		AppliedStores: []AppliedStore{},
		ActiveStore:   "",
		Paths:         make(map[string]PathOwnership),
	}
}

func (ws *WorkspaceState) AddAppliedStore(store string, mode string) {
	ws.RemoveAppliedStore(store)
	ws.AppliedStores = append(ws.AppliedStores, AppliedStore{Store: store, Type: mode})
}

func (ws *WorkspaceState) RemoveAppliedStore(store string) {
	for i, appliedStore := range ws.AppliedStores {
		if appliedStore.Store == store {
			ws.AppliedStores = append(ws.AppliedStores[:i], ws.AppliedStores[i+1:]...)
			break
		}
	}
}

func (ws *WorkspaceState) GetAppliedStore(store string) *AppliedStore {
	for _, appliedStore := range ws.AppliedStores {
		if appliedStore.Store == store {
			return &appliedStore
		}
	}
	return nil
}

func (ws *WorkspaceState) PruneAppliedStores() {
	// removes applied stores list that do not have a path in the workspace
	newAppliedStores := []AppliedStore{}
	for _, appliedStore := range ws.AppliedStores {
		if appliedStore.Store != ws.ActiveStore {
			newAppliedStores = append(newAppliedStores, appliedStore)
		}
	}
	ws.AppliedStores = newAppliedStores
}
