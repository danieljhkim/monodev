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
	"strings"

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
	gitRepo             gitx.GitRepo
	storeResolver       *engineStoreResolver
	stateStore          state.StateStore
	componentStateStore state.StateStore
	fs                  fsops.FS
	hasher              hash.Hasher
	clock               clock.Clock
	configPaths         config.Paths
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
		gitRepo:       gitRepo,
		storeResolver: newEngineStoreResolver(storeRepo, storeRepo, nil),
		stateStore:    stateStore,
		fs:            fs,
		hasher:        hasher,
		clock:         clk,
		configPaths:   paths,
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
	globalStateStore := state.NewFileStateStore(fs, scopedPaths.Global.Workspaces)
	var componentStateStore state.StateStore
	if scopedPaths.Component != nil {
		componentStateStore = state.NewFileStateStore(fs, scopedPaths.Component.Workspaces)
	}

	return &Engine{
		gitRepo:             gitRepo,
		storeResolver:       newScopedEngineStoreResolver(fs, scopedPaths),
		stateStore:          globalStateStore,
		componentStateStore: componentStateStore,
		fs:                  fs,
		hasher:              hasher,
		clock:               clk,
		configPaths:         *scopedPaths.Global,
		scopedPaths:         scopedPaths,
	}
}

type workspaceStateSource struct {
	dir   string
	store state.StateStore
}

// workspaceStateSources returns workspace scan roots paired with the state store
// that owns each root. Global is first, so global state wins duplicate IDs.
func (e *Engine) workspaceStateSources() []workspaceStateSource {
	sources := []workspaceStateSource{}
	if e.stateStore != nil && e.configPaths.Workspaces != "" {
		sources = append(sources, workspaceStateSource{
			dir:   e.configPaths.Workspaces,
			store: e.stateStore,
		})
	}

	if e.scopedPaths != nil && e.scopedPaths.Component != nil {
		componentStore := e.componentStateStore
		if componentStore == nil && e.fs != nil {
			componentStore = state.NewFileStateStore(e.fs, e.scopedPaths.Component.Workspaces)
		}
		if componentStore != nil {
			sources = append(sources, workspaceStateSource{
				dir:   e.scopedPaths.Component.Workspaces,
				store: componentStore,
			})
		}
	}

	return sources
}

func (e *Engine) workspaceStateStores() []state.StateStore {
	stores := []state.StateStore{}
	if e.stateStore != nil {
		stores = append(stores, e.stateStore)
	}
	if e.scopedPaths != nil && e.scopedPaths.Component != nil {
		componentStore := e.componentStateStore
		if componentStore == nil && e.fs != nil {
			componentStore = state.NewFileStateStore(e.fs, e.scopedPaths.Component.Workspaces)
		}
		if componentStore != nil {
			stores = append(stores, componentStore)
		}
	}
	return stores
}

// workspacesDirs returns workspace directory paths for scanning (both scopes).
func (e *Engine) workspacesDirs() []string {
	var dirs []string
	for _, source := range e.workspaceStateSources() {
		dirs = append(dirs, source.dir)
	}
	return dirs
}

func (e *Engine) forEachWorkspaceState(fn func(workspaceID string, ws *state.WorkspaceState) error) error {
	seen := make(map[string]bool)

	for _, source := range e.workspaceStateSources() {
		entries, err := os.ReadDir(source.dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("failed to read workspaces directory: %w", err)
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}

			workspaceID := strings.TrimSuffix(entry.Name(), ".json")
			if seen[workspaceID] {
				continue
			}
			seen[workspaceID] = true

			ws, err := source.store.LoadWorkspace(workspaceID)
			if err != nil {
				continue
			}

			if err := fn(workspaceID, ws); err != nil {
				return err
			}
		}
	}

	return nil
}

func (e *Engine) loadWorkspaceFromScopes(workspaceID string) (*state.WorkspaceState, state.StateStore, error) {
	for _, store := range e.workspaceStateStores() {
		ws, err := store.LoadWorkspace(workspaceID)
		if err == nil {
			return ws, store, nil
		}
		if os.IsNotExist(err) {
			continue
		}
		return nil, nil, err
	}

	return nil, nil, os.ErrNotExist
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

func (e *Engine) LoadOrCreateWorkspaceState(root, repoFingerprint, workspacePath, mode string) (*state.WorkspaceState, string, error) {
	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			workspaceState = state.NewWorkspaceState(repoFingerprint, workspacePath, mode)
		} else {
			return nil, workspaceID, fmt.Errorf("failed to load workspace state: %w", err)
		}
	}
	workspaceState.AbsolutePath = filepath.Join(root, workspacePath)
	return workspaceState, workspaceID, nil
}
