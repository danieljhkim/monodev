package engine

import (
	"context"
	"fmt"
	"os"
	"slices"

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

// StackList returns the current store stack for the repository.
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

	// Load repo state
	repoState, err := e.stateStore.LoadRepoState(repoFingerprint)
	if err != nil {
		if os.IsNotExist(err) {
			// No repo state yet, return empty stack
			return &StackListResult{
				Stack:       []string{},
				ActiveStore: "",
			}, nil
		}
		return nil, fmt.Errorf("failed to load repo state: %w", err)
	}

	return &StackListResult{
		Stack:       repoState.Stack,
		ActiveStore: repoState.ActiveStore,
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

	// Verify store exists
	exists, err := e.storeRepo.Exists(req.StoreID)
	if err != nil {
		return fmt.Errorf("failed to check if store exists: %w", err)
	}
	if !exists {
		return fmt.Errorf("%w: store %s does not exist", ErrNotFound, req.StoreID)
	}

	// Load or create repo state
	repoState, err := e.stateStore.LoadRepoState(repoFingerprint)
	if err != nil {
		if os.IsNotExist(err) {
			repoState = state.NewRepoState(repoFingerprint)
		} else {
			return fmt.Errorf("failed to load repo state: %w", err)
		}
	}

	// Check for duplicates
	if slices.Contains(repoState.Stack, req.StoreID) {
		return fmt.Errorf("%w: store %s is already in the stack", ErrValidation, req.StoreID)
	}

	// Add to stack
	repoState.Stack = append(repoState.Stack, req.StoreID)

	// Save repo state
	if err := e.stateStore.SaveRepoState(repoFingerprint, repoState); err != nil {
		return fmt.Errorf("failed to save repo state: %w", err)
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

	// Load repo state
	repoState, err := e.stateStore.LoadRepoState(repoFingerprint)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: stack is empty", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to load repo state: %w", err)
	}

	if len(repoState.Stack) == 0 {
		return nil, fmt.Errorf("%w: stack is empty", ErrNotFound)
	}

	var removed string

	if req.StoreID == "" {
		// LIFO: remove last store
		removed = repoState.Stack[len(repoState.Stack)-1]
		repoState.Stack = repoState.Stack[:len(repoState.Stack)-1]
	} else {
		// Remove specific store
		found := false
		newStack := make([]string, 0, len(repoState.Stack))
		for _, s := range repoState.Stack {
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
		repoState.Stack = newStack
	}

	// Save repo state
	if err := e.stateStore.SaveRepoState(repoFingerprint, repoState); err != nil {
		return nil, fmt.Errorf("failed to save repo state: %w", err)
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

	// Load repo state
	repoState, err := e.stateStore.LoadRepoState(repoFingerprint)
	if err != nil {
		if os.IsNotExist(err) {
			// No repo state yet, nothing to clear
			return nil
		}
		return fmt.Errorf("failed to load repo state: %w", err)
	}

	// Clear stack
	repoState.Stack = []string{}

	// Save repo state
	if err := e.stateStore.SaveRepoState(repoFingerprint, repoState); err != nil {
		return fmt.Errorf("failed to save repo state: %w", err)
	}

	return nil
}
