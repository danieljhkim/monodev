package sync

// PushRequest contains parameters for pushing stores and workspaces to a remote.
type PushRequest struct {
	// RepoRoot is the root directory of the repository
	RepoRoot string

	// StoreIDs is the list of store IDs to push
	StoreIDs []string

	// WorkspaceID is the ID of the workspace to push (optional)
	WorkspaceID string

	// WithWorkspace indicates whether to push workspace refs along with stores
	WithWorkspace bool

	// Remote is the name of the Git remote to push to (defaults to config value)
	Remote string

	// DryRun indicates whether to perform a dry run without actually pushing
	DryRun bool

	// Force indicates whether to force push (overwrite remote changes)
	Force bool
}

// PushResult contains the result of a push operation.
type PushResult struct {
	// PushedStores is the list of store IDs that were pushed
	PushedStores []string

	// PushedWorkspace indicates whether a workspace ref was pushed
	PushedWorkspace bool

	// CommitMessage is the commit message used
	CommitMessage string

	// Remote is the remote that was pushed to
	Remote string

	// Branch is the branch that was pushed
	Branch string

	// DryRun indicates whether this was a dry run
	DryRun bool
}

// PullRequest contains parameters for pulling stores and workspaces from a remote.
type PullRequest struct {
	// RepoRoot is the root directory of the repository
	RepoRoot string

	// StoreIDs is the list of store IDs to pull
	StoreIDs []string

	// WorkspaceID is the ID of the workspace to pull (optional)
	WorkspaceID string

	// WithStores indicates whether to recursively pull stores referenced by workspace
	WithStores bool

	// Remote is the name of the Git remote to pull from (defaults to config value)
	Remote string

	// Force indicates whether to overwrite local stores
	Force bool

	// Verify indicates whether to verify checksums after pulling
	Verify bool
}

// PullResult contains the result of a pull operation.
type PullResult struct {
	// PulledStores is the list of store IDs that were pulled
	PulledStores []string

	// PulledWorkspace indicates whether a workspace ref was pulled
	PulledWorkspace bool

	// Verified indicates whether checksums were verified
	Verified bool

	// Remote is the remote that was pulled from
	Remote string

	// Branch is the branch that was pulled
	Branch string
}
