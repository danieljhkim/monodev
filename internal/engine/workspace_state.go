package engine

import (
	"fmt"
	"os"
	"strings"

	"github.com/danieljhkim/monodev/internal/state"
)

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
