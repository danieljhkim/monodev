package engine

import (
	"context"
	"os"
	"testing"

	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// copyCapturingFS extends trackFileInfoFS to record Copy calls.
type copyCapturingFS struct {
	existingPaths map[string]bool
	copyCalls     []copyCall
}

type copyCall struct {
	src string
	dst string
}

func newCopyCapturingFS(paths ...string) *copyCapturingFS {
	m := &copyCapturingFS{existingPaths: make(map[string]bool)}
	for _, p := range paths {
		m.existingPaths[p] = true
	}
	return m
}

func (m *copyCapturingFS) ReadFile(path string) ([]byte, error)                         { return nil, nil }
func (m *copyCapturingFS) AtomicWrite(path string, data []byte, perm os.FileMode) error { return nil }
func (m *copyCapturingFS) Exists(path string) (bool, error) {
	return m.existingPaths[path], nil
}
func (m *copyCapturingFS) MkdirAll(path string, perm os.FileMode) error { return nil }
func (m *copyCapturingFS) Remove(path string) error                     { return nil }
func (m *copyCapturingFS) RemoveAll(path string) error                  { return nil }
func (m *copyCapturingFS) Symlink(oldname, newname string) error        { return nil }
func (m *copyCapturingFS) Readlink(name string) (string, error)         { return "", nil }
func (m *copyCapturingFS) Lstat(name string) (os.FileInfo, error) {
	if m.existingPaths[name] {
		return &trackFakeFileInfo{name: name, isDir: false}, nil
	}
	return nil, os.ErrNotExist
}
func (m *copyCapturingFS) Copy(src, dst string) error {
	m.copyCalls = append(m.copyCalls, copyCall{src: src, dst: dst})
	return nil
}
func (m *copyCapturingFS) ValidateRelPath(relPath string) error { return nil }
func (m *copyCapturingFS) ValidateIdentifier(id string) error   { return nil }

func newCommitEngine(gitRepo *trackGitRepo, storeRepo *trackStoreRepo, stateStore *mockStateStore, fs *copyCapturingFS) *Engine {
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

// TestCommit_CopiesFileFromWorkspaceSubdirectory verifies that Commit reads from the
// workspace subdirectory (CWD), not from the repo root when relPath is CWD-relative.
func TestCommit_CopiesFileFromWorkspaceSubdirectory(t *testing.T) {
	gitRepo := &trackGitRepo{
		root:          "/repo",
		fingerprint:   "fp1",
		workspacePath: "packages/web",
	}

	storeRepo := newTrackStoreRepo()
	// Pre-load track file with a CWD-relative path (as Track() would now store it)
	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{
		{Path: "file.txt", Kind: "file"},
	}
	storeRepo.tracks["store1"] = track

	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID("fp1", "packages/web")
	ws := state.NewWorkspaceState("fp1", "packages/web", "copy")
	ws.ActiveStore = "store1"
	stateStore.workspaces[workspaceID] = ws

	// File exists at workspace subdir path: /repo/packages/web/file.txt
	fs := newCopyCapturingFS("/repo/packages/web/file.txt")

	eng := newCommitEngine(gitRepo, storeRepo, stateStore, fs)

	result, err := eng.Commit(context.Background(), &CommitRequest{
		CWD: "/repo/packages/web",
		All: true,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Missing) > 0 {
		t.Fatalf("unexpected missing paths: %v", result.Missing)
	}
	if len(result.Committed) != 1 {
		t.Fatalf("expected 1 committed path, got %d", len(result.Committed))
	}

	if len(fs.copyCalls) != 1 {
		t.Fatalf("expected 1 Copy call, got %d", len(fs.copyCalls))
	}

	srcCalled := fs.copyCalls[0].src
	// Source should be workspace-relative: /repo/packages/web/file.txt
	// NOT repo-root-relative: /repo/file.txt
	wantSrc := "/repo/packages/web/file.txt"
	if srcCalled != wantSrc {
		t.Errorf("Copy called with src=%q, want %q (should read from workspace subdir)", srcCalled, wantSrc)
	}
}

// TestCommit_RepoRootWorkspaceUnchanged verifies commit from repo root is unchanged.
func TestCommit_RepoRootWorkspaceUnchanged(t *testing.T) {
	gitRepo := &trackGitRepo{
		root:          "/repo",
		fingerprint:   "fp1",
		workspacePath: ".",
	}

	storeRepo := newTrackStoreRepo()
	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{
		{Path: "docs/readme.md", Kind: "file"},
	}
	storeRepo.tracks["store1"] = track

	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID("fp1", ".")
	ws := state.NewWorkspaceState("fp1", ".", "copy")
	ws.ActiveStore = "store1"
	stateStore.workspaces[workspaceID] = ws

	fs := newCopyCapturingFS("/repo/docs/readme.md")

	eng := newCommitEngine(gitRepo, storeRepo, stateStore, fs)

	result, err := eng.Commit(context.Background(), &CommitRequest{
		CWD: "/repo",
		All: true,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Missing) > 0 {
		t.Fatalf("unexpected missing: %v", result.Missing)
	}

	if len(fs.copyCalls) != 1 {
		t.Fatalf("expected 1 Copy call, got %d", len(fs.copyCalls))
	}

	srcCalled := fs.copyCalls[0].src
	wantSrc := "/repo/docs/readme.md"
	if srcCalled != wantSrc {
		t.Errorf("Copy called with src=%q, want %q", srcCalled, wantSrc)
	}
}
