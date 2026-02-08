// Package engine provides the core business logic for monodev operations.
//
// The engine package acts as the orchestration layer between CLI commands and
// lower-level operations. It coordinates workspace discovery, state management,
// store operations, and overlay application/removal.
//
// Key components:
//   - Engine: Main orchestrator that coordinates all operations
//   - Apply/Unapply: Manages overlay application and removal
//   - Track/Commit: Handles tracking and persisting changes
//   - State management: Workspace and store state operations
package engine

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/clock"
	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/gitx"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/planner"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// Engine orchestrates all monodev operations.
// It is the main API surface called by the CLI.
type Engine struct {
	gitRepo     gitx.GitRepo
	storeRepo   stores.StoreRepo
	stateStore  state.StateStore
	fs          fsops.FS
	hasher      hash.Hasher
	clock       clock.Clock
	configPaths config.Paths

	// Dual-scope fields
	globalStoreRepo     stores.StoreRepo
	componentStoreRepo  stores.StoreRepo
	globalStateStore    state.StateStore
	componentStateStore state.StateStore
	scopedPaths         *config.ScopedPaths
}

// New creates a new Engine with the given dependencies.
func New(
	gitRepo gitx.GitRepo,
	storeRepo stores.StoreRepo,
	stateStore state.StateStore,
	fs fsops.FS,
	hasher hash.Hasher,
	clk clock.Clock,
	paths config.Paths,
) *Engine {
	return &Engine{
		gitRepo:          gitRepo,
		storeRepo:        storeRepo,
		stateStore:       stateStore,
		fs:               fs,
		hasher:           hasher,
		clock:            clk,
		configPaths:      paths,
		globalStoreRepo:  storeRepo,
		globalStateStore: stateStore,
	}
}

// NewScoped creates a new Engine with dual-scope StoreRepo instances.
// Global stores live at ~/.monodev/stores/, component stores at repo_root/.monodev/stores/.
func NewScoped(
	gitRepo gitx.GitRepo,
	scopedPaths *config.ScopedPaths,
	fs fsops.FS,
	hasher hash.Hasher,
	clk clock.Clock,
) *Engine {
	globalStoreRepo := stores.NewFileStoreRepo(fs, scopedPaths.Global.Stores)
	globalStateStore := state.NewFileStateStore(fs, scopedPaths.Global.Workspaces)

	e := &Engine{
		gitRepo:          gitRepo,
		fs:               fs,
		hasher:           hasher,
		clock:            clk,
		configPaths:      *scopedPaths.Global,
		globalStoreRepo:  globalStoreRepo,
		globalStateStore: globalStateStore,
		scopedPaths:      scopedPaths,
		// Legacy fields default to global
		storeRepo:  globalStoreRepo,
		stateStore: globalStateStore,
	}

	if scopedPaths.Component != nil {
		componentStoreRepo := stores.NewFileStoreRepo(fs, scopedPaths.Component.Stores)
		componentStateStore := state.NewFileStateStore(fs, scopedPaths.Component.Workspaces)
		e.componentStoreRepo = componentStoreRepo
		e.componentStateStore = componentStateStore
	}

	return e
}

// storeRepoForScope returns the StoreRepo for the given scope.
func (e *Engine) storeRepoForScope(scope string) (stores.StoreRepo, error) {
	switch scope {
	case stores.ScopeGlobal:
		if e.globalStoreRepo != nil {
			return e.globalStoreRepo, nil
		}
		return e.storeRepo, nil
	case stores.ScopeComponent:
		if e.componentStoreRepo != nil {
			return e.componentStoreRepo, nil
		}
		return nil, fmt.Errorf("no component scope available (not in a repo with .monodev)")
	default:
		return nil, fmt.Errorf("unknown scope: %s", scope)
	}
}

// findStore searches both scopes for a store with the given ID.
// Returns the locations where the store was found.
func (e *Engine) findStore(storeID string) ([]stores.StoreLocation, error) {
	var locations []stores.StoreLocation

	// Check global scope
	if e.globalStoreRepo != nil {
		exists, err := e.globalStoreRepo.Exists(storeID)
		if err != nil {
			return nil, fmt.Errorf("failed to check global store: %w", err)
		}
		if exists {
			locations = append(locations, stores.StoreLocation{
				Scope: stores.ScopeGlobal,
				Repo:  e.globalStoreRepo,
			})
		}
	}

	// Check component scope
	if e.componentStoreRepo != nil {
		exists, err := e.componentStoreRepo.Exists(storeID)
		if err != nil {
			return nil, fmt.Errorf("failed to check component store: %w", err)
		}
		if exists {
			locations = append(locations, stores.StoreLocation{
				Scope: stores.ScopeComponent,
				Repo:  e.componentStoreRepo,
			})
		}
	}

	return locations, nil
}

// defaultScope returns "component" if repo context exists, else "global".
func (e *Engine) defaultScope() string {
	if e.componentStoreRepo != nil {
		return stores.ScopeComponent
	}
	return stores.ScopeGlobal
}

// activeStoreRepo resolves the StoreRepo for the workspace's active store.
// It uses ActiveStoreScope if set, otherwise searches both scopes.
func (e *Engine) activeStoreRepo(ws *state.WorkspaceState) (stores.StoreRepo, error) {
	if ws.ActiveStore == "" {
		return nil, ErrNoActiveStore
	}

	// If scope is explicitly set, use it
	if ws.ActiveStoreScope != "" {
		return e.storeRepoForScope(ws.ActiveStoreScope)
	}

	// Legacy: search both scopes
	locations, err := e.findStore(ws.ActiveStore)
	if err != nil {
		return nil, err
	}
	if len(locations) == 0 {
		// Fallback to legacy storeRepo
		return e.storeRepo, nil
	}
	// Prefer component scope for legacy states
	for _, loc := range locations {
		if loc.Scope == stores.ScopeComponent {
			return loc.Repo, nil
		}
	}
	return locations[0].Repo, nil
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

// resolveStoreRepo resolves the StoreRepo for a given storeID and optional scope hint.
// If scope is provided, uses that scope directly. Otherwise searches both scopes.
// If found in exactly one scope, uses that. If found in both, returns error.
func (e *Engine) resolveStoreRepo(storeID, scope string) (stores.StoreRepo, string, error) {
	if scope != "" {
		repo, err := e.storeRepoForScope(scope)
		if err != nil {
			return nil, "", err
		}
		return repo, scope, nil
	}

	locations, err := e.findStore(storeID)
	if err != nil {
		return nil, "", err
	}

	switch len(locations) {
	case 0:
		return nil, "", fmt.Errorf("%w: store '%s' not found", ErrNotFound, storeID)
	case 1:
		return locations[0].Repo, locations[0].Scope, nil
	default:
		return nil, "", fmt.Errorf("store '%s' exists in both global and component scopes; specify --scope to disambiguate", storeID)
	}
}

// workspacesDirs returns workspace directory paths for scanning (both scopes).
func (e *Engine) workspacesDirs() []string {
	dirs := []string{e.configPaths.Workspaces}
	if e.scopedPaths != nil && e.scopedPaths.Component != nil {
		dirs = append(dirs, e.scopedPaths.Component.Workspaces)
	}
	return dirs
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
	exists, err := e.fs.Exists(op.DestPath)
	if err != nil {
		return fmt.Errorf("failed to check if path exists: %w", err)
	}
	if !exists {
		return nil
	}
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
	if err := e.fs.Symlink(op.SourcePath, op.DestPath); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	return nil
}

// executeCopy copies a file or directory.
func (e *Engine) executeCopy(op planner.Operation) error {
	if err := e.fs.Copy(op.SourcePath, op.DestPath); err != nil {
		return fmt.Errorf("failed to copy: %w", err)
	}

	return nil
}

// discoverWorkspace returns repo root, fingerprint, and workspace path
func (e *Engine) DiscoverWorkspace(cwd string) (root, fingerprint, workspacePath string, err error) {
	root, err = e.gitRepo.Discover(cwd)
	if err != nil {
		// Fallback to using absolute path for non-git repositories
		// This is a valid fallback behavior, not an error condition
		root = cwd
	}

	fingerprint, err = e.gitRepo.Fingerprint(root)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get workspace fingerprint: %w", err)
	}

	workspacePath, err = e.gitRepo.RelPath(root, cwd)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to compute workspace path: %w", err)
	}

	return root, fingerprint, workspacePath, nil
}

func (e *Engine) LoadOrCreateWorkspaceState(repoFingerprint, workspacePath, mode string) (*state.WorkspaceState, string, error) {
	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			workspaceState = state.NewWorkspaceState(repoFingerprint, workspacePath, mode)
		} else {
			return nil, workspaceID, fmt.Errorf("failed to load workspace state: %w", err)
		}
	}
	return workspaceState, workspaceID, nil
}
