package engine

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

	// IsModified indicates if the workspace version differs from the store overlay
	IsModified bool
}

// WorkspaceUsage describes how a workspace uses a store.
type WorkspaceUsage struct {
	WorkspaceID      string
	WorkspacePath    string
	IsActive         bool
	InStack          bool
	AppliedPathCount int
}

// WorkspaceInfo contains summary information about a workspace.
type WorkspaceInfo struct {
	WorkspaceID      string
	WorkspacePath    string
	AbsolutePath     string
	Repo             string
	Applied          bool
	Mode             string
	ActiveStore      string
	StackCount       int
	AppliedPathCount int
}

// DiffFileInfo contains information about a single diffed file.
type DiffFileInfo struct {
	// Path is the relative path from workspace root
	Path string

	// Status is the diff status: "modified", "added", "removed", "unchanged"
	Status string

	// WorkspaceHash is the hash of the file in the workspace (empty if doesn't exist)
	WorkspaceHash string

	// StoreHash is the hash of the file in store overlay (empty if doesn't exist)
	StoreHash string

	// UnifiedDiff contains the unified diff content (if ShowContent is true)
	UnifiedDiff string

	// Additions is the number of added lines in the diff
	Additions int

	// Deletions is the number of removed lines in the diff
	Deletions int

	// IsDir indicates if the path is a directory
	IsDir bool
}
