package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
	// The global lock also serializes the single journal shared by both scopes.
	unlockGlobal, err := lockWorkspace(ctx, e.stateStore, id, mode)
	if err != nil {
		return nil, err
	}
	owner, err := e.workspaceOwnerForID(id)
	if err != nil {
		unlockGlobal()
		return nil, err
	}
	unlockOwner, err := e.lockWorkspaceOwner(ctx, owner, id, mode)
	if err != nil {
		unlockGlobal()
		return nil, err
	}
	return func() { unlockOwner(); unlockGlobal() }, nil
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

// lockWorkspaceIdentity resolves ownership under the global coordinator lock,
// then locks the owner before any migration, reload, or mutation. New records
// retain the historical global default; existing component records stay local.
func (e *Engine) lockWorkspaceIdentity(ctx context.Context, root, fingerprint, path string, mode lockfile.Mode) (state.StateStore, func(), error) {
	id := state.ComputeWorkspaceID(fingerprint, path)
	unlockGlobal, err := lockWorkspace(ctx, e.stateStore, id, mode)
	if err != nil {
		return nil, nil, err
	}
	resolved, err := e.resolveWorkspaceState(root, fingerprint, path)
	if err != nil {
		unlockGlobal()
		return nil, nil, err
	}
	owner, err := e.workspaceOwnerWithJournal(id, resolved.store)
	if err != nil {
		unlockGlobal()
		return nil, nil, err
	}
	unlockOwner, err := e.lockWorkspaceOwner(ctx, owner, id, mode)
	if err != nil {
		unlockGlobal()
		return nil, nil, err
	}
	return owner, func() { unlockOwner(); unlockGlobal() }, nil
}

func (e *Engine) lockWorkspaceOwner(ctx context.Context, owner state.StateStore, id string, mode lockfile.Mode) (func(), error) {
	if owner == e.stateStore {
		return func() {}, nil
	}
	return lockWorkspace(ctx, owner, id, mode)
}

// workspaceOwnerForID includes committed journals so ownership survives ledger
// deletion. Older journals without a scope use normal global-first discovery.
func (e *Engine) workspaceOwnerForID(id string) (state.StateStore, error) {
	owner, err := e.workspaceStoreForID(id)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return e.workspaceOwnerWithJournal(id, owner)
}

func (e *Engine) workspaceOwnerWithJournal(id string, owner state.StateStore) (state.StateStore, error) {
	if e.fs != nil && e.configPaths.Workspaces != "" {
		journal, _, pathErr := e.overlayTxnPaths(id)
		if pathErr != nil {
			return nil, pathErr
		}
		// Inspect without migrating the journal: shared-lock and dry-run paths
		// must not rewrite it. Recovery performs the durable migration.
		data, loadErr := e.fs.ReadFile(journal)
		var txn overlayTxn
		if loadErr == nil && len(data) > 0 {
			migrated, _, migrateErr := migrateOverlayTxnJSON(journal, data)
			if migrateErr != nil {
				return nil, migrateErr
			}
			if err := json.Unmarshal(migrated, &txn); err != nil {
				return nil, err
			}
		}
		if loadErr == nil && txn.StateScope != "" {
			journalOwner, scopeErr := e.overlayTxnStore(id, &txn)
			if scopeErr != nil {
				return nil, scopeErr
			}
			if owner != nil && (owner == e.stateStore) != (journalOwner == e.stateStore) {
				return nil, fmt.Errorf("workspace %s owner conflicts with pending transaction scope %q", id, txn.StateScope)
			}
			return journalOwner, nil
		}
		if loadErr != nil && !os.IsNotExist(loadErr) {
			return nil, loadErr
		}
	}
	if owner != nil {
		return owner, nil
	}
	return e.stateStore, nil
}
