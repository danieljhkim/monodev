package state

import (
	"sort"
	"time"
)

// WorkspaceState represents the state of overlays applied to a workspace.
// This is the authoritative record of what monodev has modified in a workspace.
type WorkspaceState struct {
	// Repo is the fingerprint of the git repository
	Repo string `json:"repo"`

	// WorkspacePath is the relative path from repo root to the workspace
	WorkspacePath string `json:"workspacePath"`

	// AbsolutePath is the absolute filesystem path to the workspace
	AbsolutePath string `json:"absolutePath,omitempty"`

	// Applied indicates whether overlays are currently applied
	Applied bool `json:"applied"`

	// Mode is the overlay mode ("symlink" or "copy")
	Mode string `json:"mode"`

	// Stack is the deprecated ordered store list from the retired stack command.
	// Migrated into AppliedStores on load; kept so old workspace JSON still unmarshals.
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

	// Checksum is the hash of the file (only used in copy mode for files)
	Checksum string `json:"checksum,omitempty"`

	// Contents is the leaf ownership manifest for a copied directory.
	// nil means the record predates directory ownership tracking.
	Contents *DirContents `json:"contents,omitempty"`
}

// DirContents records the files Monodev copied into an owned directory.
// Keys are slash-separated paths relative to the owned directory.
type DirContents struct {
	Files map[string]string `json:"files"`
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

// CloneWorkspaceState returns a deep copy so overlay transactions can mutate a
// candidate ledger without touching the caller-owned in-memory state until commit.
func CloneWorkspaceState(ws *WorkspaceState) *WorkspaceState {
	if ws == nil {
		return nil
	}
	clone := *ws
	if ws.Stack != nil {
		clone.Stack = append([]string{}, ws.Stack...)
	}
	if ws.AppliedStores != nil {
		clone.AppliedStores = append([]AppliedStore{}, ws.AppliedStores...)
	}
	clone.Paths = make(map[string]PathOwnership, len(ws.Paths))
	for relPath, ownership := range ws.Paths {
		if ownership.Contents != nil {
			files := make(map[string]string, len(ownership.Contents.Files))
			for name, checksum := range ownership.Contents.Files {
				files[name] = checksum
			}
			ownership.Contents = &DirContents{Files: files}
		}
		clone.Paths[relPath] = ownership
	}
	return &clone
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

// AppliedStoreIDs returns applied store IDs in ledger order.
func (ws *WorkspaceState) AppliedStoreIDs() []string {
	if ws == nil {
		return []string{}
	}
	ids := make([]string, 0, len(ws.AppliedStores))
	for _, applied := range ws.AppliedStores {
		ids = append(ids, applied.Store)
	}
	return ids
}

// MigrateDeprecatedStack folds the retired stack field into AppliedStores.
// Stores that own at least one path are recorded in stack order, then any
// remaining AppliedStores and path owners. Pending stack entries that never
// applied are dropped. Returns whether the in-memory state changed.
func (ws *WorkspaceState) MigrateDeprecatedStack() bool {
	if ws == nil || len(ws.Stack) == 0 {
		return false
	}

	ownsPath := make(map[string]string, len(ws.Paths))
	for _, ownership := range ws.Paths {
		if _, exists := ownsPath[ownership.Store]; !exists {
			ownsPath[ownership.Store] = ownership.Type
		}
	}

	seen := make(map[string]bool)
	next := make([]AppliedStore, 0, len(ws.Stack)+len(ws.AppliedStores))
	appendOwner := func(id, typ string) {
		if id == "" || seen[id] {
			return
		}
		pathType, ok := ownsPath[id]
		if !ok {
			return
		}
		if pathType != "" {
			typ = pathType
		} else if typ == "" {
			typ = ws.Mode
		}
		next = append(next, AppliedStore{Store: id, Type: typ})
		seen[id] = true
	}

	for _, id := range ws.Stack {
		appendOwner(id, ws.Mode)
	}
	for _, applied := range ws.AppliedStores {
		appendOwner(applied.Store, applied.Type)
	}
	leftover := make([]string, 0, len(ownsPath))
	for id := range ownsPath {
		if !seen[id] {
			leftover = append(leftover, id)
		}
	}
	sort.Strings(leftover)
	for _, id := range leftover {
		appendOwner(id, ownsPath[id])
	}

	ws.AppliedStores = next
	if len(ws.Paths) > 0 {
		ws.Applied = true
	}
	ws.Stack = []string{}
	return true
}
