package engine

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/danieljhkim/monodev/internal/state"
)

// DeleteStore deletes a store after checking for usage by workspaces.
// Algorithm steps:
// 1. Validate store exists
// 2. Find all workspaces using the store
// 3. Return early if dry-run
// 4. If store is in use and not forced, return error with affected workspaces
// 5. Clean workspace references
// 6. Delete store
// 7. Return result
func (e *Engine) DeleteStore(ctx context.Context, req *DeleteStoreRequest) (*DeleteStoreResult, error) {
	// Step 1: Validate store exists
	exists, err := e.storeRepo.Exists(req.StoreID)
	if err != nil {
		return nil, fmt.Errorf("failed to check if store exists: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("%w: store '%s' not found", ErrNotFound, req.StoreID)
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
	if err := e.storeRepo.Delete(req.StoreID); err != nil {
		return nil, fmt.Errorf("failed to delete store: %w", err)
	}

	return &DeleteStoreResult{
		StoreID:            req.StoreID,
		AffectedWorkspaces: affectedWorkspaces,
		DryRun:             false,
		Deleted:            true,
	}, nil
}

// findWorkspacesUsingStore enumerates all workspaces and finds which ones use the given store.
func (e *Engine) findWorkspacesUsingStore(storeID string) ([]WorkspaceUsage, error) {
	// Read all workspace files
	entries, err := os.ReadDir(e.configPaths.Workspaces)
	if err != nil {
		if os.IsNotExist(err) {
			return []WorkspaceUsage{}, nil
		}
		return nil, fmt.Errorf("failed to read workspaces directory: %w", err)
	}

	var usages []WorkspaceUsage
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		// Extract workspace ID (strip .json extension)
		workspaceID := strings.TrimSuffix(entry.Name(), ".json")

		// Load workspace state
		ws, err := e.stateStore.LoadWorkspace(workspaceID)
		if err != nil {
			// Skip corrupted or unreadable workspace files
			continue
		}

		// Check usage
		usage := e.checkWorkspaceUsage(ws, storeID, workspaceID)
		if usage != nil {
			usages = append(usages, *usage)
		}
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
		ws, err := e.stateStore.LoadWorkspace(usage.WorkspaceID)
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
		if err := e.stateStore.SaveWorkspace(usage.WorkspaceID, ws); err != nil {
			return fmt.Errorf("failed to save workspace %s: %w", usage.WorkspaceID, err)
		}
	}

	return nil
}
