package engine

import (
	"context"
	"fmt"
	"os"

	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// UseStoreRequest represents a request to select a store as active.
type UseStoreRequest struct {
	// CWD is the current working directory
	CWD string

	// StoreID is the store to select
	StoreID string
}

// CreateStoreRequest represents a request to create a new store.
type CreateStoreRequest struct {
	// CWD is the current working directory (needed to set as active store)
	CWD string

	// StoreID is the ID of the new store
	StoreID string

	// Name is the human-readable name
	Name string

	// Scope is the store scope ("global", "profile", "component")
	Scope string

	// Description is an optional description
	Description string
}

// StoreDetails contains detailed information about a store.
type StoreDetails struct {
	// Meta is the store metadata
	Meta *stores.StoreMeta

	// TrackedPaths is the list of tracked paths
	TrackedPaths []string
}

// UseStore selects a store as the active store for the current repository.
// If there's existing workspace state for a different store, it will be cleared
// to avoid inconsistent state where applied=true but for the wrong store.
func (e *Engine) UseStore(ctx context.Context, req *UseStoreRequest) error {
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

	// Load or create repo state
	repoState, err := e.stateStore.LoadRepoState(repoFingerprint)
	if err != nil {
		if os.IsNotExist(err) {
			repoState = state.NewRepoState(repoFingerprint)
		} else {
			return fmt.Errorf("failed to load repo state: %w", err)
		}
	}

	// If switching to a different store, mark workspace as not applied
	// This prevents showing applied=true when the active store has changed
	// User can still unapply to clean up old store's files
	if repoState.ActiveStore != "" && repoState.ActiveStore != req.StoreID {
		// Check if workspace state exists
		workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
		if err == nil && workspaceState.Applied {
			// Mark as not applied since we're switching stores
			workspaceState.Applied = false
			if err := e.stateStore.SaveWorkspace(workspaceID, workspaceState); err != nil {
				return fmt.Errorf("failed to update workspace state: %w", err)
			}
		}
	}

	// Update active store
	repoState.ActiveStore = req.StoreID

	// Save repo state
	if err := e.stateStore.SaveRepoState(repoFingerprint, repoState); err != nil {
		return fmt.Errorf("failed to save repo state: %w", err)
	}

	return nil
}

// CreateStore creates a new store and sets it as the active store for the current repository.
// If there's existing workspace state for a different store, it will be cleared.
func (e *Engine) CreateStore(ctx context.Context, req *CreateStoreRequest) error {
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

	// Create store metadata
	meta := stores.NewStoreMeta(req.Name, req.Scope, e.clock.Now())
	meta.Description = req.Description

	// Create the store
	if err := e.storeRepo.Create(req.StoreID, meta); err != nil {
		return fmt.Errorf("failed to create store: %w", err)
	}

	// Set as active store for this repository
	repoState, err := e.stateStore.LoadRepoState(repoFingerprint)
	if err != nil {
		if os.IsNotExist(err) {
			repoState = state.NewRepoState(repoFingerprint)
		} else {
			return fmt.Errorf("failed to load repo state: %w", err)
		}
	}

	// If switching from a different store, mark workspace as not applied
	if repoState.ActiveStore != "" && repoState.ActiveStore != req.StoreID {
		// Check if workspace state exists
		workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
		if err == nil && workspaceState.Applied {
			// Mark as not applied since we're creating/switching to new store
			workspaceState.Applied = false
			if err := e.stateStore.SaveWorkspace(workspaceID, workspaceState); err != nil {
				return fmt.Errorf("failed to update workspace state: %w", err)
			}
		}
	}

	repoState.ActiveStore = req.StoreID

	// Save repo state
	if err := e.stateStore.SaveRepoState(repoFingerprint, repoState); err != nil {
		return fmt.Errorf("failed to save repo state: %w", err)
	}

	return nil
}

// ListStores returns all available stores.
func (e *Engine) ListStores(ctx context.Context) ([]stores.StoreMeta, error) {
	// Get list of store IDs
	storeIDs, err := e.storeRepo.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list stores: %w", err)
	}

	// Load metadata for each store
	var storeList []stores.StoreMeta
	for _, id := range storeIDs {
		meta, err := e.storeRepo.LoadMeta(id)
		if err != nil {
			// Skip stores with missing/corrupt metadata
			continue
		}
		storeList = append(storeList, *meta)
	}

	return storeList, nil
}

// DescribeStore returns detailed information about a store.
func (e *Engine) DescribeStore(ctx context.Context, storeID string) (*StoreDetails, error) {
	// Load metadata
	meta, err := e.storeRepo.LoadMeta(storeID)
	if err != nil {
		return nil, fmt.Errorf("failed to load store metadata: %w", err)
	}

	// Load track file
	track, err := e.storeRepo.LoadTrack(storeID)
	if err != nil {
		return nil, fmt.Errorf("failed to load track file: %w", err)
	}

	return &StoreDetails{
		Meta:         meta,
		TrackedPaths: track.Paths(),
	}, nil
}
