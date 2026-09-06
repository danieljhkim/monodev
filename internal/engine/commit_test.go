package engine

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/hash"
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

func TestCommit_AllRemovesUntrackedSiblingFromOverlay(t *testing.T) {
	repoRoot := t.TempDir()
	overlayRoot := t.TempDir()
	writeOverlayFile(t, repoRoot, "notes/a")
	writeOverlayFile(t, overlayRoot, "notes/a")
	writeOverlayFile(t, overlayRoot, "notes/b")

	track := stores.NewTrackFile()
	// This is the post-untrack state: notes/a remains tracked while notes/b
	// remains in the prior overlay snapshot.
	track.Tracked = []stores.TrackedPath{{Path: "notes/a", Kind: "file"}}
	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID("fp1", ".")
	workspaceState := state.NewWorkspaceState("fp1", ".", "copy")
	workspaceState.ActiveStore = "untrusted-store"
	stateStore.workspaces[workspaceID] = workspaceState

	eng := newRealOverlayEngine(repoRoot, overlayRoot, track, stateStore)
	result, err := eng.Commit(context.Background(), &CommitRequest{CWD: repoRoot, All: true})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if want := []string{"notes/b"}; !reflect.DeepEqual(result.Removed, want) {
		t.Fatalf("Removed = %v, want %v", result.Removed, want)
	}
	if _, err := os.Stat(filepath.Join(overlayRoot, "notes", "a")); err != nil {
		t.Fatalf("tracked file was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(overlayRoot, "notes")); err != nil {
		t.Fatalf("structural ancestor was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(overlayRoot, "notes", "b")); !os.IsNotExist(err) {
		t.Fatalf("untracked sibling still exists, stat error = %v", err)
	}
}

func TestCommit_DirectoryRefreshesLeafOwnershipManifest(t *testing.T) {
	repoRoot := t.TempDir()
	overlayRoot := t.TempDir()
	writeOverlayFile(t, repoRoot, "config/app.yml")
	writeOverlayFile(t, repoRoot, "config/nested/job.yml")

	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{{Path: "config", Kind: "dir"}}
	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID("fp1", ".")
	workspaceState := state.NewWorkspaceState("fp1", ".", "copy")
	workspaceState.ActiveStore = "active-store"
	stateStore.workspaces[workspaceID] = workspaceState

	storeRepo := &realOverlayStoreRepo{trackStoreRepo: newTrackStoreRepo(), overlayRoot: overlayRoot}
	storeRepo.tracks["active-store"] = track
	hasher := hash.NewSHA256Hasher()
	eng := New(
		&trackGitRepo{root: repoRoot, fingerprint: "fp1", workspacePath: "."},
		storeRepo,
		stateStore,
		fsops.NewRealFS(),
		hasher,
		&mockClock{},
		config.Paths{Root: filepath.Join(repoRoot, ".monodev"), Stores: filepath.Dir(overlayRoot), Workspaces: filepath.Join(repoRoot, ".state")},
	)

	commit := func() state.PathOwnership {
		t.Helper()
		if _, err := eng.Commit(context.Background(), &CommitRequest{CWD: repoRoot, All: true}); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		updated, err := stateStore.LoadWorkspace(workspaceID)
		if err != nil {
			t.Fatalf("load workspace: %v", err)
		}
		ownership, ok := updated.Paths["config"]
		if !ok {
			t.Fatal("missing config ownership")
		}
		if ownership.Contents == nil {
			t.Fatal("directory ownership has no leaf manifest")
		}
		return ownership
	}

	first := commit()
	if len(first.Contents.Files) != 2 {
		t.Fatalf("first manifest = %#v, want two leaves", first.Contents.Files)
	}
	firstAppHash := first.Contents.Files["app.yml"]
	if firstAppHash == "" || first.Contents.Files["nested/job.yml"] == "" {
		t.Fatalf("first manifest has missing checksums: %#v", first.Contents.Files)
	}

	if err := os.WriteFile(filepath.Join(repoRoot, "config", "app.yml"), []byte("changed committed content"), 0600); err != nil {
		t.Fatalf("rewrite committed directory leaf: %v", err)
	}
	second := commit()
	if second.Contents.Files["app.yml"] == firstAppHash {
		t.Fatalf("app.yml manifest checksum = %q after recommit, want refreshed value", second.Contents.Files["app.yml"])
	}
}

func TestCleanupOrphanedFiles_RetainsIntentionallyTrackedDirectory(t *testing.T) {
	overlayRoot := t.TempDir()
	writeOverlayFile(t, overlayRoot, "notes/a")
	writeOverlayFile(t, overlayRoot, "notes/nested/b")
	writeOverlayFile(t, overlayRoot, "untracked/c")

	eng := &Engine{fs: fsops.NewRealFS()}
	removed, err := eng.cleanupOrphanedFiles(overlayRoot, []stores.TrackedPath{{Path: "notes", Kind: "dir"}}, false)
	if err != nil {
		t.Fatalf("cleanupOrphanedFiles() error = %v", err)
	}
	if want := []string{"untracked"}; !reflect.DeepEqual(removed, want) {
		t.Fatalf("removed = %v, want %v", removed, want)
	}
	if _, err := os.Stat(filepath.Join(overlayRoot, "notes", "nested", "b")); err != nil {
		t.Fatalf("tracked directory descendant was removed: %v", err)
	}
}

func TestCleanupOrphanedFiles_DryRunMatchesRemovalWithoutMutating(t *testing.T) {
	overlayRoot := t.TempDir()
	writeOverlayFile(t, overlayRoot, "notes/a")
	writeOverlayFile(t, overlayRoot, "notes/b")

	eng := &Engine{fs: fsops.NewRealFS()}
	tracked := []stores.TrackedPath{{Path: "notes/a", Kind: "file"}}
	dryRunRemoved, err := eng.cleanupOrphanedFiles(overlayRoot, tracked, true)
	if err != nil {
		t.Fatalf("dry-run cleanupOrphanedFiles() error = %v", err)
	}
	if want := []string{"notes/b"}; !reflect.DeepEqual(dryRunRemoved, want) {
		t.Fatalf("dry-run removed = %v, want %v", dryRunRemoved, want)
	}
	if _, err := os.Stat(filepath.Join(overlayRoot, "notes", "b")); err != nil {
		t.Fatalf("dry-run removed a file: %v", err)
	}

	removed, err := eng.cleanupOrphanedFiles(overlayRoot, tracked, false)
	if err != nil {
		t.Fatalf("cleanupOrphanedFiles() error = %v", err)
	}
	if !reflect.DeepEqual(removed, dryRunRemoved) {
		t.Fatalf("removed = %v, dry-run removed = %v", removed, dryRunRemoved)
	}
	if _, err := os.Stat(filepath.Join(overlayRoot, "notes", "b")); !os.IsNotExist(err) {
		t.Fatalf("real cleanup retained orphan, stat error = %v", err)
	}
}
