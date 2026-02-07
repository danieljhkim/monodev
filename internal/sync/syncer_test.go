package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/clock"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/persist"
	"github.com/danieljhkim/monodev/internal/remote"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// fakeStoreRepo implements a simple in-memory store repository for testing.
type fakeStoreRepo struct {
	stores      map[string]*stores.StoreMeta
	tracks      map[string]*stores.TrackFile
	overlayRoot string
}

func newFakeStoreRepo(overlayRoot string) *fakeStoreRepo {
	return &fakeStoreRepo{
		stores:      make(map[string]*stores.StoreMeta),
		tracks:      make(map[string]*stores.TrackFile),
		overlayRoot: overlayRoot,
	}
}

func (r *fakeStoreRepo) List() ([]string, error) {
	ids := make([]string, 0, len(r.stores))
	for id := range r.stores {
		ids = append(ids, id)
	}
	return ids, nil
}

func (r *fakeStoreRepo) Exists(id string) (bool, error) {
	_, exists := r.stores[id]
	return exists, nil
}

func (r *fakeStoreRepo) Create(id string, meta *stores.StoreMeta) error {
	if _, exists := r.stores[id]; exists {
		return fmt.Errorf("store already exists")
	}
	r.stores[id] = meta
	r.tracks[id] = stores.NewTrackFile()
	return nil
}

func (r *fakeStoreRepo) LoadMeta(id string) (*stores.StoreMeta, error) {
	meta, exists := r.stores[id]
	if !exists {
		return nil, fmt.Errorf("store not found")
	}
	return meta, nil
}

func (r *fakeStoreRepo) SaveMeta(id string, meta *stores.StoreMeta) error {
	r.stores[id] = meta
	return nil
}

func (r *fakeStoreRepo) LoadTrack(id string) (*stores.TrackFile, error) {
	track, exists := r.tracks[id]
	if !exists {
		return stores.NewTrackFile(), nil
	}
	return track, nil
}

func (r *fakeStoreRepo) SaveTrack(id string, track *stores.TrackFile) error {
	r.tracks[id] = track
	return nil
}

func (r *fakeStoreRepo) OverlayRoot(id string) string {
	return filepath.Join(r.overlayRoot, id, "overlay")
}

func (r *fakeStoreRepo) Delete(id string) error {
	delete(r.stores, id)
	delete(r.tracks, id)
	return nil
}

// fakeRemoteConfigStore implements an in-memory config store for testing.
type fakeRemoteConfigStore struct {
	configs map[string]*remote.RemoteConfig
}

func newFakeRemoteConfigStore() *fakeRemoteConfigStore {
	return &fakeRemoteConfigStore{
		configs: make(map[string]*remote.RemoteConfig),
	}
}

func (s *fakeRemoteConfigStore) Load(repoRoot string) (*remote.RemoteConfig, error) {
	config, exists := s.configs[repoRoot]
	if !exists {
		return nil, remote.ErrRemoteNotConfigured
	}
	return config, nil
}

func (s *fakeRemoteConfigStore) Save(repoRoot string, config *remote.RemoteConfig) error {
	s.configs[repoRoot] = config
	return nil
}

func (s *fakeRemoteConfigStore) Delete(repoRoot string) error {
	delete(s.configs, repoRoot)
	return nil
}

func (s *fakeRemoteConfigStore) Exists(repoRoot string) (bool, error) {
	_, exists := s.configs[repoRoot]
	return exists, nil
}

// setupSyncerTest creates a test environment with temp directories and a configured Syncer.
func setupSyncerTest(t *testing.T) (
	repoRoot string,
	storesDir string,
	syncer *Syncer,
	git *remote.FakeGitPersistence,
	storeRepo *fakeStoreRepo,
	configStore *fakeRemoteConfigStore,
	cleanup func(),
) {
	t.Helper()

	// Create temp directories
	tmpDir, err := os.MkdirTemp("", "sync-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	repoRoot = filepath.Join(tmpDir, "repo")
	storesDir = filepath.Join(tmpDir, "stores")

	if err := os.MkdirAll(repoRoot, 0755); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create repo root: %v", err)
	}

	if err := os.MkdirAll(storesDir, 0755); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create stores dir: %v", err)
	}

	// Create dependencies
	fs := fsops.NewRealFS()
	git = remote.NewFakeGitPersistence()
	storeRepo = newFakeStoreRepo(storesDir)
	configStore = newFakeRemoteConfigStore()
	snapshotMgr := persist.NewSnapshotManager(fs)
	hasher := hash.NewFakeHasher()
	clk := clock.NewFakeClock(time.Now())

	// Create a fake state store (not used in current tests but required by Syncer)
	stateStore := &fakeStateStore{}

	syncer = New(git, storeRepo, stateStore, snapshotMgr, configStore, fs, hasher, clk)

	cleanup = func() {
		os.RemoveAll(tmpDir)
	}

	return repoRoot, storesDir, syncer, git, storeRepo, configStore, cleanup
}

// fakeStateStore is a minimal state store for testing.
type fakeStateStore struct{}

func (s *fakeStateStore) LoadWorkspace(workspaceID string) (*state.WorkspaceState, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *fakeStateStore) SaveWorkspace(workspaceID string, st *state.WorkspaceState) error {
	return fmt.Errorf("not implemented")
}

func (s *fakeStateStore) DeleteWorkspace(workspaceID string) error {
	return fmt.Errorf("not implemented")
}

func TestSyncer_PushStore(t *testing.T) {
	t.Run("pushes single store successfully", func(t *testing.T) {
		repoRoot, _, syncer, git, storeRepo, configStore, cleanup := setupSyncerTest(t)
		defer cleanup()

		// Create a test store
		storeID := "test-store"
		meta := stores.NewStoreMeta("Test Store", "global", time.Now())
		if err := storeRepo.Create(storeID, meta); err != nil {
			t.Fatalf("failed to create store: %v", err)
		}

		// Create store directory with a file
		overlayDir := storeRepo.OverlayRoot(storeID)
		if err := os.MkdirAll(overlayDir, 0755); err != nil {
			t.Fatalf("failed to create overlay dir: %v", err)
		}
		testFile := filepath.Join(overlayDir, "test.txt")
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		// Push
		req := &PushRequest{
			RepoRoot: repoRoot,
			StoreIDs: []string{storeID},
			Remote:   "origin",
		}

		result, err := syncer.PushStore(context.Background(), req)
		if err != nil {
			t.Fatalf("PushStore failed: %v", err)
		}

		// Verify result
		if len(result.PushedStores) != 1 {
			t.Errorf("Expected 1 pushed store, got %d", len(result.PushedStores))
		}

		if result.PushedStores[0] != storeID {
			t.Errorf("PushedStores[0] = %s, want %s", result.PushedStores[0], storeID)
		}

		if result.Remote != "origin" {
			t.Errorf("Remote = %s, want origin", result.Remote)
		}

		// Verify git operations were called
		if len(git.EnsureRepoCalls) == 0 {
			t.Error("EnsureRepo should have been called")
		}

		if len(git.CommitCalls) == 0 {
			t.Error("Commit should have been called")
		}

		if len(git.PushCalls) == 0 {
			t.Error("Push should have been called")
		}

		// Verify config was saved
		config, err := configStore.Load(repoRoot)
		if err != nil {
			t.Fatalf("Config not saved: %v", err)
		}

		if config.Remote != "origin" {
			t.Errorf("Config.Remote = %s, want origin", config.Remote)
		}
	})

	t.Run("pushes all stores when none specified", func(t *testing.T) {
		repoRoot, _, syncer, _, storeRepo, _, cleanup := setupSyncerTest(t)
		defer cleanup()

		// Create multiple stores
		for i := 1; i <= 3; i++ {
			storeID := fmt.Sprintf("store-%d", i)
			meta := stores.NewStoreMeta(fmt.Sprintf("Store %d", i), "global", time.Now())
			if err := storeRepo.Create(storeID, meta); err != nil {
				t.Fatalf("failed to create store %s: %v", storeID, err)
			}

			// Create minimal directory structure
			overlayDir := storeRepo.OverlayRoot(storeID)
			if err := os.MkdirAll(overlayDir, 0755); err != nil {
				t.Fatalf("failed to create overlay dir: %v", err)
			}
		}

		// Push without specifying store IDs
		req := &PushRequest{
			RepoRoot: repoRoot,
			Remote:   "origin",
		}

		result, err := syncer.PushStore(context.Background(), req)
		if err != nil {
			t.Fatalf("PushStore failed: %v", err)
		}

		// Should push all 3 stores
		if len(result.PushedStores) != 3 {
			t.Errorf("Expected 3 pushed stores, got %d", len(result.PushedStores))
		}
	})

	t.Run("dry run does not execute git operations", func(t *testing.T) {
		repoRoot, _, syncer, git, storeRepo, _, cleanup := setupSyncerTest(t)
		defer cleanup()

		// Create a test store
		storeID := "test-store"
		meta := stores.NewStoreMeta("Test", "global", time.Now())
		if err := storeRepo.Create(storeID, meta); err != nil {
			t.Fatalf("failed to create store: %v", err)
		}

		overlayDir := storeRepo.OverlayRoot(storeID)
		if err := os.MkdirAll(overlayDir, 0755); err != nil {
			t.Fatalf("failed to create overlay dir: %v", err)
		}

		// Push with DryRun
		req := &PushRequest{
			RepoRoot: repoRoot,
			StoreIDs: []string{storeID},
			Remote:   "origin",
			DryRun:   true,
		}

		result, err := syncer.PushStore(context.Background(), req)
		if err != nil {
			t.Fatalf("PushStore failed: %v", err)
		}

		if !result.DryRun {
			t.Error("Expected DryRun = true in result")
		}

		// Git operations should not have been called
		if len(git.EnsureRepoCalls) > 0 {
			t.Error("EnsureRepo should not be called in dry run")
		}

		if len(git.CommitCalls) > 0 {
			t.Error("Commit should not be called in dry run")
		}

		if len(git.PushCalls) > 0 {
			t.Error("Push should not be called in dry run")
		}
	})

	t.Run("returns error when repo root is empty", func(t *testing.T) {
		_, _, syncer, _, _, _, cleanup := setupSyncerTest(t)
		defer cleanup()

		req := &PushRequest{
			RepoRoot: "",
			StoreIDs: []string{"store1"},
		}

		_, err := syncer.PushStore(context.Background(), req)
		if err == nil {
			t.Error("Expected error for empty repo root, got nil")
		}
	})

	t.Run("returns error when no stores exist and none specified", func(t *testing.T) {
		repoRoot, _, syncer, _, _, _, cleanup := setupSyncerTest(t)
		defer cleanup()

		req := &PushRequest{
			RepoRoot: repoRoot,
		}

		_, err := syncer.PushStore(context.Background(), req)
		if err == nil {
			t.Error("Expected error when no stores exist, got nil")
		}
	})
}

func TestSyncer_PullStore(t *testing.T) {
	t.Run("pulls stores successfully", func(t *testing.T) {
		repoRoot, _, syncer, git, storeRepo, configStore, cleanup := setupSyncerTest(t)
		defer cleanup()

		// Setup remote config
		config := remote.DefaultRemoteConfig()
		config.Remote = "origin"
		if err := configStore.Save(repoRoot, config); err != nil {
			t.Fatalf("failed to save config: %v", err)
		}

		// Create a store in the persist directory (simulating remote store)
		storeID := "remote-store"
		meta := stores.NewStoreMeta("Remote Store", "global", time.Now())
		if err := storeRepo.Create(storeID, meta); err != nil {
			t.Fatalf("failed to create store: %v", err)
		}

		// Materialize to persist directory
		fs := fsops.NewRealFS()
		snapshotMgr := persist.NewSnapshotManager(fs)

		overlayDir := storeRepo.OverlayRoot(storeID)
		if err := os.MkdirAll(overlayDir, 0755); err != nil {
			t.Fatalf("failed to create overlay dir: %v", err)
		}

		testFile := filepath.Join(overlayDir, "remote.txt")
		if err := os.WriteFile(testFile, []byte("remote content"), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		if err := snapshotMgr.Materialize(storeID, storeRepo, repoRoot); err != nil {
			t.Fatalf("failed to materialize: %v", err)
		}

		// Delete from stores dir to simulate it only existing remotely
		storeDir := filepath.Dir(overlayDir)
		if err := os.RemoveAll(storeDir); err != nil {
			t.Fatalf("failed to remove store dir: %v", err)
		}

		// Pull
		req := &PullRequest{
			RepoRoot: repoRoot,
			StoreIDs: []string{storeID},
		}

		result, err := syncer.PullStore(context.Background(), req)
		if err != nil {
			t.Fatalf("PullStore failed: %v", err)
		}

		// Verify result
		if len(result.PulledStores) != 1 {
			t.Errorf("Expected 1 pulled store, got %d", len(result.PulledStores))
		}

		if result.PulledStores[0] != storeID {
			t.Errorf("PulledStores[0] = %s, want %s", result.PulledStores[0], storeID)
		}

		// Verify git operations were called
		if len(git.EnsureRepoCalls) == 0 {
			t.Error("EnsureRepo should have been called")
		}

		if len(git.FetchCalls) == 0 {
			t.Error("Fetch should have been called")
		}

		if len(git.CheckoutCalls) == 0 {
			t.Error("Checkout should have been called")
		}

		// Verify store was dematerialized back to stores dir
		if _, err := os.Stat(storeDir); os.IsNotExist(err) {
			t.Error("Store was not dematerialized to stores directory")
		}
	})

	t.Run("pulls all stores when none specified", func(t *testing.T) {
		repoRoot, _, syncer, _, storeRepo, configStore, cleanup := setupSyncerTest(t)
		defer cleanup()

		// Setup remote config
		config := remote.DefaultRemoteConfig()
		if err := configStore.Save(repoRoot, config); err != nil {
			t.Fatalf("failed to save config: %v", err)
		}

		// Create multiple stores in persist directory
		fs := fsops.NewRealFS()
		snapshotMgr := persist.NewSnapshotManager(fs)

		for i := 1; i <= 2; i++ {
			storeID := fmt.Sprintf("store-%d", i)
			meta := stores.NewStoreMeta(fmt.Sprintf("Store %d", i), "global", time.Now())
			if err := storeRepo.Create(storeID, meta); err != nil {
				t.Fatalf("failed to create store: %v", err)
			}

			overlayDir := storeRepo.OverlayRoot(storeID)
			if err := os.MkdirAll(overlayDir, 0755); err != nil {
				t.Fatalf("failed to create overlay dir: %v", err)
			}

			if err := snapshotMgr.Materialize(storeID, storeRepo, repoRoot); err != nil {
				t.Fatalf("failed to materialize: %v", err)
			}
		}

		// Pull without specifying store IDs
		req := &PullRequest{
			RepoRoot: repoRoot,
		}

		result, err := syncer.PullStore(context.Background(), req)
		if err != nil {
			t.Fatalf("PullStore failed: %v", err)
		}

		// Should pull all stores
		if len(result.PulledStores) != 2 {
			t.Errorf("Expected 2 pulled stores, got %d", len(result.PulledStores))
		}
	})

	t.Run("returns empty result when no persisted stores exist", func(t *testing.T) {
		repoRoot, _, syncer, _, _, configStore, cleanup := setupSyncerTest(t)
		defer cleanup()

		// Setup remote config
		config := remote.DefaultRemoteConfig()
		if err := configStore.Save(repoRoot, config); err != nil {
			t.Fatalf("failed to save config: %v", err)
		}

		// Pull without any persisted stores
		req := &PullRequest{
			RepoRoot: repoRoot,
		}

		result, err := syncer.PullStore(context.Background(), req)
		if err != nil {
			t.Fatalf("PullStore failed: %v", err)
		}

		if len(result.PulledStores) != 0 {
			t.Errorf("Expected 0 pulled stores, got %d", len(result.PulledStores))
		}
	})

	t.Run("returns error when repo root is empty", func(t *testing.T) {
		_, _, syncer, _, _, _, cleanup := setupSyncerTest(t)
		defer cleanup()

		req := &PullRequest{
			RepoRoot: "",
		}

		_, err := syncer.PullStore(context.Background(), req)
		if err == nil {
			t.Error("Expected error for empty repo root, got nil")
		}
	})

	t.Run("returns error when remote config not found", func(t *testing.T) {
		repoRoot, _, syncer, _, _, _, cleanup := setupSyncerTest(t)
		defer cleanup()

		// Don't set up config - should fail
		req := &PullRequest{
			RepoRoot: repoRoot,
		}

		_, err := syncer.PullStore(context.Background(), req)
		if err == nil {
			t.Error("Expected error when config not found, got nil")
		}
	})
}

func TestBuildPushCommitMessage(t *testing.T) {
	_, _, syncer, _, _, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	tests := []struct {
		name          string
		storeIDs      []string
		withWorkspace bool
		expected      string
	}{
		{
			name:          "single store",
			storeIDs:      []string{"store1"},
			withWorkspace: false,
			expected:      "push: store store1",
		},
		{
			name:          "multiple stores",
			storeIDs:      []string{"store1", "store2", "store3"},
			withWorkspace: false,
			expected:      "push: 3 stores",
		},
		{
			name:          "with workspace",
			storeIDs:      []string{"store1"},
			withWorkspace: true,
			expected:      "push: store store1, workspace",
		},
		{
			name:          "multiple stores with workspace",
			storeIDs:      []string{"store1", "store2"},
			withWorkspace: true,
			expected:      "push: 2 stores, workspace",
		},
		{
			name:          "workspace only",
			storeIDs:      []string{},
			withWorkspace: true,
			expected:      "push: workspace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := syncer.buildPushCommitMessage(tt.storeIDs, tt.withWorkspace)
			if message != tt.expected {
				t.Errorf("buildPushCommitMessage() = %q, want %q", message, tt.expected)
			}
		})
	}
}
