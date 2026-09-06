package engine

import (
	"context"
	"os"
	"os/exec"
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

func TestGenerateUnifiedDiff_PreservesFinalNewlineChanges(t *testing.T) {
	tests := []struct {
		name          string
		oldData       []byte
		newData       []byte
		wantAdditions int
		wantDeletions int
		wantOldMarker bool
		wantNewMarker bool
	}{
		{
			name:          "removed final newline",
			oldData:       []byte("hello\n"),
			newData:       []byte("hello"),
			wantAdditions: 1,
			wantDeletions: 1,
			wantNewMarker: true,
		},
		{
			name:          "added final newline",
			oldData:       []byte("hello"),
			newData:       []byte("hello\n"),
			wantAdditions: 1,
			wantDeletions: 1,
			wantOldMarker: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff, additions, deletions := generateUnifiedDiff("test.txt", tt.oldData, tt.newData, "modified")
			if additions != tt.wantAdditions || deletions != tt.wantDeletions {
				t.Fatalf("line stats = +%d/-%d, want +%d/-%d; diff:\n%s", additions, deletions, tt.wantAdditions, tt.wantDeletions, diff)
			}
			if !strings.Contains(diff, "-hello\n") || !strings.Contains(diff, "+hello\n") {
				t.Fatalf("diff should contain replacement lines:\n%s", diff)
			}
			marker := "\\ No newline at end of file\n"
			if strings.Count(diff, marker) != 1 {
				t.Fatalf("marker count = %d, want 1; diff:\n%s", strings.Count(diff, marker), diff)
			}
			if tt.wantOldMarker && !strings.Contains(diff, "-hello\n"+marker) {
				t.Fatalf("missing old-line no-newline marker:\n%s", diff)
			}
			if tt.wantNewMarker && !strings.Contains(diff, "+hello\n"+marker) {
				t.Fatalf("missing new-line no-newline marker:\n%s", diff)
			}
		})
	}
}

func TestGenerateUnifiedDiff_ModifiedUnterminatedLastLine(t *testing.T) {
	diff, additions, deletions := generateUnifiedDiff(
		"test.txt",
		[]byte("first\nold"),
		[]byte("first\nnew"),
		"modified",
	)

	if additions != 1 || deletions != 1 {
		t.Fatalf("line stats = +%d/-%d, want +1/-1", additions, deletions)
	}
	marker := "\\ No newline at end of file\n"
	if !strings.Contains(diff, "-old\n"+marker) || !strings.Contains(diff, "+new\n"+marker) {
		t.Fatalf("unterminated last-line markers missing:\n%s", diff)
	}
}

func TestGenerateUnifiedDiff_PatchRoundTripPreservesBytes(t *testing.T) {
	tests := []struct {
		name    string
		oldData []byte
		newData []byte
	}{
		{name: "remove final newline", oldData: []byte("hello\n"), newData: []byte("hello")},
		{name: "add final newline", oldData: []byte("hello"), newData: []byte("hello\n")},
		{name: "modify unterminated last line", oldData: []byte("first\nold"), newData: []byte("first\nnew")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			runGit := func(args ...string) {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir = repo
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v failed: %v\n%s", args, err, output)
				}
			}
			runGit("init", "--quiet")
			runGit("config", "user.email", "test@example.com")
			runGit("config", "user.name", "Test")

			filePath := filepath.Join(repo, "test.txt")
			if err := os.WriteFile(filePath, tt.oldData, 0644); err != nil {
				t.Fatalf("failed to write old file: %v", err)
			}
			runGit("add", "test.txt")
			runGit("commit", "--quiet", "-m", "initial")

			patchPath := filepath.Join(repo, "change.patch")
			diff, _, _ := generateUnifiedDiff("test.txt", tt.oldData, tt.newData, "modified")
			if err := os.WriteFile(patchPath, []byte(diff), 0644); err != nil {
				t.Fatalf("failed to write patch: %v", err)
			}
			runGit("apply", patchPath)

			got, err := os.ReadFile(filePath)
			if err != nil {
				t.Fatalf("failed to read patched file: %v", err)
			}
			if string(got) != string(tt.newData) {
				t.Fatalf("patched bytes = %q, want %q", got, tt.newData)
			}
		})
	}
}

func TestGenerateUnifiedDiff_EmptyAndTerminatedFilesHaveExpectedMarkers(t *testing.T) {
	tests := []struct {
		name    string
		oldData []byte
		newData []byte
		markers int
	}{
		{name: "empty files", oldData: []byte{}, newData: []byte{}, markers: 0},
		{name: "terminated files", oldData: []byte("old\n"), newData: []byte("new\n"), markers: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff, _, _ := generateUnifiedDiff("test.txt", tt.oldData, tt.newData, "modified")
			if got := strings.Count(diff, "\\ No newline at end of file\n"); got != tt.markers {
				t.Fatalf("marker count = %d, want %d; diff:\n%s", got, tt.markers, diff)
			}
		})
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
