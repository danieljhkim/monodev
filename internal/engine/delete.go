package engine

import (
	"context"
	"fmt"
	"slices"

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
	// Step 1: Resolve store scope
	repo, _, err := e.storeResolver.resolveStoreRepo(req.StoreID, req.Scope)
	if err != nil {
		return nil, err
	}

	// Step 2: Find affected workspaces
	affectedWorkspaces, err := e.findWorkspacesUsingStore(req.StoreID)
	if err != nil {
		return nil, fmt.Errorf("failed to find workspaces using store: %w", err)
	}

	// Step 3: Return early if dry-run
	if req.DryRun {
		return &DeleteStoreResult{
			StoreID:            req.StoreID,
			AffectedWorkspaces: affectedWorkspaces,
			DryRun:             true,
			Deleted:            false,
		}, nil
	}

	// Step 4: If store is in use and not forced, return error
	if len(affectedWorkspaces) > 0 && !req.Force {
		return &DeleteStoreResult{
			StoreID:            req.StoreID,
			AffectedWorkspaces: affectedWorkspaces,
			DryRun:             false,
			Deleted:            false,
		}, fmt.Errorf("store '%s' is in use by %d workspace(s)", req.StoreID, len(affectedWorkspaces))
	}

	// Step 5: Clean workspace references
	if len(affectedWorkspaces) > 0 {
		if err := e.cleanWorkspaceReferences(req.StoreID, affectedWorkspaces); err != nil {
			return nil, fmt.Errorf("failed to clean workspace references: %w", err)
		}
	}

	// Step 6: Delete store
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

// findWorkspacesUsingStore enumerates all workspaces (both scopes) and finds which ones use the given store.
func (e *Engine) findWorkspacesUsingStore(storeID string) ([]WorkspaceUsage, error) {
	var usages []WorkspaceUsage

	if err := e.forEachWorkspaceState(func(workspaceID string, ws *state.WorkspaceState) error {
		usage := e.checkWorkspaceUsage(ws, storeID, workspaceID)
		if usage != nil {
			usages = append(usages, *usage)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return usages, nil
}

// checkWorkspaceUsage checks if a workspace uses the given store.
func (e *Engine) checkWorkspaceUsage(ws *state.WorkspaceState, storeID, workspaceID string) *WorkspaceUsage {
	isActive := ws.ActiveStore == storeID
	inStack := slices.Contains(ws.Stack, storeID)
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

// cleanWorkspaceReferences removes all references to the store from affected workspaces.
func (e *Engine) cleanWorkspaceReferences(storeID string, affectedWorkspaces []WorkspaceUsage) error {
	for _, usage := range affectedWorkspaces {
		// Load workspace state
		ws, workspaceStore, err := e.loadWorkspaceFromScopes(usage.WorkspaceID)
		if err != nil {
			return fmt.Errorf("failed to load workspace %s: %w", usage.WorkspaceID, err)
		}

		// Clear active store if it matches
		if ws.ActiveStore == storeID {
			ws.ActiveStore = ""
		}

		// Remove from stack
		newStack := []string{}
		for _, s := range ws.Stack {
			if s != storeID {
				newStack = append(newStack, s)
			}
		}
		ws.Stack = newStack

		// Remove from applied stores
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
