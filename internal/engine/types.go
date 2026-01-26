package engine

import "github.com/danieljhkim/monodev/internal/planner"

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
}

// PathInfo contains information about an applied path.
type PathInfo struct {
	// Store is the store that owns this path
	Store string

	// Type is the path type ("symlink" or "copy")
	Type string
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
