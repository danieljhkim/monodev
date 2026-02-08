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

	// Source indicates how the store was created (human, agent, other)
	Source string

	// Type categorizes the store (issue, plan, feature, task, other)
	Type string

	// Owner identifies who owns the store
	Owner string

	// TaskID links the store to an external task
	TaskID string

	// ParentTaskID links the store to a parent task
	ParentTaskID string

	// Priority indicates the store's priority (low, medium, high, none)
	Priority string

	// Status indicates the store's workflow status
	Status string
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
	Description  *string
	Source       *string
	Type         *string
	Owner        *string
	TaskID       *string
	ParentTaskID *string
	Priority     *string
	Status       *string
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
	// Scope is where the store is located ("global" or "component")
	Scope string

	// Meta is the store metadata
	Meta *stores.StoreMeta

	// TrackedPaths is the list of tracked paths
	TrackedPaths []stores.TrackedPath
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
	meta.Source = req.Source
	meta.Type = req.Type
	meta.Owner = req.Owner
	if meta.Owner == "" {
		meta.Owner = e.gitRepo.Username(req.CWD)
	}
	meta.TaskID = req.TaskID
	meta.ParentTaskID = req.ParentTaskID
	meta.Priority = req.Priority
	meta.Status = req.Status

	// Validate metadata
	if err := meta.Validate(); err != nil {
		return fmt.Errorf("invalid store metadata: %w", err)
	}

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
	repo, _, err := e.resolveStoreRepo(req.StoreID, req.Scope)
	if err != nil {
		return err
	}

	// Load current metadata
	meta, err := repo.LoadMeta(req.StoreID)
	if err != nil {
		return fmt.Errorf("failed to load store metadata: %w", err)
	}

	// Apply non-nil fields
	if req.Description != nil {
		meta.Description = *req.Description
	}
	if req.Source != nil {
		meta.Source = *req.Source
	}
	if req.Type != nil {
		meta.Type = *req.Type
	}
	if req.Owner != nil {
		meta.Owner = *req.Owner
	}
	if req.TaskID != nil {
		meta.TaskID = *req.TaskID
	}
	if req.ParentTaskID != nil {
		meta.ParentTaskID = *req.ParentTaskID
	}
	if req.Priority != nil {
		meta.Priority = *req.Priority
	}
	if req.Status != nil {
		meta.Status = *req.Status
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
