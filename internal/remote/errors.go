package remote

import "errors"

var (
	// ErrRemoteNotConfigured is returned when a remote operation is attempted
	// but no remote configuration exists.
	ErrRemoteNotConfigured = errors.New("remote not configured, run 'monodev remote use <name>'")

	// ErrRemoteNotFound is returned when the specified remote doesn't exist
	// in the main repository's git config.
	ErrRemoteNotFound = errors.New("remote not found in repository")

	// ErrBranchNotFound is returned when the persistence branch doesn't exist
	// on the remote.
	ErrBranchNotFound = errors.New("persistence branch not found on remote")

	// ErrStoreNotInRemote is returned when attempting to pull a store that
	// doesn't exist in the remote persistence repository.
	ErrStoreNotInRemote = errors.New("store not found in remote")

	// ErrFingerprintMismatch is returned when a workspace ref's repo fingerprint
	// doesn't match the current repository.
	ErrFingerprintMismatch = errors.New("workspace repository fingerprint mismatch")
)
