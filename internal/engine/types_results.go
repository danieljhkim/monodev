package engine

import (
	"github.com/danieljhkim/monodev/internal/planner"
	"github.com/danieljhkim/monodev/internal/state"
)

// ApplyResult represents the result of applying store overlays.
type ApplyResult struct {
	// Plan is the generated plan
	Plan *planner.ApplyPlan

	// Applied is the list of operations that were executed (empty if DryRun)
	Applied []planner.Operation

	// WorkspaceID is the computed workspace ID
	WorkspaceID string

	// RepoFingerprint is the repository fingerprint
	RepoFingerprint string

	// WorkspacePath is the relative path from repo root
	WorkspacePath string
}

// UnapplyResult represents the result of unapplying overlays.
type UnapplyResult struct {
	// Removed is the list of paths that were removed
	Removed []string

	// WorkspaceID is the workspace ID
	WorkspaceID string

	// Warnings contains non-fatal follow-up issues, such as an unavailable
	// repository-local exclude file.
	Warnings []string

	message string
}

// EjectResult describes the workspace paths affected while detaching monodev.
// Stores remain available; eject only removes this workspace's ownership ledger.
type EjectResult struct {
	// Retained lists paths left in place by keep-files mode.
	Retained []string

	// Removed lists paths deleted by remove-files mode.
	Removed []string

	// WorkspaceID is the detached workspace's ID.
	WorkspaceID string

	// RemoveFiles reports whether remove-files mode was selected.
	RemoveFiles bool

	// DryRun reports whether this was only a plan.
	DryRun bool

	// Warnings contains non-fatal follow-up issues, such as an unavailable
	// repository-local exclude file.
	Warnings []string
}

// StatusResult represents the current workspace status.
type StatusResult struct {

	// WorkspaceID is the workspace ID
	WorkspaceID string

	// RepoFingerprint is the repository fingerprint
	RepoFingerprint string

	// WorkspacePath is the relative path from repo root
	WorkspacePath string

	// AbsolutePath is the absolute path to the repository root
	AbsolutePath string

	// GitURL is the git remote origin URL (empty if not a git repo)
	GitURL string

	// Applied indicates if overlays are currently applied
	Applied bool

	// Mode is the current overlay mode
	Mode string

	// Stack is retained for JSON compatibility and is always empty after stack retirement.
	Stack []string

	// ActiveStore is the active store
	ActiveStore string

	// Paths is the map of applied paths
	Paths map[string]PathInfo

	// TrackedPaths is the list of paths tracked in the active store
	TrackedPaths []string

	// AppliedStores is the list of stores that have been applied
	AppliedStores []string

	// All paths in the workspace
	AllPaths []string

	// AppliedStoreDetails contains per-store applied path counts
	AppliedStoreDetails []AppliedStoreInfo

	// TrackedPathDetails contains detailed info for tracked paths in active store
	TrackedPathDetails []TrackedPathInfo

	// ActiveStoreStatus is the application status of the active store
	ActiveStoreStatus string // "Applied", "Not Applied", or "Partial"
}

// DeleteStoreResult represents the result of deleting a store.
type DeleteStoreResult struct {
	StoreID            string
	AffectedWorkspaces []WorkspaceUsage
	DryRun             bool
	Deleted            bool
}

// ListWorkspacesResult represents the result of listing workspaces.
type ListWorkspacesResult struct {
	Workspaces []WorkspaceInfo
}

// DescribeWorkspaceResult represents the result of describing a workspace.
type DescribeWorkspaceResult struct {
	WorkspaceID   string
	WorkspacePath string
	Repo          string
	Applied       bool
	Mode          string
	ActiveStore   string
	Stack         []string
	AppliedStores []state.AppliedStore
	Paths         map[string]state.PathOwnership
}

// DeleteWorkspaceResult represents the result of deleting a workspace.
type DeleteWorkspaceResult struct {
	WorkspaceID   string
	WorkspacePath string
	Deleted       bool
	DryRun        bool
	PathsRemoved  int
}

// OrphanedWorkspace describes a workspace file that belongs to the current
// repository but is stored under a fingerprint that no longer matches.
type OrphanedWorkspace struct {
	WorkspaceID      string
	CurrentID        string
	WorkspacePath    string
	AbsolutePath     string
	Repo             string
	ActiveStore      string
	Applied          bool
	AppliedPathCount int
}

// ListOrphanedWorkspacesResult is the repair listing payload.
type ListOrphanedWorkspacesResult struct {
	RepoFingerprint string
	RepoRoot        string
	Orphans         []OrphanedWorkspace
}

// RebindWorkspaceResult is the outcome of rebinding an orphaned workspace.
type RebindWorkspaceResult struct {
	OldWorkspaceID string
	NewWorkspaceID string
	WorkspacePath  string
	ActiveStore    string
	Applied        bool
	AppliedPaths   int
}

// DiffResult represents the result of a diff operation.
type DiffResult struct {
	// WorkspaceID is the workspace identifier
	WorkspaceID string

	// StoreID is the store that was diffed against
	StoreID string

	// Files contains all diffed files with their status
	Files []DiffFileInfo
}
