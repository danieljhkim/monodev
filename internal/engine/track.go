package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// TrackRequest represents a request to track paths.
type TrackRequest struct {
	// CWD is the current working directory
	CWD string

	// Paths is the list of paths to track (relative to CWD)
	Paths []string
}

// UntrackRequest represents a request to untrack paths.
type UntrackRequest struct {
	// CWD is the current working directory
	CWD string

	// Paths is the list of paths to untrack (relative to CWD)
	Paths []string
}

// Track adds paths to the active store's track file.
func (e *Engine) Track(ctx context.Context, req *TrackRequest) error {
	// Discover repository
	_, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return fmt.Errorf("failed to discover workspace: %w", err)
	}

	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)

	// Load workspace state to get active store
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNoActiveStore
		}
		return fmt.Errorf("failed to load workspace state: %w", err)
	}

	if workspaceState.ActiveStore == "" {
		return ErrNoActiveStore
	}

	activeStore := workspaceState.ActiveStore

	// Load current track file
	track, err := e.storeRepo.LoadTrack(activeStore)
	if err != nil {
		return fmt.Errorf("failed to load track file: %w", err)
	}

	// Add new paths (avoid duplicates)
	pathSet := make(map[string]bool)
	for _, tp := range track.Tracked {
		pathSet[tp.Path] = true
	}

	for _, relPath := range req.Paths {
		if !pathSet[relPath] {
			// Determine if path is file or directory
			kind := "file"
			absPath := relPath
			if !filepath.IsAbs(relPath) {
				absPath = filepath.Join(req.CWD, relPath)
			}

			info, err := e.fs.Lstat(absPath)
			if err == nil && info.IsDir() {
				kind = "dir"
			}

			track.Tracked = append(track.Tracked, stores.TrackedPath{
				Path:     relPath,
				Kind:     kind,
				Location: req.CWD,
			})
			pathSet[relPath] = true
		}
	}

	// Save updated track file
	if err := e.storeRepo.SaveTrack(activeStore, track); err != nil {
		return fmt.Errorf("failed to save track file: %w", err)
	}

	// Update store metadata (UpdatedAt timestamp)
	if err := e.touchStoreMeta(activeStore); err != nil {
		return err
	}

	return nil
}

// Untrack removes paths from the active store's track file.
func (e *Engine) Untrack(ctx context.Context, req *UntrackRequest) error {
	_, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return fmt.Errorf("failed to discover workspace: %w", err)
	}
	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)

	// Load workspace state to get active store
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNoActiveStore
		}
		return fmt.Errorf("failed to load workspace state: %w", err)
	}

	if workspaceState.ActiveStore == "" {
		return ErrNoActiveStore
	}

	activeStore := workspaceState.ActiveStore

	// Load current track file
	track, err := e.storeRepo.LoadTrack(activeStore)
	if err != nil {
		return fmt.Errorf("failed to load track file: %w", err)
	}

	// Create set of paths to remove
	removeSet := make(map[string]bool)
	for _, p := range req.Paths {
		removeSet[p] = true
	}

	// Filter out paths to remove
	newTracked := []stores.TrackedPath{}
	for _, tp := range track.Tracked {
		if !removeSet[tp.Path] {
			newTracked = append(newTracked, tp)
		}
	}
	track.Tracked = newTracked

	// Save updated track file
	if err := e.storeRepo.SaveTrack(activeStore, track); err != nil {
		return fmt.Errorf("failed to save track file: %w", err)
	}

	// Update store metadata (UpdatedAt timestamp)
	if err := e.touchStoreMeta(activeStore); err != nil {
		return err
	}

	return nil
}
