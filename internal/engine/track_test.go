package engine

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// --- Track-specific mocks ---

type trackGitRepo struct {
	root          string
	fingerprint   string
	workspacePath string
}

func (m *trackGitRepo) Discover(path string) (string, error)      { return m.root, nil }
func (m *trackGitRepo) Fingerprint(root string) (string, error)   { return m.fingerprint, nil }
func (m *trackGitRepo) RelPath(root, path string) (string, error) { return m.workspacePath, nil }
func (m *trackGitRepo) GetFingerprintComponents(root string) (string, string, error) {
	return "", "", nil
}
func (m *trackGitRepo) Username(root string) string { return "user" }

type trackStoreRepo struct {
	tracks      map[string]*stores.TrackFile
	savedTracks map[string]*stores.TrackFile
}

func newTrackStoreRepo() *trackStoreRepo {
	return &trackStoreRepo{
		tracks:      make(map[string]*stores.TrackFile),
		savedTracks: make(map[string]*stores.TrackFile),
	}
}

func (m *trackStoreRepo) List() ([]string, error)                        { return nil, nil }
func (m *trackStoreRepo) Exists(id string) (bool, error)                 { return true, nil }
func (m *trackStoreRepo) Create(id string, meta *stores.StoreMeta) error { return nil }
func (m *trackStoreRepo) LoadMeta(id string) (*stores.StoreMeta, error) {
	now := time.Now()
	return &stores.StoreMeta{Name: id, Scope: "global", CreatedAt: now, UpdatedAt: now}, nil
}
func (m *trackStoreRepo) SaveMeta(id string, meta *stores.StoreMeta) error { return nil }
func (m *trackStoreRepo) LoadTrack(id string) (*stores.TrackFile, error) {
	if t, ok := m.tracks[id]; ok {
		return t, nil
	}
	return stores.NewTrackFile(), nil
}
func (m *trackStoreRepo) SaveTrack(id string, track *stores.TrackFile) error {
	m.savedTracks[id] = track
	return nil
}
func (m *trackStoreRepo) OverlayRoot(id string) string { return "/stores/" + id + "/overlay" }
func (m *trackStoreRepo) Delete(id string) error       { return nil }

type trackFileInfoFS struct {
	existingPaths map[string]bool
}

func newTrackFileInfoFS(paths ...string) *trackFileInfoFS {
	m := &trackFileInfoFS{existingPaths: make(map[string]bool)}
	for _, p := range paths {
		m.existingPaths[p] = true
	}
	return m
}

func (m *trackFileInfoFS) ReadFile(path string) ([]byte, error)                         { return nil, nil }
func (m *trackFileInfoFS) AtomicWrite(path string, data []byte, perm os.FileMode) error { return nil }
func (m *trackFileInfoFS) Exists(path string) (bool, error)                             { return m.existingPaths[path], nil }
func (m *trackFileInfoFS) MkdirAll(path string, perm os.FileMode) error                 { return nil }
func (m *trackFileInfoFS) Remove(path string) error                                     { return nil }
func (m *trackFileInfoFS) RemoveAll(path string) error                                  { return nil }
func (m *trackFileInfoFS) Symlink(oldname, newname string) error                        { return nil }
func (m *trackFileInfoFS) Readlink(name string) (string, error)                         { return "", nil }
func (m *trackFileInfoFS) Lstat(name string) (os.FileInfo, error) {
	if m.existingPaths[name] {
		return &trackFakeFileInfo{name: name, isDir: false}, nil
	}
	return nil, os.ErrNotExist
}
func (m *trackFileInfoFS) Copy(src, dst string) error           { return nil }
func (m *trackFileInfoFS) ValidateRelPath(relPath string) error { return nil }
func (m *trackFileInfoFS) ValidateIdentifier(id string) error   { return nil }

type trackFakeFileInfo struct {
	name  string
	isDir bool
}

func (f *trackFakeFileInfo) Name() string       { return f.name }
func (f *trackFakeFileInfo) Size() int64        { return 0 }
func (f *trackFakeFileInfo) Mode() os.FileMode  { return 0644 }
func (f *trackFakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f *trackFakeFileInfo) IsDir() bool        { return f.isDir }
func (f *trackFakeFileInfo) Sys() interface{}   { return nil }

func newTrackEngine(gitRepo *trackGitRepo, storeRepo *trackStoreRepo, stateStore *mockStateStore, fs *trackFileInfoFS) *Engine {
	return New(
		gitRepo,
		storeRepo,
		stateStore,
		fs,
		&mockHasher{},
		&mockClock{},
		config.Paths{Root: "/tmp/monodev", Stores: "/tmp/monodev/stores", Workspaces: "/tmp/workspaces"},
	)
}

func setupWorkspaceWithStore(stateStore *mockStateStore, workspaceID, storeID string) {
	ws := state.NewWorkspaceState("fingerprint", ".", "copy")
	ws.ActiveStore = storeID
	stateStore.workspaces[workspaceID] = ws
}

// TestTrack_StoresPathRelativeToCWD verifies that tracking a file from a subdirectory
// stores the path relative to that subdirectory (not the repo root).
func TestTrack_StoresPathRelativeToCWD(t *testing.T) {
	gitRepo := &trackGitRepo{
		root:          "/repo",
		fingerprint:   "fp1",
		workspacePath: "packages/web", // cwd is /repo/packages/web
	}
	storeRepo := newTrackStoreRepo()
	stateStore := newMockStateStore()
	fs := newTrackFileInfoFS("/repo/packages/web/file.txt")

	workspaceID := state.ComputeWorkspaceID("fp1", "packages/web")
	setupWorkspaceWithStore(stateStore, workspaceID, "store1")

	eng := newTrackEngine(gitRepo, storeRepo, stateStore, fs)

	result, err := eng.Track(context.Background(), &TrackRequest{
		CWD:   "/repo/packages/web",
		Paths: []string{"file.txt"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.MissingPaths) > 0 {
		t.Fatalf("unexpected missing paths: %v", result.MissingPaths)
	}

	saved := storeRepo.savedTracks["store1"]
	if saved == nil {
		t.Fatal("expected SaveTrack to be called")
	}
	if len(saved.Tracked) != 1 {
		t.Fatalf("expected 1 tracked path, got %d", len(saved.Tracked))
	}

	got := saved.Tracked[0].Path
	// Should be CWD-relative: "file.txt", NOT repo-root-relative: "packages/web/file.txt"
	if got != "file.txt" {
		t.Errorf("stored path = %q, want %q (should be CWD-relative, not repo-root-relative)", got, "file.txt")
	}
}

// TestTrack_StoresNestedPathRelativeToCWD verifies nested paths are stored CWD-relative.
func TestTrack_StoresNestedPathRelativeToCWD(t *testing.T) {
	gitRepo := &trackGitRepo{
		root:          "/repo",
		fingerprint:   "fp1",
		workspacePath: "packages/web",
	}
	storeRepo := newTrackStoreRepo()
	stateStore := newMockStateStore()
	fs := newTrackFileInfoFS("/repo/packages/web/src/index.ts")

	workspaceID := state.ComputeWorkspaceID("fp1", "packages/web")
	setupWorkspaceWithStore(stateStore, workspaceID, "store1")

	eng := newTrackEngine(gitRepo, storeRepo, stateStore, fs)

	result, err := eng.Track(context.Background(), &TrackRequest{
		CWD:   "/repo/packages/web",
		Paths: []string{"src/index.ts"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.MissingPaths) > 0 {
		t.Fatalf("unexpected missing paths: %v", result.MissingPaths)
	}

	saved := storeRepo.savedTracks["store1"]
	if saved == nil || len(saved.Tracked) != 1 {
		t.Fatalf("expected 1 tracked path, got SaveTrack=%v", saved)
	}

	got := saved.Tracked[0].Path
	if got != "src/index.ts" {
		t.Errorf("stored path = %q, want %q", got, "src/index.ts")
	}
}

// TestTrack_RepoRootWorkspaceUnchanged verifies tracking from repo root still works correctly.
func TestTrack_RepoRootWorkspaceUnchanged(t *testing.T) {
	gitRepo := &trackGitRepo{
		root:          "/repo",
		fingerprint:   "fp1",
		workspacePath: ".", // at repo root
	}
	storeRepo := newTrackStoreRepo()
	stateStore := newMockStateStore()
	fs := newTrackFileInfoFS("/repo/docs/readme.md")

	workspaceID := state.ComputeWorkspaceID("fp1", ".")
	setupWorkspaceWithStore(stateStore, workspaceID, "store1")

	eng := newTrackEngine(gitRepo, storeRepo, stateStore, fs)

	result, err := eng.Track(context.Background(), &TrackRequest{
		CWD:   "/repo",
		Paths: []string{"docs/readme.md"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.MissingPaths) > 0 {
		t.Fatalf("unexpected missing paths: %v", result.MissingPaths)
	}

	saved := storeRepo.savedTracks["store1"]
	if saved == nil || len(saved.Tracked) != 1 {
		t.Fatal("expected 1 tracked path")
	}

	got := saved.Tracked[0].Path
	// From repo root, docs/readme.md should remain docs/readme.md
	if got != "docs/readme.md" {
		t.Errorf("stored path = %q, want %q", got, "docs/readme.md")
	}
}

// TestTrackRequest_HasCWDField verifies that TrackRequest has CWD field
// which is used to populate Location in tracked paths.
func TestTrackRequest_HasCWDField(t *testing.T) {
	req := &TrackRequest{
		CWD:   "/test/workspace",
		Paths: []string{"file.txt"},
	}

	if req.CWD != "/test/workspace" {
		t.Errorf("TrackRequest.CWD = %s, want '/test/workspace'", req.CWD)
	}
}

// TestTrackedPath_LocationFieldExists verifies the deprecated Location field still exists.
// As of schema v2, Location is unused; paths are repo-root-relative.
func TestTrackedPath_LocationFieldExists(t *testing.T) {
	tp := stores.TrackedPath{
		Path:     "test.txt",
		Kind:     "file",
		Location: "/workspace/path", //nolint:staticcheck // Testing deprecated field for backward compatibility
	}

	//nolint:staticcheck // Testing deprecated field for backward compatibility
	if tp.Location != "/workspace/path" {
		t.Errorf("TrackedPath.Location = %s, want '/workspace/path'", tp.Location) //nolint:staticcheck
	}
}

// TestTrackResult_ResolvedPaths verifies TrackResult structure.
func TestTrackResult_ResolvedPaths(t *testing.T) {
	result := &TrackResult{
		ResolvedPaths: map[string]string{
			"../../Makefile": "Makefile",
			"config.yaml":    "packages/web/config.yaml",
		},
	}

	if result.ResolvedPaths["../../Makefile"] != "Makefile" {
		t.Errorf("expected resolved path 'Makefile', got %q", result.ResolvedPaths["../../Makefile"])
	}
	if result.ResolvedPaths["config.yaml"] != "packages/web/config.yaml" {
		t.Errorf("expected resolved path 'packages/web/config.yaml', got %q", result.ResolvedPaths["config.yaml"])
	}
}

// TestUntrackRequest_HasCWDField verifies that UntrackRequest has CWD field.
func TestUntrackRequest_HasCWDField(t *testing.T) {
	req := &UntrackRequest{
		CWD:   "/test/workspace",
		Paths: []string{"file.txt"},
	}

	if req.CWD != "/test/workspace" {
		t.Errorf("UntrackRequest.CWD = %s, want '/test/workspace'", req.CWD)
	}
}
