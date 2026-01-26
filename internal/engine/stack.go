package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/danieljhkim/monodev/internal/planner"
	"github.com/danieljhkim/monodev/internal/state"
)

// StackListRequest represents a request to list the store stack.
type StackListRequest struct {
	// CWD is the current working directory
	CWD string
}

// StackListResult represents the result of listing the store stack.
type StackListResult struct {
	// Stack is the ordered list of stores
	Stack []string

	// ActiveStore is the currently active store
	ActiveStore string
}

// StackAddRequest represents a request to add a store to the stack.
type StackAddRequest struct {
	// CWD is the current working directory
	CWD string

	// StoreID is the store to add to the stack
	StoreID string
}

// StackPopRequest represents a request to remove a store from the stack.
type StackPopRequest struct {
	// CWD is the current working directory
	CWD string

	// StoreID is the store to remove (if empty, removes last store - LIFO)
	StoreID string
}

// StackPopResult represents the result of removing a store from the stack.
type StackPopResult struct {
	// Removed is the store that was removed
	Removed string
}

// StackClearRequest represents a request to clear the stack.
type StackClearRequest struct {
	// CWD is the current working directory
	CWD string
}

// StackList returns the current store stack for the workspace.
func (e *Engine) StackList(ctx context.Context, req *StackListRequest) (*StackListResult, error) {
	// Discover repository
	repoRoot, err := e.gitRepo.Discover(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotInRepo, err)
	}

	repoFingerprint, err := e.gitRepo.Fingerprint(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to compute repo fingerprint: %w", err)
	}

	workspacePath, err := e.gitRepo.RelPath(repoRoot, req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to compute workspace path: %w", err)
	}

	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)

	// Load workspace state
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			// No workspace state yet, return empty stack
			return &StackListResult{
				Stack:       []string{},
				ActiveStore: "",
			}, nil
		}
		return nil, fmt.Errorf("failed to load workspace state: %w", err)
	}

	return &StackListResult{
		Stack:       workspaceState.Stack,
		ActiveStore: workspaceState.ActiveStore,
	}, nil
}

// StackAdd adds a store to the stack.
func (e *Engine) StackAdd(ctx context.Context, req *StackAddRequest) error {
	// Discover repository
	repoRoot, err := e.gitRepo.Discover(req.CWD)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotInRepo, err)
	}

	repoFingerprint, err := e.gitRepo.Fingerprint(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to compute repo fingerprint: %w", err)
	}

	workspacePath, err := e.gitRepo.RelPath(repoRoot, req.CWD)
	if err != nil {
		return fmt.Errorf("failed to compute workspace path: %w", err)
	}

	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)

	// Verify store exists
	exists, err := e.storeRepo.Exists(req.StoreID)
	if err != nil {
		return fmt.Errorf("failed to check if store exists: %w", err)
	}
	if !exists {
		return fmt.Errorf("%w: store %s does not exist", ErrNotFound, req.StoreID)
	}

	// Load or create workspace state
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			// Create new workspace state
			workspaceState = state.NewWorkspaceState(repoFingerprint, workspacePath, "symlink")
		} else {
			return fmt.Errorf("failed to load workspace state: %w", err)
		}
	}

	// Check for duplicates
	if slices.Contains(workspaceState.Stack, req.StoreID) {
		return fmt.Errorf("%w: store %s is already in the stack", ErrValidation, req.StoreID)
	}

	// Add to stack
	workspaceState.Stack = append(workspaceState.Stack, req.StoreID)

	// Save workspace state
	if err := e.stateStore.SaveWorkspace(workspaceID, workspaceState); err != nil {
		return fmt.Errorf("failed to save workspace state: %w", err)
	}

	return nil
}

// StackPop removes a store from the stack.
// If StoreID is empty, removes the last store (LIFO).
// If StoreID is specified, removes that specific store.
func (e *Engine) StackPop(ctx context.Context, req *StackPopRequest) (*StackPopResult, error) {
	// Discover repository
	repoRoot, err := e.gitRepo.Discover(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotInRepo, err)
	}

	repoFingerprint, err := e.gitRepo.Fingerprint(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to compute repo fingerprint: %w", err)
	}

	workspacePath, err := e.gitRepo.RelPath(repoRoot, req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to compute workspace path: %w", err)
	}

	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)

	// Load workspace state
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: stack is empty", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to load workspace state: %w", err)
	}

	if len(workspaceState.Stack) == 0 {
		return nil, fmt.Errorf("%w: stack is empty", ErrNotFound)
	}

	var removed string

	if req.StoreID == "" {
		// LIFO: remove last store
		removed = workspaceState.Stack[len(workspaceState.Stack)-1]
		workspaceState.Stack = workspaceState.Stack[:len(workspaceState.Stack)-1]
	} else {
		// Remove specific store
		found := false
		newStack := make([]string, 0, len(workspaceState.Stack))
		for _, s := range workspaceState.Stack {
			if s == req.StoreID {
				removed = s
				found = true
			} else {
				newStack = append(newStack, s)
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: store %s is not in the stack", ErrNotFound, req.StoreID)
		}
		workspaceState.Stack = newStack
	}

	// Save workspace state
	if err := e.stateStore.SaveWorkspace(workspaceID, workspaceState); err != nil {
		return nil, fmt.Errorf("failed to save workspace state: %w", err)
	}

	return &StackPopResult{
		Removed: removed,
	}, nil
}

// StackClear removes all stores from the stack.
func (e *Engine) StackClear(ctx context.Context, req *StackClearRequest) error {
	// Discover repository
	repoRoot, err := e.gitRepo.Discover(req.CWD)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotInRepo, err)
	}

	repoFingerprint, err := e.gitRepo.Fingerprint(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to compute repo fingerprint: %w", err)
	}

	workspacePath, err := e.gitRepo.RelPath(repoRoot, req.CWD)
	if err != nil {
		return fmt.Errorf("failed to compute workspace path: %w", err)
	}

	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)

	// Load workspace state
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			// No workspace state yet, nothing to clear
			return nil
		}
		return fmt.Errorf("failed to load workspace state: %w", err)
	}

	// Clear stack
	workspaceState.Stack = []string{}

	// Save workspace state
	if err := e.stateStore.SaveWorkspace(workspaceID, workspaceState); err != nil {
		return fmt.Errorf("failed to save workspace state: %w", err)
	}

	return nil
}

// StackApply applies all stores in the configured stack to the workspace.
// This does not include the active store - only stores added via 'stack add'.
func (e *Engine) StackApply(ctx context.Context, req *StackApplyRequest) (*StackApplyResult, error) {
	// Step 1: Discover repository
	repoRoot, err := e.gitRepo.Discover(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotInRepo, err)
	}

	repoFingerprint, err := e.gitRepo.Fingerprint(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to compute repo fingerprint: %w", err)
	}

	workspacePath, err := e.gitRepo.RelPath(repoRoot, req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to compute workspace path: %w", err)
	}

	// Step 2: Compute workspace ID
	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)

	// Step 3: Load workspace state
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: no workspace state (use 'stack add' first)", ErrValidation)
		}
		return nil, fmt.Errorf("failed to load workspace state: %w", err)
	}

	// Check if stack is empty
	if len(workspaceState.Stack) == 0 {
		return nil, fmt.Errorf("%w: stack is empty (use 'stack add' first)", ErrValidation)
	}

	// If workspace state exists, verify mode matches
	if workspaceState.Applied && workspaceState.Mode != req.Mode && !req.Force {
		return nil, fmt.Errorf("%w: existing mode is %s, requested mode is %s", ErrValidation, workspaceState.Mode, req.Mode)
	}

	// Step 5: Build apply plan using only stack stores (no active store)
	orderedStores := append([]string{}, workspaceState.Stack...)

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

	// Step 6: Apply overlays
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

	// Update workspace state metadata
	workspaceState.Applied = true
	workspaceState.Mode = req.Mode
	// Note: Stack and ActiveStore remain unchanged

	// Step 7: Persist workspace state atomically
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
// Paths applied by the active store are not affected.
func (e *Engine) StackUnapply(ctx context.Context, req *StackUnapplyRequest) (*StackUnapplyResult, error) {
	// Step 1: Discover repository
	repoRoot, err := e.gitRepo.Discover(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotInRepo, err)
	}

	repoFingerprint, err := e.gitRepo.Fingerprint(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to compute repo fingerprint: %w", err)
	}

	workspacePath, err := e.gitRepo.RelPath(repoRoot, req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to compute workspace path: %w", err)
	}

	// Step 2: Compute workspace ID
	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)

	// Step 3: Load workspace state
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: workspace has no managed paths", ErrStateMissing)
		}
		return nil, fmt.Errorf("failed to load workspace state: %w", err)
	}

	// Step 4: Filter to only paths owned by stores in workspaceState.Stack
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

	// Check if there are any stack paths to remove
	if len(stackPaths) == 0 {
		return &StackUnapplyResult{
			Removed:     []string{},
			WorkspaceID: workspaceID,
		}, nil
	}

	// If dry run, just return the list of paths that would be removed
	if req.DryRun {
		return &StackUnapplyResult{
			Removed:     stackPaths,
			WorkspaceID: workspaceID,
		}, nil
	}

	// Step 5: Remove stack paths in deepest-first order
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

		// Convert relative path to absolute for filesystem operations
		absPath := filepath.Join(req.CWD, relPath)

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

	// Step 6: Update workspace state (clear stack, keep active store paths)
	workspaceState.Stack = []string{}

	// Check if there are any remaining paths
	if len(workspaceState.Paths) == 0 {
		// No more managed paths - delete workspace state
		if err := e.stateStore.DeleteWorkspace(workspaceID); err != nil {
			return nil, fmt.Errorf("failed to delete workspace state: %w", err)
		}
	} else {
		// Active store still has paths - save workspace state
		if err := e.stateStore.SaveWorkspace(workspaceID, workspaceState); err != nil {
			return nil, fmt.Errorf("failed to save workspace state: %w", err)
		}
	}

	return &StackUnapplyResult{
		Removed:     removed,
		WorkspaceID: workspaceID,
	}, nil
}
