package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PruneRequest represents a request to prune untracked files from a store.
type PruneRequest struct {
	// CWD is the current working directory
	CWD string

	// DryRun indicates whether to only show what would be deleted
	DryRun bool

	// Force indicates whether to skip confirmation prompt
	Force bool
}

// PruneResult contains the result of a prune operation.
type PruneResult struct {
	// StoreID is the ID of the pruned store
	StoreID string

	// DeletedPaths is the list of paths that were (or would be) deleted
	DeletedPaths []string

	// DryRun indicates whether this was a dry run
	DryRun bool
}

// Prune deletes overlay store content for paths that are no longer tracked.
func (e *Engine) Prune(ctx context.Context, req *PruneRequest) (*PruneResult, error) {
	// Discover repository
	root, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to discover workspace: %w", err)
	}

	workspaceState, workspaceID, err := e.LoadOrCreateWorkspaceState(root, repoFingerprint, workspacePath, "copy")
	if err != nil {
		return nil, fmt.Errorf("failed to load workspace state: %w", err)
	}

	if workspaceState.ActiveStore == "" {
		return nil, ErrNoActiveStore
	}

	// Resolve the store repo for the active store
	repo, err := e.activeStoreRepo(workspaceState)
	if err != nil {
		return nil, err
	}

	// Load track file to get tracked paths
	track, err := repo.LoadTrack(workspaceState.ActiveStore)
	if err != nil {
		return nil, fmt.Errorf("failed to load track file: %w", err)
	}

	// Build set of tracked paths for fast lookup
	trackedSet := make(map[string]bool)
	for _, tp := range track.Tracked {
		trackedSet[tp.Path] = true
	}

	// Get overlay root
	overlayRoot := repo.OverlayRoot(workspaceState.ActiveStore)

	// Find all files in overlay directory
	var untrackedPaths []string
	err = filepath.Walk(overlayRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the overlay root itself
		if path == overlayRoot {
			return nil
		}

		// Get relative path from overlay root
		relPath, err := filepath.Rel(overlayRoot, path)
		if err != nil {
			return fmt.Errorf("failed to compute relative path: %w", err)
		}

		// Skip if tracked
		if trackedSet[relPath] {
			return nil
		}

		// If this is a directory, check if any tracked path is a child of it
		// If so, don't delete the directory (it's a parent of tracked content)
		if info.IsDir() {
			hasTrackedChildren := false
			for trackedPath := range trackedSet {
				if strings.HasPrefix(trackedPath, relPath+string(filepath.Separator)) {
					hasTrackedChildren = true
					break
				}
			}
			if hasTrackedChildren {
				// Don't delete this directory, but continue walking
				return nil
			}
		}

		untrackedPaths = append(untrackedPaths, relPath)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk overlay directory: %w", err)
	}

	result := &PruneResult{
		StoreID:      workspaceState.ActiveStore,
		DeletedPaths: untrackedPaths,
		DryRun:       req.DryRun,
	}

	// If nothing to delete, return early
	if len(untrackedPaths) == 0 {
		return result, nil
	}

	// In dry-run mode, just return the list
	if req.DryRun {
		return result, nil
	}

	// If not force mode, return early with the list of paths to delete
	// The CLI layer should handle user confirmation and call again with Force=true
	if !req.Force {
		return result, nil
	}

	// Reload workspace state to get latest version
	// (we need to reload because we might have modified it)
	workspaceState, wsErr := e.stateStore.LoadWorkspace(workspaceID)
	hasWorkspaceState := wsErr == nil

	// Delete untracked paths from store overlay
	// Sort in reverse order so we delete files before directories
	// This is important because we need to delete directory contents before the directory itself
	for i := len(untrackedPaths) - 1; i >= 0; i-- {
		path := untrackedPaths[i]
		absPath := filepath.Join(overlayRoot, path)

		if err := e.fs.RemoveAll(absPath); err != nil {
			return nil, fmt.Errorf("failed to delete %s from store: %w", path, err)
		}

		// If workspace state exists and this path is in it, clean it up
		if hasWorkspaceState {
			if pathInfo, exists := workspaceState.Paths[path]; exists {
				// Only remove if it belongs to the active store
				if pathInfo.Store == workspaceState.ActiveStore {
					// Remove the overlay from workspace (symlink or copied file)
					workspacePath := filepath.Join(req.CWD, path)
					_ = e.fs.RemoveAll(workspacePath)
					// Ignore errors - workspace file might already be gone

					// Remove from workspace state
					delete(workspaceState.Paths, path)
				}
			}
		}
	}

	workspaceState.PruneAppliedStores()

	// Save updated workspace state if we modified it
	if hasWorkspaceState {
		if err := e.stateStore.SaveWorkspace(workspaceID, workspaceState); err != nil {
			return nil, fmt.Errorf("failed to save workspace state: %w", err)
		}
	}

	// Update store metadata (UpdatedAt timestamp)
	if err := e.touchStoreMetaIn(repo, workspaceState.ActiveStore); err != nil {
		return nil, err
	}

	return result, nil
}
