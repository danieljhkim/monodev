package engine

import (
	"context"
	"fmt"
	"os"
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
	repoRoot, err := e.gitRepo.Discover(req.CWD)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotInRepo, err)
	}

	repoFingerprint, err := e.gitRepo.Fingerprint(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to compute repo fingerprint: %w", err)
	}

	// Load repo state to get active store
	repoState, err := e.stateStore.LoadRepoState(repoFingerprint)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNoActiveStore
		}
		return fmt.Errorf("failed to load repo state: %w", err)
	}

	if repoState.ActiveStore == "" {
		return ErrNoActiveStore
	}

	// Load current track file
	track, err := e.storeRepo.LoadTrack(repoState.ActiveStore)
	if err != nil {
		return fmt.Errorf("failed to load track file: %w", err)
	}

	// Add new paths (avoid duplicates)
	pathSet := make(map[string]bool)
	for _, p := range track.Paths {
		pathSet[p] = true
	}

	for _, p := range req.Paths {
		if !pathSet[p] {
			track.Paths = append(track.Paths, p)
			pathSet[p] = true
		}
	}

	// Save updated track file
	if err := e.storeRepo.SaveTrack(repoState.ActiveStore, track); err != nil {
		return fmt.Errorf("failed to save track file: %w", err)
	}

	// Update store metadata (UpdatedAt timestamp)
	meta, err := e.storeRepo.LoadMeta(repoState.ActiveStore)
	if err != nil {
		return fmt.Errorf("failed to load store metadata: %w", err)
	}

	meta.UpdatedAt = e.clock.Now()
	if err := e.storeRepo.SaveMeta(repoState.ActiveStore, meta); err != nil {
		return fmt.Errorf("failed to save store metadata: %w", err)
	}

	return nil
}

// Untrack removes paths from the active store's track file.
func (e *Engine) Untrack(ctx context.Context, req *UntrackRequest) error {
	// Discover repository
	repoRoot, err := e.gitRepo.Discover(req.CWD)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotInRepo, err)
	}

	repoFingerprint, err := e.gitRepo.Fingerprint(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to compute repo fingerprint: %w", err)
	}

	// Load repo state to get active store
	repoState, err := e.stateStore.LoadRepoState(repoFingerprint)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNoActiveStore
		}
		return fmt.Errorf("failed to load repo state: %w", err)
	}

	if repoState.ActiveStore == "" {
		return ErrNoActiveStore
	}

	// Load current track file
	track, err := e.storeRepo.LoadTrack(repoState.ActiveStore)
	if err != nil {
		return fmt.Errorf("failed to load track file: %w", err)
	}

	// Create set of paths to remove
	removeSet := make(map[string]bool)
	for _, p := range req.Paths {
		removeSet[p] = true
	}

	// Filter out paths to remove
	newPaths := []string{}
	for _, p := range track.Paths {
		if !removeSet[p] {
			newPaths = append(newPaths, p)
		}
	}
	track.Paths = newPaths

	// Save updated track file
	if err := e.storeRepo.SaveTrack(repoState.ActiveStore, track); err != nil {
		return fmt.Errorf("failed to save track file: %w", err)
	}

	// Update store metadata (UpdatedAt timestamp)
	meta, err := e.storeRepo.LoadMeta(repoState.ActiveStore)
	if err != nil {
		return fmt.Errorf("failed to load store metadata: %w", err)
	}

	meta.UpdatedAt = e.clock.Now()
	if err := e.storeRepo.SaveMeta(repoState.ActiveStore, meta); err != nil {
		return fmt.Errorf("failed to save store metadata: %w", err)
	}

	return nil
}
