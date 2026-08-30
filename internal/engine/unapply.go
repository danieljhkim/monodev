package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/lockfile"
	"github.com/danieljhkim/monodev/internal/state"
)

// Unapply removes paths owned by the active store from the workspace.
//
// Only removes paths that were applied via 'monodev apply' (the active store).
// Paths applied by the stack (via 'stack apply') are not affected.
// Use 'stack unapply' to remove stack-applied paths.
//
// Algorithm:
//  1. Discover repo and load workspace state (must exist)
//  2. Collect paths owned by the active store
//  3. Remove paths in deepest-first order
//  4. Delete workspace state when no managed paths remain; otherwise retain
//     any stack-owned paths in state.
func (e *Engine) Unapply(ctx context.Context, req *UnapplyRequest) (*UnapplyResult, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	// Step 1: Discover repository
	root, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to discover workspace: %w", err)
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	// Step 2: Compute workspace ID
	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)
	unlockWorkspace, err := e.lockWorkspace(ctx, workspaceID, lockfile.Exclusive)
	if err != nil {
		return nil, err
	}
	defer unlockWorkspace()

	// Step 3: Load workspace state
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: workspace has no managed paths", ErrStateMissing)
		}
		return nil, fmt.Errorf("failed to load workspace state: %w", err)
	}
	workspaceState.AbsolutePath = filepath.Join(root, workspacePath)
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	workspaceRoot := filepath.Join(root, workspacePath)
	var warnings []string
	if !req.DryRun {
		recoveryWarnings, recoverErr := e.recoverWorkspaceOverlay(ctx, workspaceID, root, workspaceRoot, workspacePath)
		if recoverErr != nil {
			return nil, recoverErr
		}
		warnings = append(warnings, recoveryWarnings...)
		reloaded, reloadErr := e.stateStore.LoadWorkspace(workspaceID)
		if reloadErr != nil {
			if os.IsNotExist(reloadErr) {
				return nil, fmt.Errorf("%w: workspace has no managed paths", ErrStateMissing)
			}
			return nil, fmt.Errorf("failed to reload workspace state: %w", reloadErr)
		}
		workspaceState = reloaded
		workspaceState.AbsolutePath = workspaceRoot
	}

	// Step 4: Collect only paths owned by the active store (not stack stores)
	activeStore := workspaceState.ActiveStore
	activeStorePaths := []string{}
	for relPath, ownership := range workspaceState.Paths {
		if ownership.Store == activeStore {
			activeStorePaths = append(activeStorePaths, relPath)
		}
	}

	// Check if there are any active store paths to remove
	if len(activeStorePaths) == 0 {
		return &UnapplyResult{
			Removed:     []string{},
			WorkspaceID: workspaceID,
			Warnings:    warnings,
			message:     "nothing to remove",
		}, nil
	}

	// If dry run, just return the list of paths that would be removed
	if req.DryRun {
		return &UnapplyResult{
			Removed:     activeStorePaths,
			WorkspaceID: workspaceID,
			Warnings:    warnings,
			message:     "dry run",
		}, nil
	}

	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	ops, removed, err := e.planManagedPathRemoval(workspaceRoot, workspaceState, activeStorePaths, req.Force)
	if err != nil {
		return nil, err
	}

	final := state.CloneWorkspaceState(workspaceState)
	for _, relPath := range removed {
		delete(final.Paths, relPath)
	}
	deleteState := len(final.Paths) == 0
	if !deleteState {
		final.Applied = true
		final.PruneAppliedStores()
	}

	if err := e.runOverlayTxn(ctx, overlayTxnRequest{
		kind:          overlayTxnUnapply,
		workspaceID:   workspaceID,
		workspaceRoot: workspaceRoot,
		ops:           ops,
		finalize: func() (*state.WorkspaceState, bool, error) {
			if deleteState {
				return nil, true, nil
			}
			return final, false, nil
		},
	}); err != nil {
		return nil, err
	}
	finalExcludeState := final
	if deleteState {
		finalExcludeState = nil
	}
	warnings = appendExcludeWarning(warnings, e.syncManagedExcludes(root, workspacePath, finalExcludeState))

	return &UnapplyResult{
		Removed:     removed,
		WorkspaceID: workspaceID,
		Warnings:    warnings,
	}, nil
}

// validateManagedPath validates that a path is still managed by monodev.
func (e *Engine) validateManagedPath(absPath, relPath string, ownership state.PathOwnership) error {
	exists, err := e.fs.Exists(absPath)
	if err != nil {
		return fmt.Errorf("failed to check if path exists: %w", err)
	}
	if !exists {
		return nil
	}

	if ownership.Type != "copy" {
		return nil
	}

	info, err := e.fs.Lstat(absPath)
	if err != nil {
		return fmt.Errorf("failed to stat path: %w", err)
	}
	if info.IsDir() || ownership.Contents != nil {
		if !info.IsDir() {
			return fmt.Errorf("%w: %w: copied directory %s is no longer a directory; %s", ErrValidation, ErrDrift, relPath, forceUnapplyHint)
		}
		return e.validateCopiedDirectory(absPath, relPath, ownership)
	}

	if ownership.Checksum == "" {
		return nil
	}
	currentHash, err := e.hasher.HashFile(absPath)
	if err != nil {
		return fmt.Errorf("%w: failed to verify copy checksum: %w", ErrValidation, err)
	}
	if currentHash != ownership.Checksum {
		return fmt.Errorf("%w: %w: local modifications detected", ErrValidation, ErrDrift)
	}

	return nil
}

// countPathSeparators counts the number of path separators in a path.
func countPathSeparators(path string) int {
	count := 0
	for _, ch := range path {
		if ch == '/' || ch == '\\' {
			count++
		}
	}
	return count
}
