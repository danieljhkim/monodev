package engine

import (
	"context"
	"fmt"
	"os"

	"github.com/danieljhkim/monodev/internal/lockfile"
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

	// Scope is the store location ("global", "component"). Empty uses the
	// resolver default (component when a repo-local .monodev exists, else global).
	Scope string

	// Description is an optional description
	Description string
}

// UpdateStoreRequest represents a request to update store metadata.
// Nil pointer fields mean "do not change".
type UpdateStoreRequest struct {
	// CWD is the current working directory
	CWD string

	// StoreID is the store to update
	StoreID string

	// Scope optionally specifies which scope to use (empty = auto-resolve)
	Scope string

	// Optional fields — nil means "do not change"
	Description *string
}

// StoreDetails contains detailed information about a store.
type StoreDetails struct {
	// Meta is the store metadata
	Meta *stores.StoreMeta

	// TrackedPaths is the list of tracked paths
	TrackedPaths []stores.TrackedPath
}

// ScopedStoreDetails contains detailed information about a store in a specific scope.
type ScopedStoreDetails struct {
	// Scope is where the store is located ("global" or "component").
	// Omitted from JSON: location is an internal routing concern, not store metadata.
	Scope string `json:"-"`

	// Meta is the store metadata
	Meta *stores.StoreMeta

	// TrackedPaths is the list of tracked paths
	TrackedPaths []stores.TrackedPath
}

// touchStoreMetaIn updates the UpdatedAt timestamp of a store's metadata using a specific repo.
func (e *Engine) touchStoreMetaIn(repo stores.StoreRepo, storeID string) error {
	meta, err := repo.LoadMeta(storeID)
	if err != nil {
		return fmt.Errorf("failed to load store metadata: %w", err)
	}

	meta.UpdatedAt = e.clock.Now()
	if err := repo.SaveMeta(storeID, meta); err != nil {
		return fmt.Errorf("failed to save store metadata: %w", err)
	}

	return nil
}

// UseStore selects a store as the active store for the current repository.
// If there's existing workspace state for a different store, it will be cleared
// to avoid inconsistent state where applied=true but for the wrong store.
func (e *Engine) UseStore(ctx context.Context, req *UseStoreRequest) error {
	root, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return fmt.Errorf("failed to discover workspace: %w", err)
	}

	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)
	unlockWorkspace, err := e.lockWorkspace(ctx, workspaceID, lockfile.Exclusive)
	if err != nil {
		return err
	}
	defer unlockWorkspace()

	// Verify store exists and resolve scope
	repo, resolvedScope, err := e.storeResolver.resolveStoreRepo(req.StoreID, req.Scope)
	if err != nil {
		return err
	}
	unlockStore, err := e.lockStores(ctx, storeLockRequest{repo: repo, id: req.StoreID, mode: lockfile.Shared})
	if err != nil {
		return err
	}
	defer unlockStore()

	workspaceState, workspaceID, err := e.LoadOrCreateWorkspaceState(root, repoFingerprint, workspacePath, "copy")
	if err != nil {
		return fmt.Errorf("failed to load workspace state: %w", err)
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
	root, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return fmt.Errorf("failed to discover workspace: %w", err)
	}

	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)
	unlockWorkspace, err := e.lockWorkspace(ctx, workspaceID, lockfile.Exclusive)
	if err != nil {
		return err
	}
	defer unlockWorkspace()

	// Determine effective scope
	scope := req.Scope
	if scope == "" {
		scope = e.storeResolver.defaultScope()
	}

	// Route to the correct StoreRepo by scope
	repo, err := e.storeResolver.repoForScope(scope)
	if err != nil {
		return fmt.Errorf("failed to resolve scope %q: %w", scope, err)
	}
	unlockStore, err := e.lockStores(ctx, storeLockRequest{repo: repo, id: req.StoreID, mode: lockfile.Exclusive})
	if err != nil {
		return err
	}
	defer unlockStore()

	// Create store metadata
	meta := stores.NewStoreMeta(req.Name, e.clock.Now())
	meta.Description = req.Description

	// Validate metadata
	if err := meta.Validate(); err != nil {
		return fmt.Errorf("invalid store metadata: %w", err)
	}

	// Create the store
	if err := repo.Create(req.StoreID, meta); err != nil {
		return fmt.Errorf("failed to create store: %w", err)
	}

	workspaceState, workspaceID, err := e.LoadOrCreateWorkspaceState(root, repoFingerprint, workspacePath, "copy")
	if err != nil {
		return fmt.Errorf("failed to load workspace state: %w", err)
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
	return e.storeResolver.listStores()
}

// DescribeStore returns detailed information about a store.
// If the store exists in both scopes, returns details for both.
func (e *Engine) DescribeStore(ctx context.Context, storeID string) ([]ScopedStoreDetails, error) {
	locations, err := e.storeResolver.findStore(storeID)
	if err != nil {
		return nil, err
	}
	if len(locations) == 0 {
		return nil, fmt.Errorf("%w: store '%s' not found", ErrNotFound, storeID)
	}
	lockRequests := make([]storeLockRequest, 0, len(locations))
	for _, loc := range locations {
		lockRequests = append(lockRequests, storeLockRequest{repo: loc.Repo, id: storeID, mode: lockfile.Shared})
	}
	unlockStores, err := e.lockStores(ctx, lockRequests...)
	if err != nil {
		return nil, err
	}
	defer unlockStores()

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
			TrackedPaths: track.Tracked,
		})
	}

	return results, nil
}

// GetActiveStoreID returns the active store ID and scope for the given working directory.
// Returns ErrNoActiveStore if no store is currently active.
func (e *Engine) GetActiveStoreID(ctx context.Context, cwd string) (storeID, scope string, err error) {
	_, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(cwd)
	if err != nil {
		return "", "", fmt.Errorf("failed to discover workspace: %w", err)
	}

	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)
	unlockWorkspace, err := e.lockWorkspace(ctx, workspaceID, lockfile.Shared)
	if err != nil {
		return "", "", err
	}
	defer unlockWorkspace()
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", ErrNoActiveStore
		}
		return "", "", fmt.Errorf("failed to load workspace state: %w", err)
	}

	if workspaceState.ActiveStore == "" {
		return "", "", ErrNoActiveStore
	}

	return workspaceState.ActiveStore, workspaceState.ActiveStoreScope, nil
}

// UpdateStore updates metadata fields on an existing store.
func (e *Engine) UpdateStore(ctx context.Context, req *UpdateStoreRequest) error {
	// Resolve the store repo
	repo, _, err := e.storeResolver.resolveStoreRepo(req.StoreID, req.Scope)
	if err != nil {
		return err
	}
	unlockStore, err := e.lockStores(ctx, storeLockRequest{repo: repo, id: req.StoreID, mode: lockfile.Exclusive})
	if err != nil {
		return err
	}
	defer unlockStore()

	// Load current metadata
	meta, err := repo.LoadMeta(req.StoreID)
	if err != nil {
		return fmt.Errorf("failed to load store metadata: %w", err)
	}

	// Apply non-nil fields
	if req.Description != nil {
		meta.Description = *req.Description
	}

	// Validate
	if err := meta.Validate(); err != nil {
		return fmt.Errorf("invalid store metadata: %w", err)
	}

	// Update timestamp
	meta.UpdatedAt = e.clock.Now()

	// Save
	if err := repo.SaveMeta(req.StoreID, meta); err != nil {
		return fmt.Errorf("failed to save store metadata: %w", err)
	}

	return nil
}
