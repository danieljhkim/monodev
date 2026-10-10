package engine

import (
	"context"
	"fmt"

	"github.com/danieljhkim/monodev/internal/lockfile"
	"github.com/danieljhkim/monodev/internal/state"
)

// DeleteStore deletes a store after checking for usage by workspaces.
// Algorithm steps:
// 1. Validate store exists (resolve scope)
// 2. Find all workspaces using the store
// 3. Return early if dry-run
// 4. If store is in use and not forced, return error with affected workspaces
// 5. Clean workspace references
// 6. Delete store
// 7. Return result
func (e *Engine) DeleteStore(ctx context.Context, req *DeleteStoreRequest) (*DeleteStoreResult, error) {
	// Step 1: Resolve the physical store. The scope is part of its identity:
	// the same store ID can exist in both global and component scopes.
	repo, resolvedScope, err := e.storeResolver.resolveStoreRepo(req.StoreID, req.Scope)
	if err != nil {
		return nil, err
	}

	// Step 2: Dry-run is a read-only, atomic-per-workspace snapshot.
	if req.DryRun {
		affectedWorkspaces, err := e.findWorkspacesUsingStore(req.StoreID, resolvedScope)
		if err != nil {
			return nil, fmt.Errorf("failed to find workspaces using store: %w", err)
		}
		return &DeleteStoreResult{
			StoreID:            req.StoreID,
			AffectedWorkspaces: affectedWorkspaces,
			DryRun:             true,
			Deleted:            false,
		}, nil
	}

	// Workspace locks always precede store locks. Re-scan after both are held;
	// if a new workspace started referencing the store in between, release and
	// retry with the expanded sorted workspace set.
	var affectedWorkspaces []WorkspaceUsage
	var unlockWorkspaces func()
	var unlockStore func()
	for attempt := 0; attempt < 8; attempt++ {
		affectedWorkspaces, err = e.findWorkspacesUsingStore(req.StoreID, resolvedScope)
		if err != nil {
			return nil, fmt.Errorf("failed to find workspaces using store: %w", err)
		}
		requests := make([]workspaceLockRequest, 0, len(affectedWorkspaces))
		lockedIDs := make(map[string]bool, len(affectedWorkspaces))
		for _, usage := range affectedWorkspaces {
			workspaceStore, storeErr := e.workspaceStoreForID(usage.WorkspaceID)
			if storeErr != nil {
				continue
			}
			requests = append(requests, workspaceLockRequest{store: workspaceStore, id: usage.WorkspaceID, mode: lockfile.Exclusive})
			lockedIDs[usage.WorkspaceID] = true
		}
		unlockWorkspaces, err = e.lockWorkspaces(ctx, requests...)
		if err != nil {
			return nil, err
		}
		unlockStore, err = e.lockStores(ctx, storeLockRequest{repo: repo, id: req.StoreID, mode: lockfile.Exclusive})
		if err != nil {
			unlockWorkspaces()
			return nil, err
		}

		fresh, scanErr := e.findWorkspacesUsingStore(req.StoreID, resolvedScope)
		if scanErr != nil {
			unlockStore()
			unlockWorkspaces()
			return nil, fmt.Errorf("failed to verify workspace references: %w", scanErr)
		}
		complete := true
		for _, usage := range fresh {
			if !lockedIDs[usage.WorkspaceID] {
				complete = false
				break
			}
		}
		if complete {
			affectedWorkspaces = fresh
			break
		}
		unlockStore()
		unlockWorkspaces()
		unlockStore = nil
		unlockWorkspaces = nil
	}
	if unlockStore == nil || unlockWorkspaces == nil {
		return nil, fmt.Errorf("%w: workspace references changed repeatedly while deleting store %s", lockfile.ErrContended, req.StoreID)
	}
	defer unlockWorkspaces()
	defer unlockStore()

	// Step 3: If store is in use and not forced, return error
	if len(affectedWorkspaces) > 0 && !req.Force {
		return &DeleteStoreResult{
			StoreID:            req.StoreID,
			AffectedWorkspaces: affectedWorkspaces,
			DryRun:             false,
			Deleted:            false,
		}, fmt.Errorf("store '%s' is in use by %d workspace(s)", req.StoreID, len(affectedWorkspaces))
	}

	// Step 4: Clean workspace references
	if len(affectedWorkspaces) > 0 {
		if err := e.cleanWorkspaceReferences(req.StoreID, resolvedScope, affectedWorkspaces); err != nil {
			return nil, fmt.Errorf("failed to clean workspace references: %w", err)
		}
	}

	// Step 5: Delete store
	if err := repo.Delete(req.StoreID); err != nil {
		return nil, fmt.Errorf("failed to delete store: %w", err)
	}

	return &DeleteStoreResult{
		StoreID:            req.StoreID,
		AffectedWorkspaces: affectedWorkspaces,
		DryRun:             false,
		Deleted:            true,
	}, nil
}

// findWorkspacesUsingStore enumerates all workspaces and finds which ones use
// the physical store identified by storeID in resolvedScope.
func (e *Engine) findWorkspacesUsingStore(storeID, resolvedScope string) ([]WorkspaceUsage, error) {
	var usages []WorkspaceUsage

	if err := e.forEachWorkspaceState(func(workspaceID string, ws *state.WorkspaceState) error {
		usage := e.checkWorkspaceUsage(ws, storeID, workspaceID, resolvedScope)
		if usage != nil {
			usages = append(usages, *usage)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return usages, nil
}

// checkWorkspaceUsage checks if a workspace uses the given physical store.
func (e *Engine) checkWorkspaceUsage(ws *state.WorkspaceState, storeID, workspaceID, resolvedScope string) *WorkspaceUsage {
	ws.MigrateDeprecatedStack()
	if !storeReferencesMatchScope(ws, storeID, resolvedScope) {
		return nil
	}
	isActive := ws.ActiveStore == storeID
	inStack := ws.GetAppliedStore(storeID) != nil
	appliedPathCount := 0

	// Count applied paths
	for _, ownership := range ws.Paths {
		if ownership.Store == storeID {
			appliedPathCount++
		}
	}

	// Return usage if store is used in any way
	if isActive || inStack || appliedPathCount > 0 {
		return &WorkspaceUsage{
			WorkspaceID:      workspaceID,
			WorkspacePath:    ws.WorkspacePath,
			IsActive:         isActive,
			InStack:          inStack,
			AppliedPathCount: appliedPathCount,
		}
	}

	return nil
}

// storeReferencesMatchScope reports whether this workspace's references to
// storeID name the physical store in resolvedScope. Applied-store and path
// records store only an ID. When that ID is the active store and
// ActiveStoreScope is set, the scope selects the physical store, so a
// same-ID store in the other scope is neither reported nor cleaned. Legacy
// records with an empty scope still match by ID.
func storeReferencesMatchScope(ws *state.WorkspaceState, storeID, resolvedScope string) bool {
	if ws.ActiveStore == storeID && ws.ActiveStoreScope != "" {
		return ws.ActiveStoreScope == resolvedScope
	}
	return true
}

// cleanWorkspaceReferences removes references to the physical store from affected workspaces.
func (e *Engine) cleanWorkspaceReferences(storeID, resolvedScope string, affectedWorkspaces []WorkspaceUsage) error {
	for _, usage := range affectedWorkspaces {
		// Load workspace state
		ws, workspaceStore, err := e.loadWorkspaceFromScopes(usage.WorkspaceID)
		if err != nil {
			return fmt.Errorf("failed to load workspace %s: %w", usage.WorkspaceID, err)
		}
		ws.MigrateDeprecatedStack()
		if !storeReferencesMatchScope(ws, storeID, resolvedScope) {
			continue
		}

		if ws.ActiveStore == storeID {
			ws.ActiveStore = ""
			ws.ActiveStoreScope = ""
		}

		ws.Stack = []string{}
		ws.RemoveAppliedStore(storeID)

		// Remove paths owned by this store
		for path, ownership := range ws.Paths {
			if ownership.Store == storeID {
				delete(ws.Paths, path)
			}
		}

		// Set applied to false if no paths remain
		if len(ws.Paths) == 0 {
			ws.Applied = false
		}

		// Save updated state
		if err := workspaceStore.SaveWorkspace(usage.WorkspaceID, ws); err != nil {
			return fmt.Errorf("failed to save workspace %s: %w", usage.WorkspaceID, err)
		}
	}

	return nil
}
