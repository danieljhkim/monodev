package engine

import (
	"context"
	"fmt"
	"path/filepath"
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

type workspaceLockRequest struct {
	store state.StateStore
	id    string
	mode  lockfile.Mode
}

func lockWorkspace(ctx context.Context, store state.StateStore, id string, mode lockfile.Mode) (func(), error) {
	return state.LockWorkspace(ctx, store, id, mode)
}

func (e *Engine) lockWorkspace(ctx context.Context, id string, mode lockfile.Mode) (func(), error) {
	return lockWorkspace(ctx, e.stateStore, id, mode)
}

// lockManagedExcludes serializes updates to the one exclude file shared by a
// repository's linked worktrees. Workspace locks protect individual ledgers;
// this lock protects the reconciliation of those ledgers into Git's common
// directory.
func lockManagedExcludes(ctx context.Context, gitDir string) (func(), error) {
	lock, err := lockfile.Acquire(ctx, filepath.Join(gitDir, "info", "exclude.monodev.lock"), lockfile.Exclusive, lockfile.DefaultTimeout)
	if err != nil {
		return nil, fmt.Errorf("lock managed excludes: %w", err)
	}
	return func() { _ = lock.Close() }, nil
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
	shared := make([]stores.LockRequest, 0, len(requests))
	for _, request := range requests {
		shared = append(shared, stores.LockRequest{Repo: request.repo, ID: request.id, Mode: request.mode})
	}
	return stores.LockStores(ctx, shared...)
}
