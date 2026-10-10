package state

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/danieljhkim/monodev/internal/lockfile"
)

// scopedStore loads workspace state from primary then secondary.
// New IDs are saved on primary, matching Engine.stateStore (the global
// workspace directory). Existing IDs stay in the store that already holds them.
type scopedStore struct {
	primary   StateStore
	secondary StateStore
}

// NewScopedStore returns a StateStore that searches primary then secondary.
// If secondary is nil, primary is returned unchanged.
func NewScopedStore(primary, secondary StateStore) StateStore {
	if secondary == nil {
		return primary
	}
	return &scopedStore{primary: primary, secondary: secondary}
}

func (s *scopedStore) storeFor(id string) (StateStore, error) {
	_, err := s.primary.LoadWorkspace(id)
	if err == nil {
		return s.primary, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	_, err = s.secondary.LoadWorkspace(id)
	if err == nil {
		return s.secondary, nil
	}
	if os.IsNotExist(err) {
		return s.primary, nil
	}
	return nil, err
}

func (s *scopedStore) LoadWorkspace(id string) (*WorkspaceState, error) {
	ws, err := s.primary.LoadWorkspace(id)
	if err == nil {
		return ws, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	return s.secondary.LoadWorkspace(id)
}

func (s *scopedStore) SaveWorkspace(id string, ws *WorkspaceState) error {
	store, err := s.storeFor(id)
	if err != nil {
		return err
	}
	return store.SaveWorkspace(id, ws)
}

func (s *scopedStore) DeleteWorkspace(id string) error {
	store, err := s.storeFor(id)
	if err != nil {
		return err
	}
	return store.DeleteWorkspace(id)
}

// LockWorkspace locks the ID in both scopes, primary first. The scope that
// holds an ID can change between resolution and acquisition (a new ID is
// saved to primary while a migration may write secondary), so holding both
// excludes every transaction that locks either scope directly. Callers that
// need workspace and store locks must still take this one first.
func (s *scopedStore) LockWorkspace(ctx context.Context, id string, mode lockfile.Mode) (*lockfile.Lock, error) {
	locks := make([]*lockfile.Lock, 0, 2)
	release := func() {
		for i := len(locks) - 1; i >= 0; i-- {
			_ = locks[i].Close()
		}
	}
	for _, store := range []StateStore{s.primary, s.secondary} {
		locker, ok := store.(WorkspaceLocker)
		if !ok {
			continue
		}
		lock, err := locker.LockWorkspace(ctx, id, mode)
		if errors.Is(err, ErrLockUnsupported) {
			continue
		}
		if err != nil {
			release()
			return nil, err
		}
		locks = append(locks, lock)
	}
	if len(locks) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrLockUnsupported, id)
	}
	return lockfile.Join(locks...), nil
}
