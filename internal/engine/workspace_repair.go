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
		if !e.workspaceBelongsToRepo(absRoot, fingerprint, ws) {
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

	// The first load only chooses a lock set. A target can be created, or the
	// source ledger replaced, before those locks are held. Reload after the
	// locks that cover the fresh source and target, and decide from that
	// snapshot: without Force a new target is left untouched, and Force
	// migrates the reloaded source rather than the stale preflight copy.
	var unlock func()
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()

	var heldSourceID, heldTargetID string
	var heldSourceStore, heldTargetStore state.StateStore
	held := false

	for attempt := 0; attempt < rebindLockAttempts; attempt++ {
		ws, sourceStore, err := e.loadWorkspaceRecord(req.WorkspaceID)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("%w: workspace '%s' not found", ErrNotFound, req.WorkspaceID)
			}
			return nil, fmt.Errorf("failed to load workspace: %w", err)
		}
		if !e.workspaceBelongsToRepo(absRoot, fingerprint, ws) {
			return nil, fmt.Errorf("workspace '%s' does not belong to the current repository", req.WorkspaceID)
		}

		newID := state.ComputeWorkspaceID(fingerprint, ws.WorkspacePath)
		targetStore := sourceStore
		var existing *state.WorkspaceState
		var existingErr error
		alreadyCurrent := newID == req.WorkspaceID && ws.Repo == fingerprint
		if !alreadyCurrent {
			var existingStore state.StateStore
			existing, existingStore, existingErr = e.loadWorkspaceRecord(newID)
			if existingErr != nil && !os.IsNotExist(existingErr) {
				return nil, fmt.Errorf("failed to load target workspace: %w", existingErr)
			}
			targetStore = firstNonNilStore(existingStore, sourceStore)
		}

		if !rebindLocksCover(held, heldSourceID, heldSourceStore, heldTargetID, heldTargetStore, req.WorkspaceID, sourceStore, newID, targetStore) {
			if unlock != nil {
				unlock()
				unlock = nil
			}
			next, lockErr := e.lockWorkspaces(ctx,
				workspaceLockRequest{store: sourceStore, id: req.WorkspaceID, mode: lockfile.Exclusive},
				workspaceLockRequest{store: targetStore, id: newID, mode: lockfile.Exclusive},
			)
			if lockErr != nil {
				return nil, lockErr
			}
			unlock = next
			held = true
			heldSourceID, heldSourceStore = req.WorkspaceID, sourceStore
			heldTargetID, heldTargetStore = newID, targetStore
			continue
		}

		if alreadyCurrent {
			return rebindWorkspaceResult(req.WorkspaceID, newID, ws), nil
		}
		if existingErr == nil && existing != nil && newID != req.WorkspaceID && !req.Force {
			return nil, fmt.Errorf("workspace '%s' already exists for this path; use --force to overwrite", newID)
		}
		if err := e.migrateWorkspaceRecord(sourceStore, ws, req.WorkspaceID, newID, fingerprint, absRoot, ws.WorkspacePath); err != nil {
			return nil, err
		}
		return rebindWorkspaceResult(req.WorkspaceID, newID, ws), nil
	}

	return nil, fmt.Errorf("failed to rebind workspace %s: records changed while acquiring locks", req.WorkspaceID)
}

const rebindLockAttempts = 5

func rebindLocksCover(held bool, heldSourceID string, heldSourceStore state.StateStore, heldTargetID string, heldTargetStore state.StateStore, sourceID string, sourceStore state.StateStore, targetID string, targetStore state.StateStore) bool {
	if !held || heldSourceID != sourceID || heldSourceStore != sourceStore {
		return false
	}
	if targetID == sourceID {
		return true
	}
	return heldTargetID == targetID && heldTargetStore == targetStore
}

func rebindWorkspaceResult(oldID, newID string, ws *state.WorkspaceState) *RebindWorkspaceResult {
	return &RebindWorkspaceResult{
		OldWorkspaceID: oldID,
		NewWorkspaceID: newID,
		WorkspacePath:  ws.WorkspacePath,
		ActiveStore:    ws.ActiveStore,
		Applied:        ws.Applied,
		AppliedPaths:   len(ws.Paths),
	}
}

func firstNonNilStore(stores ...state.StateStore) state.StateStore {
	for _, store := range stores {
		if store != nil {
			return store
		}
	}
	return nil
}

func (e *Engine) workspaceBelongsToRepo(repoRoot, fingerprint string, ws *state.WorkspaceState) bool {
	if ws == nil || ws.WorkspacePath == "" || filepath.IsAbs(ws.WorkspacePath) {
		return false
	}
	candidate := filepath.Join(repoRoot, ws.WorkspacePath)
	if !pathIsInside(repoRoot, candidate) {
		return false
	}
	resolvedRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return false
	}
	if ws.AbsolutePath != "" {
		if !filepath.IsAbs(ws.AbsolutePath) {
			return false
		}
		abs, err := filepath.EvalSymlinks(ws.AbsolutePath)
		if err == nil {
			// An existing path outside this checkout is another workspace,
			// even if its relative path (or remote) also exists here.
			return pathIsInside(resolvedRoot, abs)
		}
		if !os.IsNotExist(err) {
			return false
		}
	}

	// A legacy record without an absolute path, or a moved clone whose old
	// path is gone, needs identity evidence. A local directory alone cannot
	// associate a record from a shared state root with this repository.
	if ws.Repo == "" {
		return false
	}
	info, err := os.Stat(candidate)
	if err != nil || !info.IsDir() {
		return false
	}
	resolvedCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil || !pathIsInside(resolvedRoot, resolvedCandidate) {
		return false
	}
	storedID := state.ComputeWorkspaceID(ws.Repo, ws.WorkspacePath)
	for _, id := range e.workspaceIDCandidates(repoRoot, fingerprint, ws.WorkspacePath) {
		if id == storedID {
			return true
		}
	}
	return false
}

func pathIsInside(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
