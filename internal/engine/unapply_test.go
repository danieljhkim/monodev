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

func newUnapplyDriftEngine(gitRepo *trackGitRepo, stateStore *mockStateStore, fs *removeCapturingFS, hasher *hash.FakeHasher) *Engine {
	return New(
		gitRepo,
		newTrackStoreRepo(),
		stateStore,
		fs,
		hasher,
		&mockClock{},
		config.Paths{Root: "/tmp/monodev", Stores: "/tmp/monodev/stores", Workspaces: "/tmp/workspaces"},
	)
}

func TestUnapply_CopyModeChecksumDriftFailsWithoutForce(t *testing.T) {
	gitRepo := &trackGitRepo{root: "/repo", fingerprint: "fp1", workspacePath: "."}
	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID("fp1", ".")
	ws := state.NewWorkspaceState("fp1", ".", "copy")
	ws.Applied = true
	ws.ActiveStore = "active-store"
	ws.Paths["config.yml"] = state.PathOwnership{
		Store:    "active-store",
		Type:     "copy",
		Checksum: "original-checksum",
	}
	stateStore.workspaces[workspaceID] = ws

	fs := newRemoveCapturingFS("/repo/config.yml")
	hasher := hash.NewFakeHasher()
	hasher.SetHash("/repo/config.yml", "modified-checksum")
	eng := newUnapplyDriftEngine(gitRepo, stateStore, fs, hasher)

	result, err := eng.Unapply(context.Background(), &UnapplyRequest{
		CWD: "/repo",
	})

	if result != nil {
		t.Fatalf("Unapply result = %#v, want nil", result)
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Unapply error = %v, want ErrValidation", err)
	}
	if !errors.Is(err, ErrDrift) {
		t.Fatalf("Unapply error = %v, want ErrDrift", err)
	}
	errText := err.Error()
	if !strings.Contains(errText, "config.yml") {
		t.Fatalf("Unapply error = %q, want workspace-relative path", errText)
	}
	if !strings.Contains(errText, "local modifications detected") {
		t.Fatalf("Unapply error = %q, want local modifications message", errText)
	}
	if len(fs.removed) != 0 {
		t.Fatalf("RemoveAll calls = %v, want none", fs.removed)
	}
	if !fs.existingPaths["/repo/config.yml"] {
		t.Fatal("drifted workspace file was removed")
	}

	updated, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("failed to load workspace state: %v", err)
	}
	if _, ok := updated.Paths["config.yml"]; !ok {
		t.Fatal("workspaceState.Paths removed drifted config.yml; want entry intact")
	}
}

func TestStackUnapply_CopyModeChecksumDriftFailsWithoutForce(t *testing.T) {
	gitRepo := &trackGitRepo{root: "/repo", fingerprint: "fp1", workspacePath: "."}
	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID("fp1", ".")
	ws := state.NewWorkspaceState("fp1", ".", "copy")
	ws.Applied = true
	ws.ActiveStore = "active-store"
	ws.Stack = []string{"stack-store"}
	ws.Paths["stack/config.yml"] = state.PathOwnership{
		Store:    "stack-store",
		Type:     "copy",
		Checksum: "original-checksum",
	}
	ws.Paths["active.yml"] = state.PathOwnership{Store: "active-store", Type: "copy"}
	stateStore.workspaces[workspaceID] = ws

	fs := newRemoveCapturingFS("/repo/stack/config.yml", "/repo/active.yml")
	hasher := hash.NewFakeHasher()
	hasher.SetHash("/repo/stack/config.yml", "modified-checksum")
	eng := newUnapplyDriftEngine(gitRepo, stateStore, fs, hasher)

	result, err := eng.StackUnapply(context.Background(), &StackUnapplyRequest{
		CWD: "/repo",
	})

	if result != nil {
		t.Fatalf("StackUnapply result = %#v, want nil", result)
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("StackUnapply error = %v, want ErrValidation", err)
	}
	if !errors.Is(err, ErrDrift) {
		t.Fatalf("StackUnapply error = %v, want ErrDrift", err)
	}
	errText := err.Error()
	if !strings.Contains(errText, "stack/config.yml") {
		t.Fatalf("StackUnapply error = %q, want workspace-relative path", errText)
	}
	if !strings.Contains(errText, "local modifications detected") {
		t.Fatalf("StackUnapply error = %q, want local modifications message", errText)
	}
	if len(fs.removed) != 0 {
		t.Fatalf("RemoveAll calls = %v, want none", fs.removed)
	}
	if !fs.existingPaths["/repo/stack/config.yml"] {
		t.Fatal("drifted stack workspace file was removed")
	}

	updated, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("failed to load workspace state: %v", err)
	}
	if _, ok := updated.Paths["stack/config.yml"]; !ok {
		t.Fatal("workspaceState.Paths removed drifted stack/config.yml; want entry intact")
	}
}

func TestUnapply_ForceRemovesDriftedCopyAndUpdatesWorkspaceState(t *testing.T) {
	gitRepo := &trackGitRepo{root: "/repo", fingerprint: "fp1", workspacePath: "."}
	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID("fp1", ".")
	ws := state.NewWorkspaceState("fp1", ".", "copy")
	ws.Applied = true
	ws.ActiveStore = "active-store"
	ws.Stack = []string{"stack-store"}
	ws.Paths["config.yml"] = state.PathOwnership{
		Store:    "active-store",
		Type:     "copy",
		Checksum: "original-checksum",
	}
	ws.Paths["stack.yml"] = state.PathOwnership{Store: "stack-store", Type: "copy"}
	ws.AppliedStores = []state.AppliedStore{
		{Store: "active-store", Type: "copy"},
		{Store: "stack-store", Type: "copy"},
	}
	stateStore.workspaces[workspaceID] = ws

	fs := newRemoveCapturingFS("/repo/config.yml", "/repo/stack.yml")
	hasher := hash.NewFakeHasher()
	hasher.SetHash("/repo/config.yml", "modified-checksum")
	eng := newUnapplyDriftEngine(gitRepo, stateStore, fs, hasher)

	result, err := eng.Unapply(context.Background(), &UnapplyRequest{
		CWD:   "/repo",
		Force: true,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "config.yml" {
		t.Fatalf("Removed = %v, want [config.yml]", result.Removed)
	}
	if len(fs.removed) != 1 || fs.removed[0] != "/repo/config.yml" {
		t.Fatalf("RemoveAll calls = %v, want [/repo/config.yml]", fs.removed)
	}
	if fs.existingPaths["/repo/config.yml"] {
		t.Fatal("drifted workspace file still exists; want force to remove it")
	}

	updated, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("failed to load workspace state: %v", err)
	}
	if _, ok := updated.Paths["config.yml"]; ok {
		t.Fatal("workspaceState.Paths still contains force-removed config.yml")
	}
	if _, ok := updated.Paths["stack.yml"]; !ok {
		t.Fatal("workspaceState.Paths removed stack.yml; want stack entry intact")
	}
	if updated.GetAppliedStore("active-store") != nil {
		t.Fatal("AppliedStores still contains active-store; want pruned after force unapply")
	}
	if updated.GetAppliedStore("stack-store") == nil {
		t.Fatal("AppliedStores removed stack-store; want stack store retained")
	}
}

func TestUnapply_MissingCopyAndSymlinkPathsStillRemoveStateEntries(t *testing.T) {
	tests := []struct {
		name      string
		relPath   string
		ownership state.PathOwnership
		existing  []string
	}{
		{
			name:    "missing copy with checksum",
			relPath: "missing.yml",
			ownership: state.PathOwnership{
				Store:    "active-store",
				Type:     "copy",
				Checksum: "original-checksum",
			},
		},
		{
			name:    "symlink skips checksum drift validation",
			relPath: "linked.yml",
			ownership: state.PathOwnership{
				Store:    "active-store",
				Type:     "symlink",
				Checksum: "original-checksum",
			},
			existing: []string{"/repo/linked.yml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gitRepo := &trackGitRepo{root: "/repo", fingerprint: "fp1", workspacePath: "."}
			stateStore := newMockStateStore()
			workspaceID := state.ComputeWorkspaceID("fp1", ".")
			ws := state.NewWorkspaceState("fp1", ".", "copy")
			ws.Applied = true
			ws.ActiveStore = "active-store"
			ws.Stack = []string{"stack-store"}
			ws.Paths[tt.relPath] = tt.ownership
			ws.Paths["stack.yml"] = state.PathOwnership{Store: "stack-store", Type: "copy"}
			stateStore.workspaces[workspaceID] = ws

			fs := newRemoveCapturingFS(tt.existing...)
			hasher := hash.NewFakeHasher()
			hasher.SetHash("/repo/"+tt.relPath, "modified-checksum")
			eng := newUnapplyDriftEngine(gitRepo, stateStore, fs, hasher)

			result, err := eng.Unapply(context.Background(), &UnapplyRequest{
				CWD: "/repo",
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result.Removed) != 1 || result.Removed[0] != tt.relPath {
				t.Fatalf("Removed = %v, want [%s]", result.Removed, tt.relPath)
			}

			updated, err := stateStore.LoadWorkspace(workspaceID)
			if err != nil {
				t.Fatalf("failed to load workspace state: %v", err)
			}
			if _, ok := updated.Paths[tt.relPath]; ok {
				t.Fatalf("workspaceState.Paths still contains %s", tt.relPath)
			}
			if _, ok := updated.Paths["stack.yml"]; !ok {
				t.Fatal("workspaceState.Paths removed stack.yml; want unrelated entry intact")
			}
		})
	}
}

type copiedDirFixture struct {
	repoRoot    string
	scriptsDir  string
	workspaceID string
	stateStore  *mockStateStore
	eng         *Engine
}

func writeCopiedDirFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("failed to create parent for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

func setupCopiedDirectoryFixture(t *testing.T, ownerStore string, stackOwned bool) *copiedDirFixture {
	t.Helper()
	repoRoot := t.TempDir()
	scriptsDir := filepath.Join(repoRoot, "scripts")
	writeCopiedDirFile(t, filepath.Join(scriptsDir, "init.sh"), "echo init\n")
	writeCopiedDirFile(t, filepath.Join(scriptsDir, "utils", "helper.sh"), "echo helper\n")

	hasher := hash.NewSHA256Hasher()
	initHash, err := hasher.HashFile(filepath.Join(scriptsDir, "init.sh"))
	if err != nil {
		t.Fatalf("hash init.sh: %v", err)
	}
	helperHash, err := hasher.HashFile(filepath.Join(scriptsDir, "utils", "helper.sh"))
	if err != nil {
		t.Fatalf("hash helper.sh: %v", err)
	}

	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID("fp1", ".")
	ws := state.NewWorkspaceState("fp1", ".", "copy")
	ws.Applied = true
	ws.ActiveStore = "active-store"
	ws.Paths["scripts"] = state.PathOwnership{
		Store: ownerStore,
		Type:  "copy",
		Contents: &state.DirContents{Files: map[string]string{
			"init.sh":         initHash,
			"utils/helper.sh": helperHash,
		}},
	}
	ws.AppliedStores = []state.AppliedStore{{Store: ownerStore, Type: "copy"}}
	if stackOwned {
		ws.Stack = []string{ownerStore}
		ws.Paths["active.yml"] = state.PathOwnership{Store: "active-store", Type: "copy"}
		ws.AppliedStores = append(ws.AppliedStores, state.AppliedStore{Store: "active-store", Type: "copy"})
		writeCopiedDirFile(t, filepath.Join(repoRoot, "active.yml"), "active\n")
	}
	stateStore.workspaces[workspaceID] = ws

	eng := New(
		&trackGitRepo{root: repoRoot, fingerprint: "fp1", workspacePath: "."},
		newTrackStoreRepo(),
		stateStore,
		fsops.NewRealFS(),
		hasher,
		&mockClock{},
		config.Paths{Root: filepath.Join(repoRoot, ".monodev"), Stores: filepath.Join(repoRoot, "stores"), Workspaces: filepath.Join(repoRoot, ".state")},
	)
	return &copiedDirFixture{
		repoRoot:    repoRoot,
		scriptsDir:  scriptsDir,
		workspaceID: workspaceID,
		stateStore:  stateStore,
		eng:         eng,
	}
}

func assertCopiedDirDriftError(t *testing.T, err error, wantPaths ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want directory drift")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
	if !errors.Is(err, ErrDrift) {
		t.Fatalf("error = %v, want ErrDrift", err)
	}
	errText := err.Error()
	for _, path := range wantPaths {
		if !strings.Contains(errText, path) {
			t.Fatalf("error = %q, want path %q", errText, path)
		}
	}
	if !strings.Contains(errText, "--force") {
		t.Fatalf("error = %q, want --force guidance", errText)
	}
	if !strings.Contains(errText, "inspect") {
		t.Fatalf("error = %q, want inspect guidance", errText)
	}
}

func TestUnapply_CopiedDirectoryDriftFailsWithoutForce(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(t *testing.T, fx *copiedDirFixture)
		wantPaths []string
	}{
		{
			name: "added file",
			mutate: func(t *testing.T, fx *copiedDirFixture) {
				writeCopiedDirFile(t, filepath.Join(fx.scriptsDir, "notes.txt"), "user work\n")
			},
			wantPaths: []string{"scripts/notes.txt", "added"},
		},
		{
			name: "modified file",
			mutate: func(t *testing.T, fx *copiedDirFixture) {
				writeCopiedDirFile(t, filepath.Join(fx.scriptsDir, "init.sh"), "echo changed\n")
			},
			wantPaths: []string{"scripts/init.sh", "modified"},
		},
		{
			name: "deleted originally-applied file",
			mutate: func(t *testing.T, fx *copiedDirFixture) {
				if err := os.Remove(filepath.Join(fx.scriptsDir, "utils", "helper.sh")); err != nil {
					t.Fatalf("remove helper.sh: %v", err)
				}
			},
			wantPaths: []string{"scripts/utils/helper.sh", "deleted"},
		},
		{
			name: "added modified and deleted",
			mutate: func(t *testing.T, fx *copiedDirFixture) {
				writeCopiedDirFile(t, filepath.Join(fx.scriptsDir, "notes.txt"), "user work\n")
				writeCopiedDirFile(t, filepath.Join(fx.scriptsDir, "init.sh"), "echo changed\n")
				if err := os.Remove(filepath.Join(fx.scriptsDir, "utils", "helper.sh")); err != nil {
					t.Fatalf("remove helper.sh: %v", err)
				}
			},
			wantPaths: []string{"scripts/notes.txt", "scripts/init.sh", "scripts/utils/helper.sh", "added", "modified", "deleted"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := setupCopiedDirectoryFixture(t, "active-store", false)
			tt.mutate(t, fx)

			result, err := fx.eng.Unapply(context.Background(), &UnapplyRequest{CWD: fx.repoRoot})
			if result != nil {
				t.Fatalf("Unapply result = %#v, want nil", result)
			}
			assertCopiedDirDriftError(t, err, tt.wantPaths...)

			if _, err := os.Stat(filepath.Join(fx.scriptsDir, "init.sh")); err != nil {
				t.Fatalf("expected copied directory contents to remain, stat init.sh: %v", err)
			}
			updated, err := fx.stateStore.LoadWorkspace(fx.workspaceID)
			if err != nil {
				t.Fatalf("load workspace: %v", err)
			}
			if _, ok := updated.Paths["scripts"]; !ok {
				t.Fatal("workspaceState.Paths removed scripts; want drifted directory intact")
			}
		})
	}
}

func TestUnapply_UnchangedCopiedDirectoryRemovesCompletely(t *testing.T) {
	repoRoot := t.TempDir()
	overlayRoot := filepath.Join(t.TempDir(), "overlay")
	writeOverlayFile(t, overlayRoot, filepath.Join("scripts", "init.sh"))
	writeOverlayFile(t, overlayRoot, filepath.Join("scripts", "utils", "helper.sh"))

	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{{Path: "scripts", Kind: "dir"}}
	storeRepo := &realOverlayStoreRepo{trackStoreRepo: newTrackStoreRepo(), overlayRoot: overlayRoot}
	storeRepo.tracks["active-store"] = track

	stateStore := newMockStateStore()
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

	if _, err := eng.Apply(context.Background(), &ApplyRequest{CWD: repoRoot, StoreID: "active-store", Mode: "copy"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	workspaceID := state.ComputeWorkspaceID("fp1", ".")
	ws, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("load workspace after apply: %v", err)
	}
	ownership, ok := ws.Paths["scripts"]
	if !ok {
		t.Fatal("expected scripts ownership after apply")
	}
	if ownership.Contents == nil || len(ownership.Contents.Files) != 2 {
		t.Fatalf("Contents = %#v, want two recorded files", ownership.Contents)
	}
	if _, ok := ownership.Contents.Files["init.sh"]; !ok {
		t.Fatalf("Contents.Files = %#v, want init.sh", ownership.Contents.Files)
	}
	if _, ok := ownership.Contents.Files["utils/helper.sh"]; !ok {
		t.Fatalf("Contents.Files = %#v, want utils/helper.sh", ownership.Contents.Files)
	}

	result, err := eng.Unapply(context.Background(), &UnapplyRequest{CWD: repoRoot})
	if err != nil {
		t.Fatalf("Unapply: %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "scripts" {
		t.Fatalf("Removed = %v, want [scripts]", result.Removed)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "scripts")); !os.IsNotExist(err) {
		t.Fatalf("scripts still exists after unapply, err=%v", err)
	}
	if _, err := stateStore.LoadWorkspace(workspaceID); !os.IsNotExist(err) {
		t.Fatalf("expected workspace state deleted, err=%v", err)
	}
}

func TestUnapply_ForceRemovesDriftedCopiedDirectory(t *testing.T) {
	fx := setupCopiedDirectoryFixture(t, "active-store", false)
	writeCopiedDirFile(t, filepath.Join(fx.scriptsDir, "notes.txt"), "user work\n")

	result, err := fx.eng.Unapply(context.Background(), &UnapplyRequest{CWD: fx.repoRoot, Force: true})
	if err != nil {
		t.Fatalf("Unapply force: %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "scripts" {
		t.Fatalf("Removed = %v, want [scripts]", result.Removed)
	}
	if _, err := os.Stat(fx.scriptsDir); !os.IsNotExist(err) {
		t.Fatalf("scripts still exists after force unapply, err=%v", err)
	}
	if _, err := fx.stateStore.LoadWorkspace(fx.workspaceID); !os.IsNotExist(err) {
		t.Fatalf("expected workspace state deleted, err=%v", err)
	}
}

func TestUnapply_LegacyCopiedDirectoryWithoutManifestFailsWithoutForce(t *testing.T) {
	repoRoot := t.TempDir()
	scriptsDir := filepath.Join(repoRoot, "scripts")
	writeCopiedDirFile(t, filepath.Join(scriptsDir, "init.sh"), "echo init\n")
	writeCopiedDirFile(t, filepath.Join(scriptsDir, "notes.txt"), "user work\n")

	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID("fp1", ".")
	ws := state.NewWorkspaceState("fp1", ".", "copy")
	ws.Applied = true
	ws.ActiveStore = "active-store"
	ws.Paths["scripts"] = state.PathOwnership{
		Store: "active-store",
		Type:  "copy",
	}
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

	result, err := eng.Unapply(context.Background(), &UnapplyRequest{CWD: repoRoot})
	if result != nil {
		t.Fatalf("Unapply result = %#v, want nil", result)
	}
	assertCopiedDirDriftError(t, err, "scripts", "ownership manifest")
	if _, err := os.Stat(filepath.Join(scriptsDir, "notes.txt")); err != nil {
		t.Fatalf("expected legacy directory contents to remain: %v", err)
	}
	updated, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	if _, ok := updated.Paths["scripts"]; !ok {
		t.Fatal("workspaceState.Paths removed legacy scripts; want entry intact")
	}

	forceResult, err := eng.Unapply(context.Background(), &UnapplyRequest{CWD: repoRoot, Force: true})
	if err != nil {
		t.Fatalf("force unapply: %v", err)
	}
	if len(forceResult.Removed) != 1 || forceResult.Removed[0] != "scripts" {
		t.Fatalf("Removed = %v, want [scripts]", forceResult.Removed)
	}
	if _, err := os.Stat(scriptsDir); !os.IsNotExist(err) {
		t.Fatalf("scripts still exists after force unapply, err=%v", err)
	}
}
