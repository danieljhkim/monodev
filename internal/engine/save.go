package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/state"
)

// SaveRequest represents a request to save workspace files to the store.
type SaveRequest struct {
	// CWD is the current working directory
	CWD string

	// Paths is the list of paths to save (relative to CWD)
	// If empty and All is false, saves nothing
	Paths []string

	// All saves all tracked paths
	All bool

	// DryRun shows what would be saved without actually saving
	DryRun bool
}

// SaveResult represents the result of a save operation.
type SaveResult struct {
	// Saved is the list of paths that were saved
	Saved []string

	// Skipped is the list of paths that were skipped (e.g., symlinks in symlink mode)
	Skipped []string
}

// Save copies workspace files to the active store.
//
// Save semantics by mode:
// - Symlink mode: Only save NEW paths (not already in workspace state)
// - Copy mode: Save all specified paths
func (e *Engine) Save(ctx context.Context, req *SaveRequest) (*SaveResult, error) {
	// Discover repository
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

	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)

	// Load repo state to get active store
	repoState, err := e.stateStore.LoadRepoState(repoFingerprint)
	if err != nil {
		return nil, fmt.Errorf("failed to load repo state: %w", err)
	}

	if repoState.ActiveStore == "" {
		return nil, ErrNoActiveStore
	}

	// Load workspace state (may not exist if overlays not applied yet)
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	workspaceExists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load workspace state: %w", err)
	}

	// Determine mode and whether we need to auto-apply after saving
	needsAutoApply := false
	mode := "symlink"

	if workspaceExists && workspaceState.Applied {
		mode = workspaceState.Mode
	} else {
		// Workspace not applied yet - we'll auto-apply after copying files to store
		needsAutoApply = true
		if !workspaceExists {
			// Create initial workspace state
			workspaceState = &state.WorkspaceState{
				Repo:          repoFingerprint,
				WorkspacePath: workspacePath,
				Applied:       false,
				Mode:          "symlink",
				Stack:         repoState.Stack,
				ActiveStore:   repoState.ActiveStore,
				Paths:         make(map[string]state.PathOwnership),
			}
		}
	}

	// Load track file to see what paths are tracked
	track, err := e.storeRepo.LoadTrack(repoState.ActiveStore)
	if err != nil {
		return nil, fmt.Errorf("failed to load track file: %w", err)
	}

	// Determine which paths to save
	var pathsToSave []string
	if req.All {
		pathsToSave = track.Paths
	} else {
		pathsToSave = req.Paths
	}

	// Get the overlay root for the active store
	overlayRoot := e.storeRepo.OverlayRoot(repoState.ActiveStore)

	result := &SaveResult{
		Saved:   []string{},
		Skipped: []string{},
	}

	// Track workspace files to remove if we're auto-applying
	filesToRemove := []string{}

	// For each path to save
	for _, relPath := range pathsToSave {
		workspaceFilePath := filepath.Join(req.CWD, relPath)
		storeFilePath := filepath.Join(overlayRoot, relPath)

		// Check if path exists in workspace
		exists, err := e.fs.Exists(workspaceFilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to check if path exists: %w", err)
		}
		if !exists {
			// Path doesn't exist - skip
			result.Skipped = append(result.Skipped, relPath)
			continue
		}

		// Check if path is already managed
		ownership, isManaged := workspaceState.Paths[workspaceFilePath]

		// Symlink mode: only save if NEW (not already managed)
		if mode == "symlink" && isManaged {
			// Already managed - skip (changes are already in the store)
			result.Skipped = append(result.Skipped, relPath)
			continue
		}

		// Copy mode: always save
		// New path: always save

		if req.DryRun {
			result.Saved = append(result.Saved, relPath)
			continue
		}

		// Copy the file/directory to the store
		if err := e.fs.Copy(workspaceFilePath, storeFilePath); err != nil {
			return nil, fmt.Errorf("failed to copy %s to store: %w", relPath, err)
		}

		result.Saved = append(result.Saved, relPath)

		// If we're going to auto-apply, track this file for removal
		if needsAutoApply {
			filesToRemove = append(filesToRemove, workspaceFilePath)
		}

		// Only update workspace state if already applied
		// If not applied, we'll call Apply after copying to store
		if !needsAutoApply {
			// In symlink mode, replace the workspace file with a symlink to the store
			if mode == "symlink" && !isManaged {
				// Remove the original file
				if err := e.fs.RemoveAll(workspaceFilePath); err != nil {
					return nil, fmt.Errorf("failed to remove original file %s: %w", relPath, err)
				}

				// Create symlink from workspace to store
				if err := e.fs.Symlink(storeFilePath, workspaceFilePath); err != nil {
					return nil, fmt.Errorf("failed to create symlink for %s: %w", relPath, err)
				}

				// Update workspace state to mark as managed
				workspaceState.Paths[workspaceFilePath] = state.PathOwnership{
					Store:     repoState.ActiveStore,
					Type:      "symlink",
					Timestamp: e.clock.Now(),
				}
			} else if mode == "copy" {
				// In copy mode, compute checksum and update workspace state
				checksum := ""
				if hash, err := e.hasher.HashFile(workspaceFilePath); err == nil {
					checksum = hash
				}

				workspaceState.Paths[workspaceFilePath] = state.PathOwnership{
					Store:     repoState.ActiveStore,
					Type:      "copy",
					Timestamp: e.clock.Now(),
					Checksum:  checksum,
				}
			}
		}

		_ = ownership
	}

	if !req.DryRun {
		// If we need to auto-apply (workspace wasn't applied before), do it now
		// after we've copied files to the store
		if needsAutoApply {
			// Remove workspace files before applying (so symlinks can be created)
			for _, filePath := range filesToRemove {
				if err := e.fs.RemoveAll(filePath); err != nil && !os.IsNotExist(err) {
					return nil, fmt.Errorf("failed to remove workspace file %s: %w", filePath, err)
				}
			}

			applyReq := &ApplyRequest{
				CWD:    req.CWD,
				Mode:   "symlink",
				Force:  true, // Force in case of any remaining conflicts
				DryRun: false,
			}
			_, err := e.Apply(ctx, applyReq)
			if err != nil {
				return nil, fmt.Errorf("failed to auto-apply after save: %w", err)
			}
		} else {
			// Persist updated workspace state (if already applied)
			if err := e.stateStore.SaveWorkspace(workspaceID, workspaceState); err != nil {
				return nil, fmt.Errorf("failed to save workspace state: %w", err)
			}
		}

		// Update store metadata (UpdatedAt timestamp)
		meta, err := e.storeRepo.LoadMeta(repoState.ActiveStore)
		if err != nil {
			return nil, fmt.Errorf("failed to load store metadata: %w", err)
		}

		meta.UpdatedAt = e.clock.Now()
		if err := e.storeRepo.SaveMeta(repoState.ActiveStore, meta); err != nil {
			return nil, fmt.Errorf("failed to save store metadata: %w", err)
		}
	}

	return result, nil
}
