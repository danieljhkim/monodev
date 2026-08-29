package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/lockfile"
	"github.com/danieljhkim/monodev/internal/planner"
	"github.com/danieljhkim/monodev/internal/state"
)

// StackApply applies all stores in the configured stack to the workspace.
// This does not include the active store - only stores added via 'stack add'.
func (e *Engine) StackApply(ctx context.Context, req *StackApplyRequest) (*StackApplyResult, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
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
	workspaceRoot := filepath.Join(root, workspacePath)
	if !req.DryRun {
		if err := e.recoverOverlayTxn(ctx, workspaceID, workspaceRoot); err != nil {
			return nil, err
		}
		reloaded, reloadErr := e.stateStore.LoadWorkspace(workspaceID)
		if reloadErr == nil {
			workspaceState = reloaded
			workspaceState.AbsolutePath = workspaceRoot
		} else if !os.IsNotExist(reloadErr) {
			return nil, fmt.Errorf("failed to reload workspace state: %w", reloadErr)
		} else {
			workspaceState = state.NewWorkspaceState(repoFingerprint, workspacePath, "copy")
			workspaceState.AbsolutePath = workspaceRoot
		}
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

	appliedOps := append([]planner.Operation{}, plan.Operations...)
	if err := e.runOverlayTxn(ctx, overlayTxnRequest{
		kind:          overlayTxnStackApply,
		workspaceID:   workspaceID,
		workspaceRoot: workspaceRoot,
		ops:           plan.Operations,
		finalize: func() (*state.WorkspaceState, bool, error) {
			final := state.CloneWorkspaceState(workspaceState)
			if final.Paths == nil {
				final.Paths = map[string]state.PathOwnership{}
			}
			for _, op := range plan.Operations {
				if op.Type != planner.OpRemove {
					final.Paths[op.RelPath] = e.ownershipForAppliedPath(op, req.Mode)
				} else {
					delete(final.Paths, op.RelPath)
				}
			}
			final.RefreshAppliedStores()
			return final, false, nil
		},
	}); err != nil {
		return nil, err
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
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
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
	workspaceRoot := filepath.Join(root, workspacePath)
	if !req.DryRun {
		if err := e.recoverOverlayTxn(ctx, workspaceID, workspaceRoot); err != nil {
			return nil, err
		}
		reloaded, reloadErr := e.stateStore.LoadWorkspace(workspaceID)
		if reloadErr == nil {
			workspaceState = reloaded
			workspaceState.AbsolutePath = workspaceRoot
		} else if !os.IsNotExist(reloadErr) {
			return nil, fmt.Errorf("failed to reload workspace state: %w", reloadErr)
		} else {
			workspaceState = state.NewWorkspaceState(repoFingerprint, workspacePath, "copy")
			workspaceState.AbsolutePath = workspaceRoot
		}
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

	ops, removed, err := e.planManagedPathRemoval(workspaceRoot, workspaceState, stackPaths, req.Force)
	if err != nil {
		return nil, err
	}

	final := state.CloneWorkspaceState(workspaceState)
	for _, relPath := range removed {
		delete(final.Paths, relPath)
	}
	final.RefreshAppliedStores()

	if err := e.runOverlayTxn(ctx, overlayTxnRequest{
		kind:          overlayTxnStackUnapply,
		workspaceID:   workspaceID,
		workspaceRoot: workspaceRoot,
		ops:           ops,
		finalize: func() (*state.WorkspaceState, bool, error) {
			return final, false, nil
		},
	}); err != nil {
		return nil, err
	}

	return &StackUnapplyResult{
		Removed:     removed,
		WorkspaceID: workspaceID,
	}, nil
}
