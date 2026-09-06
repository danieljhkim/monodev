package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

type diffStoreRepo struct {
	*trackStoreRepo
	overlayRoot string
}

func (r *diffStoreRepo) OverlayRoot(string) string { return r.overlayRoot }

func newDiffEngine(t *testing.T, repoRoot, workspacePath, overlayRoot string, tracked []stores.TrackedPath) *Engine {
	t.Helper()

	storeRepo := &diffStoreRepo{
		trackStoreRepo: newTrackStoreRepo(),
		overlayRoot:    overlayRoot,
	}
	track := stores.NewTrackFile()
	track.Tracked = tracked
	storeRepo.tracks["store1"] = track

	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID("fp1", workspacePath)
	workspaceState := state.NewWorkspaceState("fp1", workspacePath, "copy")
	workspaceState.ActiveStore = "store1"
	stateStore.workspaces[workspaceID] = workspaceState

	return New(
		&trackGitRepo{root: repoRoot, fingerprint: "fp1", workspacePath: workspacePath},
		storeRepo,
		stateStore,
		fsops.NewRealFS(),
		hash.NewSHA256Hasher(),
		&mockClock{},
		config.Paths{Root: filepath.Join(repoRoot, ".monodev"), Stores: filepath.Dir(overlayRoot), Workspaces: filepath.Join(repoRoot, ".state")},
	)
}

func writeDiffFixtureFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create fixture parent: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatalf("failed to write fixture file: %v", err)
	}
}

func TestGenerateUnifiedDiff_ModifiedFile(t *testing.T) {
	diff, additions, deletions := generateUnifiedDiff(
		"test.txt",
		[]byte("line1\nline2\nline3\n"),
		[]byte("line1\nline-two\nline3\n"),
		"modified",
	)

	if additions != 1 {
		t.Fatalf("additions = %d, want 1", additions)
	}
	if deletions != 1 {
		t.Fatalf("deletions = %d, want 1", deletions)
	}

	checks := []string{
		"diff --git a/test.txt b/test.txt",
		"--- a/test.txt",
		"+++ b/test.txt",
		"@@",
		"-line2",
		"+line-two",
	}
	for _, want := range checks {
		if !strings.Contains(diff, want) {
			t.Fatalf("diff missing %q:\n%s", want, diff)
		}
	}
}

func TestGenerateUnifiedDiff_AddedFile(t *testing.T) {
	diff, additions, deletions := generateUnifiedDiff(
		"new.txt",
		nil,
		[]byte("first\nsecond\n"),
		"added",
	)

	if additions != 2 {
		t.Fatalf("additions = %d, want 2", additions)
	}
	if deletions != 0 {
		t.Fatalf("deletions = %d, want 0", deletions)
	}

	checks := []string{
		"--- /dev/null",
		"+++ b/new.txt",
		"+first",
		"+second",
	}
	for _, want := range checks {
		if !strings.Contains(diff, want) {
			t.Fatalf("diff missing %q:\n%s", want, diff)
		}
	}
}

func TestComparePath_ShowContentPopulatesUnifiedDiff(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "store.txt")
	workspacePath := filepath.Join(tmpDir, "workspace.txt")

	if err := os.WriteFile(storePath, []byte("alpha\nbeta\n"), 0644); err != nil {
		t.Fatalf("failed to write store file: %v", err)
	}
	if err := os.WriteFile(workspacePath, []byte("alpha\ngamma\n"), 0644); err != nil {
		t.Fatalf("failed to write workspace file: %v", err)
	}

	eng := &Engine{
		fs:     fsops.NewRealFS(),
		hasher: hash.NewSHA256Hasher(),
	}

	info := eng.comparePath(workspacePath, storePath, "example.txt", "file", true)

	if info.Status != "modified" {
		t.Fatalf("status = %q, want modified", info.Status)
	}
	if info.UnifiedDiff == "" {
		t.Fatal("expected UnifiedDiff to be populated")
	}
	if info.Additions != 1 || info.Deletions != 1 {
		t.Fatalf("line stats = +%d/-%d, want +1/-1", info.Additions, info.Deletions)
	}
	if !strings.Contains(info.UnifiedDiff, "-beta") || !strings.Contains(info.UnifiedDiff, "+gamma") {
		t.Fatalf("unexpected UnifiedDiff:\n%s", info.UnifiedDiff)
	}
}

func TestDiff_UsesSelectedNestedWorkspaceRoot(t *testing.T) {
	repoRoot := t.TempDir()
	workspaceRoot := filepath.Join(repoRoot, "nested")
	overlayRoot := filepath.Join(t.TempDir(), "overlay")

	// The root and nested workspace intentionally contain same-named files.
	writeDiffFixtureFile(t, filepath.Join(repoRoot, "note.txt"), "root version\n")
	writeDiffFixtureFile(t, filepath.Join(workspaceRoot, "note.txt"), "store version\n")
	writeDiffFixtureFile(t, filepath.Join(overlayRoot, "note.txt"), "store version\n")

	eng := newDiffEngine(t, repoRoot, "nested", overlayRoot, []stores.TrackedPath{{Path: "note.txt", Kind: "file"}})
	result, err := eng.Diff(context.Background(), &DiffRequest{
		CWD:         workspaceRoot,
		StoreID:     "store1",
		ShowContent: true,
	})
	if err != nil {
		t.Fatalf("nested diff failed: %v", err)
	}
	if len(result.Files) != 1 || result.Files[0].Status != "unchanged" {
		t.Fatalf("nested unchanged file result = %#v, want one unchanged file", result.Files)
	}

	writeDiffFixtureFile(t, filepath.Join(workspaceRoot, "note.txt"), "nested edit\n")
	result, err = eng.Diff(context.Background(), &DiffRequest{
		CWD:         workspaceRoot,
		StoreID:     "store1",
		ShowContent: true,
	})
	if err != nil {
		t.Fatalf("nested edited diff failed: %v", err)
	}
	if len(result.Files) != 1 || result.Files[0].Status != "modified" {
		t.Fatalf("nested edited file result = %#v, want one modified file", result.Files)
	}
}

func TestDiff_UsesSelectedNestedWorkspaceRootForDirectories(t *testing.T) {
	repoRoot := t.TempDir()
	workspaceRoot := filepath.Join(repoRoot, "nested")
	overlayRoot := filepath.Join(t.TempDir(), "overlay")

	// Content under the repository root must not be mixed into a nested
	// workspace's directory comparison.
	writeDiffFixtureFile(t, filepath.Join(repoRoot, "config", "note.txt"), "root version\n")
	writeDiffFixtureFile(t, filepath.Join(repoRoot, "config", "root-only.txt"), "root only\n")
	writeDiffFixtureFile(t, filepath.Join(workspaceRoot, "config", "note.txt"), "store version\n")
	writeDiffFixtureFile(t, filepath.Join(overlayRoot, "config", "note.txt"), "store version\n")

	eng := newDiffEngine(t, repoRoot, "nested", overlayRoot, []stores.TrackedPath{{Path: "config", Kind: "dir"}})
	result, err := eng.Diff(context.Background(), &DiffRequest{
		CWD:         workspaceRoot,
		StoreID:     "store1",
		ShowContent: true,
	})
	if err != nil {
		t.Fatalf("nested directory diff failed: %v", err)
	}
	if len(result.Files) != 1 || result.Files[0].Path != filepath.Join("config", "note.txt") || result.Files[0].Status != "unchanged" {
		t.Fatalf("nested directory result = %#v, want only unchanged config/note.txt", result.Files)
	}
}

func TestDiff_RootWorkspaceStillUsesRepositoryRoot(t *testing.T) {
	repoRoot := t.TempDir()
	overlayRoot := filepath.Join(t.TempDir(), "overlay")
	writeDiffFixtureFile(t, filepath.Join(repoRoot, "note.txt"), "store version\n")
	writeDiffFixtureFile(t, filepath.Join(overlayRoot, "note.txt"), "store version\n")

	eng := newDiffEngine(t, repoRoot, ".", overlayRoot, []stores.TrackedPath{{Path: "note.txt", Kind: "file"}})
	result, err := eng.Diff(context.Background(), &DiffRequest{
		CWD:     repoRoot,
		StoreID: "store1",
	})
	if err != nil {
		t.Fatalf("root diff failed: %v", err)
	}
	if len(result.Files) != 1 || result.Files[0].Status != "unchanged" {
		t.Fatalf("root workspace result = %#v, want one unchanged file", result.Files)
	}
}
