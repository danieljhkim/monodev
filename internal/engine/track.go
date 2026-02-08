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

	// Paths is the list of paths to track (relative to CWD, absolute, or containing "..")
	Paths []string
}

// TrackResult represents the result of a track operation.
type TrackResult struct {
	// ResolvedPaths maps each user-provided path to its repo-root-relative resolved path.
	ResolvedPaths map[string]string
}

// UntrackRequest represents a request to untrack paths.
type UntrackRequest struct {
	// CWD is the current working directory
	CWD string

	// Paths is the list of paths to untrack (relative to CWD, absolute, or containing "..")
	Paths []string
}

// Track adds paths to the active store's track file.
func (e *Engine) Track(ctx context.Context, req *TrackRequest) (*TrackResult, error) {
	// Discover repository
	root, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to discover workspace: %w", err)
	}

	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)

	// Load workspace state to get active store
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoActiveStore
		}
		return nil, fmt.Errorf("failed to load workspace state: %w", err)
	}

	if workspaceState.ActiveStore == "" {
		return nil, ErrNoActiveStore
	}

	activeStore := workspaceState.ActiveStore

	// Resolve the store repo for the active store
	repo, err := e.activeStoreRepo(workspaceState)
	if err != nil {
		return nil, err
	}

	// Load current track file
	track, err := repo.LoadTrack(activeStore)
	if err != nil {
		return nil, fmt.Errorf("failed to load track file: %w", err)
	}

	// Add new paths (avoid duplicates)
	pathSet := make(map[string]bool)
	for _, tp := range track.Tracked {
		pathSet[tp.Path] = true
	}

	result := &TrackResult{
		ResolvedPaths: make(map[string]string),
	}

	for _, userPath := range req.Paths {
		// Resolve to repo-root-relative path
		repoRelPath, err := resolveToRepoRelative(userPath, req.CWD, root)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve path %q: %w", userPath, err)
		}

		result.ResolvedPaths[userPath] = repoRelPath

		if !pathSet[repoRelPath] {
			// Determine if path is file or directory
			kind := "file"
			absPath := filepath.Join(root, repoRelPath)

			info, err := e.fs.Lstat(absPath)
			if err == nil && info.IsDir() {
				kind = "dir"
			}

			track.Tracked = append(track.Tracked, stores.TrackedPath{
				Path: repoRelPath,
				Kind: kind,
			})
			pathSet[repoRelPath] = true
		}
	}

	// Save updated track file
	if err := repo.SaveTrack(activeStore, track); err != nil {
		return nil, fmt.Errorf("failed to save track file: %w", err)
	}

	// Update store metadata (UpdatedAt timestamp)
	if err := e.touchStoreMetaIn(repo, activeStore); err != nil {
		return nil, err
	}

	return result, nil
}

// Untrack removes paths from the active store's track file.
func (e *Engine) Untrack(ctx context.Context, req *UntrackRequest) error {
	root, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
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

	// Resolve the store repo for the active store
	repo, err := e.activeStoreRepo(workspaceState)
	if err != nil {
		return err
	}

	// Load current track file
	track, err := repo.LoadTrack(activeStore)
	if err != nil {
		return fmt.Errorf("failed to load track file: %w", err)
	}

	// Create set of paths to remove (resolved to repo-relative)
	removeSet := make(map[string]bool)
	for _, p := range req.Paths {
		repoRelPath, err := resolveToRepoRelative(p, req.CWD, root)
		if err != nil {
			return fmt.Errorf("failed to resolve path %q: %w", p, err)
		}
		removeSet[repoRelPath] = true
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
	if err := repo.SaveTrack(activeStore, track); err != nil {
		return fmt.Errorf("failed to save track file: %w", err)
	}

	// Update store metadata (UpdatedAt timestamp)
	if err := e.touchStoreMetaIn(repo, activeStore); err != nil {
		return err
	}

	return nil
}
