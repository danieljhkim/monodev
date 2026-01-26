package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/planner"
	"github.com/danieljhkim/monodev/internal/state"
)

// Apply applies store overlays to the workspace following the algorithm from PLAN.md.
//
// Algorithm steps:
// 1. Resolve stores (stack + active store)
// 2. Discover repo and compute workspace ID
// 3. Load workspace state (if exists)
// 4. Preflight checks (generate plan, check for conflicts)
// 5. Apply overlays (if not DryRun)
// 6. Persist workspace state
// 7. Return result
func (e *Engine) Apply(ctx context.Context, req *ApplyRequest) (*ApplyResult, error) {
	// Step 1: Discover repository
	_, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to discover workspace: %w", err)
	}

	// Step 2: Compute workspace ID
	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)

	// Step 3: Load or create workspace state
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			// Create new workspace state
			workspaceState = state.NewWorkspaceState(repoFingerprint, workspacePath, req.Mode)
		} else {
			return nil, fmt.Errorf("failed to load workspace state: %w", err)
		}
	}

	// Step 4: Resolve store to apply (single store only, NOT the stack)
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
	if workspaceState.Applied && workspaceState.Mode != req.Mode && !req.Force {
		return nil, fmt.Errorf("%w: existing mode is %s, requested mode is %s", ErrValidation, workspaceState.Mode, req.Mode)
	}

	// Step 6: Preflight checks - generate plan
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

	// Check for conflicts
	if plan.HasConflicts() && !req.Force {
		return &ApplyResult{
			Plan:            plan,
			Applied:         []planner.Operation{},
			WorkspaceID:     workspaceID,
			RepoFingerprint: repoFingerprint,
			WorkspacePath:   workspacePath,
		}, fmt.Errorf("%w: %d conflicts detected", ErrConflict, len(plan.Conflicts))
	}

	// If dry run, return plan without executing
	if req.DryRun {
		return &ApplyResult{
			Plan:            plan,
			Applied:         []planner.Operation{},
			WorkspaceID:     workspaceID,
			RepoFingerprint: repoFingerprint,
			WorkspacePath:   workspacePath,
		}, nil
	}

	// Step 7: Apply overlays
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
			// Remove operation - delete from workspace state (use relative path)
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

// executeOperation executes a single operation.
func (e *Engine) executeOperation(op planner.Operation) error {
	switch op.Type {
	case planner.OpRemove:
		return e.executeRemove(op)
	case planner.OpCreateSymlink:
		return e.executeCreateSymlink(op)
	case planner.OpCopy:
		return e.executeCopy(op)
	default:
		return fmt.Errorf("unknown operation type: %s", op.Type)
	}
}

// executeRemove removes a path.
func (e *Engine) executeRemove(op planner.Operation) error {
	// Check if path exists
	exists, err := e.fs.Exists(op.DestPath)
	if err != nil {
		return fmt.Errorf("failed to check if path exists: %w", err)
	}
	if !exists {
		// Path doesn't exist - nothing to remove
		return nil
	}

	// Remove the path
	if err := e.fs.RemoveAll(op.DestPath); err != nil {
		return fmt.Errorf("failed to remove path: %w", err)
	}

	return nil
}

// executeCreateSymlink creates a symlink.
func (e *Engine) executeCreateSymlink(op planner.Operation) error {
	// Create parent directory if needed
	parentDir := filepath.Dir(op.DestPath)
	if err := e.fs.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Create symlink
	if err := e.fs.Symlink(op.SourcePath, op.DestPath); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	return nil
}

// executeCopy copies a file or directory.
func (e *Engine) executeCopy(op planner.Operation) error {
	// Copy the path
	if err := e.fs.Copy(op.SourcePath, op.DestPath); err != nil {
		return fmt.Errorf("failed to copy: %w", err)
	}

	return nil
}
