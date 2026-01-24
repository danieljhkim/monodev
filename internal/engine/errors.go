package engine

import "errors"

var (
	// ErrConflict indicates a conflict was detected during apply.
	ErrConflict = errors.New("conflict detected")

	// ErrValidation indicates a validation failure.
	ErrValidation = errors.New("validation failed")

	// ErrNotFound indicates a resource was not found.
	ErrNotFound = errors.New("not found")

	// ErrDrift indicates drift was detected in copy mode.
	ErrDrift = errors.New("drift detected")

	// ErrStateMissing indicates workspace state is missing.
	ErrStateMissing = errors.New("state missing")

	// ErrNotInRepo indicates the current directory is not in a git repository.
	ErrNotInRepo = errors.New("not in a git repository")

	// ErrNoActiveStore indicates no active store is set.
	ErrNoActiveStore = errors.New("no active store set")
)
