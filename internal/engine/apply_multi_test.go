package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
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

func TestApply_DirectoryAndDescendantEitherOrderAndSelectiveUnapply(t *testing.T) {
	for _, order := range [][]string{{"parent", "child"}, {"child", "parent"}} {
		t.Run(strings.Join(order, "-then-"), func(t *testing.T) {
			repoRoot, eng, stateStore := setupOverlappingStores(t)
			if _, err := eng.Apply(context.Background(), &ApplyRequest{
				CWD: repoRoot, Mode: "copy", StoreIDs: order, DryRun: true,
			}); err != nil {
				t.Fatalf("dry-run apply: %v", err)
			}
			if _, err := os.Stat(filepath.Join(repoRoot, "context")); !os.IsNotExist(err) {
				t.Fatalf("dry-run created context, stat err=%v", err)
			}

			if _, err := eng.Apply(context.Background(), &ApplyRequest{
				CWD: repoRoot, Mode: "copy", StoreIDs: order,
			}); err != nil {
				t.Fatalf("apply %v: %v", order, err)
			}
			assertOverlapApplied(t, repoRoot, stateStore)

			result, err := eng.Unapply(context.Background(), &UnapplyRequest{
				CWD: repoRoot, StoreIDs: []string{"parent"},
			})
			if err != nil {
				t.Fatalf("unapply parent: %v", err)
			}
			if len(result.Removed) != 1 || result.Removed[0] != "context" {
				t.Fatalf("Removed = %v, want [context]", result.Removed)
			}
			assertFileGone(t, filepath.Join(repoRoot, "context", "parent.txt"))
			assertFileGone(t, filepath.Join(repoRoot, "context", "sub"))
			assertFileContent(t, filepath.Join(repoRoot, "context", "child.txt"), "from-child")
			ws := loadHierarchyWorkspace(t, stateStore)
			if _, ok := ws.Paths["context"]; ok {
				t.Fatal("ledger still records context after unapply parent")
			}
			if ws.Paths["context/child.txt"].Store != "child" {
				t.Fatalf("child ownership = %+v, want child", ws.Paths["context/child.txt"])
			}
			if ids := ws.AppliedStoreIDs(); len(ids) != 1 || ids[0] != "child" {
				t.Fatalf("AppliedStores = %v, want [child]", ids)
			}

			if _, err := eng.Unapply(context.Background(), &UnapplyRequest{
				CWD: repoRoot, StoreIDs: []string{"child"},
			}); err != nil {
				t.Fatalf("unapply child: %v", err)
			}
			assertFileGone(t, filepath.Join(repoRoot, "context", "child.txt"))
			ws = loadHierarchyWorkspace(t, stateStore)
			if _, ok := ws.Paths["context/child.txt"]; ok {
				t.Fatal("ledger still records context/child.txt after unapply child")
			}
		})
	}

	t.Run("unapply child keeps parent directory", func(t *testing.T) {
		repoRoot, eng, stateStore := setupOverlappingStores(t)
		if _, err := eng.Apply(context.Background(), &ApplyRequest{
			CWD: repoRoot, Mode: "copy", StoreIDs: []string{"child", "parent"},
		}); err != nil {
			t.Fatalf("apply: %v", err)
		}
		if _, err := eng.Unapply(context.Background(), &UnapplyRequest{
			CWD: repoRoot, StoreIDs: []string{"child"},
		}); err != nil {
			t.Fatalf("unapply child: %v", err)
		}
		assertFileContent(t, filepath.Join(repoRoot, "context", "parent.txt"), "from-parent")
		assertFileContent(t, filepath.Join(repoRoot, "context", "sub", "extra.txt"), "extra")
		assertFileGone(t, filepath.Join(repoRoot, "context", "child.txt"))
		ws := loadHierarchyWorkspace(t, stateStore)
		ownership := ws.Paths["context"]
		if ownership.Store != "parent" || ownership.Contents == nil {
			t.Fatalf("parent ownership = %+v", ownership)
		}
		if _, ok := ownership.Contents.Files["child.txt"]; ok {
			t.Fatalf("parent manifest still lists child.txt: %+v", ownership.Contents.Files)
		}
		if _, err := eng.Unapply(context.Background(), &UnapplyRequest{
			CWD: repoRoot, StoreIDs: []string{"parent"},
		}); err != nil {
			t.Fatalf("unapply parent: %v", err)
		}
		assertFileGone(t, filepath.Join(repoRoot, "context"))
	})

	t.Run("legacy manifest including the nested file", func(t *testing.T) {
		repoRoot, eng, stateStore := setupOverlappingStores(t)
		if _, err := eng.Apply(context.Background(), &ApplyRequest{
			CWD: repoRoot, Mode: "copy", StoreIDs: []string{"parent", "child"},
		}); err != nil {
			t.Fatalf("apply: %v", err)
		}
		ws := loadHierarchyWorkspace(t, stateStore)
		childHash, err := hash.NewSHA256Hasher().HashFile(filepath.Join(repoRoot, "context", "child.txt"))
		if err != nil {
			t.Fatal(err)
		}
		ws.Paths["context"].Contents.Files["child.txt"] = childHash
		if _, err := eng.Unapply(context.Background(), &UnapplyRequest{
			CWD: repoRoot, StoreIDs: []string{"parent"},
		}); err != nil {
			t.Fatalf("unapply parent with legacy manifest: %v", err)
		}
		assertFileContent(t, filepath.Join(repoRoot, "context", "child.txt"), "from-child")
		updated := loadHierarchyWorkspace(t, stateStore)
		if _, ok := updated.Paths["context"]; ok {
			t.Fatal("ledger still records parent directory")
		}
		if updated.Paths["context/child.txt"].Store != "child" {
			t.Fatal("child path missing from ledger")
		}
	})

	t.Run("parent drift does not remove the nested store", func(t *testing.T) {
		repoRoot, eng, stateStore := setupOverlappingStores(t)
		if _, err := eng.Apply(context.Background(), &ApplyRequest{
			CWD: repoRoot, Mode: "copy", StoreIDs: []string{"parent", "child"},
		}); err != nil {
			t.Fatalf("apply: %v", err)
		}
		parentPath := filepath.Join(repoRoot, "context", "parent.txt")
		if err := os.WriteFile(parentPath, []byte("local edit"), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := eng.Unapply(context.Background(), &UnapplyRequest{
			CWD: repoRoot, StoreIDs: []string{"parent"},
		})
		if !errors.Is(err, ErrDrift) {
			t.Fatalf("unapply parent = %v, want drift", err)
		}
		assertFileContent(t, parentPath, "local edit")
		assertFileContent(t, filepath.Join(repoRoot, "context", "child.txt"), "from-child")
		ws := loadHierarchyWorkspace(t, stateStore)
		if ws.Paths["context"].Store != "parent" || ws.Paths["context/child.txt"].Store != "child" {
			t.Fatalf("ledger changed after refused unapply: %+v", ws.Paths)
		}

		if _, err := eng.Unapply(context.Background(), &UnapplyRequest{
			CWD: repoRoot, StoreIDs: []string{"parent"}, Force: true,
		}); err != nil {
			t.Fatalf("force unapply parent: %v", err)
		}
		assertFileGone(t, parentPath)
		assertFileContent(t, filepath.Join(repoRoot, "context", "child.txt"), "from-child")
		ws = loadHierarchyWorkspace(t, stateStore)
		if _, ok := ws.Paths["context"]; ok {
			t.Fatal("force unapply left the parent directory in the ledger")
		}
		if ws.Paths["context/child.txt"].Store != "child" {
			t.Fatal("force unapply dropped the child path")
		}
	})
}

func TestApply_UnsafeHierarchyConflictDoesNotMutate(t *testing.T) {
	t.Run("symlink nested overlap", func(t *testing.T) {
		repoRoot, eng, stateStore := setupOverlappingStores(t)
		for _, order := range [][]string{{"parent", "child"}, {"child", "parent"}} {
			_, err := eng.Apply(context.Background(), &ApplyRequest{
				CWD: repoRoot, Mode: "symlink", StoreIDs: order, Force: true,
			})
			if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "symlink apply cannot share") {
				t.Fatalf("apply %v error = %v, want blocking symlink conflict", order, err)
			}
		}
		if _, err := os.Stat(filepath.Join(repoRoot, "context")); !os.IsNotExist(err) {
			t.Fatalf("symlink conflict mutated context, stat err=%v", err)
		}
		if _, err := stateStore.LoadWorkspace(state.ComputeWorkspaceID("fp1", ".")); !os.IsNotExist(err) {
			t.Fatalf("symlink conflict persisted workspace state, err=%v", err)
		}
	})

	t.Run("file overlapping a nested path", func(t *testing.T) {
		repoRoot, eng, stateStore := setupFileOverlappingChild(t)
		for _, order := range [][]string{{"parent", "child"}, {"child", "parent"}} {
			_, err := eng.Apply(context.Background(), &ApplyRequest{
				CWD: repoRoot, Mode: "copy", StoreIDs: order, Force: true,
			})
			if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "only a copied directory") {
				t.Fatalf("apply %v error = %v, want blocking file overlap", order, err)
			}
		}
		if _, err := os.Stat(filepath.Join(repoRoot, "context")); !os.IsNotExist(err) {
			t.Fatalf("file overlap mutated context, stat err=%v", err)
		}
		if _, err := stateStore.LoadWorkspace(state.ComputeWorkspaceID("fp1", ".")); !os.IsNotExist(err) {
			t.Fatalf("file overlap persisted workspace state, err=%v", err)
		}
	})
}

func TestUnapply_NonDirectoryOverlapConflictsBeforeMutation(t *testing.T) {
	repoRoot := t.TempDir()
	writeCopiedDirFile(t, filepath.Join(repoRoot, "context"), "parent-file")
	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID("fp1", ".")
	ws := state.NewWorkspaceState("fp1", ".", "copy")
	ws.Applied = true
	ws.Paths["context"] = state.PathOwnership{Store: "parent", Type: "copy", Checksum: ""}
	ws.Paths["context/child.txt"] = state.PathOwnership{Store: "child", Type: "copy", Checksum: ""}
	ws.AppliedStores = []state.AppliedStore{{Store: "parent", Type: "copy"}, {Store: "child", Type: "copy"}}
	stateStore.workspaces[workspaceID] = ws
	eng := New(
		&trackGitRepo{root: repoRoot, fingerprint: "fp1", workspacePath: "."},
		newTrackStoreRepo(),
		stateStore,
		fsops.NewRealFS(),
		hash.NewSHA256Hasher(),
		&mockClock{},
		config.Paths{Root: filepath.Join(repoRoot, ".monodev"), Stores: filepath.Join(repoRoot, "stores"), Workspaces: filepath.Join(repoRoot, ".state")},
	)

	_, err := eng.Unapply(context.Background(), &UnapplyRequest{CWD: repoRoot, StoreIDs: []string{"parent"}, Force: true})
	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "still owned by child") {
		t.Fatalf("unapply = %v, want conflict naming the child store", err)
	}
	assertFileContent(t, filepath.Join(repoRoot, "context"), "parent-file")
	updated, loadErr := stateStore.LoadWorkspace(workspaceID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if updated.Paths["context"].Store != "parent" || updated.Paths["context/child.txt"].Store != "child" {
		t.Fatalf("ledger changed: %+v", updated.Paths)
	}
}

func setupOverlappingStores(t *testing.T) (string, *Engine, *mockStateStore) {
	t.Helper()
	repoRoot := t.TempDir()
	storesRoot := t.TempDir()
	parentRoot := filepath.Join(storesRoot, "parent")
	childRoot := filepath.Join(storesRoot, "child")
	writeOverlayBytes(t, parentRoot, filepath.Join("context", "parent.txt"), "from-parent")
	writeOverlayBytes(t, parentRoot, filepath.Join("context", "sub", "extra.txt"), "extra")
	writeOverlayBytes(t, childRoot, filepath.Join("context", "child.txt"), "from-child")

	parentTrack := stores.NewTrackFile()
	parentTrack.Tracked = []stores.TrackedPath{{Path: "context", Kind: "dir"}}
	childTrack := stores.NewTrackFile()
	childTrack.Tracked = []stores.TrackedPath{{Path: "context/child.txt", Kind: "file"}}
	storeRepo := newTrackStoreRepo()
	storeRepo.tracks["parent"] = parentTrack
	storeRepo.tracks["child"] = childTrack
	eng, stateStore := newOrderedApplyEngine(t, repoRoot, storesRoot, storeRepo, map[string]string{
		"parent": parentRoot,
		"child":  childRoot,
	})
	return repoRoot, eng, stateStore
}

func setupFileOverlappingChild(t *testing.T) (string, *Engine, *mockStateStore) {
	t.Helper()
	repoRoot := t.TempDir()
	storesRoot := t.TempDir()
	parentRoot := filepath.Join(storesRoot, "parent")
	childRoot := filepath.Join(storesRoot, "child")
	writeOverlayBytes(t, parentRoot, "context", "from-parent")
	writeOverlayBytes(t, childRoot, filepath.Join("context", "child.txt"), "from-child")

	parentTrack := stores.NewTrackFile()
	parentTrack.Tracked = []stores.TrackedPath{{Path: "context", Kind: "file"}}
	childTrack := stores.NewTrackFile()
	childTrack.Tracked = []stores.TrackedPath{{Path: "context/child.txt", Kind: "file"}}
	storeRepo := newTrackStoreRepo()
	storeRepo.tracks["parent"] = parentTrack
	storeRepo.tracks["child"] = childTrack
	eng, stateStore := newOrderedApplyEngine(t, repoRoot, storesRoot, storeRepo, map[string]string{
		"parent": parentRoot,
		"child":  childRoot,
	})
	return repoRoot, eng, stateStore
}

func newOrderedApplyEngine(t *testing.T, repoRoot, storesRoot string, storeRepo *trackStoreRepo, roots map[string]string) (*Engine, *mockStateStore) {
	t.Helper()
	stateStore := newMockStateStore()
	eng := New(
		&trackGitRepo{root: repoRoot, fingerprint: "fp1", workspacePath: "."},
		&orderedOverlayStoreRepo{trackStoreRepo: storeRepo, roots: roots},
		stateStore,
		fsops.NewRealFS(),
		hash.NewSHA256Hasher(),
		&mockClock{},
		config.Paths{Root: filepath.Join(repoRoot, ".monodev"), Stores: storesRoot, Workspaces: filepath.Join(repoRoot, ".state")},
	)
	return eng, stateStore
}

func writeOverlayBytes(t *testing.T, overlayRoot, relPath, content string) {
	t.Helper()
	path := filepath.Join(overlayRoot, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir overlay parent: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write overlay file: %v", err)
	}
}

func assertOverlapApplied(t *testing.T, repoRoot string, stateStore *mockStateStore) {
	t.Helper()
	assertFileContent(t, filepath.Join(repoRoot, "context", "parent.txt"), "from-parent")
	assertFileContent(t, filepath.Join(repoRoot, "context", "sub", "extra.txt"), "extra")
	assertFileContent(t, filepath.Join(repoRoot, "context", "child.txt"), "from-child")
	ws := loadHierarchyWorkspace(t, stateStore)
	parent := ws.Paths["context"]
	child := ws.Paths["context/child.txt"]
	if parent.Store != "parent" || child.Store != "child" {
		t.Fatalf("ownership = parent:%+v child:%+v", parent, child)
	}
	if parent.Contents == nil {
		t.Fatal("parent directory has no manifest")
	}
	if _, ok := parent.Contents.Files["child.txt"]; ok {
		t.Fatalf("parent manifest includes child.txt: %+v", parent.Contents.Files)
	}
	if parent.Contents.Files["parent.txt"] == "" || parent.Contents.Files["sub/extra.txt"] == "" {
		t.Fatalf("parent manifest = %+v, want parent.txt and sub/extra.txt", parent.Contents.Files)
	}
	ids := append([]string{}, ws.AppliedStoreIDs()...)
	sort.Strings(ids)
	if len(ids) != 2 || ids[0] != "child" || ids[1] != "parent" {
		t.Fatalf("AppliedStores = %v, want parent and child", ws.AppliedStoreIDs())
	}
}

func loadHierarchyWorkspace(t *testing.T, stateStore *mockStateStore) *state.WorkspaceState {
	t.Helper()
	ws, err := stateStore.LoadWorkspace(state.ComputeWorkspaceID("fp1", "."))
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	return ws
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func assertFileGone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s still exists, stat err=%v", path, err)
	}
}
