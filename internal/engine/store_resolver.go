package engine

import (
	"fmt"

	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// engineStoreResolver owns store repository lifecycle and scope-aware lookup.
type engineStoreResolver struct {
	fallback  stores.StoreRepo
	global    stores.StoreRepo
	component stores.StoreRepo
}

func newEngineStoreResolver(fallback, global, component stores.StoreRepo) *engineStoreResolver {
	if global == nil {
		global = fallback
	}
	if fallback == nil {
		fallback = global
	}
	return &engineStoreResolver{
		fallback:  fallback,
		global:    global,
		component: component,
	}
}

func newScopedEngineStoreResolver(fs fsops.FS, scopedPaths *config.ScopedPaths) *engineStoreResolver {
	global := stores.NewFileStoreRepo(fs, scopedPaths.Global.Stores)
	var component stores.StoreRepo
	if scopedPaths.Component != nil {
		component = stores.NewFileStoreRepo(fs, scopedPaths.Component.Stores)
	}
	return newEngineStoreResolver(global, global, component)
}

func (r *engineStoreResolver) repoForScope(scope string) (stores.StoreRepo, error) {
	switch scope {
	case stores.ScopeGlobal:
		if r.global != nil {
			return r.global, nil
		}
		return r.fallback, nil
	case stores.ScopeComponent:
		if r.component != nil {
			return r.component, nil
		}
		return nil, fmt.Errorf("no component scope available (not in a repo with .monodev)")
	default:
		return nil, fmt.Errorf("unknown scope: %s", scope)
	}
}

func (r *engineStoreResolver) findStore(storeID string) ([]stores.StoreLocation, error) {
	var locations []stores.StoreLocation

	if r.global != nil {
		exists, err := r.global.Exists(storeID)
		if err != nil {
			return nil, fmt.Errorf("failed to check global store: %w", err)
		}
		if exists {
			locations = append(locations, stores.StoreLocation{
				Scope: stores.ScopeGlobal,
				Repo:  r.global,
			})
		}
	}

	if r.component != nil {
		exists, err := r.component.Exists(storeID)
		if err != nil {
			return nil, fmt.Errorf("failed to check component store: %w", err)
		}
		if exists {
			locations = append(locations, stores.StoreLocation{
				Scope: stores.ScopeComponent,
				Repo:  r.component,
			})
		}
	}

	return locations, nil
}

func (r *engineStoreResolver) defaultScope() string {
	if r.component != nil {
		return stores.ScopeComponent
	}
	return stores.ScopeGlobal
}

func (r *engineStoreResolver) resolveActiveStoreRepo(ws *state.WorkspaceState) (stores.StoreRepo, string, error) {
	if ws.ActiveStore == "" {
		return nil, "", ErrNoActiveStore
	}

	if ws.ActiveStoreScope != "" {
		if ws.ActiveStoreScope == stores.ScopeComponent && r.component == nil {
			if r.global != nil {
				return r.global, stores.ScopeGlobal, nil
			}
			return r.fallback, stores.ScopeGlobal, nil
		}

		repo, err := r.repoForScope(ws.ActiveStoreScope)
		if err != nil {
			return nil, "", err
		}
		return repo, ws.ActiveStoreScope, nil
	}

	locations, err := r.findStore(ws.ActiveStore)
	if err != nil {
		return nil, "", err
	}
	if len(locations) == 0 {
		return r.fallback, stores.ScopeGlobal, nil
	}
	for _, loc := range locations {
		if loc.Scope == stores.ScopeComponent {
			return loc.Repo, loc.Scope, nil
		}
	}
	return locations[0].Repo, locations[0].Scope, nil
}

func (r *engineStoreResolver) activeStoreRepo(ws *state.WorkspaceState) (stores.StoreRepo, error) {
	repo, _, err := r.resolveActiveStoreRepo(ws)
	return repo, err
}

func (r *engineStoreResolver) resolveStoreRepo(storeID, scope string) (stores.StoreRepo, string, error) {
	if scope != "" {
		repo, err := r.repoForScope(scope)
		if err != nil {
			return nil, "", err
		}
		return repo, scope, nil
	}

	locations, err := r.findStore(storeID)
	if err != nil {
		return nil, "", err
	}

	switch len(locations) {
	case 0:
		return nil, "", fmt.Errorf("%w: store '%s' not found", ErrNotFound, storeID)
	case 1:
		return locations[0].Repo, locations[0].Scope, nil
	default:
		// Prefer component when the same ID exists in both locations. This
		// matches defaultScope() and multiRepoForStores now that --scope is
		// no longer a CLI flag.
		for _, loc := range locations {
			if loc.Scope == stores.ScopeComponent {
				return loc.Repo, loc.Scope, nil
			}
		}
		return locations[0].Repo, locations[0].Scope, nil
	}
}

func (r *engineStoreResolver) listStores() ([]stores.ScopedStore, error) {
	var storeList []stores.ScopedStore

	if r.global != nil {
		ids, err := r.global.List()
		if err != nil {
			return nil, fmt.Errorf("failed to list global stores: %w", err)
		}
		for _, id := range ids {
			meta, err := r.global.LoadMeta(id)
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

	if r.component != nil {
		ids, err := r.component.List()
		if err != nil {
			return nil, fmt.Errorf("failed to list component stores: %w", err)
		}
		for _, id := range ids {
			meta, err := r.component.LoadMeta(id)
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

func (r *engineStoreResolver) multiRepoForStores(storeIDs []string) (*stores.MultiStoreRepo, error) {
	storeMapping := make(map[string]stores.StoreRepo)
	for _, storeID := range storeIDs {
		locations, err := r.findStore(storeID)
		if err != nil {
			return nil, fmt.Errorf("failed to find store %s: %w", storeID, err)
		}
		for _, loc := range locations {
			if loc.Scope == stores.ScopeComponent {
				storeMapping[storeID] = loc.Repo
				break
			}
		}
		if _, ok := storeMapping[storeID]; !ok && len(locations) > 0 {
			storeMapping[storeID] = locations[0].Repo
		}
	}
	return stores.NewMultiStoreRepo(storeMapping, r.fallback), nil
}
