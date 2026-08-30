package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/danieljhkim/monodev/internal/lockfile"
	"github.com/danieljhkim/monodev/internal/state"
)

// ListOrphanedWorkspaces returns workspace files that belong to the current
// repository but are stored under an identity that no longer matches.
func (e *Engine) ListOrphanedWorkspaces(ctx context.Context, cwd string) (*ListOrphanedWorkspacesResult, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	root, fingerprint, _, err := e.DiscoverWorkspace(cwd)
	if err != nil {
		return nil, fmt.Errorf("failed to discover workspace: %w", err)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repository root: %w", err)
	}

	result := &ListOrphanedWorkspacesResult{
		RepoFingerprint: fingerprint,
		RepoRoot:        absRoot,
		Orphans:         []OrphanedWorkspace{},
	}

	if err := e.forEachWorkspaceState(func(workspaceID string, ws *state.WorkspaceState) error {
		if !workspaceBelongsToRepo(absRoot, ws) {
			return nil
		}
		currentID := state.ComputeWorkspaceID(fingerprint, ws.WorkspacePath)
		if workspaceID == currentID && ws.Repo == fingerprint {
			return nil
		}
		result.Orphans = append(result.Orphans, OrphanedWorkspace{
			WorkspaceID:      workspaceID,
			CurrentID:        currentID,
			WorkspacePath:    ws.WorkspacePath,
			AbsolutePath:     ws.AbsolutePath,
			Repo:             ws.Repo,
			ActiveStore:      ws.ActiveStore,
			Applied:          ws.Applied,
			AppliedPathCount: len(ws.Paths),
		})
		return nil
	}); err != nil {
		return nil, err
	}

	return result, nil
}

// RebindWorkspace rewrites an orphaned workspace onto the current repository
// fingerprint, preserving the active store and applied-overlay ledger.
func (e *Engine) RebindWorkspace(ctx context.Context, req *RebindWorkspaceRequest) (*RebindWorkspaceResult, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.WorkspaceID) == "" {
		return nil, fmt.Errorf("workspace id is required")
	}

	root, fingerprint, _, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to discover workspace: %w", err)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repository root: %w", err)
	}

	ws, sourceStore, err := e.loadWorkspaceRecord(req.WorkspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: workspace '%s' not found", ErrNotFound, req.WorkspaceID)
		}
		return nil, fmt.Errorf("failed to load workspace: %w", err)
	}
	if !workspaceBelongsToRepo(absRoot, ws) {
		return nil, fmt.Errorf("workspace '%s' does not belong to the current repository", req.WorkspaceID)
	}

	newID := state.ComputeWorkspaceID(fingerprint, ws.WorkspacePath)
	if newID == req.WorkspaceID && ws.Repo == fingerprint {
		return &RebindWorkspaceResult{
			OldWorkspaceID: req.WorkspaceID,
			NewWorkspaceID: newID,
			WorkspacePath:  ws.WorkspacePath,
			ActiveStore:    ws.ActiveStore,
			Applied:        ws.Applied,
			AppliedPaths:   len(ws.Paths),
		}, nil
	}

	existing, existingStore, existingErr := e.loadWorkspaceRecord(newID)
	if existingErr != nil && !os.IsNotExist(existingErr) {
		return nil, fmt.Errorf("failed to load target workspace: %w", existingErr)
	}
	if existingErr == nil && existing != nil && newID != req.WorkspaceID && !req.Force {
		return nil, fmt.Errorf("workspace '%s' already exists for this path; use --force to overwrite", newID)
	}

	unlock, err := e.lockWorkspaces(ctx,
		workspaceLockRequest{store: sourceStore, id: req.WorkspaceID, mode: lockfile.Exclusive},
		workspaceLockRequest{store: firstNonNilStore(existingStore, sourceStore), id: newID, mode: lockfile.Exclusive},
	)
	if err != nil {
		return nil, err
	}
	defer unlock()

	if err := e.migrateWorkspaceRecord(sourceStore, ws, req.WorkspaceID, newID, fingerprint, absRoot, ws.WorkspacePath); err != nil {
		return nil, err
	}

	return &RebindWorkspaceResult{
		OldWorkspaceID: req.WorkspaceID,
		NewWorkspaceID: newID,
		WorkspacePath:  ws.WorkspacePath,
		ActiveStore:    ws.ActiveStore,
		Applied:        ws.Applied,
		AppliedPaths:   len(ws.Paths),
	}, nil
}

func firstNonNilStore(stores ...state.StateStore) state.StateStore {
	for _, store := range stores {
		if store != nil {
			return store
		}
	}
	return nil
}

func workspaceBelongsToRepo(repoRoot string, ws *state.WorkspaceState) bool {
	if ws == nil {
		return false
	}
	if ws.AbsolutePath != "" {
		abs, err := filepath.Abs(ws.AbsolutePath)
		if err == nil && pathIsInside(repoRoot, abs) {
			return true
		}
	}
	if ws.WorkspacePath == "" {
		return false
	}
	candidate := filepath.Join(repoRoot, ws.WorkspacePath)
	info, err := os.Stat(candidate)
	return err == nil && info.IsDir()
}

func pathIsInside(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
