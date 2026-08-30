package engine

import (
	"context"
	"errors"
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

func TestUnapply_RemovesOnlyRequestedStorePathsForSubdirectoryWorkspace(t *testing.T) {
	gitRepo := &trackGitRepo{
		root:          "/repo",
		fingerprint:   "fp1",
		workspacePath: "services/api",
	}
	storeRepo := newTrackStoreRepo()
	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID("fp1", "services/api")
	ws := state.NewWorkspaceState("fp1", "services/api", "copy")
	ws.ActiveStore = "active-store"
	ws.AppliedStores = []state.AppliedStore{
		{Store: "store-a", Type: "copy"},
		{Store: "active-store", Type: "copy"},
	}
	ws.Paths["config.yml"] = state.PathOwnership{Store: "store-a", Type: "copy"}
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

	result, err := eng.Unapply(context.Background(), &UnapplyRequest{
		CWD:      "/repo/services/api",
		StoreIDs: []string{"store-a"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "config.yml" {
		t.Fatalf("Removed = %v, want [config.yml]", result.Removed)
	}

	removedCalls := workspaceRemoveAllCalls(fs.removed)
	if len(removedCalls) != 1 {
		t.Fatalf("RemoveAll calls = %v, want one workspace call", removedCalls)
	}
	if got, want := removedCalls[0], "/repo/services/api/config.yml"; got != want {
		t.Fatalf("RemoveAll path = %q, want %q", got, want)
	}
	if !fs.existingPaths["/repo/config.yml"] {
		t.Fatal("repo-root config.yml was removed; want unrelated root file untouched")
	}
	if fs.existingPaths["/repo/services/api/config.yml"] {
		t.Fatal("workspace config.yml still exists; want store-a workspace file removed")
	}

	updated, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("failed to load workspace state: %v", err)
	}
	if _, ok := updated.Paths["config.yml"]; ok {
		t.Fatal("workspaceState.Paths still contains store-a config.yml")
	}
	if _, ok := updated.Paths["active.yml"]; !ok {
		t.Fatal("workspaceState.Paths removed active-store path; want only store-a path removed")
	}
}

func TestApply_RejectsSymlinkedParentOutsideWorkspaceForNamedStore(t *testing.T) {
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
	eng := New(
		&trackGitRepo{root: repoRoot, fingerprint: "fp1", workspacePath: "."},
		storeRepo,
		stateStore,
		fsops.NewRealFS(),
		&mockHasher{},
		&mockClock{},
		config.Paths{Root: filepath.Join(repoRoot, ".monodev"), Stores: filepath.Dir(overlayRoot), Workspaces: filepath.Join(repoRoot, ".state")},
	)

	_, err := eng.Apply(context.Background(), &ApplyRequest{CWD: repoRoot, StoreIDs: []string{"untrusted-store"}, Mode: "copy"})
	if err == nil || !strings.Contains(err.Error(), "symlinked destination ancestor") {
		t.Fatalf("Apply error = %v, want symlinked destination ancestor rejection", err)
	}
	entries, readErr := os.ReadDir(outside)
	if readErr != nil {
		t.Fatalf("failed to inspect outside directory: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("outside directory was mutated by apply: %v", entries)
	}
}

func TestUnapply_CopiedDirectoryDriftFailsWithoutForceForNamedStore(t *testing.T) {
	fx := setupCopiedDirectoryFixture(t, "stack-store", true)
	writeCopiedDirFile(t, filepath.Join(fx.scriptsDir, "notes.txt"), "user work\n")
	writeCopiedDirFile(t, filepath.Join(fx.scriptsDir, "init.sh"), "echo changed\n")
	if err := os.Remove(filepath.Join(fx.scriptsDir, "utils", "helper.sh")); err != nil {
		t.Fatalf("remove helper.sh: %v", err)
	}

	result, err := fx.eng.Unapply(context.Background(), &UnapplyRequest{CWD: fx.repoRoot, StoreIDs: []string{"stack-store"}})
	if result != nil {
		t.Fatalf("Unapply result = %#v, want nil", result)
	}
	assertCopiedDirDriftError(t, err, "scripts/notes.txt", "scripts/init.sh", "scripts/utils/helper.sh")
	if _, err := os.Stat(filepath.Join(fx.scriptsDir, "notes.txt")); err != nil {
		t.Fatalf("expected drifted directory to remain: %v", err)
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

func TestUnapply_ForceRemovesDriftedCopiedDirectoryForNamedStore(t *testing.T) {
	fx := setupCopiedDirectoryFixture(t, "stack-store", true)
	writeCopiedDirFile(t, filepath.Join(fx.scriptsDir, "notes.txt"), "user work\n")

	result, err := fx.eng.Unapply(context.Background(), &UnapplyRequest{CWD: fx.repoRoot, StoreIDs: []string{"stack-store"}, Force: true})
	if err != nil {
		t.Fatalf("Unapply force: %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "scripts" {
		t.Fatalf("Removed = %v, want [scripts]", result.Removed)
	}
	if _, err := os.Stat(fx.scriptsDir); !os.IsNotExist(err) {
		t.Fatalf("scripts still exists after force unapply, err=%v", err)
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

func TestApply_CopyModeDirectoryRecordsLeafChecksumsForNamedStore(t *testing.T) {
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
	eng := New(
		&trackGitRepo{root: repoRoot, fingerprint: "fp1", workspacePath: "."},
		storeRepo,
		stateStore,
		fsops.NewRealFS(),
		hash.NewSHA256Hasher(),
		&mockClock{},
		config.Paths{Root: filepath.Join(repoRoot, ".monodev"), Stores: filepath.Dir(overlayRoot), Workspaces: filepath.Join(repoRoot, ".state")},
	)

	if _, err := eng.Apply(context.Background(), &ApplyRequest{CWD: repoRoot, StoreIDs: []string{"stack-store"}, Mode: "copy"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	updated, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	ownership, ok := updated.Paths["scripts"]
	if !ok {
		t.Fatal("expected scripts ownership after apply")
	}
	if ownership.Contents == nil || len(ownership.Contents.Files) != 2 {
		t.Fatalf("Contents = %#v, want two recorded files", ownership.Contents)
	}

	result, err := eng.Unapply(context.Background(), &UnapplyRequest{CWD: repoRoot, StoreIDs: []string{"stack-store"}})
	if err != nil {
		t.Fatalf("Unapply: %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "scripts" {
		t.Fatalf("Removed = %v, want [scripts]", result.Removed)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "scripts")); !os.IsNotExist(err) {
		t.Fatalf("scripts still exists after unapply, err=%v", err)
	}
}

func TestApply_MultiStoreLaterStoreWinsPathConflicts(t *testing.T) {
	repoRoot := t.TempDir()
	storesRoot := t.TempDir()
	writeOverlayFile(t, filepath.Join(storesRoot, "store-a"), "shared.txt")
	writeOverlayFile(t, filepath.Join(storesRoot, "store-b"), "shared.txt")
	if err := os.WriteFile(filepath.Join(storesRoot, "store-a", "shared.txt"), []byte("from-a"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storesRoot, "store-b", "shared.txt"), []byte("from-b"), 0600); err != nil {
		t.Fatal(err)
	}

	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{{Path: "shared.txt", Kind: "file"}}
	storeRepo := newTrackStoreRepo()
	storeRepo.tracks["store-a"] = track
	storeRepo.tracks["store-b"] = track
	multi := &orderedOverlayStoreRepo{trackStoreRepo: storeRepo, roots: map[string]string{
		"store-a": filepath.Join(storesRoot, "store-a"),
		"store-b": filepath.Join(storesRoot, "store-b"),
	}}

	stateStore := newMockStateStore()
	eng := New(
		&trackGitRepo{root: repoRoot, fingerprint: "fp1", workspacePath: "."},
		multi,
		stateStore,
		fsops.NewRealFS(),
		hash.NewSHA256Hasher(),
		&mockClock{},
		config.Paths{Root: filepath.Join(repoRoot, ".monodev"), Stores: storesRoot, Workspaces: filepath.Join(repoRoot, ".state")},
	)

	if _, err := eng.Apply(context.Background(), &ApplyRequest{
		CWD:      repoRoot,
		Mode:     "copy",
		StoreIDs: []string{"store-a", "store-b"},
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(repoRoot, "shared.txt"))
	if err != nil {
		t.Fatalf("read shared.txt: %v", err)
	}
	if string(got) != "from-b" {
		t.Fatalf("shared.txt = %q, want from-b", got)
	}
	ws, err := stateStore.LoadWorkspace(state.ComputeWorkspaceID("fp1", "."))
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	if ws.Paths["shared.txt"].Store != "store-b" {
		t.Fatalf("owner = %q, want store-b", ws.Paths["shared.txt"].Store)
	}
	ids := ws.AppliedStoreIDs()
	if len(ids) != 1 || ids[0] != "store-b" {
		t.Fatalf("AppliedStores = %v, want [store-b]", ids)
	}
}

func TestApply_UnmanagedConflictRequiresForceAndDryRunDoesNotWrite(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "Makefile"), []byte("user"), 0600); err != nil {
		t.Fatal(err)
	}
	overlayRoot := filepath.Join(t.TempDir(), "overlay")
	writeOverlayFile(t, overlayRoot, "Makefile")
	if err := os.WriteFile(filepath.Join(overlayRoot, "Makefile"), []byte("store"), 0600); err != nil {
		t.Fatal(err)
	}
	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{{Path: "Makefile", Kind: "file"}}
	storeRepo := &realOverlayStoreRepo{trackStoreRepo: newTrackStoreRepo(), overlayRoot: overlayRoot}
	storeRepo.tracks["store-a"] = track
	stateStore := newMockStateStore()
	eng := New(
		&trackGitRepo{root: repoRoot, fingerprint: "fp1", workspacePath: "."},
		storeRepo,
		stateStore,
		fsops.NewRealFS(),
		hash.NewSHA256Hasher(),
		&mockClock{},
		config.Paths{Root: filepath.Join(repoRoot, ".monodev"), Stores: filepath.Dir(overlayRoot), Workspaces: filepath.Join(repoRoot, ".state")},
	)

	result, err := eng.Apply(context.Background(), &ApplyRequest{
		CWD:      repoRoot,
		Mode:     "copy",
		StoreIDs: []string{"store-a"},
		DryRun:   true,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("dry-run without force error = %v, want ErrConflict", err)
	}
	if result == nil || !result.Plan.HasConflicts() {
		t.Fatal("expected conflict plan on dry-run")
	}
	got, readErr := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if readErr != nil || string(got) != "user" {
		t.Fatalf("dry-run mutated Makefile: %q err=%v", got, readErr)
	}

	if _, err := eng.Apply(context.Background(), &ApplyRequest{
		CWD:      repoRoot,
		Mode:     "copy",
		StoreIDs: []string{"store-a"},
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("apply without force error = %v, want ErrConflict", err)
	}

	if _, err := eng.Apply(context.Background(), &ApplyRequest{
		CWD:      repoRoot,
		Mode:     "copy",
		StoreIDs: []string{"store-a"},
		Force:    true,
	}); err != nil {
		t.Fatalf("apply --force: %v", err)
	}
	got, err = os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "store" {
		t.Fatalf("Makefile = %q, want store after --force", got)
	}
}

type orderedOverlayStoreRepo struct {
	*trackStoreRepo
	roots map[string]string
}

func (r *orderedOverlayStoreRepo) OverlayRoot(id string) string {
	if root, ok := r.roots[id]; ok {
		return root
	}
	return r.trackStoreRepo.OverlayRoot(id)
}
