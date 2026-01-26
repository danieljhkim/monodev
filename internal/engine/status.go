package engine

import (
	"context"
	"fmt"
	"os"

	"github.com/danieljhkim/monodev/internal/state"
)

// Status returns the current status of the workspace.
func (e *Engine) Status(ctx context.Context, req *StatusRequest) (*StatusResult, error) {
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

	// Load workspace state (may not exist)
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load workspace state: %w", err)
	}

	// Build result from workspace state if it exists
	result := &StatusResult{
		WorkspaceID:     workspaceID,
		RepoFingerprint: repoFingerprint,
		WorkspacePath:   workspacePath,
		Applied:         false,
		Mode:            "",
		Stack:           []string{},
		ActiveStore:     "",
		Paths:           make(map[string]PathInfo),
		TrackedPaths:    []string{},
	}

	// If workspace state exists, populate from it
	if workspaceState != nil {
		result.Applied = workspaceState.Applied
		result.Mode = workspaceState.Mode
		result.Stack = workspaceState.Stack
		result.ActiveStore = workspaceState.ActiveStore

		// Convert paths to PathInfo
		for path, ownership := range workspaceState.Paths {
			result.Paths[path] = PathInfo{
				Store: ownership.Store,
				Type:  ownership.Type,
			}
		}
	}

	// Load tracked paths from active store
	if result.ActiveStore != "" {
		track, err := e.storeRepo.LoadTrack(result.ActiveStore)
		if err == nil {
			result.TrackedPaths = track.Paths()
		}
		// Ignore errors - store might not exist or have no track file
	}

	return result, nil
}
