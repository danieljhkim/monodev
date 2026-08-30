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

// Apply applies one or more stores to the workspace in argument order.
// With no StoreIDs, it applies the active store. Later stores win path
// conflicts, matching the retired stack precedence rule.
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
	workspaceState.MigrateDeprecatedStack()
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
			workspaceState.MigrateDeprecatedStack()
		} else if !os.IsNotExist(reloadErr) {
			return nil, fmt.Errorf("failed to reload workspace state: %w", reloadErr)
		} else {
			workspaceState = state.NewWorkspaceState(repoFingerprint, workspacePath, req.Mode)
			workspaceState.AbsolutePath = workspaceRoot
		}
	}

	orderedStores, applyRepo, lastScope, err := e.resolveApplyStores(workspaceState, req.StoreIDs)
	if err != nil {
		return nil, err
	}

	if workspaceState.Applied && workspaceState.Mode != req.Mode {
		return nil, fmt.Errorf("%w: existing mode is %s, requested mode is %s", ErrValidation, workspaceState.Mode, req.Mode)
	}

	storeRequests := make([]storeLockRequest, 0, len(orderedStores))
	for _, storeID := range orderedStores {
		storeRequests = append(storeRequests, storeLockRequest{repo: applyRepo, id: storeID, mode: lockfile.Shared})
	}
	unlockStores, err := e.lockStores(ctx, storeRequests...)
	if err != nil {
		return nil, err
	}
	defer unlockStores()

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
			if len(req.StoreIDs) > 0 {
				final.ActiveStore = orderedStores[len(orderedStores)-1]
				if lastScope != "" {
					final.ActiveStoreScope = lastScope
				}
			} else {
				final.ActiveStore = orderedStores[0]
			}
			for _, storeID := range orderedStores {
				ownsPath := false
				for _, ownership := range final.Paths {
					if ownership.Store == storeID {
						ownsPath = true
						break
					}
				}
				if ownsPath {
					final.AddAppliedStore(storeID, req.Mode)
				}
			}
			final.PruneAppliedStores()
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

func (e *Engine) resolveApplyStores(workspaceState *state.WorkspaceState, storeIDs []string) ([]string, stores.StoreRepo, string, error) {
	if len(storeIDs) == 0 {
		if workspaceState.ActiveStore == "" {
			return nil, nil, "", ErrNoActiveStore
		}
		applyRepo, err := e.storeResolver.activeStoreRepo(workspaceState)
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to resolve store repo: %w", err)
		}
		return []string{workspaceState.ActiveStore}, applyRepo, workspaceState.ActiveStoreScope, nil
	}

	for _, storeID := range storeIDs {
		locations, findErr := e.storeResolver.findStore(storeID)
		if findErr != nil {
			return nil, nil, "", fmt.Errorf("failed to resolve store: %w", findErr)
		}
		if len(locations) == 0 {
			return nil, nil, "", fmt.Errorf("failed to resolve store: %w: store '%s' not found", ErrNotFound, storeID)
		}
	}

	applyRepo, err := e.storeResolver.multiRepoForStores(storeIDs)
	if err != nil {
		return nil, nil, "", err
	}

	last := storeIDs[len(storeIDs)-1]
	locations, err := e.storeResolver.findStore(last)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to resolve store: %w", err)
	}
	chosen := locations[0]
	for _, loc := range locations {
		if loc.Scope == stores.ScopeComponent {
			chosen = loc
			break
		}
	}
	return storeIDs, applyRepo, chosen.Scope, nil
}
