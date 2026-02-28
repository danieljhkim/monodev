package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/danieljhkim/monodev/internal/state"
)

// Unapply removes paths owned by the active store from the workspace.
//
// Only removes paths that were applied via 'monodev apply' (the active store).
// Paths applied by the stack (via 'stack apply') are not affected.
// Use 'stack unapply' to remove stack-applied paths.
//
// Algorithm:
// 1. Discover repo and load workspace state (must exist)
// 2. Collect paths owned by the active store
// 3. Remove paths in deepest-first order
// 4. Update workspace state
func (e *Engine) Unapply(ctx context.Context, req *UnapplyRequest) (*UnapplyResult, error) {
	// Step 1: Discover repository
	root, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to discover workspace: %w", err)
	}

	// Step 2: Compute workspace ID
	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)

	// Step 3: Load workspace state
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: workspace has no managed paths", ErrStateMissing)
		}
		return nil, fmt.Errorf("failed to load workspace state: %w", err)
	}
	workspaceState.AbsolutePath = filepath.Join(root, workspacePath)

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

	// Step 5: Remove active store paths in deepest-first order
	// Sort paths by depth (deepest first)
	sort.Slice(activeStorePaths, func(i, j int) bool {
		// Count path separators to determine depth
		depthI := countPathSeparators(activeStorePaths[i])
		depthJ := countPathSeparators(activeStorePaths[j])
		if depthI != depthJ {
			return depthI > depthJ // Deeper paths first
		}
		return activeStorePaths[i] > activeStorePaths[j] // Alphabetically for same depth
	})

	workspaceRoot := filepath.Join(root, workspacePath)

	removed := []string{}
	for _, relPath := range activeStorePaths {
		ownership := workspaceState.Paths[relPath]

		// Validate relative path for safety
		if err := e.fs.ValidateRelPath(relPath); err != nil {
			return nil, fmt.Errorf("invalid path %q in workspace state: %w", relPath, err)
		}

		// Convert workspace-relative path to absolute for filesystem operations
		absPath := filepath.Join(workspaceRoot, relPath)

		// Validate the path before removing (unless force)
		if !req.Force {
			if err := e.validateManagedPath(absPath, ownership); err != nil {
				return nil, fmt.Errorf("validation failed for %s: %w", relPath, err)
			}
		}

		// Remove the path (use absolute path)
		if err := e.fs.RemoveAll(absPath); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to remove %s: %w", relPath, err)
		}

		// Remove from workspace state
		delete(workspaceState.Paths, relPath)
		removed = append(removed, relPath)
	}

	// Step 6: Update workspace state
	// do not delete workspace state if no paths remain
	if len(workspaceState.Paths) > 0 {
		// Still have paths from other stores - update state
		workspaceState.Applied = false
		workspaceState.PruneAppliedStores()
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
func (e *Engine) validateManagedPath(path string, ownership state.PathOwnership) error {
	// Check if path exists
	exists, err := e.fs.Exists(path)
	if err != nil {
		return fmt.Errorf("failed to check if path exists: %w", err)
	}
	if !exists {
		// Path doesn't exist - nothing to validate
		return nil
	}

	// For copies, check the checksum to detect drift
	if ownership.Checksum != "" {
		currentHash, err := e.hasher.HashFile(path)
		if err == nil && currentHash != ownership.Checksum {
			// File has been modified - this is drift
			// For unapply, we still remove it (user modifications are lost)
			// A warning could be added here in the future
			_ = currentHash // Acknowledge drift detection (no-op for now)
		}
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
