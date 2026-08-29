package sync

// PushRequest contains parameters for pushing stores and workspaces to a remote.
type PushRequest struct {
	// RepoRoot is the root directory of the repository
	RepoRoot string

	// StoreIDs is the list of store IDs to push
	StoreIDs []string

	// WorkspaceID is the ID of the workspace to push (optional)
	WorkspaceID string

	// RepositoryIdentity is a portable identity for the repository containing
	// the workspace. It is distinct from the local workspace fingerprint,
	// which can include an absolute checkout path.
	RepositoryIdentity string

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

	// PushedWorkspace indicates whether a workspace ref was pushed or would be pushed in a dry run
	PushedWorkspace bool

	// WorkspaceID is the ID of the workspace ref that was pushed
	WorkspaceID string

	// WorkspaceRefPath is the persistence work tree path for the workspace ref artifact
	WorkspaceRefPath string

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

	// LocalWorkspaceID is the workspace state ID for this checkout. It may
	// differ from WorkspaceID because a persisted reference can originate on
	// another machine.
	LocalWorkspaceID string

	// RepoFingerprint is the local fingerprint persisted in restored workspace
	// state. It is not used to authorize a remote workspace reference.
	RepoFingerprint string

	// RepositoryIdentity is the portable identity of the current repository,
	// used to validate a persisted workspace reference before restoration.
	RepositoryIdentity string

	// WorkspacePath is the current workspace's path relative to RepoRoot.
	WorkspacePath string

	// WithStores indicates whether to recursively pull stores referenced by workspace
	WithStores bool

	// Remote is the name of the Git remote to pull from (defaults to config value)
	Remote string

	// Force indicates whether to overwrite a local store whose content
	// differs from what is about to be pulled.
	Force bool
}

// PullResult contains the result of a pull operation.
type PullResult struct {
	// PulledStores is the list of store IDs that were pulled
	PulledStores []string

	// PulledWorkspace indicates whether a workspace ref was pulled
	PulledWorkspace bool

	// WorkspaceReferenceFound reports whether the requested persisted workspace
	// reference was present after fetching the persistence branch.
	WorkspaceReferenceFound bool

	// WorkspaceReferenceValidated reports whether the requested reference passed
	// schema, repository, workspace, store, and local-state validation.
	WorkspaceReferenceValidated bool

	// WorkspaceID is the local workspace state ID restored from the reference.
	WorkspaceID string

	// Verified indicates whether checksums were verified
	Verified bool

	// Remote is the remote that was pulled from
	Remote string

	// Branch is the branch that was pulled
	Branch string

	// Warnings contains operator-visible messages about trust or integrity
	// conditions that did not block the pull but should not pass silently,
	// such as a store pulled without a verification manifest.
	Warnings []string
}
