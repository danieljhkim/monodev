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
			message:     "nothing to remove",
		}, nil
	}

	// If dry run, just return the list of paths that would be removed
	if req.DryRun {
		return &UnapplyResult{
			Removed:     activeStorePaths,
			WorkspaceID: workspaceID,
			message:     "dry run",
		}, nil
	}

	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	workspaceRoot := filepath.Join(root, workspacePath)
	removed, err := e.removeManagedPaths(workspaceRoot, workspaceState, activeStorePaths, req.Force)
	if err != nil {
		return nil, err
	}

	// Step 6: Update workspace state. Unapply removes only active-store paths:
	// if that empties the managed path set, the workspace state is no longer
	// useful and should be removed. Stack-owned paths keep the state alive.
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if len(workspaceState.Paths) == 0 {
		if err := e.stateStore.DeleteWorkspace(workspaceID); err != nil {
			return nil, fmt.Errorf("failed to delete workspace state: %w", err)
		}
		return &UnapplyResult{
			Removed:     removed,
			WorkspaceID: workspaceID,
		}, nil
	}

	workspaceState.Applied = true
	workspaceState.PruneAppliedStores()

	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := e.stateStore.SaveWorkspace(workspaceID, workspaceState); err != nil {
		return nil, fmt.Errorf("failed to save workspace state: %w", err)
	}
	return &UnapplyResult{
		Removed:     removed,
		WorkspaceID: workspaceID,
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
