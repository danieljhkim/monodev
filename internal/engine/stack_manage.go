package engine

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/danieljhkim/monodev/internal/state"
)

// StackList returns the current store stack for the workspace.
func (e *Engine) StackList(ctx context.Context, req *StackListRequest) (*StackListResult, error) {
	_, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to discover workspace: %w", err)
	}

	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)
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
	root, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return fmt.Errorf("failed to discover workspace: %w", err)
	}
	workspaceState, workspaceID, err := e.LoadOrCreateWorkspaceState(root, repoFingerprint, workspacePath, "copy")
	if err != nil {
		return fmt.Errorf("failed to load or create workspace state: %w", err)
	}

	// Verify store exists in either scope
	locations, err := e.findStore(req.StoreID)
	if err != nil {
		return fmt.Errorf("failed to check if store exists: %w", err)
	}
	if len(locations) == 0 {
		return fmt.Errorf("%w: store %s does not exist", ErrNotFound, req.StoreID)
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
	root, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to discover workspace: %w", err)
	}

	workspaceState, workspaceID, err := e.LoadOrCreateWorkspaceState(root, repoFingerprint, workspacePath, "copy")
	if err != nil {
		return nil, fmt.Errorf("failed to load or create workspace state: %w", err)
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

	if err := e.stateStore.SaveWorkspace(workspaceID, workspaceState); err != nil {
		return nil, fmt.Errorf("failed to save workspace state: %w", err)
	}

	return &StackPopResult{
		Removed: removed,
	}, nil
}

// StackClear removes all stores from the stack.
func (e *Engine) StackClear(ctx context.Context, req *StackClearRequest) error {
	root, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return fmt.Errorf("failed to discover workspace: %w", err)
	}

	workspaceState, workspaceID, err := e.LoadOrCreateWorkspaceState(root, repoFingerprint, workspacePath, "copy")
	if err != nil {
		return fmt.Errorf("failed to load or create workspace state: %w", err)
	}

	// Clear stack
	workspaceState.Stack = []string{}

	if err := e.stateStore.SaveWorkspace(workspaceID, workspaceState); err != nil {
		return fmt.Errorf("failed to save workspace state: %w", err)
	}

	return nil
}
