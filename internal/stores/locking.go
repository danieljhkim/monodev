package stores

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/danieljhkim/monodev/internal/lockfile"
)

// LockRequest names one store transaction lock.
type LockRequest struct {
	Repo StoreRepo
	ID   string
	Mode lockfile.Mode
}

type keyedLockRequest struct {
	LockRequest
	key string
}

// LockStores sorts canonical lock paths before acquisition and returns one
// release function. Requests for the same lock path are merged, and an
// exclusive request wins over a shared one. Repositories without locking are
// skipped. Callers that need both resource kinds must acquire their workspace
// lock first, then call this helper. That workspace-before-store rule is the
// global deadlock order.
func LockStores(ctx context.Context, requests ...LockRequest) (func(), error) {
	byKey := make(map[string]keyedLockRequest)
	for _, request := range requests {
		locker, ok := request.Repo.(StoreLocker)
		if !ok {
			continue
		}
		key, err := locker.StoreLockKey(request.ID)
		if err != nil {
			if errors.Is(err, ErrLockUnsupported) {
				continue
			}
			return nil, fmt.Errorf("resolve store lock %s: %w", request.ID, err)
		}
		if existing, ok := byKey[key]; ok {
			if request.Mode == lockfile.Exclusive {
				existing.Mode = lockfile.Exclusive
				byKey[key] = existing
			}
			continue
		}
		byKey[key] = keyedLockRequest{LockRequest: request, key: key}
	}

	ordered := make([]keyedLockRequest, 0, len(byKey))
	for _, request := range byKey {
		ordered = append(ordered, request)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].key < ordered[j].key })

	locks := make([]*lockfile.Lock, 0, len(ordered))
	for _, request := range ordered {
		locker := request.Repo.(StoreLocker)
		lock, err := locker.LockStore(ctx, request.ID, request.Mode)
		if err != nil {
			for i := len(locks) - 1; i >= 0; i-- {
				_ = locks[i].Close()
			}
			return nil, fmt.Errorf("lock store %s: %w", request.ID, err)
		}
		locks = append(locks, lock)
	}

	return func() {
		for i := len(locks) - 1; i >= 0; i-- {
			_ = locks[i].Close()
		}
	}, nil
}
