package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/danieljhkim/monodev/internal/state"
)

// validateRelPath validates a relative path for safety.
// Returns an error if the path is invalid or unsafe.
func validateRelPath(relPath string) error {
	// Clean the path first
	cleaned := filepath.Clean(relPath)

	// Reject empty or current directory
	if cleaned == "" || cleaned == "." {
		return fmt.Errorf("invalid path: empty or current directory")
	}

	// Reject absolute paths
	if filepath.IsAbs(cleaned) {
		return fmt.Errorf("invalid path: must be relative, got absolute path %q", cleaned)
	}

	// Reject path traversal attempts
	if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, string(filepath.Separator)+"..") {
		return fmt.Errorf("invalid path: path traversal not allowed in %q", cleaned)
	}

	return nil
}

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

	// Missing is the list of paths that could not be saved because they don't exist in workspace
	Missing []string
}

// Save copies workspace files to the active store and records them in workspace state.
//
// Behavior:
// - Copies files from workspace → store overlay
// - Updates workspace state to mark paths as managed (adds to workspace.Paths)
// - Does NOT set applied=true (that's only done by Apply)
// - Does NOT create overlays (symlinks/copies) - that's Apply's job
//
// This allows tracking which files are managed by monodev even before overlays are created.
// The workspace state is the "intent" layer, while Apply creates the actual overlays.
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

	// Load track file to see what paths are tracked
	track, err := e.storeRepo.LoadTrack(repoState.ActiveStore)
	if err != nil {
		return nil, fmt.Errorf("failed to load track file: %w", err)
	}

	// Get the overlay root for the active store
	overlayRoot := e.storeRepo.OverlayRoot(repoState.ActiveStore)

	// Load or create workspace state
	// Note: save updates workspace state to track managed files, but does NOT set applied=true
	// Only the apply command sets applied=true when overlays are created
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			// Create new workspace state (applied=false by default)
			workspaceState = state.NewWorkspaceState(repoFingerprint, workspacePath, "symlink")
			workspaceState.ActiveStore = repoState.ActiveStore
			workspaceState.Stack = repoState.Stack
		} else {
			return nil, fmt.Errorf("failed to load workspace state: %w", err)
		}
	}

	result := &SaveResult{
		Saved:   []string{},
		Skipped: []string{},
		Missing: []string{},
	}

	now := e.clock.Now()

	if req.All {
		// Save all tracked paths, respecting the 'required' field
		for _, trackedPath := range track.Tracked {
			// Validate path before any file IO
			if err := validateRelPath(trackedPath.Path); err != nil {
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
				result.Saved = append(result.Saved, relPath)
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
				Store:     repoState.ActiveStore,
				Type:      "copy", // Record as copy since it's the original file
				Timestamp: now,
				Checksum:  checksum,
			}

			result.Saved = append(result.Saved, relPath)
		}
	} else {
		// Save specific paths (all treated as required)
		for _, rawPath := range req.Paths {
			// Validate path before any file IO
			if err := validateRelPath(rawPath); err != nil {
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
				result.Saved = append(result.Saved, relPath)
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
				Store:     repoState.ActiveStore,
				Type:      "copy", // Record as copy since it's the original file
				Timestamp: now,
				Checksum:  checksum,
			}

			result.Saved = append(result.Saved, relPath)
		}
	}

	if !req.DryRun {
		// Update store metadata (UpdatedAt timestamp)
		meta, err := e.storeRepo.LoadMeta(repoState.ActiveStore)
		if err != nil {
			return nil, fmt.Errorf("failed to load store metadata: %w", err)
		}

		meta.UpdatedAt = e.clock.Now()
		if err := e.storeRepo.SaveMeta(repoState.ActiveStore, meta); err != nil {
			return nil, fmt.Errorf("failed to save store metadata: %w", err)
		}

		// Save workspace state to record managed paths
		// NOTE: We do NOT set applied=true here - that's only done by the apply command
		// This allows tracking which files are managed even before overlays are created
		if err := e.stateStore.SaveWorkspace(workspaceID, workspaceState); err != nil {
			return nil, fmt.Errorf("failed to save workspace state: %w", err)
		}
	}

	return result, nil
}
