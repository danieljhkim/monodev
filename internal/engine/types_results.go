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

	message string
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

	// Stack is the store stack
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

// StackApplyResult represents the result of applying the stack.
type StackApplyResult struct {
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

// StackUnapplyResult represents the result of unapplying the stack.
type StackUnapplyResult struct {
	// Removed is the list of paths that were removed
	Removed []string

	// WorkspaceID is the workspace ID
	WorkspaceID string
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

// DiffResult represents the result of a diff operation.
type DiffResult struct {
	// WorkspaceID is the workspace identifier
	WorkspaceID string

	// StoreID is the store that was diffed against
	StoreID string

	// Files contains all diffed files with their status
	Files []DiffFileInfo
}

// StackListResult represents the result of listing the store stack.
type StackListResult struct {
	// Stack is the ordered list of stores
	Stack []string

	// ActiveStore is the currently active store
	ActiveStore string
}

// StackPopResult represents the result of removing a store from the stack.
type StackPopResult struct {
	// Removed is the store that was removed
	Removed string
}
