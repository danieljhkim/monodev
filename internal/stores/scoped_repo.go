package stores

import (
	"context"
	"fmt"

	"github.com/danieljhkim/monodev/internal/lockfile"
)

// scopedRepo looks up stores in both global and component scopes.
// Existing stores are routed to the scope that already holds them (component
// wins when both do). A lookup error is not absence: it is returned, and no
// other scope is read, mutated, or locked in its place. New stores are
// created in the component scope when it is present, matching engine
// default-scope behavior after repo-local `.monodev` exists (auto-created on
// first use, or via `monodev init`).
type scopedRepo struct {
	global    StoreRepo
	component StoreRepo
}

// NewScopedRepo returns a StoreRepo that searches component then global.
// If component is nil, global is returned unchanged.
func NewScopedRepo(global, component StoreRepo) StoreRepo {
	if component == nil {
		return global
	}
	return &scopedRepo{global: global, component: component}
}

// RepoLocalLister lists only the stores that belong to the current
// repository, excluding stores in a shared root (~/.monodev or MONODEV_ROOT)
// that other repositories can also see.
type RepoLocalLister interface {
	ListRepoLocal() ([]string, error)
}

// NewSharedRepo wraps a store root that is shared across repositories, with
// no repo-local scope: every store resolves through it, but none counts as
// repo-local.
func NewSharedRepo(global StoreRepo) StoreRepo {
	return &scopedRepo{global: global}
}

// ListRepoLocal returns the component-scope stores only.
func (r *scopedRepo) ListRepoLocal() ([]string, error) {
	if r.component == nil {
		return []string{}, nil
	}
	return r.component.List()
}

func (r *scopedRepo) defaultRepo() StoreRepo {
	if r.component != nil {
		return r.component
	}
	return r.global
}

// repoFor selects the scope that serves id.
// Component wins when that scope confirms the store exists. Global is used
// only after the component scope confirms the store is absent. Exists errors
// are returned so a failed lookup is never treated as absence and never
// rerouted. When neither scope has the store, the default scope receives the
// operation.
func (r *scopedRepo) repoFor(id string) (StoreRepo, error) {
	if r.component != nil {
		exists, err := r.component.Exists(id)
		if err != nil {
			return nil, fmt.Errorf("component scope lookup for store %s: %w", id, err)
		}
		if exists {
			return r.component, nil
		}
	}
	if r.global != nil {
		exists, err := r.global.Exists(id)
		if err != nil {
			return nil, fmt.Errorf("global scope lookup for store %s: %w", id, err)
		}
		if exists {
			return r.global, nil
		}
	}
	repo := r.defaultRepo()
	if repo == nil {
		return nil, fmt.Errorf("no repo found for store %s", id)
	}
	return repo, nil
}

func (r *scopedRepo) List() ([]string, error) {
	seen := make(map[string]bool)
	var ids []string
	for _, repo := range []StoreRepo{r.global, r.component} {
		if repo == nil {
			continue
		}
		listed, err := repo.List()
		if err != nil {
			return nil, err
		}
		for _, id := range listed {
			if seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (r *scopedRepo) Exists(id string) (bool, error) {
	if r.component != nil {
		exists, err := r.component.Exists(id)
		if err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}
	if r.global != nil {
		return r.global.Exists(id)
	}
	return false, nil
}

func (r *scopedRepo) Create(id string, meta *StoreMeta) error {
	repo, err := r.repoFor(id)
	if err != nil {
		return err
	}
	return repo.Create(id, meta)
}

func (r *scopedRepo) LoadMeta(id string) (*StoreMeta, error) {
	repo, err := r.repoFor(id)
	if err != nil {
		return nil, err
	}
	return repo.LoadMeta(id)
}

func (r *scopedRepo) SaveMeta(id string, meta *StoreMeta) error {
	repo, err := r.repoFor(id)
	if err != nil {
		return err
	}
	return repo.SaveMeta(id, meta)
}

func (r *scopedRepo) LoadTrack(id string) (*TrackFile, error) {
	repo, err := r.repoFor(id)
	if err != nil {
		return nil, err
	}
	return repo.LoadTrack(id)
}

func (r *scopedRepo) SaveTrack(id string, track *TrackFile) error {
	repo, err := r.repoFor(id)
	if err != nil {
		return err
	}
	return repo.SaveTrack(id, track)
}

// OverlayRoot returns the overlay directory for the resolved scope.
// A lookup error returns an empty string, the same safe failure FileStoreRepo
// uses for an invalid store ID, so callers must reject that value before
// deriving a path. Confirmed absence still resolves to the default scope.
func (r *scopedRepo) OverlayRoot(id string) string {
	repo, err := r.repoFor(id)
	if err != nil || repo == nil {
		return ""
	}
	return repo.OverlayRoot(id)
}

func (r *scopedRepo) Delete(id string) error {
	repo, err := r.repoFor(id)
	if err != nil {
		return err
	}
	return repo.Delete(id)
}

func (r *scopedRepo) StoreLockKey(id string) (string, error) {
	repo, err := r.repoFor(id)
	if err != nil {
		return "", err
	}
	locker, ok := repo.(StoreLocker)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrLockUnsupported, id)
	}
	return locker.StoreLockKey(id)
}

func (r *scopedRepo) LockStore(ctx context.Context, id string, mode lockfile.Mode) (*lockfile.Lock, error) {
	repo, err := r.repoFor(id)
	if err != nil {
		return nil, err
	}
	locker, ok := repo.(StoreLocker)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrLockUnsupported, id)
	}
	return locker.LockStore(ctx, id, mode)
}
