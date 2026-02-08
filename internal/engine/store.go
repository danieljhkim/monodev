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

	// Scope optionally specifies which scope to use (empty = auto-resolve)
	Scope string
}

type UnUseStoreRequest struct {
	// CWD is the current working directory
	CWD string
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

// ScopedStoreDetails contains detailed information about a store in a specific scope.
type ScopedStoreDetails struct {
	// Scope is where the store is located ("global" or "component")
	Scope string

	// Meta is the store metadata
	Meta *stores.StoreMeta

	// TrackedPaths is the list of tracked paths
	TrackedPaths []string
}

// UseStore selects a store as the active store for the current repository.
// If there's existing workspace state for a different store, it will be cleared
// to avoid inconsistent state where applied=true but for the wrong store.
func (e *Engine) UseStore(ctx context.Context, req *UseStoreRequest) error {
	_, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return fmt.Errorf("failed to discover workspace: %w", err)
	}

	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)

	// Verify store exists and resolve scope
	_, resolvedScope, err := e.resolveStoreRepo(req.StoreID, req.Scope)
	if err != nil {
		return err
	}

	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			// Create new workspace state
			workspaceState = state.NewWorkspaceState(repoFingerprint, workspacePath, "copy")
		} else {
			return fmt.Errorf("failed to load workspace state: %w", err)
		}
	}
	if workspaceState.ActiveStore == req.StoreID && workspaceState.ActiveStoreScope == resolvedScope {
		return nil // already active store
	}

	appliedStore := workspaceState.GetAppliedStore(req.StoreID)
	if appliedStore != nil {
		workspaceState.Applied = true
		workspaceState.Mode = appliedStore.Type
	} else {
		workspaceState.Applied = false
	}
	workspaceState.ActiveStore = req.StoreID
	workspaceState.ActiveStoreScope = resolvedScope
	if err := e.stateStore.SaveWorkspace(workspaceID, workspaceState); err != nil {
		return fmt.Errorf("failed to save workspace state: %w", err)
	}

	return nil
}

// CreateStore creates a new store and sets it as the active store for the current repository.
// If there's existing workspace state for a different store, it will be cleared.
func (e *Engine) CreateStore(ctx context.Context, req *CreateStoreRequest) error {
	_, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return fmt.Errorf("failed to discover workspace: %w", err)
	}

	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)

	// Determine effective scope
	scope := req.Scope
	if scope == "" {
		scope = e.defaultScope()
	}

	// Route to the correct StoreRepo by scope
	repo, err := e.storeRepoForScope(scope)
	if err != nil {
		return fmt.Errorf("failed to resolve scope %q: %w", scope, err)
	}

	// Create store metadata
	meta := stores.NewStoreMeta(req.Name, scope, e.clock.Now())
	meta.Description = req.Description

	// Create the store
	if err := repo.Create(req.StoreID, meta); err != nil {
		return fmt.Errorf("failed to create store: %w", err)
	}

	// Load or create workspace state
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			// Create new workspace state
			workspaceState = state.NewWorkspaceState(repoFingerprint, workspacePath, "copy")
		} else {
			return fmt.Errorf("failed to load workspace state: %w", err)
		}
	}

	workspaceState.Applied = false
	workspaceState.ActiveStore = req.StoreID
	workspaceState.ActiveStoreScope = scope

	// Save workspace state
	if err := e.stateStore.SaveWorkspace(workspaceID, workspaceState); err != nil {
		return fmt.Errorf("failed to save workspace state: %w", err)
	}

	return nil
}

// ListStores returns all available stores from both scopes.
// Global stores are listed first, then component stores.
func (e *Engine) ListStores(ctx context.Context) ([]stores.ScopedStore, error) {
	var storeList []stores.ScopedStore

	// List global stores first
	if e.globalStoreRepo != nil {
		ids, err := e.globalStoreRepo.List()
		if err != nil {
			return nil, fmt.Errorf("failed to list global stores: %w", err)
		}
		for _, id := range ids {
			meta, err := e.globalStoreRepo.LoadMeta(id)
			if err != nil {
				continue
			}
			storeList = append(storeList, stores.ScopedStore{
				ID:    id,
				Meta:  meta,
				Scope: stores.ScopeGlobal,
			})
		}
	}

	// List component stores
	if e.componentStoreRepo != nil {
		ids, err := e.componentStoreRepo.List()
		if err != nil {
			return nil, fmt.Errorf("failed to list component stores: %w", err)
		}
		for _, id := range ids {
			meta, err := e.componentStoreRepo.LoadMeta(id)
			if err != nil {
				continue
			}
			storeList = append(storeList, stores.ScopedStore{
				ID:    id,
				Meta:  meta,
				Scope: stores.ScopeComponent,
			})
		}
	}

	return storeList, nil
}

// DescribeStore returns detailed information about a store.
// If the store exists in both scopes, returns details for both.
func (e *Engine) DescribeStore(ctx context.Context, storeID string) ([]ScopedStoreDetails, error) {
	locations, err := e.findStore(storeID)
	if err != nil {
		return nil, err
	}
	if len(locations) == 0 {
		return nil, fmt.Errorf("%w: store '%s' not found", ErrNotFound, storeID)
	}

	var results []ScopedStoreDetails
	for _, loc := range locations {
		meta, err := loc.Repo.LoadMeta(storeID)
		if err != nil {
			return nil, fmt.Errorf("failed to load store metadata (%s): %w", loc.Scope, err)
		}
		track, err := loc.Repo.LoadTrack(storeID)
		if err != nil {
			return nil, fmt.Errorf("failed to load track file (%s): %w", loc.Scope, err)
		}
		results = append(results, ScopedStoreDetails{
			Scope:        loc.Scope,
			Meta:         meta,
			TrackedPaths: track.Paths(),
		})
	}

	return results, nil
}
