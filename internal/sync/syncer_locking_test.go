package sync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/clock"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/lockfile"
	"github.com/danieljhkim/monodev/internal/persist"
	"github.com/danieljhkim/monodev/internal/remote"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// lockedSyncerEnv wires the real file-backed repositories, dual-scope state
// store, and SnapshotManager. Only Git transport is faked.
type lockedSyncerEnv struct {
	repoRoot       string
	syncer         *Syncer
	git            *remote.FakeGitPersistence
	storeRepo      *stores.FileStoreRepo
	globalState    *state.FileStateStore
	componentState *state.FileStateStore
}

const lockedSyncerRepository = "git@example.test:team/repo.git"

// maxBoundedLockWait is generous against lockfile.DefaultTimeout so a slow
// host does not flake, while still failing if acquisition waits forever.
const maxBoundedLockWait = 10 * time.Second

func setupLockedSyncerTest(t *testing.T) *lockedSyncerEnv {
	t.Helper()

	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := &lockedSyncerEnv{repoRoot: filepath.Join(tmpDir, "repo")}
	if err := os.MkdirAll(env.repoRoot, 0755); err != nil {
		t.Fatal(err)
	}

	fs := fsops.NewRealFS()
	env.git = remote.NewFakeGitPersistence()
	env.storeRepo = stores.NewFileStoreRepo(fs, filepath.Join(tmpDir, "stores"))
	env.globalState = state.NewFileStateStore(fs, filepath.Join(tmpDir, "global", "workspaces"))
	env.componentState = state.NewFileStateStore(fs, filepath.Join(tmpDir, "component", "workspaces"))
	configStore := newFakeRemoteConfigStore()
	savePullRemoteConfig(t, env.repoRoot, configStore)
	env.syncer = New(
		env.git,
		env.storeRepo,
		state.NewScopedStore(env.globalState, env.componentState),
		persist.NewSnapshotManager(fs),
		configStore,
		fs,
		hash.NewSHA256Hasher(),
		clock.NewFakeClock(time.Now()),
	)
	return env
}

func (env *lockedSyncerEnv) createStore(t *testing.T, storeID, content string) {
	t.Helper()
	if err := env.storeRepo.Create(storeID, stores.NewStoreMeta(storeID, time.Now())); err != nil {
		t.Fatalf("create store %q: %v", storeID, err)
	}
	env.writeOverlay(t, storeID, content)
}

func (env *lockedSyncerEnv) overlayFile(storeID string) string {
	return filepath.Join(env.storeRepo.OverlayRoot(storeID), "file.txt")
}

func (env *lockedSyncerEnv) writeOverlay(t *testing.T, storeID, content string) {
	t.Helper()
	if err := os.WriteFile(env.overlayFile(storeID), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func (env *lockedSyncerEnv) readOverlay(t *testing.T, storeID string) string {
	t.Helper()
	data, err := os.ReadFile(env.overlayFile(storeID))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// stageChangedRemoteStore persists "remote" content for storeID, then edits
// the local copy so a pull must overwrite it.
func (env *lockedSyncerEnv) stageChangedRemoteStore(t *testing.T, storeID string) {
	t.Helper()
	env.createStore(t, storeID, "remote")
	if err := env.syncer.snapshotMgr.Materialize(storeID, env.storeRepo, env.repoRoot); err != nil {
		t.Fatal(err)
	}
	env.writeOverlay(t, storeID, "local edit")
}

func (env *lockedSyncerEnv) holdStore(t *testing.T, storeID string, mode lockfile.Mode) *lockfile.Lock {
	t.Helper()
	lock, err := env.storeRepo.LockStore(context.Background(), storeID, mode)
	if err != nil {
		t.Fatalf("hold store %q: %v", storeID, err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	return lock
}

func holdWorkspace(t *testing.T, store *state.FileStateStore, workspaceID string) *lockfile.Lock {
	t.Helper()
	lock, err := store.LockWorkspace(context.Background(), workspaceID, lockfile.Exclusive)
	if err != nil {
		t.Fatalf("hold workspace %q: %v", workspaceID, err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	return lock
}

// assertReleased proves a sync operation released its locks, including on
// failure, by acquiring them exclusively without waiting out a holder.
func assertReleased(t *testing.T, acquire func(context.Context) (*lockfile.Lock, error)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	lock, err := acquire(ctx)
	if err != nil {
		t.Fatalf("lock still held after sync returned: %v", err)
	}
	_ = lock.Close()
}

func (env *lockedSyncerEnv) assertStoreReleased(t *testing.T, storeID string) {
	t.Helper()
	assertReleased(t, func(ctx context.Context) (*lockfile.Lock, error) {
		return env.storeRepo.LockStore(ctx, storeID, lockfile.Exclusive)
	})
}

func (env *lockedSyncerEnv) assertWorkspaceReleased(t *testing.T, workspaceID string) {
	t.Helper()
	for _, store := range []*state.FileStateStore{env.globalState, env.componentState} {
		assertReleased(t, func(ctx context.Context) (*lockfile.Lock, error) {
			return store.LockWorkspace(ctx, workspaceID, lockfile.Exclusive)
		})
	}
}

func assertBoundedContention(t *testing.T, err error, started time.Time) {
	t.Helper()
	if !errors.Is(err, lockfile.ErrContended) {
		t.Fatalf("error = %v, want lock contention", err)
	}
	if elapsed := time.Since(started); elapsed > maxBoundedLockWait {
		t.Fatalf("contention took %s, want bounded wait", elapsed)
	}
}

func TestSyncer_PullStoreForceWaitsForStoreTransaction(t *testing.T) {
	t.Parallel()
	env := setupLockedSyncerTest(t)
	env.stageChangedRemoteStore(t, "shared-store")

	held := env.holdStore(t, "shared-store", lockfile.Exclusive)
	started := time.Now()
	_, err := env.syncer.PullStore(context.Background(), &PullRequest{
		RepoRoot: env.repoRoot,
		StoreIDs: []string{"shared-store"},
		Force:    true,
	})
	assertBoundedContention(t, err, started)
	if got := env.readOverlay(t, "shared-store"); got != "local edit" {
		t.Fatalf("local store changed under a held transaction lock: %q", got)
	}

	_ = held.Close()
	result, err := env.syncer.PullStore(context.Background(), &PullRequest{
		RepoRoot: env.repoRoot,
		StoreIDs: []string{"shared-store"},
		Force:    true,
	})
	if err != nil {
		t.Fatalf("pull after release: %v", err)
	}
	if !result.Verified {
		t.Fatal("pull after release should verify the persisted store")
	}
	if got := env.readOverlay(t, "shared-store"); got != "remote" {
		t.Fatalf("local store = %q, want forced remote content", got)
	}
	env.assertStoreReleased(t, "shared-store")
}

func TestSyncer_PullStoreReleasesStoreLockOnFailureAndCancellation(t *testing.T) {
	t.Parallel()
	env := setupLockedSyncerTest(t)
	env.stageChangedRemoteStore(t, "shared-store")

	_, err := env.syncer.PullStore(context.Background(), &PullRequest{
		RepoRoot: env.repoRoot,
		StoreIDs: []string{"shared-store"},
	})
	if !errors.Is(err, ErrPulledContentChanged) {
		t.Fatalf("PullStore error = %v, want ErrPulledContentChanged", err)
	}
	env.assertStoreReleased(t, "shared-store")

	held := env.holdStore(t, "shared-store", lockfile.Exclusive)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(50*time.Millisecond, cancel)
	started := time.Now()
	_, err = env.syncer.PullStore(ctx, &PullRequest{
		RepoRoot: env.repoRoot,
		StoreIDs: []string{"shared-store"},
		Force:    true,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PullStore error = %v, want context.Canceled while waiting for the lock", err)
	}
	if elapsed := time.Since(started); elapsed >= lockfile.DefaultTimeout {
		t.Fatalf("cancellation took %s, want it to interrupt the lock wait", elapsed)
	}
	if got := env.readOverlay(t, "shared-store"); got != "local edit" {
		t.Fatalf("local store changed after cancelled pull: %q", got)
	}
	_ = held.Close()
	env.assertStoreReleased(t, "shared-store")
}

func TestSyncer_PullStoreDoesNotWaitForIndependentStore(t *testing.T) {
	t.Parallel()
	env := setupLockedSyncerTest(t)
	env.stageChangedRemoteStore(t, "pulled-store")
	env.createStore(t, "busy-store", "busy")
	env.holdStore(t, "busy-store", lockfile.Exclusive)

	if _, err := env.syncer.PullStore(context.Background(), &PullRequest{
		RepoRoot: env.repoRoot,
		StoreIDs: []string{"pulled-store"},
		Force:    true,
	}); err != nil {
		t.Fatalf("pull of an unlocked store: %v", err)
	}
	if got := env.readOverlay(t, "pulled-store"); got != "remote" {
		t.Fatalf("pulled store = %q, want remote content", got)
	}
}

func TestSyncer_PushStoreWaitsForStoreTransaction(t *testing.T) {
	t.Parallel()
	env := setupLockedSyncerTest(t)
	env.createStore(t, "shared-store", "committed")

	held := env.holdStore(t, "shared-store", lockfile.Exclusive)
	started := time.Now()
	_, err := env.syncer.PushStore(context.Background(), &PushRequest{
		RepoRoot: env.repoRoot,
		StoreIDs: []string{"shared-store"},
		Remote:   "origin",
	})
	assertBoundedContention(t, err, started)
	persisted := filepath.Join(env.repoRoot, ".monodev", "persist", "stores", "shared-store")
	if _, err := os.Stat(persisted); !os.IsNotExist(err) {
		t.Fatalf("push snapshotted a store under a held transaction lock: %v", err)
	}
	if len(env.git.CommitCalls) != 0 {
		t.Fatalf("Commit calls = %d, want 0", len(env.git.CommitCalls))
	}
	_ = held.Close()
	env.assertStoreReleased(t, "shared-store")

	// Readers hold shared locks; a snapshot is compatible with them.
	env.holdStore(t, "shared-store", lockfile.Shared)
	if _, err := env.syncer.PushStore(context.Background(), &PushRequest{
		RepoRoot: env.repoRoot,
		StoreIDs: []string{"shared-store"},
		Remote:   "origin",
	}); err != nil {
		t.Fatalf("push alongside a shared reader: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(persisted, "overlay", "file.txt"))
	if err != nil || string(data) != "committed" {
		t.Fatalf("persisted overlay = %q, %v", data, err)
	}
}

func TestSyncer_PushWorkspaceReferenceWaitsForWorkspaceTransaction(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"global", "component"} {
		t.Run(scope, func(t *testing.T) {
			t.Parallel()
			env := setupLockedSyncerTest(t)
			env.createStore(t, "active-store", "content")
			owner := env.globalState
			if scope == "component" {
				owner = env.componentState
			}
			ws := state.NewWorkspaceState("fingerprint", "services/api", "copy")
			ws.ActiveStore = "active-store"
			if err := owner.SaveWorkspace("workspace", ws); err != nil {
				t.Fatal(err)
			}

			held := holdWorkspace(t, owner, "workspace")
			started := time.Now()
			_, err := env.syncer.PushStore(context.Background(), &PushRequest{
				RepoRoot:           env.repoRoot,
				StoreIDs:           []string{"active-store"},
				WorkspaceID:        "workspace",
				RepositoryIdentity: lockedSyncerRepository,
				WithWorkspace:      true,
				Remote:             "origin",
			})
			assertBoundedContention(t, err, started)
			if _, err := os.Stat(workspaceReferencePath(env.repoRoot, "workspace")); !os.IsNotExist(err) {
				t.Fatalf("workspace reference written under a held transaction lock: %v", err)
			}
			_ = held.Close()
			env.assertWorkspaceReleased(t, "workspace")
		})
	}
}

// pushWorkspaceReference publishes a reference to "source-workspace" that
// names "referenced-store", then removes the source state.
func (env *lockedSyncerEnv) pushWorkspaceReference(t *testing.T) {
	t.Helper()
	env.createStore(t, "referenced-store", "content")
	env.createStore(t, "unrelated-store", "content")
	ws := state.NewWorkspaceState("source-fingerprint", "services/api", "copy")
	ws.ActiveStore = "referenced-store"
	if err := env.globalState.SaveWorkspace("source-workspace", ws); err != nil {
		t.Fatal(err)
	}
	if _, err := env.syncer.PushStore(context.Background(), &PushRequest{
		RepoRoot:           env.repoRoot,
		StoreIDs:           []string{"referenced-store", "unrelated-store"},
		WorkspaceID:        "source-workspace",
		RepositoryIdentity: lockedSyncerRepository,
		WithWorkspace:      true,
		Remote:             "origin",
	}); err != nil {
		t.Fatalf("push workspace reference: %v", err)
	}
	if err := env.globalState.DeleteWorkspace("source-workspace"); err != nil {
		t.Fatal(err)
	}
}

// restoreRequest pulls only the unrelated store, so the referenced store's
// lock is taken by the restore step alone.
func (env *lockedSyncerEnv) restoreRequest() *PullRequest {
	return &PullRequest{
		RepoRoot:           env.repoRoot,
		StoreIDs:           []string{"unrelated-store"},
		WorkspaceID:        "source-workspace",
		LocalWorkspaceID:   "local-workspace",
		RepoFingerprint:    "local-fingerprint",
		RepositoryIdentity: lockedSyncerRepository,
		WorkspacePath:      "services/api",
	}
}

func TestSyncer_RestoreWorkspaceReferenceWaitsForWorkspaceTransaction(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"global", "component"} {
		t.Run(scope, func(t *testing.T) {
			t.Parallel()
			env := setupLockedSyncerTest(t)
			env.pushWorkspaceReference(t)
			owner := env.globalState
			if scope == "component" {
				owner = env.componentState
			}

			held := holdWorkspace(t, owner, "local-workspace")
			started := time.Now()
			_, err := env.syncer.PullStore(context.Background(), env.restoreRequest())
			assertBoundedContention(t, err, started)
			if _, err := env.syncer.stateStore.LoadWorkspace("local-workspace"); !os.IsNotExist(err) {
				t.Fatalf("restored state under a held workspace lock: %v", err)
			}
			_ = held.Close()
			env.assertWorkspaceReleased(t, "local-workspace")
			env.assertStoreReleased(t, "referenced-store")

			if _, err := env.syncer.PullStore(context.Background(), env.restoreRequest()); err != nil {
				t.Fatalf("restore after release: %v", err)
			}
			restored, err := env.syncer.stateStore.LoadWorkspace("local-workspace")
			if err != nil || restored.Repo != "local-fingerprint" {
				t.Fatalf("restored state = %#v, %v", restored, err)
			}
		})
	}
}

func TestSyncer_RestoreWorkspaceReferenceDoesNotOverwriteStateCreatedWhileWaiting(t *testing.T) {
	t.Parallel()
	env := setupLockedSyncerTest(t)
	env.pushWorkspaceReference(t)

	// A workspace transaction holds the lock, so a pull that checked absence
	// before waiting would overwrite the state this transaction creates.
	held := holdWorkspace(t, env.globalState, "local-workspace")
	done := make(chan error, 1)
	go func() {
		_, err := env.syncer.PullStore(context.Background(), env.restoreRequest())
		done <- err
	}()
	time.Sleep(200 * time.Millisecond)
	created := state.NewWorkspaceState("concurrent-fingerprint", "services/api", "symlink")
	if err := env.globalState.SaveWorkspace("local-workspace", created); err != nil {
		t.Fatal(err)
	}
	_ = held.Close()

	err := <-done
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("PullStore error = %v, want refusal to overwrite concurrently created state", err)
	}
	got, err := env.globalState.LoadWorkspace("local-workspace")
	if err != nil {
		t.Fatal(err)
	}
	if got.Repo != "concurrent-fingerprint" || got.Mode != "symlink" {
		t.Fatalf("concurrently created state was overwritten: %#v", got)
	}
	env.assertWorkspaceReleased(t, "local-workspace")
}

func TestSyncer_RestoreWorkspaceReferenceLocksWorkspaceBeforeStores(t *testing.T) {
	t.Parallel()
	env := setupLockedSyncerTest(t)
	env.pushWorkspaceReference(t)

	// With only the referenced store busy, restore reaches the store lock
	// while holding the workspace lock, and releases both on failure.
	held := env.holdStore(t, "referenced-store", lockfile.Exclusive)
	started := time.Now()
	_, err := env.syncer.PullStore(context.Background(), env.restoreRequest())
	assertBoundedContention(t, err, started)
	if !strings.Contains(err.Error(), "lock store referenced-store") {
		t.Fatalf("PullStore error = %v, want referenced store contention", err)
	}
	if _, err := env.syncer.stateStore.LoadWorkspace("local-workspace"); !os.IsNotExist(err) {
		t.Fatalf("restored state while a referenced store was busy: %v", err)
	}
	env.assertWorkspaceReleased(t, "local-workspace")

	// With both busy, the workspace is attempted first and the store is
	// never reached.
	workspace := holdWorkspace(t, env.globalState, "local-workspace")
	started = time.Now()
	_, err = env.syncer.PullStore(context.Background(), env.restoreRequest())
	assertBoundedContention(t, err, started)
	if !strings.Contains(err.Error(), "lock workspace local-workspace") {
		t.Fatalf("PullStore error = %v, want workspace contention before store", err)
	}
	_ = workspace.Close()
	_ = held.Close()
	env.assertStoreReleased(t, "referenced-store")
}
