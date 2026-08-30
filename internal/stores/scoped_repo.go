package stores

import (
	"context"
	"fmt"

	"github.com/danieljhkim/monodev/internal/lockfile"
)

// scopedRepo looks up stores in both global and component scopes.
// Existing stores are routed to the scope that already holds them (component
// wins when both do). New stores are created in the component scope when it
// is present, matching engine default-scope behavior after repo-local
// `.monodev` exists (auto-created on first use, or via `monodev init`).
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

func (r *scopedRepo) defaultRepo() StoreRepo {
	if r.component != nil {
		return r.component
	}
	return r.global
}

func (r *scopedRepo) repoFor(id string) StoreRepo {
	if r.component != nil {
		if exists, err := r.component.Exists(id); err == nil && exists {
			return r.component
		}
	}
	if r.global != nil {
		if exists, err := r.global.Exists(id); err == nil && exists {
			return r.global
		}
	}
	return r.defaultRepo()
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
	return r.repoFor(id).Create(id, meta)
}

func (r *scopedRepo) LoadMeta(id string) (*StoreMeta, error) {
	return r.repoFor(id).LoadMeta(id)
}

func (r *scopedRepo) SaveMeta(id string, meta *StoreMeta) error {
	return r.repoFor(id).SaveMeta(id, meta)
}

func (r *scopedRepo) LoadTrack(id string) (*TrackFile, error) {
	return r.repoFor(id).LoadTrack(id)
}

func (r *scopedRepo) SaveTrack(id string, track *TrackFile) error {
	return r.repoFor(id).SaveTrack(id, track)
}

func (r *scopedRepo) OverlayRoot(id string) string {
	repo := r.repoFor(id)
	if repo == nil {
		return ""
	}
	return repo.OverlayRoot(id)
}

func (r *scopedRepo) Delete(id string) error {
	repo := r.repoFor(id)
	if repo == nil {
		return fmt.Errorf("no repo found for store %s", id)
	}
	return repo.Delete(id)
}

func (r *scopedRepo) StoreLockKey(id string) (string, error) {
	locker, ok := r.repoFor(id).(StoreLocker)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrLockUnsupported, id)
	}
	return locker.StoreLockKey(id)
}

func (r *scopedRepo) LockStore(ctx context.Context, id string, mode lockfile.Mode) (*lockfile.Lock, error) {
	locker, ok := r.repoFor(id).(StoreLocker)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrLockUnsupported, id)
	}
	return locker.LockStore(ctx, id, mode)
}
