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

	// StoreID is an optional store ID to apply instead of the active store
	StoreID string
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

// StatusRequest represents a request for workspace status.
type StatusRequest struct {
	// CWD is the current working directory
	CWD string
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

// StackUnapplyRequest represents a request to unapply the stack portion only.
type StackUnapplyRequest struct {
	// CWD is the current working directory (workspace path)
	CWD string

	// Force allows removing paths even if validation fails
	Force bool

	// DryRun shows what would be removed without actually removing
	DryRun bool
}

// DeleteStoreRequest represents a request to delete a store.
type DeleteStoreRequest struct {
	StoreID string
	Force   bool // Skip safety checks
	DryRun  bool // Preview only
}

// DeleteWorkspaceRequest represents a request to delete a workspace.
type DeleteWorkspaceRequest struct {
	WorkspaceID string
	Force       bool
	DryRun      bool
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

// StackListRequest represents a request to list the store stack.
type StackListRequest struct {
	// CWD is the current working directory
	CWD string
}

// StackAddRequest represents a request to add a store to the stack.
type StackAddRequest struct {
	// CWD is the current working directory
	CWD string

	// StoreID is the store to add to the stack
	StoreID string
}

// StackPopRequest represents a request to remove a store from the stack.
type StackPopRequest struct {
	// CWD is the current working directory
	CWD string

	// StoreID is the store to remove (if empty, removes last store - LIFO)
	StoreID string
}

// StackClearRequest represents a request to clear the stack.
type StackClearRequest struct {
	// CWD is the current working directory
	CWD string
}
