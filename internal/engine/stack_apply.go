package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/danieljhkim/monodev/internal/planner"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// StackApply applies all stores in the configured stack to the workspace.
// This does not include the active store - only stores added via 'stack add'.
func (e *Engine) StackApply(ctx context.Context, req *StackApplyRequest) (*StackApplyResult, error) {
	root, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to discover workspace: %w", err)
	}
	workspaceState, workspaceID, err := e.LoadOrCreateWorkspaceState(root, repoFingerprint, workspacePath, "copy")
	if err != nil {
		return nil, fmt.Errorf("failed to load or create workspace state: %w", err)
	}

	if len(workspaceState.Stack) == 0 {
		return nil, fmt.Errorf("%w: stack is empty (use 'stack add' first)", ErrValidation)
	}

	// If workspace state exists, verify mode matches
	if workspaceState.Applied && workspaceState.Mode != req.Mode {
		return nil, fmt.Errorf("%w: existing mode is %s, requested mode is %s", ErrValidation, workspaceState.Mode, req.Mode)
	}

	// Build apply plan using only stack stores (no active store)
	orderedStores := append([]string{}, workspaceState.Stack...)

	// Resolve each stack store's scope and build a MultiStoreRepo
	storeMapping := make(map[string]stores.StoreRepo)
	for _, sid := range orderedStores {
		locations, findErr := e.findStore(sid)
		if findErr != nil {
			return nil, fmt.Errorf("failed to find store %s: %w", sid, findErr)
		}
		if len(locations) > 0 {
			// Prefer component scope if available
			for _, loc := range locations {
				if loc.Scope == stores.ScopeComponent {
					storeMapping[sid] = loc.Repo
					break
				}
			}
			if _, ok := storeMapping[sid]; !ok {
				storeMapping[sid] = locations[0].Repo
			}
		}
	}
	multiRepo := stores.NewMultiStoreRepo(storeMapping, e.storeRepo)

	// Always detect conflicts (force=false for detection)
	plan, err := planner.BuildApplyPlan(
		workspaceState,
		orderedStores,
		req.Mode,
		root,
		multiRepo,
		e.fs,
		false, // Always detect conflicts in planning phase
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build apply plan: %w", err)
	}

	// Check for conflicts
	if plan.HasConflicts() && !req.Force {
		return &StackApplyResult{
			Plan:            plan,
			Applied:         []planner.Operation{},
			WorkspaceID:     workspaceID,
			RepoFingerprint: repoFingerprint,
			WorkspacePath:   workspacePath,
		}, fmt.Errorf("%w: %d conflicts detected", ErrConflict, len(plan.Conflicts))
	}

	// If dry run, return plan without executing
	if req.DryRun {
		return &StackApplyResult{
			Plan:            plan,
			Applied:         []planner.Operation{},
			WorkspaceID:     workspaceID,
			RepoFingerprint: repoFingerprint,
			WorkspacePath:   workspacePath,
		}, nil
	}

	// Apply overlays
	appliedOps := []planner.Operation{}
	for _, op := range plan.Operations {
		if err := e.executeOperation(op); err != nil {
			return nil, fmt.Errorf("failed to execute operation: %w", err)
		}
		appliedOps = append(appliedOps, op)

		// Update workspace state for non-remove operations
		if op.Type != planner.OpRemove {
			ownership := state.PathOwnership{
				Store:     op.Store,
				Type:      req.Mode,
				Timestamp: e.clock.Now(),
			}

			// Compute checksum for copy mode (files only, not directories)
			if req.Mode == "copy" {
				info, err := e.fs.Lstat(op.DestPath)
				if err == nil && !info.IsDir() {
					checksum, err := e.hasher.HashFile(op.DestPath)
					if err == nil {
						ownership.Checksum = checksum
					}
				}
			}

			// Use relative path as key for workspace state
			workspaceState.Paths[op.RelPath] = ownership
		} else {
			// Remove operation - delete from workspace state
			delete(workspaceState.Paths, op.RelPath)
		}
	}

	workspaceState.RefreshAppliedStores()

	if err := e.stateStore.SaveWorkspace(workspaceID, workspaceState); err != nil {
		return nil, fmt.Errorf("failed to save workspace state: %w", err)
	}

	return &StackApplyResult{
		Plan:            plan,
		Applied:         appliedOps,
		WorkspaceID:     workspaceID,
		RepoFingerprint: repoFingerprint,
		WorkspacePath:   workspacePath,
	}, nil
}

// StackUnapply removes only paths applied by the stack stores.
// Paths applied by the active store are not affected, unless they overlap
func (e *Engine) StackUnapply(ctx context.Context, req *StackUnapplyRequest) (*StackUnapplyResult, error) {
	root, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to discover workspace: %w", err)
	}
	workspaceState, workspaceID, err := e.LoadOrCreateWorkspaceState(root, repoFingerprint, workspacePath, "copy")
	if err != nil {
		return nil, fmt.Errorf("failed to load or create workspace state: %w", err)
	}
	if len(workspaceState.Stack) == 0 {
		return nil, fmt.Errorf("%w: stack is empty", ErrValidation)
	}

	stackStores := make(map[string]bool)
	for _, store := range workspaceState.Stack {
		stackStores[store] = true
	}

	stackPaths := []string{}
	for relPath, ownership := range workspaceState.Paths {
		if stackStores[ownership.Store] {
			stackPaths = append(stackPaths, relPath)
		}
	}

	if len(stackPaths) == 0 {
		return &StackUnapplyResult{
			Removed:     []string{},
			WorkspaceID: workspaceID,
		}, nil
	}

	if req.DryRun {
		return &StackUnapplyResult{
			Removed:     stackPaths,
			WorkspaceID: workspaceID,
		}, nil
	}

	// Remove stack paths in deepest-first order
	sort.Slice(stackPaths, func(i, j int) bool {
		depthI := countPathSeparators(stackPaths[i])
		depthJ := countPathSeparators(stackPaths[j])
		if depthI != depthJ {
			return depthI > depthJ // Deeper paths first
		}
		return stackPaths[i] > stackPaths[j] // Alphabetically for same depth
	})

	removed := []string{}
	for _, relPath := range stackPaths {
		ownership := workspaceState.Paths[relPath]

		// Convert repo-relative path to absolute for filesystem operations
		absPath := filepath.Join(root, relPath)

		// Validate the path before removing (unless force)
		if !req.Force {
			if err := e.validateManagedPath(absPath, ownership); err != nil {
				return nil, fmt.Errorf("validation failed for %s: %w", relPath, err)
			}
		}

		// Remove the path
		if err := e.fs.RemoveAll(absPath); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to remove %s: %w", relPath, err)
		}

		// Remove from workspace state
		delete(workspaceState.Paths, relPath)
		removed = append(removed, relPath)
	}

	if err := e.stateStore.SaveWorkspace(workspaceID, workspaceState); err != nil {
		return nil, fmt.Errorf("failed to save workspace state: %w", err)
	}

	return &StackUnapplyResult{
		Removed:     removed,
		WorkspaceID: workspaceID,
	}, nil
}
