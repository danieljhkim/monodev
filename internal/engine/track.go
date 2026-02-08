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

	// Role categorizes the tracked paths (script, docs, style, config, other)
	Role string

	// Description provides additional context about the tracked paths
	Description string

	// Origin indicates how the paths were tracked (user, agent, other)
	Origin string
}

// TrackResult represents the result of a track operation.
type TrackResult struct {
	// ResolvedPaths maps each user-provided path to its repo-root-relative resolved path.
	ResolvedPaths map[string]string

	// MissingPaths contains user-provided paths that were not found in the workspace.
	MissingPaths []string
}

// UntrackRequest represents a request to untrack paths.
type UntrackRequest struct {
	// CWD is the current working directory
	CWD string

	// Paths is the list of paths to untrack (relative to CWD, absolute, or containing "..")
	Paths []string
}

// UntrackResult represents the result of an untrack operation.
type UntrackResult struct {
	// RemovedPaths contains user-provided paths that were found and removed from tracking.
	RemovedPaths []string

	// NotFoundPaths contains user-provided paths that were not found in the track file.
	NotFoundPaths []string
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

		// Check if path exists in the workspace
		absPath := filepath.Join(root, repoRelPath)
		info, err := e.fs.Lstat(absPath)
		if err != nil {
			result.MissingPaths = append(result.MissingPaths, userPath)
			continue
		}

		result.ResolvedPaths[userPath] = repoRelPath

		if !pathSet[repoRelPath] {
			// Determine if path is file or directory
			kind := "file"
			if info.IsDir() {
				kind = "dir"
			}

			now := e.clock.Now()
			origin := req.Origin
			if origin == "" {
				origin = "user"
			}
			tp := stores.TrackedPath{
				Path:        repoRelPath,
				Kind:        kind,
				Role:        req.Role,
				Description: req.Description,
				CreatedAt:   &now,
				UpdatedAt:   &now,
				Origin:      origin,
			}
			track.Tracked = append(track.Tracked, tp)
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
func (e *Engine) Untrack(ctx context.Context, req *UntrackRequest) (*UntrackResult, error) {
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

	// Create set of paths to remove (resolved to repo-relative)
	// Also keep a reverse map from resolved path back to user-provided path
	removeSet := make(map[string]bool)
	resolvedToUser := make(map[string]string)
	for _, p := range req.Paths {
		repoRelPath, err := resolveToRepoRelative(p, req.CWD, root)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve path %q: %w", p, err)
		}
		removeSet[repoRelPath] = true
		resolvedToUser[repoRelPath] = p
	}

	// Build set of currently tracked paths for lookup
	trackedSet := make(map[string]bool)
	for _, tp := range track.Tracked {
		trackedSet[tp.Path] = true
	}

	// Determine which requested paths were found and which were not
	result := &UntrackResult{}
	for resolvedPath, userPath := range resolvedToUser {
		if trackedSet[resolvedPath] {
			result.RemovedPaths = append(result.RemovedPaths, userPath)
		} else {
			result.NotFoundPaths = append(result.NotFoundPaths, userPath)
		}
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
		return nil, fmt.Errorf("failed to save track file: %w", err)
	}

	// Update store metadata (UpdatedAt timestamp)
	if err := e.touchStoreMetaIn(repo, activeStore); err != nil {
		return nil, err
	}

	return result, nil
}
