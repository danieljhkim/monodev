package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/danieljhkim/monodev/internal/lockfile"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

type storeLockRequest struct {
	repo stores.StoreRepo
	id   string
	mode lockfile.Mode
}

type keyedStoreLock struct {
	storeLockRequest
	key string
}

type workspaceLockRequest struct {
	store state.StateStore
	id    string
	mode  lockfile.Mode
}

func noUnlock() {}

func lockWorkspace(ctx context.Context, store state.StateStore, id string, mode lockfile.Mode) (func(), error) {
	locker, ok := store.(state.WorkspaceLocker)
	if !ok {
		return noUnlock, nil
	}
	lock, err := locker.LockWorkspace(ctx, id, mode)
	if err != nil {
		return nil, fmt.Errorf("lock workspace %s: %w", id, err)
	}
	return func() { _ = lock.Close() }, nil
}

func (e *Engine) lockWorkspace(ctx context.Context, id string, mode lockfile.Mode) (func(), error) {
	return lockWorkspace(ctx, e.stateStore, id, mode)
}

func (e *Engine) lockWorkspaces(ctx context.Context, requests ...workspaceLockRequest) (func(), error) {
	sort.Slice(requests, func(i, j int) bool { return requests[i].id < requests[j].id })
	unlocks := make([]func(), 0, len(requests))
	seen := make(map[string]bool)
	for _, request := range requests {
		if seen[request.id] {
			continue
		}
		seen[request.id] = true
		unlock, err := lockWorkspace(ctx, request.store, request.id, request.mode)
		if err != nil {
			for i := len(unlocks) - 1; i >= 0; i-- {
				unlocks[i]()
			}
			return nil, err
		}
		unlocks = append(unlocks, unlock)
	}
	return func() {
		for i := len(unlocks) - 1; i >= 0; i-- {
			unlocks[i]()
		}
	}, nil
}

// lockStores sorts canonical lock paths before acquisition. Callers that need
// both resource kinds must acquire their workspace lock first, then call this
// helper. That workspace-before-store rule is the global deadlock order.
func (e *Engine) lockStores(ctx context.Context, requests ...storeLockRequest) (func(), error) {
	byKey := make(map[string]keyedStoreLock)
	for _, request := range requests {
		locker, ok := request.repo.(stores.StoreLocker)
		if !ok {
			continue
		}
		key, err := locker.StoreLockKey(request.id)
		if err != nil {
			if errors.Is(err, stores.ErrLockUnsupported) {
				continue
			}
			return nil, fmt.Errorf("resolve store lock %s: %w", request.id, err)
		}
		if existing, ok := byKey[key]; ok {
			if request.mode == lockfile.Exclusive {
				existing.mode = lockfile.Exclusive
				byKey[key] = existing
			}
			continue
		}
		byKey[key] = keyedStoreLock{storeLockRequest: request, key: key}
	}

	ordered := make([]keyedStoreLock, 0, len(byKey))
	for _, request := range byKey {
		ordered = append(ordered, request)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].key < ordered[j].key })

	locks := make([]*lockfile.Lock, 0, len(ordered))
	for _, request := range ordered {
		locker := request.repo.(stores.StoreLocker)
		lock, err := locker.LockStore(ctx, request.id, request.mode)
		if err != nil {
			for i := len(locks) - 1; i >= 0; i-- {
				_ = locks[i].Close()
			}
			return nil, fmt.Errorf("lock store %s: %w", request.id, err)
		}
		locks = append(locks, lock)
	}

	return func() {
		for i := len(locks) - 1; i >= 0; i-- {
			_ = locks[i].Close()
		}
	}, nil
}
