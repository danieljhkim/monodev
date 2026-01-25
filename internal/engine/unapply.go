package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/danieljhkim/monodev/internal/state"
)

// Unapply removes managed paths from the currently active store.
//
// Only removes paths that belong to the active store, not all managed paths.
// This allows multiple stores to coexist in a workspace - unapply only affects
// the currently active store's contributions.
//
// Algorithm:
// 1. Discover repo and load repo state to get active store
// 2. Load workspace state (must exist)
// 3. Filter paths to only those from active store
// 4. Remove filtered paths in deepest-first order
// 5. Update workspace state (remove only the paths we deleted)
// 6. If no paths remain, delete workspace state; otherwise save it
func (e *Engine) Unapply(ctx context.Context, req *UnapplyRequest) (*UnapplyResult, error) {
	// Step 1: Discover repository
	repoRoot, err := e.gitRepo.Discover(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotInRepo, err)
	}

	repoFingerprint, err := e.gitRepo.Fingerprint(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to compute repo fingerprint: %w", err)
	}

	workspacePath, err := e.gitRepo.RelPath(repoRoot, req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to compute workspace path: %w", err)
	}

	// Step 2: Compute workspace ID
	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)

	// Step 3: Load repo state to get active store
	repoState, err := e.stateStore.LoadRepoState(repoFingerprint)
	if err != nil {
		return nil, fmt.Errorf("failed to load repo state: %w", err)
	}

	if repoState.ActiveStore == "" {
		return nil, ErrNoActiveStore
	}

	// Step 4: Load workspace state
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: workspace has no managed paths", ErrStateMissing)
		}
		return nil, fmt.Errorf("failed to load workspace state: %w", err)
	}

	// Step 5: Filter paths to only those from the active store
	// Workspace state now uses relative paths as keys
	activeStorePaths := make([]string, 0)
	for relPath, ownership := range workspaceState.Paths {
		if ownership.Store == repoState.ActiveStore {
			activeStorePaths = append(activeStorePaths, relPath)
		}
	}

	// Check if there are any paths to remove from active store
	if len(activeStorePaths) == 0 {
		return &UnapplyResult{
			Removed:     []string{},
			WorkspaceID: workspaceID,
		}, nil
	}

	// If dry run, just return the list of paths that would be removed
	if req.DryRun {
		return &UnapplyResult{
			Removed:     activeStorePaths,
			WorkspaceID: workspaceID,
		}, nil
	}

	// Step 6: Remove active store's paths in deepest-first order
	relPaths := activeStorePaths

	// Sort paths by depth (deepest first)
	sort.Slice(relPaths, func(i, j int) bool {
		// Count path separators to determine depth
		depthI := countPathSeparators(relPaths[i])
		depthJ := countPathSeparators(relPaths[j])
		if depthI != depthJ {
			return depthI > depthJ // Deeper paths first
		}
		return relPaths[i] > relPaths[j] // Alphabetically for same depth
	})

	removed := []string{}
	for _, relPath := range relPaths {
		ownership := workspaceState.Paths[relPath]

		// Convert relative path to absolute for filesystem operations
		absPath := filepath.Join(req.CWD, relPath)

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

	// Step 7: Update or delete workspace state
	if len(workspaceState.Paths) == 0 {
		// No more managed paths - delete workspace state
		if err := e.stateStore.DeleteWorkspace(workspaceID); err != nil {
			return nil, fmt.Errorf("failed to delete workspace state: %w", err)
		}
	} else {
		// Other stores still have paths - update workspace state
		workspaceState.Applied = false // Mark as not applied since we removed some paths
		if err := e.stateStore.SaveWorkspace(workspaceID, workspaceState); err != nil {
			return nil, fmt.Errorf("failed to save workspace state: %w", err)
		}
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

	// For symlinks, validate the target
	if ownership.Type == "symlink" {
		target, err := e.fs.Readlink(path)
		if err != nil {
			return fmt.Errorf("expected symlink but got error reading link: %w", err)
		}

		// We could validate the target matches the store path here,
		// but that requires knowing which store owned it.
		// For now, just verify it's a symlink.
		_ = target
	}

	// For copies, we could check the checksum
	if ownership.Type == "copy" && ownership.Checksum != "" {
		currentHash, err := e.hasher.HashFile(path)
		if err == nil && currentHash != ownership.Checksum {
			// File has been modified - this is drift
			// For unapply, we still remove it (user modifications are lost)
			// A warning could be added here in the future
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
