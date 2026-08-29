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

type removeCapturingFS struct {
	*trackFileInfoFS
	removed []string
}

func newRemoveCapturingFS(paths ...string) *removeCapturingFS {
	return &removeCapturingFS{
		trackFileInfoFS: newTrackFileInfoFS(paths...),
	}
}

func (m *removeCapturingFS) RemoveAll(path string) error {
	m.removed = append(m.removed, path)
	delete(m.existingPaths, path)
	return nil
}

func TestStackUnapply_RemovesWorkspaceRelativePathForSubdirectoryWorkspace(t *testing.T) {
	gitRepo := &trackGitRepo{
		root:          "/repo",
		fingerprint:   "fp1",
		workspacePath: "services/api",
	}
	storeRepo := newTrackStoreRepo()
	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID("fp1", "services/api")
	ws := state.NewWorkspaceState("fp1", "services/api", "copy")
	ws.Stack = []string{"stack-store"}
	ws.ActiveStore = "active-store"
	ws.Paths["config.yml"] = state.PathOwnership{Store: "stack-store", Type: "copy"}
	ws.Paths["active.yml"] = state.PathOwnership{Store: "active-store", Type: "copy"}
	stateStore.workspaces[workspaceID] = ws

	fs := newRemoveCapturingFS(
		"/repo/services/api/config.yml",
		"/repo/services/api/active.yml",
		"/repo/config.yml",
	)
	eng := New(
		gitRepo,
		storeRepo,
		stateStore,
		fs,
		&mockHasher{},
		&mockClock{},
		config.Paths{Root: "/tmp/monodev", Stores: "/tmp/monodev/stores", Workspaces: "/tmp/workspaces"},
	)

	result, err := eng.StackUnapply(context.Background(), &StackUnapplyRequest{
		CWD: "/repo/services/api",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "config.yml" {
		t.Fatalf("Removed = %v, want [config.yml]", result.Removed)
	}

	if len(fs.removed) != 1 {
		t.Fatalf("RemoveAll calls = %v, want one call", fs.removed)
	}
	if got, want := fs.removed[0], "/repo/services/api/config.yml"; got != want {
		t.Fatalf("RemoveAll path = %q, want %q", got, want)
	}
	if !fs.existingPaths["/repo/config.yml"] {
		t.Fatal("repo-root config.yml was removed; want unrelated root file untouched")
	}
	if fs.existingPaths["/repo/services/api/config.yml"] {
		t.Fatal("workspace config.yml still exists; want stack-applied workspace file removed")
	}

	updated, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("failed to load workspace state: %v", err)
	}
	if _, ok := updated.Paths["config.yml"]; ok {
		t.Fatal("workspaceState.Paths still contains stack-owned config.yml")
	}
	if _, ok := updated.Paths["active.yml"]; !ok {
		t.Fatal("workspaceState.Paths removed active-store path; want only stack path removed")
	}
}

func TestStackApply_RejectsSymlinkedParentOutsideWorkspace(t *testing.T) {
	repoRoot := t.TempDir()
	outside := t.TempDir()
	requireEngineSymlink(t, outside, filepath.Join(repoRoot, "escape"))

	overlayRoot := filepath.Join(t.TempDir(), "overlay")
	writeOverlayFile(t, overlayRoot, filepath.Join("escape", "payload.txt"))
	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{{Path: "escape/payload.txt", Kind: "file"}}
	storeRepo := &realOverlayStoreRepo{trackStoreRepo: newTrackStoreRepo(), overlayRoot: overlayRoot}
	storeRepo.tracks["untrusted-store"] = track

	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID("fp1", ".")
	ws := state.NewWorkspaceState("fp1", ".", "copy")
	ws.Stack = []string{"untrusted-store"}
	stateStore.workspaces[workspaceID] = ws
	eng := New(
		&trackGitRepo{root: repoRoot, fingerprint: "fp1", workspacePath: "."},
		storeRepo,
		stateStore,
		fsops.NewRealFS(),
		&mockHasher{},
		&mockClock{},
		config.Paths{Root: filepath.Join(repoRoot, ".monodev"), Stores: filepath.Dir(overlayRoot), Workspaces: filepath.Join(repoRoot, ".state")},
	)

	_, err := eng.StackApply(context.Background(), &StackApplyRequest{CWD: repoRoot, Mode: "copy"})
	if err == nil || !strings.Contains(err.Error(), "symlinked destination ancestor") {
		t.Fatalf("StackApply error = %v, want symlinked destination ancestor rejection", err)
	}
	entries, readErr := os.ReadDir(outside)
	if readErr != nil {
		t.Fatalf("failed to inspect outside directory: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("outside directory was mutated by stack apply: %v", entries)
	}
}

func TestStackUnapply_CopiedDirectoryDriftFailsWithoutForce(t *testing.T) {
	fx := setupCopiedDirectoryFixture(t, "stack-store", true)
	writeCopiedDirFile(t, filepath.Join(fx.scriptsDir, "notes.txt"), "user work\n")
	writeCopiedDirFile(t, filepath.Join(fx.scriptsDir, "init.sh"), "echo changed\n")
	if err := os.Remove(filepath.Join(fx.scriptsDir, "utils", "helper.sh")); err != nil {
		t.Fatalf("remove helper.sh: %v", err)
	}

	result, err := fx.eng.StackUnapply(context.Background(), &StackUnapplyRequest{CWD: fx.repoRoot})
	if result != nil {
		t.Fatalf("StackUnapply result = %#v, want nil", result)
	}
	assertCopiedDirDriftError(t, err, "scripts/notes.txt", "scripts/init.sh", "scripts/utils/helper.sh")
	if _, err := os.Stat(filepath.Join(fx.scriptsDir, "notes.txt")); err != nil {
		t.Fatalf("expected drifted stack directory to remain: %v", err)
	}
	updated, err := fx.stateStore.LoadWorkspace(fx.workspaceID)
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	if _, ok := updated.Paths["scripts"]; !ok {
		t.Fatal("workspaceState.Paths removed stack-owned scripts; want entry intact")
	}
	if _, ok := updated.Paths["active.yml"]; !ok {
		t.Fatal("workspaceState.Paths removed active.yml; want active-store path intact")
	}
}

func TestStackUnapply_ForceRemovesDriftedCopiedDirectory(t *testing.T) {
	fx := setupCopiedDirectoryFixture(t, "stack-store", true)
	writeCopiedDirFile(t, filepath.Join(fx.scriptsDir, "notes.txt"), "user work\n")

	result, err := fx.eng.StackUnapply(context.Background(), &StackUnapplyRequest{CWD: fx.repoRoot, Force: true})
	if err != nil {
		t.Fatalf("StackUnapply force: %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "scripts" {
		t.Fatalf("Removed = %v, want [scripts]", result.Removed)
	}
	if _, err := os.Stat(fx.scriptsDir); !os.IsNotExist(err) {
		t.Fatalf("scripts still exists after force stack unapply, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(fx.repoRoot, "active.yml")); err != nil {
		t.Fatalf("active-store file was removed: %v", err)
	}
	updated, err := fx.stateStore.LoadWorkspace(fx.workspaceID)
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	if _, ok := updated.Paths["scripts"]; ok {
		t.Fatal("workspaceState.Paths still contains force-removed scripts")
	}
	if _, ok := updated.Paths["active.yml"]; !ok {
		t.Fatal("workspaceState.Paths removed active.yml; want active-store path intact")
	}
}

func TestStackApply_CopyModeDirectoryRecordsLeafChecksums(t *testing.T) {
	repoRoot := t.TempDir()
	overlayRoot := filepath.Join(t.TempDir(), "overlay")
	writeOverlayFile(t, overlayRoot, filepath.Join("scripts", "init.sh"))
	writeOverlayFile(t, overlayRoot, filepath.Join("scripts", "utils", "helper.sh"))

	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{{Path: "scripts", Kind: "dir"}}
	storeRepo := &realOverlayStoreRepo{trackStoreRepo: newTrackStoreRepo(), overlayRoot: overlayRoot}
	storeRepo.tracks["stack-store"] = track

	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID("fp1", ".")
	ws := state.NewWorkspaceState("fp1", ".", "copy")
	ws.Stack = []string{"stack-store"}
	stateStore.workspaces[workspaceID] = ws

	eng := New(
		&trackGitRepo{root: repoRoot, fingerprint: "fp1", workspacePath: "."},
		storeRepo,
		stateStore,
		fsops.NewRealFS(),
		hash.NewSHA256Hasher(),
		&mockClock{},
		config.Paths{Root: filepath.Join(repoRoot, ".monodev"), Stores: filepath.Dir(overlayRoot), Workspaces: filepath.Join(repoRoot, ".state")},
	)

	if _, err := eng.StackApply(context.Background(), &StackApplyRequest{CWD: repoRoot, Mode: "copy"}); err != nil {
		t.Fatalf("StackApply: %v", err)
	}

	updated, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	ownership, ok := updated.Paths["scripts"]
	if !ok {
		t.Fatal("expected scripts ownership after stack apply")
	}
	if ownership.Contents == nil || len(ownership.Contents.Files) != 2 {
		t.Fatalf("Contents = %#v, want two recorded files", ownership.Contents)
	}

	result, err := eng.StackUnapply(context.Background(), &StackUnapplyRequest{CWD: repoRoot})
	if err != nil {
		t.Fatalf("StackUnapply: %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "scripts" {
		t.Fatalf("Removed = %v, want [scripts]", result.Removed)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "scripts")); !os.IsNotExist(err) {
		t.Fatalf("scripts still exists after stack unapply, err=%v", err)
	}
}
