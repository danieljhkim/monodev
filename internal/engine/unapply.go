package engine

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/danieljhkim/monodev/internal/state"
)

// Unapply removes previously applied overlays from the workspace.
//
// Algorithm:
// 1. Discover repo and compute workspace ID
// 2. Load workspace state (must exist)
// 3. Remove all managed paths in deepest-first order
// 4. Delete workspace state file
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

	// Step 3: Load workspace state
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: workspace has no applied overlays", ErrStateMissing)
		}
		return nil, fmt.Errorf("failed to load workspace state: %w", err)
	}

	if !workspaceState.Applied {
		return &UnapplyResult{
			Removed:     []string{},
			WorkspaceID: workspaceID,
		}, nil
	}

	// If dry run, just return the list of paths that would be removed
	if req.DryRun {
		paths := make([]string, 0, len(workspaceState.Paths))
		for path := range workspaceState.Paths {
			paths = append(paths, path)
		}
		return &UnapplyResult{
			Removed:     paths,
			WorkspaceID: workspaceID,
		}, nil
	}

	// Step 4: Remove all managed paths in deepest-first order
	paths := make([]string, 0, len(workspaceState.Paths))
	for path := range workspaceState.Paths {
		paths = append(paths, path)
	}

	// Sort paths by depth (deepest first)
	sort.Slice(paths, func(i, j int) bool {
		// Count path separators to determine depth
		depthI := countPathSeparators(paths[i])
		depthJ := countPathSeparators(paths[j])
		if depthI != depthJ {
			return depthI > depthJ // Deeper paths first
		}
		return paths[i] > paths[j] // Alphabetically for same depth
	})

	removed := []string{}
	for _, path := range paths {
		ownership := workspaceState.Paths[path]

		// Validate the path before removing (unless force)
		if !req.Force {
			if err := e.validateManagedPath(path, ownership); err != nil {
				return nil, fmt.Errorf("validation failed for %s: %w", path, err)
			}
		}

		// Remove the path
		if err := e.fs.RemoveAll(path); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to remove %s: %w", path, err)
		}

		removed = append(removed, path)
	}

	// Step 5: Delete workspace state file
	if err := e.stateStore.DeleteWorkspace(workspaceID); err != nil {
		return nil, fmt.Errorf("failed to delete workspace state: %w", err)
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
