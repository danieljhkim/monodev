package sync

import (
	"context"

	"github.com/danieljhkim/monodev/internal/clock"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/persist"
	"github.com/danieljhkim/monodev/internal/remote"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// Syncer orchestrates push and pull operations for remote persistence.
type Syncer struct {
	git         remote.GitPersistence
	storeRepo   stores.StoreRepo
	stateStore  state.StateStore
	snapshotMgr *persist.SnapshotManager
	configStore remote.RemoteConfigStore
	fs          fsops.FS
	hasher      hash.Hasher
	clock       clock.Clock
}

// New creates a new Syncer with the specified dependencies.
func New(
	git remote.GitPersistence,
	storeRepo stores.StoreRepo,
	stateStore state.StateStore,
	snapshotMgr *persist.SnapshotManager,
	configStore remote.RemoteConfigStore,
	fs fsops.FS,
	hasher hash.Hasher,
	clock clock.Clock,
) *Syncer {
	return &Syncer{
		git:         git,
		storeRepo:   storeRepo,
		stateStore:  stateStore,
		snapshotMgr: snapshotMgr,
		configStore: configStore,
		fs:          fs,
		hasher:      hasher,
		clock:       clock,
	}
}

// PushStore pushes stores to the remote persistence repository.
func (s *Syncer) PushStore(ctx context.Context, req *PushRequest) (*PushResult, error) {
	return s.pushStore(ctx, req)
}

// PullStore pulls stores from the remote persistence repository.
func (s *Syncer) PullStore(ctx context.Context, req *PullRequest) (*PullResult, error) {
	return s.pullStore(ctx, req)
}
