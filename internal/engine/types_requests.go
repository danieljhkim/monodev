package engine

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

	// StoreIDs are stores to apply in argument order. Empty means the active store.
	StoreIDs []string
}

// UnapplyRequest represents a request to unapply overlays.
type UnapplyRequest struct {
	// CWD is the current working directory (workspace path)
	CWD string

	// Force allows removing paths even if validation fails
	Force bool

	// DryRun shows what would be removed without actually removing
	DryRun bool

	// StoreIDs are stores whose paths to remove. Empty means the active store.
	StoreIDs []string
}

// StatusRequest represents a request for workspace status.
type StatusRequest struct {
	// CWD is the current working directory
	CWD string
}

// DeleteStoreRequest represents a request to delete a store.
type DeleteStoreRequest struct {
	StoreID string
	Force   bool   // Skip safety checks
	DryRun  bool   // Preview only
	Scope   string // Optional scope to disambiguate (empty = auto-resolve)
}

// DeleteWorkspaceRequest represents a request to delete a workspace.
type DeleteWorkspaceRequest struct {
	WorkspaceID string
	Force       bool
	DryRun      bool
}

// RebindWorkspaceRequest rebinds an orphaned workspace onto the current repo identity.
type RebindWorkspaceRequest struct {
	CWD         string
	WorkspaceID string
	Force       bool
}

// DiffRequest represents a request to diff workspace files against store overlay.
type DiffRequest struct {
	// CWD is the current working directory
	CWD string

	// StoreID is an optional store ID to diff against (default: active store)
	StoreID string

	// ShowContent indicates whether to show actual diff content (unified diff)
	ShowContent bool

	// NameOnly shows only filenames without status indicators
	NameOnly bool

	// NameStatus shows filenames with status indicators (M, A, D)
	NameStatus bool
}
