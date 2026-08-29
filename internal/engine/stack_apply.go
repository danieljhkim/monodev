package engine

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/lockfile"
	"github.com/danieljhkim/monodev/internal/planner"
	"github.com/danieljhkim/monodev/internal/state"
)

// StackApply applies all stores in the configured stack to the workspace.
// This does not include the active store - only stores added via 'stack add'.
func (e *Engine) StackApply(ctx context.Context, req *StackApplyRequest) (*StackApplyResult, error) {
	root, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to discover workspace: %w", err)
	}
	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)
	unlockWorkspace, err := e.lockWorkspace(ctx, workspaceID, lockfile.Exclusive)
	if err != nil {
		return nil, err
	}
	defer unlockWorkspace()
	workspaceState, _, err := e.LoadOrCreateWorkspaceState(root, repoFingerprint, workspacePath, "copy")
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

	multiRepo, err := e.storeResolver.multiRepoForStores(orderedStores)
	if err != nil {
		return nil, err
	}
	storeRequests := make([]storeLockRequest, 0, len(orderedStores))
	for _, storeID := range orderedStores {
		storeRequests = append(storeRequests, storeLockRequest{repo: multiRepo, id: storeID, mode: lockfile.Shared})
	}
	unlockStores, err := e.lockStores(ctx, storeRequests...)
	if err != nil {
		return nil, err
	}
	defer unlockStores()

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
	workspaceRoot := filepath.Join(root, workspacePath)
	for _, op := range plan.Operations {
		if err := e.executeOperation(workspaceRoot, op); err != nil {
			return nil, fmt.Errorf("failed to execute operation: %w", err)
		}
		appliedOps = append(appliedOps, op)

		// Update workspace state for non-remove operations
		if op.Type != planner.OpRemove {
			workspaceState.Paths[op.RelPath] = e.ownershipForAppliedPath(op, req.Mode)
		} else {
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
	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)
	unlockWorkspace, err := e.lockWorkspace(ctx, workspaceID, lockfile.Exclusive)
	if err != nil {
		return nil, err
	}
	defer unlockWorkspace()
	workspaceState, _, err := e.LoadOrCreateWorkspaceState(root, repoFingerprint, workspacePath, "copy")
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

	workspaceRoot := filepath.Join(root, workspacePath)
	removed, err := e.removeManagedPaths(workspaceRoot, workspaceState, stackPaths, req.Force)
	if err != nil {
		return nil, err
	}

	if err := e.stateStore.SaveWorkspace(workspaceID, workspaceState); err != nil {
		return nil, fmt.Errorf("failed to save workspace state: %w", err)
	}

	return &StackUnapplyResult{
		Removed:     removed,
		WorkspaceID: workspaceID,
	}, nil
}
