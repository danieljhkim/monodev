package engine

import (
	"github.com/danieljhkim/monodev/internal/planner"
	"github.com/danieljhkim/monodev/internal/state"
)

// ApplyRequest represents a request to apply store overlays.
type ApplyRequest struct {
	// CWD is the current working directory (workspace path)
	CWD string

	// Mode is the overlay mode ("symlink" or "copy")
	Mode string

	// Force allows overwriting conflicts
	Force bool

	// DryRun performs planning only without making changes
	DryRun bool

	// StoreID is an optional store ID to apply instead of the active store
	StoreID string
}

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

// UnapplyRequest represents a request to unapply overlays.
type UnapplyRequest struct {
	// CWD is the current working directory (workspace path)
	CWD string

	// Force allows removing paths even if validation fails
	Force bool

	// DryRun shows what would be removed without actually removing
	DryRun bool
}

// UnapplyResult represents the result of unapplying overlays.
type UnapplyResult struct {
	// Removed is the list of paths that were removed
	Removed []string

	// WorkspaceID is the workspace ID
	WorkspaceID string

	message string
}

// StatusRequest represents a request for workspace status.
type StatusRequest struct {
	// CWD is the current working directory
	CWD string
}

// StatusResult represents the current workspace status.
type StatusResult struct {

	// WorkspaceID is the workspace ID
	WorkspaceID string

	// RepoFingerprint is the repository fingerprint
	RepoFingerprint string

	// WorkspacePath is the relative path from repo root
	WorkspacePath string

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

// PathInfo contains information about an applied path.
type PathInfo struct {
	// Store is the store that owns this path
	Store string

	// Type is the path type ("symlink" or "copy")
	Type string
}

// AppliedStoreInfo contains information about an applied store.
type AppliedStoreInfo struct {
	// StoreID is the store identifier
	StoreID string

	// Mode is the overlay mode for this store
	Mode string

	// AppliedCount is the number of paths applied from this store
	AppliedCount int
}

// TrackedPathInfo contains detailed information about a tracked path.
type TrackedPathInfo struct {
	// Path is the relative path
	Path string

	// IsApplied indicates if the path exists in workspace.Paths
	IsApplied bool

	// IsSaved indicates if the path exists in the store overlay
	IsSaved bool
}

// StackApplyRequest represents a request to apply the configured stack.
type StackApplyRequest struct {
	// CWD is the current working directory (workspace path)
	CWD string

	// Mode is the overlay mode ("symlink" or "copy")
	Mode string

	// Force allows overwriting conflicts
	Force bool

	// DryRun performs planning only without making changes
	DryRun bool
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

// StackUnapplyRequest represents a request to unapply the stack portion only.
type StackUnapplyRequest struct {
	// CWD is the current working directory (workspace path)
	CWD string

	// Force allows removing paths even if validation fails
	Force bool

	// DryRun shows what would be removed without actually removing
	DryRun bool
}

// StackUnapplyResult represents the result of unapplying the stack.
type StackUnapplyResult struct {
	// Removed is the list of paths that were removed
	Removed []string

	// WorkspaceID is the workspace ID
	WorkspaceID string
}

// DeleteStoreRequest represents a request to delete a store.
type DeleteStoreRequest struct {
	StoreID string
	Force   bool // Skip safety checks
	DryRun  bool // Preview only
}

// DeleteStoreResult represents the result of deleting a store.
type DeleteStoreResult struct {
	StoreID            string
	AffectedWorkspaces []WorkspaceUsage
	DryRun             bool
	Deleted            bool
}

// WorkspaceUsage describes how a workspace uses a store.
type WorkspaceUsage struct {
	WorkspaceID      string
	WorkspacePath    string
	IsActive         bool
	InStack          bool
	AppliedPathCount int
}

// ListWorkspacesResult represents the result of listing workspaces.
type ListWorkspacesResult struct {
	Workspaces []WorkspaceInfo
}

// WorkspaceInfo contains summary information about a workspace.
type WorkspaceInfo struct {
	WorkspaceID      string
	WorkspacePath    string
	Repo             string
	Applied          bool
	Mode             string
	ActiveStore      string
	StackCount       int
	AppliedPathCount int
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

// DeleteWorkspaceRequest represents a request to delete a workspace.
type DeleteWorkspaceRequest struct {
	WorkspaceID string
	Force       bool
	DryRun      bool
}

// DeleteWorkspaceResult represents the result of deleting a workspace.
type DeleteWorkspaceResult struct {
	WorkspaceID   string
	WorkspacePath string
	Deleted       bool
	DryRun        bool
	PathsRemoved  int
}
