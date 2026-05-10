package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	storePath := filepath.Dir(r.OverlayRoot(id))
	if err := os.MkdirAll(r.OverlayRoot(id), 0755); err != nil {
		return err
	}
	if err := r.writeJSON(filepath.Join(storePath, "meta.json"), meta); err != nil {
		return err
	}
	if err := r.writeJSON(filepath.Join(storePath, "track.json"), r.tracks[id]); err != nil {
		return err
	}
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
	return r.writeJSON(filepath.Join(filepath.Dir(r.OverlayRoot(id)), "meta.json"), meta)
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
	return r.writeJSON(filepath.Join(filepath.Dir(r.OverlayRoot(id)), "track.json"), track)
}

func (r *fakeStoreRepo) OverlayRoot(id string) string {
	return filepath.Join(r.overlayRoot, id, "overlay")
}

func (r *fakeStoreRepo) Delete(id string) error {
	delete(r.stores, id)
	delete(r.tracks, id)
	return nil
}

func (r *fakeStoreRepo) writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
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
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("failed to create repo root: %v", err)
	}

	if err := os.MkdirAll(storesDir, 0755); err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("failed to create stores dir: %v", err)
	}

	// Create dependencies
	fs := fsops.NewRealFS()
	git = remote.NewFakeGitPersistence()
	storeRepo = newFakeStoreRepo(storesDir)
	configStore = newFakeRemoteConfigStore()
	snapshotMgr := persist.NewSnapshotManager(fs)
	hasher := hash.NewSHA256Hasher()
	clk := clock.NewFakeClock(time.Now())

	// Create a fake state store (not used in current tests but required by Syncer)
	stateStore := &fakeStateStore{workspaces: make(map[string]*state.WorkspaceState)}

	syncer = New(git, storeRepo, stateStore, snapshotMgr, configStore, fs, hasher, clk)

	cleanup = func() {
		_ = os.RemoveAll(tmpDir)
	}

	return repoRoot, storesDir, syncer, git, storeRepo, configStore, cleanup
}

// fakeStateStore is a minimal state store for testing.
type fakeStateStore struct {
	workspaces map[string]*state.WorkspaceState
}

func (s *fakeStateStore) LoadWorkspace(workspaceID string) (*state.WorkspaceState, error) {
	ws, ok := s.workspaces[workspaceID]
	if !ok {
		return nil, os.ErrNotExist
	}
	return ws, nil
}

func (s *fakeStateStore) SaveWorkspace(workspaceID string, st *state.WorkspaceState) error {
	if s.workspaces == nil {
		s.workspaces = make(map[string]*state.WorkspaceState)
	}
	s.workspaces[workspaceID] = st
	return nil
}

func (s *fakeStateStore) DeleteWorkspace(workspaceID string) error {
	delete(s.workspaces, workspaceID)
	return nil
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

	t.Run("pushes store with workspace reference", func(t *testing.T) {
		repoRoot, _, syncer, git, storeRepo, _, cleanup := setupSyncerTest(t)
		defer cleanup()

		storeID := "test-store"
		meta := stores.NewStoreMeta("Test Store", "global", time.Now())
		if err := storeRepo.Create(storeID, meta); err != nil {
			t.Fatalf("failed to create store: %v", err)
		}

		overlayDir := storeRepo.OverlayRoot(storeID)
		if err := os.MkdirAll(overlayDir, 0755); err != nil {
			t.Fatalf("failed to create overlay dir: %v", err)
		}

		workspaceID := "workspace-123"
		appliedAt := time.Date(2026, 5, 10, 19, 0, 0, 0, time.UTC)
		if err := syncer.stateStore.SaveWorkspace(workspaceID, &state.WorkspaceState{
			Repo:             "repo-fingerprint",
			WorkspacePath:    "services/api",
			AbsolutePath:     filepath.Join(repoRoot, "services", "api"),
			Applied:          true,
			Mode:             "copy",
			Stack:            []string{"stack-store"},
			AppliedStores:    []state.AppliedStore{{Store: storeID, Type: "copy"}},
			ActiveStore:      storeID,
			ActiveStoreScope: "component",
			Paths: map[string]state.PathOwnership{
				"cmd/api/main.go": {
					Store:     storeID,
					Type:      "copy",
					Timestamp: appliedAt,
					Checksum:  "sha256:abc",
				},
			},
		}); err != nil {
			t.Fatalf("failed to save workspace: %v", err)
		}

		result, err := syncer.PushStore(context.Background(), &PushRequest{
			RepoRoot:      repoRoot,
			StoreIDs:      []string{storeID},
			WorkspaceID:   workspaceID,
			WithWorkspace: true,
			Remote:        "origin",
		})
		if err != nil {
			t.Fatalf("PushStore failed: %v", err)
		}

		if !result.PushedWorkspace {
			t.Fatal("expected workspace reference to be pushed")
		}
		expectedRefPath := filepath.Join(repoRoot, ".monodev", "persist", "workspaces", workspaceID+".json")
		if result.WorkspaceRefPath != expectedRefPath {
			t.Fatalf("WorkspaceRefPath = %q, want %q", result.WorkspaceRefPath, expectedRefPath)
		}

		data, err := os.ReadFile(expectedRefPath)
		if err != nil {
			t.Fatalf("failed to read workspace reference: %v", err)
		}

		var ref workspaceReference
		if err := json.Unmarshal(data, &ref); err != nil {
			t.Fatalf("workspace reference is not valid JSON: %v", err)
		}
		if ref.SchemaVersion != workspaceReferenceSchemaVersion {
			t.Errorf("SchemaVersion = %d, want %d", ref.SchemaVersion, workspaceReferenceSchemaVersion)
		}
		if ref.WorkspaceID != workspaceID {
			t.Errorf("WorkspaceID = %q, want %q", ref.WorkspaceID, workspaceID)
		}
		if ref.ActiveStore != storeID {
			t.Errorf("ActiveStore = %q, want %q", ref.ActiveStore, storeID)
		}
		if len(ref.Stack) != 1 || ref.Stack[0] != "stack-store" {
			t.Errorf("Stack = %#v, want [stack-store]", ref.Stack)
		}
		if ref.Mode != "copy" {
			t.Errorf("Mode = %q, want copy", ref.Mode)
		}
		if ref.PathOwnership.Count != 1 || len(ref.PathOwnership.Paths) != 1 {
			t.Fatalf("PathOwnership = %#v, want one owned path", ref.PathOwnership)
		}
		if ref.PathOwnership.Paths[0].Path != "cmd/api/main.go" {
			t.Errorf("owned path = %q, want cmd/api/main.go", ref.PathOwnership.Paths[0].Path)
		}
		if len(git.CommitCalls) != 1 {
			t.Fatalf("Commit calls = %d, want 1", len(git.CommitCalls))
		}
		if git.CommitCalls[0].Message != "push: store test-store, workspace" {
			t.Errorf("commit message = %q", git.CommitCalls[0].Message)
		}
	})

	t.Run("pushes workspace only", func(t *testing.T) {
		repoRoot, _, syncer, git, _, _, cleanup := setupSyncerTest(t)
		defer cleanup()

		workspaceID := "workspace-only"
		if err := syncer.stateStore.SaveWorkspace(workspaceID, &state.WorkspaceState{
			Repo:          "repo-fingerprint",
			WorkspacePath: ".",
			AbsolutePath:  repoRoot,
			Applied:       false,
			Mode:          "symlink",
			Stack:         []string{},
			AppliedStores: []state.AppliedStore{},
			ActiveStore:   "active-store",
			Paths:         map[string]state.PathOwnership{},
		}); err != nil {
			t.Fatalf("failed to save workspace: %v", err)
		}

		result, err := syncer.PushStore(context.Background(), &PushRequest{
			RepoRoot:      repoRoot,
			WorkspaceID:   workspaceID,
			WithWorkspace: true,
			Remote:        "origin",
		})
		if err != nil {
			t.Fatalf("PushStore failed: %v", err)
		}

		if len(result.PushedStores) != 0 {
			t.Errorf("PushedStores = %#v, want empty", result.PushedStores)
		}
		if !result.PushedWorkspace {
			t.Error("expected workspace reference to be pushed")
		}
		if _, err := os.Stat(result.WorkspaceRefPath); err != nil {
			t.Fatalf("workspace reference was not written: %v", err)
		}
		if len(git.CommitCalls) != 1 {
			t.Fatalf("Commit calls = %d, want 1", len(git.CommitCalls))
		}
		if git.CommitCalls[0].Message != "push: workspace" {
			t.Errorf("commit message = %q, want push: workspace", git.CommitCalls[0].Message)
		}
	})

	t.Run("dry run with workspace does not write artifact or execute git operations", func(t *testing.T) {
		repoRoot, _, syncer, git, _, _, cleanup := setupSyncerTest(t)
		defer cleanup()

		workspaceID := "dry-run-workspace"
		if err := syncer.stateStore.SaveWorkspace(workspaceID, &state.WorkspaceState{
			Repo:          "repo-fingerprint",
			WorkspacePath: ".",
			AbsolutePath:  repoRoot,
			Mode:          "copy",
			Stack:         []string{"stack-store"},
			ActiveStore:   "active-store",
			Paths:         map[string]state.PathOwnership{},
		}); err != nil {
			t.Fatalf("failed to save workspace: %v", err)
		}

		result, err := syncer.PushStore(context.Background(), &PushRequest{
			RepoRoot:      repoRoot,
			WorkspaceID:   workspaceID,
			WithWorkspace: true,
			Remote:        "origin",
			DryRun:        true,
		})
		if err != nil {
			t.Fatalf("PushStore failed: %v", err)
		}

		if !result.DryRun {
			t.Error("Expected DryRun = true in result")
		}
		if !result.PushedWorkspace {
			t.Error("expected dry run to report workspace reference would be pushed")
		}
		if _, err := os.Stat(result.WorkspaceRefPath); !os.IsNotExist(err) {
			t.Fatalf("workspace reference should not be written in dry run, stat err = %v", err)
		}
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

func TestSyncer_PushStoreCancellationStopsBeforeLaterGitSteps(t *testing.T) {
	repoRoot, _, syncer, git, storeRepo, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	storeID := "test-store"
	if err := storeRepo.Create(storeID, stores.NewStoreMeta("Test Store", "global", time.Now())); err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	git.EnsureRepoHook = func(context.Context, remote.EnsureRepoCall) error {
		cancel()
		return nil
	}

	_, err := syncer.PushStore(ctx, &PushRequest{
		RepoRoot: repoRoot,
		StoreIDs: []string{storeID},
		Remote:   "origin",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PushStore error = %v, want context.Canceled", err)
	}
	if len(git.EnsureRepoCalls) != 1 {
		t.Fatalf("EnsureRepo calls = %d, want 1", len(git.EnsureRepoCalls))
	}
	if len(git.GetRemoteCalls) != 0 {
		t.Fatalf("GetRemoteURL calls = %d, want 0 after cancellation", len(git.GetRemoteCalls))
	}
	if len(git.SetRemoteCalls) != 0 || len(git.CommitCalls) != 0 || len(git.PushCalls) != 0 {
		t.Fatalf("later git calls ran after cancellation: set=%d commit=%d push=%d", len(git.SetRemoteCalls), len(git.CommitCalls), len(git.PushCalls))
	}
}

func savePullRemoteConfig(t *testing.T, repoRoot string, configStore *fakeRemoteConfigStore) {
	t.Helper()

	config := remote.DefaultRemoteConfig()
	config.Remote = "origin"
	if err := configStore.Save(repoRoot, config); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}
}

func stagePersistedStoreForPull(t *testing.T, repoRoot string, snapshotMgr *persist.SnapshotManager, storeRepo *fakeStoreRepo, storeID string) (storeDir string, persistStorePath string) {
	t.Helper()

	meta := stores.NewStoreMeta("Remote Store", "global", time.Now())
	if err := storeRepo.Create(storeID, meta); err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

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

	storeDir = filepath.Dir(overlayDir)
	if err := os.RemoveAll(storeDir); err != nil {
		t.Fatalf("failed to remove local store dir: %v", err)
	}

	persistStorePath = filepath.Join(repoRoot, ".monodev", "persist", "stores", storeID)
	return storeDir, persistStorePath
}

func assertPullVerificationPathError(t *testing.T, err error, storeID, path string) {
	t.Helper()

	if err == nil {
		t.Fatal("Expected PullStore verification error, got nil")
	}
	if !strings.Contains(err.Error(), storeID) || !strings.Contains(err.Error(), path) {
		t.Fatalf("PullStore error %q should name store %q and path %q", err, storeID, path)
	}
}

func rewriteManifestHash(t *testing.T, manifestPath, targetPath, newHash string) {
	t.Helper()

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read manifest: %v", err)
	}

	var manifest struct {
		SchemaVersion int    `json:"schemaVersion"`
		HashAlgorithm string `json:"hashAlgorithm"`
		Files         []struct {
			Path string `json:"path"`
			Hash string `json:"hash"`
		} `json:"files"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("failed to decode manifest: %v", err)
	}

	found := false
	for i := range manifest.Files {
		if manifest.Files[i].Path == targetPath {
			manifest.Files[i].Hash = newHash
			found = true
		}
	}
	if !found {
		t.Fatalf("manifest did not contain path %q", targetPath)
	}

	data, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("failed to encode manifest: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}
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

	t.Run("pull verify succeeds when persisted files match manifest", func(t *testing.T) {
		repoRoot, _, syncer, _, storeRepo, configStore, cleanup := setupSyncerTest(t)
		defer cleanup()

		savePullRemoteConfig(t, repoRoot, configStore)
		storeID := "remote-store"
		stagePersistedStoreForPull(t, repoRoot, syncer.snapshotMgr, storeRepo, storeID)

		result, err := syncer.PullStore(context.Background(), &PullRequest{
			RepoRoot: repoRoot,
			StoreIDs: []string{storeID},
			Verify:   true,
		})
		if err != nil {
			t.Fatalf("PullStore failed: %v", err)
		}
		if !result.Verified {
			t.Fatal("Verified = false, want true for manifest-backed pull")
		}
	})

	t.Run("pull verify fails when persisted overlay file is corrupted", func(t *testing.T) {
		repoRoot, _, syncer, _, storeRepo, configStore, cleanup := setupSyncerTest(t)
		defer cleanup()

		savePullRemoteConfig(t, repoRoot, configStore)
		storeID := "remote-store"
		storeDir, persistStorePath := stagePersistedStoreForPull(t, repoRoot, syncer.snapshotMgr, storeRepo, storeID)
		corruptPath := filepath.Join(persistStorePath, "overlay", "remote.txt")
		if err := os.WriteFile(corruptPath, []byte("tampered"), 0644); err != nil {
			t.Fatalf("failed to corrupt persisted file: %v", err)
		}

		_, err := syncer.PullStore(context.Background(), &PullRequest{
			RepoRoot: repoRoot,
			StoreIDs: []string{storeID},
			Verify:   true,
		})
		assertPullVerificationPathError(t, err, storeID, corruptPath)
		if _, statErr := os.Stat(storeDir); !os.IsNotExist(statErr) {
			t.Fatalf("store should not be dematerialized after failed verification, stat err = %v", statErr)
		}
	})

	t.Run("pull verify fails when persisted overlay file is missing", func(t *testing.T) {
		repoRoot, _, syncer, _, storeRepo, configStore, cleanup := setupSyncerTest(t)
		defer cleanup()

		savePullRemoteConfig(t, repoRoot, configStore)
		storeID := "remote-store"
		stagePersistedStoreForPull(t, repoRoot, syncer.snapshotMgr, storeRepo, storeID)
		missingPath := filepath.Join(repoRoot, ".monodev", "persist", "stores", storeID, "overlay", "remote.txt")
		if err := os.Remove(missingPath); err != nil {
			t.Fatalf("failed to remove persisted file: %v", err)
		}

		_, err := syncer.PullStore(context.Background(), &PullRequest{
			RepoRoot: repoRoot,
			StoreIDs: []string{storeID},
			Verify:   true,
		})
		assertPullVerificationPathError(t, err, storeID, missingPath)
	})

	t.Run("pull verify fails when manifest hash mismatches", func(t *testing.T) {
		repoRoot, _, syncer, _, storeRepo, configStore, cleanup := setupSyncerTest(t)
		defer cleanup()

		savePullRemoteConfig(t, repoRoot, configStore)
		storeID := "remote-store"
		_, persistStorePath := stagePersistedStoreForPull(t, repoRoot, syncer.snapshotMgr, storeRepo, storeID)
		manifestPath := filepath.Join(persistStorePath, "verification-manifest.json")
		rewriteManifestHash(t, manifestPath, "overlay/remote.txt", "not-the-recorded-hash")

		mismatchedPath := filepath.Join(persistStorePath, "overlay", "remote.txt")
		_, err := syncer.PullStore(context.Background(), &PullRequest{
			RepoRoot: repoRoot,
			StoreIDs: []string{storeID},
			Verify:   true,
		})
		assertPullVerificationPathError(t, err, storeID, mismatchedPath)
	})

	t.Run("pull verify keeps legacy stores pullable without reporting verified", func(t *testing.T) {
		repoRoot, _, syncer, _, storeRepo, configStore, cleanup := setupSyncerTest(t)
		defer cleanup()

		savePullRemoteConfig(t, repoRoot, configStore)
		storeID := "legacy-store"
		storeDir, persistStorePath := stagePersistedStoreForPull(t, repoRoot, syncer.snapshotMgr, storeRepo, storeID)
		if err := os.Remove(filepath.Join(persistStorePath, "verification-manifest.json")); err != nil {
			t.Fatalf("failed to remove manifest: %v", err)
		}

		result, err := syncer.PullStore(context.Background(), &PullRequest{
			RepoRoot: repoRoot,
			StoreIDs: []string{storeID},
			Verify:   true,
		})
		if err != nil {
			t.Fatalf("PullStore failed for legacy manifest-free store: %v", err)
		}
		if result.Verified {
			t.Fatal("Verified = true for legacy manifest-free store, want false")
		}
		if _, err := os.Stat(storeDir); err != nil {
			t.Fatalf("legacy store should still be dematerialized: %v", err)
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

func TestSyncer_PullStoreCancellationStopsBeforeCheckout(t *testing.T) {
	repoRoot, _, syncer, git, _, configStore, cleanup := setupSyncerTest(t)
	defer cleanup()

	config := remote.DefaultRemoteConfig()
	config.Remote = "origin"
	if err := configStore.Save(repoRoot, config); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	git.FetchHook = func(context.Context, remote.FetchCall) error {
		cancel()
		return nil
	}

	_, err := syncer.PullStore(ctx, &PullRequest{
		RepoRoot: repoRoot,
		StoreIDs: []string{"remote-store"},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PullStore error = %v, want context.Canceled", err)
	}
	if len(git.FetchCalls) != 1 {
		t.Fatalf("Fetch calls = %d, want 1", len(git.FetchCalls))
	}
	if len(git.CheckoutCalls) != 0 {
		t.Fatalf("Checkout calls = %d, want 0 after cancellation", len(git.CheckoutCalls))
	}
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
