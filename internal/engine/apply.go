package engine

import (
	"context"
	"fmt"

	"github.com/danieljhkim/monodev/internal/planner"
	"github.com/danieljhkim/monodev/internal/state"
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
	_, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to discover workspace: %w", err)
	}

	workspaceState, workspaceID, err := e.LoadOrCreateWorkspaceState(repoFingerprint, workspacePath, req.Mode)
	if err != nil {
		return nil, fmt.Errorf("failed to load or create workspace state: %w", err)
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

	plan, err := planner.BuildApplyPlan(
		workspaceState,
		orderedStores,
		req.Mode,
		req.CWD,
		e.storeRepo,
		e.fs,
		req.Force,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build apply plan: %w", err)
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

			workspaceState.Paths[op.RelPath] = ownership
		} else {
			delete(workspaceState.Paths, op.RelPath)
		}
	}

	// Update workspace state metadata (only active store, preserve stack)
	workspaceState.Applied = true
	workspaceState.Mode = req.Mode
	// Note: Stack is NOT modified here - apply is for single stores only
	workspaceState.ActiveStore = storeToApply
	workspaceState.AddAppliedStore(storeToApply, req.Mode)

	// Step 8: Persist workspace state atomically
	if err := e.stateStore.SaveWorkspace(workspaceID, workspaceState); err != nil {
		return nil, fmt.Errorf("failed to save workspace state: %w", err)
	}

	return &ApplyResult{
		Plan:            plan,
		Applied:         appliedOps,
		WorkspaceID:     workspaceID,
		RepoFingerprint: repoFingerprint,
		WorkspacePath:   workspacePath,
	}, nil
}
