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

	// ActiveStoreScope records which scope the active store belongs to
	ActiveStoreScope string `json:"activeStoreScope,omitempty"`

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
	for i := range ws.AppliedStores {
		if ws.AppliedStores[i].Store == store {
			return &ws.AppliedStores[i]
		}
	}
	return nil
}

// removes the applied stores list based on the paths in the workspace
func (ws *WorkspaceState) PruneAppliedStores() {
	newAppliedStores := []AppliedStore{}
	for _, appliedStore := range ws.AppliedStores {
		for _, path := range ws.Paths {
			if path.Store == appliedStore.Store {
				newAppliedStores = append(newAppliedStores, appliedStore)
				break
			}
		}
	}
	ws.AppliedStores = newAppliedStores
}

// updates the applied stores list based on the paths in the workspace
func (ws *WorkspaceState) RefreshAppliedStores() {
	newAppliedStores := []AppliedStore{}
	appliedStoresMap := make(map[string]struct{})
	for _, path := range ws.Paths {
		appliedStoresMap[path.Store] = struct{}{}
	}

	for key := range appliedStoresMap { // TODO: just one mode for now per workspace
		newAppliedStores = append(newAppliedStores, AppliedStore{Store: key, Type: ws.Mode})
	}
	ws.AppliedStores = newAppliedStores
}
