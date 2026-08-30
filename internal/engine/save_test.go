package engine

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/gitx"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create parent dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

func TestDiscoverNewFilesInDir_FindsFilesMissingFromOverlay(t *testing.T) {
	workspaceRoot := t.TempDir()
	overlayRoot := t.TempDir()

	writeTestFile(t, filepath.Join(workspaceRoot, "sub", "existing.txt"), "old")
	writeTestFile(t, filepath.Join(overlayRoot, "sub", "existing.txt"), "old")
	writeTestFile(t, filepath.Join(workspaceRoot, "sub", "new.txt"), "fresh")

	eng := &Engine{fs: fsops.NewRealFS()}

	got, err := eng.discoverNewFilesInDir(workspaceRoot, overlayRoot, "sub")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{filepath.Join("sub", "new.txt")}
	sort.Strings(got)
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("discoverNewFilesInDir() = %v, want %v", got, want)
	}
}

func TestDiscoverNewFilesInDir_NoTrackedDirectoryYieldsNoCandidates(t *testing.T) {
	workspaceRoot := t.TempDir()
	overlayRoot := t.TempDir()

	eng := &Engine{fs: fsops.NewRealFS()}

	got, err := eng.discoverNewFilesInDir(workspaceRoot, overlayRoot, "missing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no candidates for a nonexistent directory, got %v", got)
	}
}

func TestDiscoverNewTracked_FindsNewFileUnderTrackedDirectory(t *testing.T) {
	workspaceRoot := t.TempDir()

	writeTestFile(t, filepath.Join(workspaceRoot, "scripts", "old.sh"), "old")
	writeTestFile(t, filepath.Join(workspaceRoot, "scripts", "new.sh"), "new")

	storeRepo := newTrackStoreRepo()
	overlayRoot := t.TempDir()
	storeRepo.tracks["store1"] = &stores.TrackFile{
		SchemaVersion: 2,
		Tracked: []stores.TrackedPath{
			{Path: "scripts", Kind: "dir"},
		},
	}
	writeTestFile(t, filepath.Join(overlayRoot, "scripts", "old.sh"), "old")

	fingerprint := "fp1"
	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID(fingerprint, ".")
	ws := state.NewWorkspaceState(fingerprint, ".", "copy")
	ws.ActiveStore = "store1"
	stateStore.workspaces[workspaceID] = ws

	gitRepo := gitx.NewFakeGitRepo(workspaceRoot, fingerprint)

	eng := New(
		gitRepo,
		&overlayOverridingStoreRepo{trackStoreRepo: storeRepo, overlayRoot: overlayRoot},
		stateStore,
		fsops.NewRealFS(),
		hash.NewSHA256Hasher(),
		&mockClock{},
		config.Paths{Root: "/tmp/monodev", Stores: "/tmp/monodev/stores", Workspaces: "/tmp/workspaces"},
	)

	result, err := eng.DiscoverNewTracked(context.Background(), &DiscoverNewTrackedRequest{CWD: workspaceRoot})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := filepath.Join("scripts", "new.sh")
	if len(result.NewPaths) != 1 || result.NewPaths[0] != want {
		t.Fatalf("NewPaths = %v, want [%s]", result.NewPaths, want)
	}
}

func TestDiscoverNewTracked_RespectsGitIgnore(t *testing.T) {
	workspaceRoot := t.TempDir()

	writeTestFile(t, filepath.Join(workspaceRoot, "scripts", "new.sh"), "new")
	writeTestFile(t, filepath.Join(workspaceRoot, "scripts", "ignored.log"), "noise")

	storeRepo := newTrackStoreRepo()
	overlayRoot := t.TempDir()
	storeRepo.tracks["store1"] = &stores.TrackFile{
		SchemaVersion: 2,
		Tracked: []stores.TrackedPath{
			{Path: "scripts", Kind: "dir"},
		},
	}

	fingerprint := "fp1"
	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID(fingerprint, ".")
	ws := state.NewWorkspaceState(fingerprint, ".", "copy")
	ws.ActiveStore = "store1"
	stateStore.workspaces[workspaceID] = ws

	gitRepo := gitx.NewFakeGitRepo(workspaceRoot, fingerprint)
	gitRepo.SetIgnored(filepath.Join("scripts", "ignored.log"))

	eng := New(
		gitRepo,
		&overlayOverridingStoreRepo{trackStoreRepo: storeRepo, overlayRoot: overlayRoot},
		stateStore,
		fsops.NewRealFS(),
		hash.NewSHA256Hasher(),
		&mockClock{},
		config.Paths{Root: "/tmp/monodev", Stores: "/tmp/monodev/stores", Workspaces: "/tmp/workspaces"},
	)

	result, err := eng.DiscoverNewTracked(context.Background(), &DiscoverNewTrackedRequest{CWD: workspaceRoot})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := filepath.Join("scripts", "new.sh")
	if len(result.NewPaths) != 1 || result.NewPaths[0] != want {
		t.Fatalf("NewPaths = %v, want [%s] (ignored.log should be excluded)", result.NewPaths, want)
	}
}

// overlayOverridingStoreRepo wraps trackStoreRepo so its OverlayRoot points
// at a real temp directory, since discovery walks the overlay on disk.
type overlayOverridingStoreRepo struct {
	*trackStoreRepo
	overlayRoot string
}

func (r *overlayOverridingStoreRepo) OverlayRoot(id string) string {
	return r.overlayRoot
}
