package engine

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/state"
)

// discoverWorkspace returns repo root, fingerprint, and workspace path
func (e *Engine) DiscoverWorkspace(cwd string) (root, fingerprint, workspacePath string, err error) {
	root, err = e.gitRepo.Discover(cwd)
	if err != nil {
		// Fallback to using absolute path for non-git repositories
		// This is a valid fallback behavior, not an error condition
		root = cwd
	}

	fingerprint, err = e.gitRepo.Fingerprint(root)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get workspace fingerprint: %w", err)
	}

	workspacePath, err = e.gitRepo.RelPath(root, cwd)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to compute workspace path: %w", err)
	}

	return root, fingerprint, workspacePath, nil
}

func (e *Engine) LoadOrCreateWorkspaceState(root, repoFingerprint, workspacePath, mode string) (*state.WorkspaceState, string, error) {
	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			workspaceState = state.NewWorkspaceState(repoFingerprint, workspacePath, mode)
		} else {
			return nil, workspaceID, fmt.Errorf("failed to load workspace state: %w", err)
		}
	}
	workspaceState.AbsolutePath = filepath.Join(root, workspacePath)
	workspaceState.MigrateDeprecatedStack()
	return workspaceState, workspaceID, nil
}
