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

// Eject detaches the current workspace from monodev while retaining stores.
// Keep-files mode only removes the ownership ledger, so the bytes currently in
// the workspace are never read, rewritten, or deleted. Remove-files mode
// removes every ledger-owned path through the same overlay transaction.
func (e *Engine) Eject(ctx context.Context, req *EjectRequest) (*EjectResult, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("eject request is required")
	}

	root, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to discover workspace: %w", err)
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)
	lockMode := lockfile.Exclusive
	if req.DryRun {
		lockMode = lockfile.Shared
	}
	unlockWorkspace, err := e.lockWorkspace(ctx, workspaceID, lockMode)
	if err != nil {
		return nil, err
	}
	defer unlockWorkspace()

	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: workspace has no managed paths", ErrStateMissing)
		}
		return nil, fmt.Errorf("failed to load workspace state: %w", err)
	}
	workspaceState.AbsolutePath = filepath.Join(root, workspacePath)
	workspaceState.MigrateDeprecatedStack()
	paths := workspaceOwnedPaths(workspaceState)

	if req.DryRun {
		return ejectResult(workspaceID, paths, req.RemoveFiles, true, nil), nil
	}

	workspaceRoot := filepath.Join(root, workspacePath)
	warnings, recoverErr := e.recoverWorkspaceOverlay(ctx, workspaceID, root, workspaceRoot, workspacePath)
	if recoverErr != nil {
		return nil, recoverErr
	}
	reloaded, reloadErr := e.stateStore.LoadWorkspace(workspaceID)
	if reloadErr != nil {
		if os.IsNotExist(reloadErr) {
			// A committed interrupted eject has already detached the ledger. Its
			// transaction recovery above also reconciled the managed exclude block.
			return ejectResult(workspaceID, paths, req.RemoveFiles, false, warnings), nil
		}
		return nil, fmt.Errorf("failed to reload workspace state: %w", reloadErr)
	}
	workspaceState = reloaded
	workspaceState.AbsolutePath = workspaceRoot
	workspaceState.MigrateDeprecatedStack()
	paths = workspaceOwnedPaths(workspaceState)

	ops := []planner.Operation{}
	if req.RemoveFiles {
		var planErr error
		ops, _, planErr = e.planManagedPathRemoval(workspaceRoot, workspaceState, append([]string{}, paths...), true)
		if planErr != nil {
			return nil, planErr
		}
	}

	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := e.runOverlayTxn(ctx, overlayTxnRequest{
		kind:          overlayTxnEject,
		workspaceID:   workspaceID,
		workspaceRoot: workspaceRoot,
		ops:           ops,
		finalize: func() (*state.WorkspaceState, bool, error) {
			return nil, true, nil
		},
	}); err != nil {
		return nil, err
	}
	warnings = appendExcludeWarning(warnings, e.syncManagedExcludes(root, workspacePath, nil))
	return ejectResult(workspaceID, paths, req.RemoveFiles, false, warnings), nil
}

func workspaceOwnedPaths(workspaceState *state.WorkspaceState) []string {
	paths := make([]string, 0, len(workspaceState.Paths))
	for relPath := range workspaceState.Paths {
		paths = append(paths, relPath)
	}
	sortDeepestFirst(paths)
	return paths
}

func ejectResult(workspaceID string, paths []string, removeFiles, dryRun bool, warnings []string) *EjectResult {
	result := &EjectResult{
		Retained:    []string{},
		Removed:     []string{},
		WorkspaceID: workspaceID,
		RemoveFiles: removeFiles,
		DryRun:      dryRun,
		Warnings:    warnings,
	}
	if removeFiles {
		result.Removed = append(result.Removed, paths...)
	} else {
		result.Retained = append(result.Retained, paths...)
	}
	return result
}
