package engine

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/state"
)

// CommitRequest represents a request to commit workspace files to the store.
type CommitRequest struct {
	// CWD is the current working directory
	CWD string

	// Paths is the list of paths to commit (relative to CWD)
	// If empty and All is false, commits nothing
	Paths []string

	// All commits all tracked paths
	All bool

	// DryRun shows what would be committed without actually committing
	DryRun bool
}

// CommitResult represents the result of a commit operation.
type CommitResult struct {
	// Committed is the list of paths that were committed
	Committed []string

	// Skipped is the list of paths that were skipped (e.g., symlinks in symlink mode)
	Skipped []string

	// Missing is the list of paths that could not be committed because they don't exist in workspace
	Missing []string
}

// Commit copies workspace files to the active store and records them in workspace state.
//
// Behavior:
// - Copies files from workspace → store overlay
// - Updates workspace state to mark paths as managed (adds to workspace.Paths)
// - Does NOT set applied=true (that's only done by Apply)
// - Does NOT create overlays (symlinks/copies) - that's Apply's job
//
// This allows tracking which files are managed by monodev even before overlays are created.
// The workspace state is the "intent" layer, while Apply creates the actual overlays.
func (e *Engine) Commit(ctx context.Context, req *CommitRequest) (*CommitResult, error) {
	// Discover repository
	_, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to discover workspace: %w", err)
	}

	workspaceState, workspaceID, err := e.LoadOrCreateWorkspaceState(repoFingerprint, workspacePath, "symlink")
	if err != nil {
		return nil, fmt.Errorf("failed to load or create workspace state: %w", err)
	}

	if workspaceState.ActiveStore == "" {
		return nil, ErrNoActiveStore
	}

	// Load track file to see what paths are tracked
	track, err := e.storeRepo.LoadTrack(workspaceState.ActiveStore)
	if err != nil {
		return nil, fmt.Errorf("failed to load track file: %w", err)
	}

	// Get the overlay root for the active store
	overlayRoot := e.storeRepo.OverlayRoot(workspaceState.ActiveStore)

	result := &CommitResult{
		Committed: []string{},
		Skipped:   []string{},
		Missing:   []string{},
	}

	now := e.clock.Now()

	if req.All {
		// Commit all tracked paths, respecting the 'required' field
		for _, trackedPath := range track.Tracked {
			// Validate path before any file IO
			if err := e.fs.ValidateRelPath(trackedPath.Path); err != nil {
				return nil, fmt.Errorf("invalid tracked path %q: %w", trackedPath.Path, err)
			}

			// Use cleaned relative path
			relPath := filepath.Clean(trackedPath.Path)
			workspaceFilePath := filepath.Join(req.CWD, relPath)
			storeFilePath := filepath.Join(overlayRoot, relPath)

			// Check if path exists in workspace
			exists, err := e.fs.Exists(workspaceFilePath)
			if err != nil {
				return nil, fmt.Errorf("failed to check if path exists: %w", err)
			}

			if !exists {
				// Path doesn't exist in workspace - add to missing list and continue
				result.Missing = append(result.Missing, relPath)
				continue
			}

			if req.DryRun {
				result.Committed = append(result.Committed, relPath)
				continue
			}

			// Copy the file/directory to the store
			if err := e.fs.Copy(workspaceFilePath, storeFilePath); err != nil {
				return nil, fmt.Errorf("failed to copy %s to store: %w", relPath, err)
			}

			// Record this path as managed in workspace state (but don't set applied=true)
			// This marks the file as "tracked" by monodev even before overlays are created
			checksum := ""
			if trackedPath.Kind == "file" {
				hash, err := e.hasher.HashFile(workspaceFilePath)
				if err == nil {
					checksum = hash
				}
			}

			workspaceState.Paths[relPath] = state.PathOwnership{
				Store:     workspaceState.ActiveStore,
				Type:      "copy", // Record as copy since it's the original file
				Timestamp: now,
				Checksum:  checksum,
			}

			result.Committed = append(result.Committed, relPath)
		}
	} else {
		// Commit specific paths (all treated as required)
		for _, rawPath := range req.Paths {
			// Validate path before any file IO
			if err := e.fs.ValidateRelPath(rawPath); err != nil {
				return nil, fmt.Errorf("invalid path %q: %w", rawPath, err)
			}

			// Use cleaned relative path
			relPath := filepath.Clean(rawPath)
			workspaceFilePath := filepath.Join(req.CWD, relPath)
			storeFilePath := filepath.Join(overlayRoot, relPath)

			// Check if path exists in workspace
			exists, err := e.fs.Exists(workspaceFilePath)
			if err != nil {
				return nil, fmt.Errorf("failed to check if path exists: %w", err)
			}

			if !exists {
				// Path doesn't exist in workspace - add to missing list and continue
				result.Missing = append(result.Missing, relPath)
				continue
			}

			if req.DryRun {
				result.Committed = append(result.Committed, relPath)
				continue
			}

			// Copy the file/directory to the store
			if err := e.fs.Copy(workspaceFilePath, storeFilePath); err != nil {
				return nil, fmt.Errorf("failed to copy %s to store: %w", relPath, err)
			}

			// Record this path as managed in workspace state (but don't set applied=true)
			checksum := ""
			info, err := e.fs.Lstat(workspaceFilePath)
			if err == nil && !info.IsDir() {
				hash, err := e.hasher.HashFile(workspaceFilePath)
				if err == nil {
					checksum = hash
				}
			}

			workspaceState.Paths[relPath] = state.PathOwnership{
				Store:     workspaceState.ActiveStore,
				Type:      "copy", // Record as copy since it's the original file
				Timestamp: now,
				Checksum:  checksum,
			}

			result.Committed = append(result.Committed, relPath)
		}
	}

	if !req.DryRun {
		// Update store metadata (UpdatedAt timestamp)
		meta, err := e.storeRepo.LoadMeta(workspaceState.ActiveStore)
		if err != nil {
			return nil, fmt.Errorf("failed to load store metadata: %w", err)
		}

		meta.UpdatedAt = e.clock.Now()
		if err := e.storeRepo.SaveMeta(workspaceState.ActiveStore, meta); err != nil {
			return nil, fmt.Errorf("failed to save store metadata: %w", err)
		}

		// Commit workspace state to record managed paths
		// NOTE: We do NOT set applied=true here - that's only done by the apply command
		// This allows tracking which files are managed even before overlays are created
		if err := e.stateStore.SaveWorkspace(workspaceID, workspaceState); err != nil {
			return nil, fmt.Errorf("failed to save workspace state: %w", err)
		}
	}

	return result, nil
}
