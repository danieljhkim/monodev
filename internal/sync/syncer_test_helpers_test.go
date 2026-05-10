package sync

import (
	"encoding/json"
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
