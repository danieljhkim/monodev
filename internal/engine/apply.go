package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/lockfile"
	"github.com/danieljhkim/monodev/internal/planner"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// Algorithm steps:
// 1. Resolve stores (stack + active store)
// 2. Discover repo and compute workspace ID
// 3. Load workspace state (if exists)
// 4. Preflight checks (generate plan, check for conflicts)
// 5. Apply overlays (if not DryRun)
// 6. Persist workspace state
// 7. Return result
func (e *Engine) Apply(ctx context.Context, req *ApplyRequest) (*ApplyResult, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	root, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to discover workspace: %w", err)
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)
	unlockWorkspace, err := e.lockWorkspace(ctx, workspaceID, lockfile.Exclusive)
	if err != nil {
		return nil, err
	}
	defer unlockWorkspace()

	workspaceState, _, err := e.LoadOrCreateWorkspaceState(root, repoFingerprint, workspacePath, req.Mode)
	if err != nil {
		return nil, fmt.Errorf("failed to load or create workspace state: %w", err)
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	workspaceRoot := filepath.Join(root, workspacePath)
	var excludeWarnings []string
	if !req.DryRun {
		recoveryWarnings, recoverErr := e.recoverWorkspaceOverlay(ctx, workspaceID, root, workspaceRoot, workspacePath)
		if recoverErr != nil {
			return nil, recoverErr
		}
		excludeWarnings = append(excludeWarnings, recoveryWarnings...)
		reloaded, reloadErr := e.stateStore.LoadWorkspace(workspaceID)
		if reloadErr == nil {
			workspaceState = reloaded
			workspaceState.AbsolutePath = workspaceRoot
		} else if !os.IsNotExist(reloadErr) {
			return nil, fmt.Errorf("failed to reload workspace state: %w", reloadErr)
		} else {
			workspaceState = state.NewWorkspaceState(repoFingerprint, workspacePath, req.Mode)
			workspaceState.AbsolutePath = workspaceRoot
		}
	}

	var storeToApply string
	if req.StoreID != "" {
		storeToApply = req.StoreID
	} else {
		if workspaceState.ActiveStore == "" {
			return nil, ErrNoActiveStore
		}
		storeToApply = workspaceState.ActiveStore
	}
	orderedStores := []string{storeToApply}

	// If workspace state exists, verify mode matches
	if workspaceState.Applied && workspaceState.Mode != req.Mode {
		// TODO: add force option - too overcomplicated for now
		return nil, fmt.Errorf("%w: existing mode is %s, requested mode is %s", ErrValidation, workspaceState.Mode, req.Mode)
	}

	// Resolve the store repo.
	// When StoreID is explicitly provided, search by store ID (no checkout required).
	// Otherwise fall back to the workspace's active store.
	var applyRepo stores.StoreRepo
	if req.StoreID != "" {
		locations, findErr := e.storeResolver.findStore(storeToApply)
		if findErr != nil {
			return nil, fmt.Errorf("failed to resolve store: %w", findErr)
		}
		if len(locations) == 0 {
			return nil, fmt.Errorf("failed to resolve store: %w: store '%s' not found", ErrNotFound, storeToApply)
		}

		// Apply by explicit ID should remain usable without checkout.
		// If duplicated across scopes, prefer component scope.
		chosen := locations[0]
		for _, loc := range locations {
			if loc.Scope == stores.ScopeComponent {
				chosen = loc
				break
			}
		}
		applyRepo = chosen.Repo
		workspaceState.ActiveStoreScope = chosen.Scope
	} else {
		applyRepo, err = e.storeResolver.activeStoreRepo(workspaceState)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve store repo: %w", err)
		}
	}

	unlockStore, err := e.lockStores(ctx, storeLockRequest{repo: applyRepo, id: storeToApply, mode: lockfile.Shared})
	if err != nil {
		return nil, err
	}
	defer unlockStore()

	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	plan, err := planner.BuildApplyPlan(
		workspaceState,
		orderedStores,
		req.Mode,
		root,
		applyRepo,
		e.fs,
		req.Force,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build apply plan: %w", err)
	}
	plan.Warnings = append(plan.Warnings, excludeWarnings...)
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	if plan.HasConflicts() && !req.Force {
		return &ApplyResult{
			Plan:            plan,
			Applied:         []planner.Operation{},
			WorkspaceID:     workspaceID,
			RepoFingerprint: repoFingerprint,
			WorkspacePath:   workspacePath,
		}, fmt.Errorf("%w: %d conflicts detected", ErrConflict, len(plan.Conflicts))
	}

	if req.DryRun {
		return &ApplyResult{
			Plan:            plan,
			Applied:         []planner.Operation{},
			WorkspaceID:     workspaceID,
			RepoFingerprint: repoFingerprint,
			WorkspacePath:   workspacePath,
		}, nil
	}

	appliedOps := append([]planner.Operation{}, plan.Operations...)
	var finalState *state.WorkspaceState
	if err := e.runOverlayTxn(ctx, overlayTxnRequest{
		kind:          overlayTxnApply,
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
			final.Applied = true
			final.Mode = req.Mode
			final.ActiveStore = storeToApply
			final.AddAppliedStore(storeToApply, req.Mode)
			finalState = final
			return final, false, nil
		},
	}); err != nil {
		return nil, err
	}
	plan.Warnings = appendExcludeWarning(plan.Warnings, e.syncManagedExcludes(root, workspacePath, finalState))

	return &ApplyResult{
		Plan:            plan,
		Applied:         appliedOps,
		WorkspaceID:     workspaceID,
		RepoFingerprint: repoFingerprint,
		WorkspacePath:   workspacePath,
	}, nil
}
