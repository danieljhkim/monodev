package integration

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/clock"
	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/engine"
	"github.com/danieljhkim/monodev/internal/gitx"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// testFS is a filesystem implementation that tracks files in memory for testing
type testFS struct {
	files    map[string][]byte
	dirs     map[string]bool
	symlinks map[string]string
	fileInfo map[string]os.FileInfo
}

func newTestFS() *testFS {
	return &testFS{
		files:    make(map[string][]byte),
		dirs:     make(map[string]bool),
		symlinks: make(map[string]string),
		fileInfo: make(map[string]os.FileInfo),
	}
}

func (fs *testFS) Exists(path string) (bool, error) {
	_, hasFile := fs.files[path]
	_, hasDir := fs.dirs[path]
	_, hasSymlink := fs.symlinks[path]
	return hasFile || hasDir || hasSymlink, nil
}

func (fs *testFS) Lstat(path string) (os.FileInfo, error) {
	if info, ok := fs.fileInfo[path]; ok {
		return info, nil
	}
	if _, ok := fs.symlinks[path]; ok {
		return &mockFileInfo{name: filepath.Base(path), isDir: false}, nil
	}
	if _, ok := fs.dirs[path]; ok {
		return &mockFileInfo{name: filepath.Base(path), isDir: true}, nil
	}
	if _, ok := fs.files[path]; ok {
		return &mockFileInfo{name: filepath.Base(path), isDir: false}, nil
	}
	return nil, os.ErrNotExist
}

func (fs *testFS) Readlink(path string) (string, error) {
	if target, ok := fs.symlinks[path]; ok {
		return target, nil
	}
	return "", os.ErrInvalid
}

func (fs *testFS) MkdirAll(path string, perm os.FileMode) error {
	fs.dirs[path] = true
	// Also create parent directories
	parent := filepath.Dir(path)
	if parent != path && parent != "." {
		fs.dirs[parent] = true
	}
	return nil
}

func (fs *testFS) Remove(path string) error {
	delete(fs.files, path)
	delete(fs.dirs, path)
	delete(fs.symlinks, path)
	delete(fs.fileInfo, path)
	return nil
}

func (fs *testFS) RemoveAll(path string) error {
	// Remove exact match
	delete(fs.files, path)
	delete(fs.dirs, path)
	delete(fs.symlinks, path)
	delete(fs.fileInfo, path)

	// Remove all paths that start with this path
	// Use proper path comparison instead of deprecated filepath.HasPrefix
	pathPrefix := path + string(filepath.Separator)
	for p := range fs.files {
		if p == path || (len(p) > len(path) && p[:len(pathPrefix)] == pathPrefix) {
			delete(fs.files, p)
		}
	}
	for p := range fs.dirs {
		if p == path || (len(p) > len(path) && p[:len(pathPrefix)] == pathPrefix) {
			delete(fs.dirs, p)
		}
	}
	for p := range fs.symlinks {
		if p == path || (len(p) > len(path) && p[:len(pathPrefix)] == pathPrefix) {
			delete(fs.symlinks, p)
		}
	}
	return nil
}

func (fs *testFS) Symlink(oldname, newname string) error {
	fs.symlinks[newname] = oldname
	fs.fileInfo[newname] = &mockFileInfo{name: filepath.Base(newname), isDir: false}
	return nil
}

func (fs *testFS) Copy(src, dst string) error {
	// Copy file content
	if content, ok := fs.files[src]; ok {
		fs.files[dst] = append([]byte(nil), content...)
	} else if fs.dirs[src] {
		fs.dirs[dst] = true
	}
	fs.fileInfo[dst] = &mockFileInfo{name: filepath.Base(dst), isDir: fs.dirs[dst]}
	return nil
}

func (fs *testFS) AtomicWrite(path string, data []byte, perm os.FileMode) error {
	fs.files[path] = append([]byte(nil), data...)
	fs.fileInfo[path] = &mockFileInfo{name: filepath.Base(path), isDir: false}
	return nil
}

func (fs *testFS) ReadFile(path string) ([]byte, error) {
	if content, ok := fs.files[path]; ok {
		return append([]byte(nil), content...), nil
	}
	return nil, os.ErrNotExist
}

// mockFileInfo implements os.FileInfo
type mockFileInfo struct {
	name  string
	size  int64
	mode  os.FileMode
	isDir bool
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) Size() int64        { return m.size }
func (m *mockFileInfo) Mode() os.FileMode  { return m.mode }
func (m *mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m *mockFileInfo) IsDir() bool        { return m.isDir }
func (m *mockFileInfo) Sys() interface{}   { return nil }

// testStateStore is an in-memory state store for testing
type testStateStore struct {
	workspaces map[string]*state.WorkspaceState
	repos      map[string]*state.RepoState
}

func newTestStateStore() *testStateStore {
	return &testStateStore{
		workspaces: make(map[string]*state.WorkspaceState),
		repos:      make(map[string]*state.RepoState),
	}
}

func (s *testStateStore) LoadWorkspace(id string) (*state.WorkspaceState, error) {
	if ws, ok := s.workspaces[id]; ok {
		// Return a copy
		wsCopy := *ws
		wsCopy.Paths = make(map[string]state.PathOwnership)
		for k, v := range ws.Paths {
			wsCopy.Paths[k] = v
		}
		wsCopy.Stack = append([]string{}, ws.Stack...)
		return &wsCopy, nil
	}
	return nil, os.ErrNotExist
}

func (s *testStateStore) SaveWorkspace(id string, ws *state.WorkspaceState) error {
	// Save a copy
	wsCopy := *ws
	wsCopy.Paths = make(map[string]state.PathOwnership)
	for k, v := range ws.Paths {
		wsCopy.Paths[k] = v
	}
	wsCopy.Stack = append([]string{}, ws.Stack...)
	s.workspaces[id] = &wsCopy
	return nil
}

func (s *testStateStore) DeleteWorkspace(id string) error {
	delete(s.workspaces, id)
	return nil
}

func (s *testStateStore) LoadRepoState(fingerprint string) (*state.RepoState, error) {
	if rs, ok := s.repos[fingerprint]; ok {
		rsCopy := *rs
		rsCopy.Stack = append([]string{}, rs.Stack...)
		return &rsCopy, nil
	}
	return nil, os.ErrNotExist
}

func (s *testStateStore) SaveRepoState(fingerprint string, rs *state.RepoState) error {
	rsCopy := *rs
	rsCopy.Stack = append([]string{}, rs.Stack...)
	s.repos[fingerprint] = &rsCopy
	return nil
}

// testStoreRepo is a mock store repository for testing
type testStoreRepo struct {
	tracks       map[string]*stores.TrackFile
	overlayRoots map[string]string
}

func newTestStoreRepo() *testStoreRepo {
	return &testStoreRepo{
		tracks:       make(map[string]*stores.TrackFile),
		overlayRoots: make(map[string]string),
	}
}

func (r *testStoreRepo) setTrack(storeID string, track *stores.TrackFile) {
	r.tracks[storeID] = track
}

func (r *testStoreRepo) setOverlayRoot(storeID, root string) {
	r.overlayRoots[storeID] = root
}

func (r *testStoreRepo) LoadTrack(id string) (*stores.TrackFile, error) {
	if track, ok := r.tracks[id]; ok {
		return track, nil
	}
	return nil, os.ErrNotExist
}

func (r *testStoreRepo) OverlayRoot(id string) string {
	if root, ok := r.overlayRoots[id]; ok {
		return root
	}
	return filepath.Join("/stores", id, "overlay")
}

func (r *testStoreRepo) List() ([]string, error)                            { return nil, nil }
func (r *testStoreRepo) Exists(id string) (bool, error)                     { return false, nil }
func (r *testStoreRepo) Create(id string, meta *stores.StoreMeta) error     { return nil }
func (r *testStoreRepo) LoadMeta(id string) (*stores.StoreMeta, error)      { return nil, nil }
func (r *testStoreRepo) SaveMeta(id string, meta *stores.StoreMeta) error   { return nil }
func (r *testStoreRepo) SaveTrack(id string, track *stores.TrackFile) error { return nil }
func (r *testStoreRepo) Delete(id string) error                             { return nil }

func setupTestEngine(t *testing.T) (*engine.Engine, *testFS, *testStateStore, *testStoreRepo, *hash.FakeHasher) {
	fs := newTestFS()
	stateStore := newTestStateStore()
	storeRepo := newTestStoreRepo()

	gitRepo := gitx.NewFakeGitRepo("/repo", "repo-fingerprint-123")
	hasher := hash.NewFakeHasher()
	clk := clock.NewFakeClock(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	paths := config.Paths{
		Root:       "/test",
		Stores:     "/test/stores",
		Workspaces: "/test/workspaces",
		Repos:      "/test/repos",
		Config:     "/test/config.yaml",
	}

	eng := engine.New(gitRepo, storeRepo, stateStore, fs, hasher, clk, paths)
	return eng, fs, stateStore, storeRepo, hasher
}
