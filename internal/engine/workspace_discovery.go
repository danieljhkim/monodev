package engine

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/gitx"
	"github.com/danieljhkim/monodev/internal/state"
)

// DiscoverWorkspace returns repo root, fingerprint, and workspace path
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
	resolved, err := e.resolveWorkspaceState(root, repoFingerprint, workspacePath)
	if err != nil {
		return nil, state.ComputeWorkspaceID(repoFingerprint, workspacePath), err
	}
	if resolved.state != nil && resolved.foundID != resolved.currentID {
		if err := e.migrateWorkspaceRecord(resolved.store, resolved.state, resolved.foundID, resolved.currentID, repoFingerprint, root, workspacePath); err != nil {
			return nil, resolved.currentID, err
		}
	}
	if resolved.state == nil {
		resolved.state = state.NewWorkspaceState(repoFingerprint, workspacePath, mode)
	}
	resolved.state.AbsolutePath = filepath.Join(root, workspacePath)
	return resolved.state, resolved.currentID, nil
}

type resolvedWorkspace struct {
	state     *state.WorkspaceState
	currentID string
	foundID   string
	store     state.StateStore
}

func (e *Engine) loadWorkspaceState(root, repoFingerprint, workspacePath string) (*state.WorkspaceState, string, error) {
	resolved, err := e.resolveWorkspaceState(root, repoFingerprint, workspacePath)
	if err != nil {
		return nil, state.ComputeWorkspaceID(repoFingerprint, workspacePath), err
	}
	if resolved.state == nil {
		return nil, resolved.currentID, nil
	}
	resolved.state.Repo = repoFingerprint
	resolved.state.WorkspacePath = workspacePath
	resolved.state.AbsolutePath = filepath.Join(root, workspacePath)
	return resolved.state, resolved.currentID, nil
}

func (e *Engine) resolveWorkspaceState(root, repoFingerprint, workspacePath string) (*resolvedWorkspace, error) {
	currentID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)
	resolved := &resolvedWorkspace{currentID: currentID}
	for _, candidateID := range e.workspaceIDCandidates(root, repoFingerprint, workspacePath) {
		ws, store, err := e.loadWorkspaceRecord(candidateID)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return resolved, fmt.Errorf("failed to load workspace state: %w", err)
		}
		resolved.state = ws
		resolved.foundID = candidateID
		resolved.store = store
		return resolved, nil
	}
	return resolved, nil
}

func (e *Engine) workspaceIDCandidates(root, repoFingerprint, workspacePath string) []string {
	seen := make(map[string]bool)
	var ids []string
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}

	add(state.ComputeWorkspaceID(repoFingerprint, workspacePath))
	if e.gitRepo == nil {
		return ids
	}

	absRoot, rawURL, err := e.gitRepo.GetFingerprintComponents(root)
	if err != nil {
		return ids
	}
	if rawURL != "" {
		add(state.ComputeWorkspaceID(gitx.RemoteFingerprint(rawURL), workspacePath))
	}
	add(state.ComputeWorkspaceID(gitx.LegacyFingerprint(absRoot, rawURL), workspacePath))
	return ids
}

func (e *Engine) loadWorkspaceRecord(id string) (*state.WorkspaceState, state.StateStore, error) {
	if e.componentStateStore != nil || (e.scopedPaths != nil && e.scopedPaths.Component != nil) {
		return e.loadWorkspaceFromScopes(id)
	}
	if e.stateStore == nil {
		return nil, nil, os.ErrNotExist
	}
	ws, err := e.stateStore.LoadWorkspace(id)
	if err != nil {
		return nil, nil, err
	}
	return ws, e.stateStore, nil
}

func (e *Engine) migrateWorkspaceRecord(store state.StateStore, ws *state.WorkspaceState, oldID, newID, repoFingerprint, root, workspacePath string) error {
	if store == nil {
		store = e.stateStore
	}
	if store == nil {
		return fmt.Errorf("failed to migrate workspace %s: no state store", oldID)
	}
	ws.Repo = repoFingerprint
	ws.WorkspacePath = workspacePath
	ws.AbsolutePath = filepath.Join(root, workspacePath)
	if err := store.SaveWorkspace(newID, ws); err != nil {
		return fmt.Errorf("failed to migrate workspace %s: %w", oldID, err)
	}
	if oldID != newID {
		if err := store.DeleteWorkspace(oldID); err != nil {
			return fmt.Errorf("failed to remove legacy workspace %s: %w", oldID, err)
		}
	}
	return nil
}
