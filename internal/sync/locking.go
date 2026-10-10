package sync

import (
	"context"

	"github.com/danieljhkim/monodev/internal/lockfile"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// Sync owns resource locks for its local critical sections. SnapshotManager
// never locks, so one flow cannot acquire the same resource twice. Each lock
// covers only local reads, comparisons, and replacements, never Git transport.
// When a section needs both kinds, the workspace lock comes first, then stores
// in the sorted order stores.LockStores enforces.

func (s *Syncer) lockWorkspace(ctx context.Context, id string, mode lockfile.Mode) (func(), error) {
	return state.LockWorkspace(lockContext(ctx), s.stateStore, id, mode)
}

func (s *Syncer) lockStores(ctx context.Context, mode lockfile.Mode, storeIDs ...string) (func(), error) {
	requests := make([]stores.LockRequest, 0, len(storeIDs))
	for _, storeID := range storeIDs {
		requests = append(requests, stores.LockRequest{Repo: s.storeRepo, ID: storeID, Mode: mode})
	}
	return stores.LockStores(lockContext(ctx), requests...)
}

// lockContext lets lock acquisition poll for cancellation; checkContext
// already treats a nil context as never cancelled.
func lockContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
