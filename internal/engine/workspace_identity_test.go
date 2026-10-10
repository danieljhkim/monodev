package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/clock"
	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/gitx"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/lockfile"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	return dir
}

func setupGitRepoWithRemote(t *testing.T, remote string) string {
	t.Helper()
	dir := setupGitRepo(t)
	runGit(t, dir, "remote", "add", "origin", remote)
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func setupIdentityEngine(t *testing.T) (*Engine, *state.FileStateStore, string) {
	t.Helper()
	root := t.TempDir()
	workspaces := filepath.Join(root, "workspaces")
	storesDir := filepath.Join(root, "stores")
	if err := os.MkdirAll(workspaces, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(storesDir, 0700); err != nil {
		t.Fatal(err)
	}
	fs := fsops.NewRealFS()
	stateStore := state.NewFileStateStore(fs, workspaces)
	eng := New(
		gitx.NewRealGitRepo(),
		stores.NewFileStoreRepo(fs, storesDir),
		stateStore,
		fs,
		hash.NewSHA256Hasher(),
		clock.NewFakeClock(time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)),
		config.Paths{
			Root:       root,
			Stores:     storesDir,
			Workspaces: workspaces,
			Config:     filepath.Join(root, "config.yaml"),
		},
	)
	return eng, stateStore, workspaces
}

func TestLoadOrCreateWorkspaceState_MigratesLegacyID(t *testing.T) {
	repoDir := setupGitRepoWithRemote(t, "git@github.com:org/legacy.git")
	eng, stateStore, workspacesDir := setupIdentityEngine(t)

	absRoot, rawURL, err := gitx.NewRealGitRepo().GetFingerprintComponents(repoDir)
	if err != nil {
		t.Fatalf("GetFingerprintComponents: %v", err)
	}
	legacyFP := gitx.LegacyFingerprint(absRoot, rawURL)
	legacyID := state.ComputeWorkspaceID(legacyFP, ".")

	fixture, err := os.ReadFile(filepath.Join("testdata", "legacy_workspace.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	replaced := strings.NewReplacer(
		"LEGACY_FINGERPRINT", legacyFP,
		"LEGACY_ABSOLUTE_PATH", absRoot,
	).Replace(string(fixture))

	var ws state.WorkspaceState
	if err := json.Unmarshal([]byte(replaced), &ws); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	if err := stateStore.SaveWorkspace(legacyID, &ws); err != nil {
		t.Fatalf("save legacy workspace: %v", err)
	}

	loaded, currentID, err := eng.LoadOrCreateWorkspaceState(repoDir, mustFingerprint(t, repoDir), ".", "copy")
	if err != nil {
		t.Fatalf("LoadOrCreateWorkspaceState: %v", err)
	}
	if currentID == legacyID {
		t.Fatal("expected current workspace ID to differ from the legacy scheme")
	}
	if loaded.ActiveStore != "dev-store" {
		t.Errorf("ActiveStore = %q, want dev-store", loaded.ActiveStore)
	}
	if !loaded.Applied {
		t.Error("expected applied overlay ledger to be preserved")
	}
	if loaded.Paths["Makefile"].Store != "dev-store" {
		t.Errorf("paths ledger not preserved: %+v", loaded.Paths)
	}
	if _, err := os.Stat(filepath.Join(workspacesDir, legacyID+".json")); !os.IsNotExist(err) {
		t.Errorf("legacy workspace file still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspacesDir, currentID+".json")); err != nil {
		t.Errorf("migrated workspace file missing: %v", err)
	}
	if loaded.Repo != mustFingerprint(t, repoDir) {
		t.Errorf("Repo = %q, want current fingerprint", loaded.Repo)
	}
}

func TestWorkspaceRepair_ListAndRebind(t *testing.T) {
	repoDir := setupGitRepoWithRemote(t, "https://github.com/org/repair.git")
	eng, stateStore, workspacesDir := setupIdentityEngine(t)
	fp := mustFingerprint(t, repoDir)

	orphanFP := "pre-repair-fingerprint"
	orphanID := state.ComputeWorkspaceID(orphanFP, ".")
	orphan := state.NewWorkspaceState(orphanFP, ".", "copy")
	orphan.AbsolutePath = repoDir
	orphan.ActiveStore = "dev-store"
	orphan.Applied = true
	orphan.AddAppliedStore("dev-store", "copy")
	orphan.Paths["Makefile"] = state.PathOwnership{
		Store:     "dev-store",
		Type:      "copy",
		Timestamp: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		Checksum:  "abc123",
	}
	if err := stateStore.SaveWorkspace(orphanID, orphan); err != nil {
		t.Fatalf("save orphan: %v", err)
	}

	listed, err := eng.ListOrphanedWorkspaces(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("ListOrphanedWorkspaces: %v", err)
	}
	if len(listed.Orphans) != 1 {
		t.Fatalf("orphans = %d, want 1: %+v", len(listed.Orphans), listed.Orphans)
	}
	if listed.Orphans[0].WorkspaceID != orphanID {
		t.Errorf("orphan id = %s, want %s", listed.Orphans[0].WorkspaceID, orphanID)
	}
	if listed.Orphans[0].ActiveStore != "dev-store" {
		t.Errorf("orphan active store = %s", listed.Orphans[0].ActiveStore)
	}

	result, err := eng.RebindWorkspace(context.Background(), &RebindWorkspaceRequest{
		CWD:         repoDir,
		WorkspaceID: orphanID,
	})
	if err != nil {
		t.Fatalf("RebindWorkspace: %v", err)
	}
	wantID := state.ComputeWorkspaceID(fp, ".")
	if result.NewWorkspaceID != wantID {
		t.Errorf("NewWorkspaceID = %s, want %s", result.NewWorkspaceID, wantID)
	}
	if result.ActiveStore != "dev-store" {
		t.Errorf("ActiveStore = %s, want dev-store", result.ActiveStore)
	}
	if result.AppliedPaths != 1 {
		t.Errorf("AppliedPaths = %d, want 1", result.AppliedPaths)
	}

	rebound, err := stateStore.LoadWorkspace(wantID)
	if err != nil {
		t.Fatalf("load rebound workspace: %v", err)
	}
	if rebound.ActiveStore != "dev-store" || !rebound.Applied {
		t.Errorf("rebound state = %+v", rebound)
	}
	if rebound.Paths["Makefile"].Store != "dev-store" {
		t.Errorf("applied overlay ledger missing: %+v", rebound.Paths)
	}
	if _, err := os.Stat(filepath.Join(workspacesDir, orphanID+".json")); !os.IsNotExist(err) {
		t.Error("orphan file still present after rebind")
	}
}

func TestWorkspaceRepair_ExcludesForeignRepositoryLedgers(t *testing.T) {
	for _, workspacePath := range []string{".", "nested"} {
		for _, location := range []string{"existing", "missing", "legacy", "symlink", "matching-identity"} {
			t.Run(workspacePath+"/"+location, func(t *testing.T) {
				repoDir := setupGitRepoWithRemote(t, "https://github.com/org/current.git")
				foreignDir := setupGitRepoWithRemote(t, "https://github.com/org/foreign.git")
				for _, root := range []string{repoDir, foreignDir} {
					if err := os.MkdirAll(filepath.Join(root, workspacePath), 0700); err != nil {
						t.Fatal(err)
					}
				}
				eng, stateStore, workspacesDir := setupIdentityEngine(t)
				fp := mustFingerprint(t, repoDir)
				foreignFP := mustFingerprint(t, foreignDir)
				foreignID := state.ComputeWorkspaceID(foreignFP, workspacePath)
				foreign := state.NewWorkspaceState(foreignFP, workspacePath, "copy")
				switch location {
				case "existing":
					foreign.AbsolutePath = filepath.Join(foreignDir, workspacePath)
				case "matching-identity":
					foreign.AbsolutePath = filepath.Join(foreignDir, workspacePath)
					foreign.Repo = fp
				case "symlink":
					link := filepath.Join(repoDir, "foreign-link")
					if err := os.Symlink(foreignDir, link); err != nil {
						t.Fatal(err)
					}
					foreign.AbsolutePath = filepath.Join(link, workspacePath)
				case "missing":
					foreign.AbsolutePath = filepath.Join(foreignDir, "removed", workspacePath)
				}
				foreign.ActiveStore = "foreign-store"
				foreign.Applied = true
				foreign.AddAppliedStore("foreign-store", "copy")
				foreign.Paths["owned.txt"] = state.PathOwnership{Store: "foreign-store", Type: "copy", Checksum: "foreign"}
				if err := stateStore.SaveWorkspace(foreignID, foreign); err != nil {
					t.Fatal(err)
				}
				currentID := state.ComputeWorkspaceID(fp, workspacePath)
				current := state.NewWorkspaceState(fp, workspacePath, "copy")
				current.AbsolutePath = filepath.Join(repoDir, workspacePath)
				current.ActiveStore = "current-store"
				if err := stateStore.SaveWorkspace(currentID, current); err != nil {
					t.Fatal(err)
				}
				before := make(map[string][]byte)
				for _, id := range []string{foreignID, currentID} {
					data, err := os.ReadFile(filepath.Join(workspacesDir, id+".json"))
					if err != nil {
						t.Fatal(err)
					}
					before[id] = data
				}
				assertUnchanged := func() {
					t.Helper()
					for id, data := range before {
						after, err := os.ReadFile(filepath.Join(workspacesDir, id+".json"))
						if err != nil || !bytes.Equal(data, after) {
							t.Fatalf("ledger %s changed: error=%v\nbefore=%s\nafter=%s", id, err, data, after)
						}
					}
				}
				listed, err := eng.ListOrphanedWorkspaces(context.Background(), repoDir)
				if err != nil || len(listed.Orphans) != 0 {
					t.Fatalf("ListOrphanedWorkspaces = %+v, %v; want no foreign orphan", listed, err)
				}
				assertUnchanged()
				for _, force := range []bool{false, true} {
					_, err := eng.RebindWorkspace(context.Background(), &RebindWorkspaceRequest{
						CWD: repoDir, WorkspaceID: foreignID, Force: force,
					})
					if err == nil || !strings.Contains(err.Error(), "does not belong") {
						t.Fatalf("RebindWorkspace(force=%v) = %v, want foreign-repository refusal", force, err)
					}
					assertUnchanged()
				}
			})
		}
	}
}

func TestWorkspaceRepair_RecognizesLegacyAndMovedCloneIdentity(t *testing.T) {
	for _, workspacePath := range []string{".", "nested"} {
		for _, identity := range []string{"legacy", "remote", "moved"} {
			t.Run(workspacePath+"/"+identity, func(t *testing.T) {
				repoDir := setupGitRepoWithRemote(t, "https://github.com/org/repair-identity.git")
				if err := os.MkdirAll(filepath.Join(repoDir, workspacePath), 0700); err != nil {
					t.Fatal(err)
				}
				absRoot, rawURL, err := gitx.NewRealGitRepo().GetFingerprintComponents(repoDir)
				if err != nil {
					t.Fatal(err)
				}
				oldFP := gitx.LegacyFingerprint(absRoot, rawURL)
				oldPath := ""
				if identity != "legacy" {
					oldFP = mustFingerprint(t, repoDir)
					if identity == "moved" {
						oldPath = filepath.Join(repoDir, workspacePath)
						movedRoot := filepath.Join(t.TempDir(), "moved")
						if err := os.Rename(repoDir, movedRoot); err != nil {
							t.Fatal(err)
						}
						repoDir = movedRoot
					}
				}
				// First use adds a durable ID; the saved remote fingerprint remains
				// verifiable even if the clone's old display path has disappeared.
				if _, err := gitx.EnsureDurableRepoID(repoDir); err != nil {
					t.Fatal(err)
				}
				fp := mustFingerprint(t, repoDir)
				eng, stateStore, workspacesDir := setupIdentityEngine(t)
				oldID := state.ComputeWorkspaceID(oldFP, workspacePath)
				orphan := state.NewWorkspaceState(oldFP, workspacePath, "copy")
				orphan.AbsolutePath = oldPath
				orphan.ActiveStore = "dev-store"
				orphan.Applied = true
				orphan.AddAppliedStore("dev-store", "copy")
				orphan.Paths["owned.txt"] = state.PathOwnership{Store: "dev-store", Type: "copy", Checksum: "owned"}
				if err := stateStore.SaveWorkspace(oldID, orphan); err != nil {
					t.Fatal(err)
				}
				listed, err := eng.ListOrphanedWorkspaces(context.Background(), repoDir)
				if err != nil || len(listed.Orphans) != 1 || listed.Orphans[0].WorkspaceID != oldID {
					t.Fatalf("ListOrphanedWorkspaces = %+v, %v; want verified orphan %s", listed, err, oldID)
				}
				result, err := eng.RebindWorkspace(context.Background(), &RebindWorkspaceRequest{CWD: repoDir, WorkspaceID: oldID})
				if err != nil {
					t.Fatal(err)
				}
				newID := state.ComputeWorkspaceID(fp, workspacePath)
				if result.NewWorkspaceID != newID {
					t.Fatalf("new ID = %s, want %s", result.NewWorkspaceID, newID)
				}
				rebound, err := stateStore.LoadWorkspace(newID)
				if err != nil {
					t.Fatal(err)
				}
				if rebound.Repo != fp || rebound.AbsolutePath != filepath.Join(repoDir, workspacePath) ||
					rebound.ActiveStore != "dev-store" || !rebound.Applied || len(rebound.AppliedStores) != 1 ||
					rebound.Paths["owned.txt"].Checksum != "owned" {
					t.Fatalf("rebound ledger not preserved: %+v", rebound)
				}
				if _, err := os.Stat(filepath.Join(workspacesDir, oldID+".json")); !os.IsNotExist(err) {
					t.Fatalf("old ledger still present: %v", err)
				}
			})
		}
	}
}

// rebindTransitionStateStore runs beforeLock inside the first moment
// LockWorkspace is entered, before the underlying lock is acquired. That is
// the preflight-to-lock window RebindWorkspace must revalidate.
type rebindTransitionStateStore struct {
	*state.FileStateStore
	beforeLock func()
}

func (s *rebindTransitionStateStore) LockWorkspace(ctx context.Context, id string, mode lockfile.Mode) (*lockfile.Lock, error) {
	if mode == lockfile.Exclusive && s.beforeLock != nil {
		s.beforeLock()
	}
	return s.FileStateStore.LockWorkspace(ctx, id, mode)
}

func setupRebindTransitionEngine(t *testing.T, beforeLock func()) (*Engine, *state.FileStateStore, string) {
	t.Helper()
	root := t.TempDir()
	workspaces := filepath.Join(root, "workspaces")
	storesDir := filepath.Join(root, "stores")
	if err := os.MkdirAll(workspaces, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(storesDir, 0700); err != nil {
		t.Fatal(err)
	}
	fs := fsops.NewRealFS()
	stateStore := state.NewFileStateStore(fs, workspaces)
	eng := New(
		gitx.NewRealGitRepo(),
		stores.NewFileStoreRepo(fs, storesDir),
		&rebindTransitionStateStore{FileStateStore: stateStore, beforeLock: beforeLock},
		fs,
		hash.NewSHA256Hasher(),
		clock.NewFakeClock(time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)),
		config.Paths{
			Root:       root,
			Stores:     storesDir,
			Workspaces: workspaces,
			Config:     filepath.Join(root, "config.yaml"),
		},
	)
	return eng, stateStore, workspaces
}

func seedRebindOrphan(t *testing.T, stateStore *state.FileStateStore, repoDir string) (orphanID string, orphan *state.WorkspaceState) {
	t.Helper()
	orphanFP := "pre-repair-fingerprint"
	orphanID = state.ComputeWorkspaceID(orphanFP, ".")
	orphan = state.NewWorkspaceState(orphanFP, ".", "copy")
	orphan.AbsolutePath = repoDir
	orphan.ActiveStore = "old-store"
	orphan.Paths = map[string]state.PathOwnership{
		"old.txt": {Store: "old-store", Type: "copy", Checksum: "old"},
	}
	if err := stateStore.SaveWorkspace(orphanID, orphan); err != nil {
		t.Fatalf("save orphan: %v", err)
	}
	return orphanID, orphan
}

func TestRebindWorkspace_RefusesTargetCreatedBeforeLockWithoutForce(t *testing.T) {
	repoDir := setupGitRepoWithRemote(t, "https://github.com/org/repair-race.git")
	fp := mustFingerprint(t, repoDir)
	var stateStore *state.FileStateStore
	var orphanID string
	fired := false
	eng, stateStore, workspacesDir := setupRebindTransitionEngine(t, func() {
		if fired {
			return
		}
		fired = true
		targetID := state.ComputeWorkspaceID(fp, ".")
		target := state.NewWorkspaceState(fp, ".", "copy")
		target.AbsolutePath = repoDir
		target.ActiveStore = "concurrent-store"
		target.Paths = map[string]state.PathOwnership{
			"new.txt": {Store: "concurrent-store", Type: "copy", Checksum: "new"},
		}
		if err := stateStore.SaveWorkspace(targetID, target); err != nil {
			t.Fatalf("save concurrent target: %v", err)
		}
	})
	orphanID, _ = seedRebindOrphan(t, stateStore, repoDir)
	targetID := state.ComputeWorkspaceID(fp, ".")

	_, err := eng.RebindWorkspace(context.Background(), &RebindWorkspaceRequest{
		CWD:         repoDir,
		WorkspaceID: orphanID,
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("RebindWorkspace error = %v, want existing-target refusal", err)
	}
	if !fired {
		t.Fatal("rebind did not acquire the exclusive workspace lock")
	}

	target, err := stateStore.LoadWorkspace(targetID)
	if err != nil {
		t.Fatalf("load target: %v", err)
	}
	if target.ActiveStore != "concurrent-store" {
		t.Fatalf("target ActiveStore = %q, want concurrent-store", target.ActiveStore)
	}
	if _, ok := target.Paths["new.txt"]; !ok {
		t.Fatalf("concurrent target ledger was replaced: %+v", target.Paths)
	}
	if _, ok := target.Paths["old.txt"]; ok {
		t.Fatalf("stale source ledger overwrote the target: %+v", target.Paths)
	}

	source, err := stateStore.LoadWorkspace(orphanID)
	if err != nil {
		t.Fatalf("load source: %v", err)
	}
	if source.ActiveStore != "old-store" {
		t.Fatalf("source ActiveStore = %q, want old-store", source.ActiveStore)
	}
	if _, err := os.Stat(filepath.Join(workspacesDir, orphanID+".json")); err != nil {
		t.Fatalf("source record missing after refused rebind: %v", err)
	}
}

func TestRebindWorkspace_MigratesSourceUpdatedBeforeLock(t *testing.T) {
	repoDir := setupGitRepoWithRemote(t, "https://github.com/org/repair-source.git")
	fp := mustFingerprint(t, repoDir)
	var stateStore *state.FileStateStore
	var orphanID string
	fired := false
	eng, stateStore, workspacesDir := setupRebindTransitionEngine(t, func() {
		if fired {
			return
		}
		fired = true
		fresh, err := stateStore.LoadWorkspace(orphanID)
		if err != nil {
			t.Fatalf("reload source in hook: %v", err)
		}
		fresh.ActiveStoreScope = "global"
		fresh.Paths["extra.txt"] = state.PathOwnership{Store: "old-store", Type: "copy", Checksum: "extra"}
		if err := stateStore.SaveWorkspace(orphanID, fresh); err != nil {
			t.Fatalf("save fresh source: %v", err)
		}
	})
	orphanID, _ = seedRebindOrphan(t, stateStore, repoDir)
	currentID := state.ComputeWorkspaceID(fp, ".")

	result, err := eng.RebindWorkspace(context.Background(), &RebindWorkspaceRequest{
		CWD:         repoDir,
		WorkspaceID: orphanID,
	})
	if err != nil {
		t.Fatalf("RebindWorkspace: %v", err)
	}
	if !fired {
		t.Fatal("rebind did not acquire the exclusive workspace lock")
	}
	if result.NewWorkspaceID != currentID {
		t.Fatalf("NewWorkspaceID = %s, want %s", result.NewWorkspaceID, currentID)
	}
	if result.ActiveStore != "old-store" || result.AppliedPaths != 2 {
		t.Fatalf("result = %+v, want fresh source ledger", result)
	}

	rebound, err := stateStore.LoadWorkspace(currentID)
	if err != nil {
		t.Fatalf("load rebound workspace: %v", err)
	}
	if rebound.ActiveStoreScope != "global" {
		t.Fatalf("ActiveStoreScope = %q, want concurrent source scope", rebound.ActiveStoreScope)
	}
	if rebound.Paths["old.txt"].Checksum != "old" || rebound.Paths["extra.txt"].Checksum != "extra" {
		t.Fatalf("fresh source ledger not migrated: %+v", rebound.Paths)
	}
	if _, err := os.Stat(filepath.Join(workspacesDir, orphanID+".json")); !os.IsNotExist(err) {
		t.Fatal("orphan file still present after rebind")
	}
}

func TestRebindWorkspace_ForceUsesFreshSourceWithoutDiscardingConcurrentChanges(t *testing.T) {
	repoDir := setupGitRepoWithRemote(t, "https://github.com/org/repair-force.git")
	fp := mustFingerprint(t, repoDir)
	var stateStore *state.FileStateStore
	var orphanID string
	fired := false
	eng, stateStore, _ := setupRebindTransitionEngine(t, func() {
		if fired {
			return
		}
		fired = true
		fresh, err := stateStore.LoadWorkspace(orphanID)
		if err != nil {
			t.Fatalf("reload source in hook: %v", err)
		}
		fresh.ActiveStoreScope = "global"
		fresh.Paths["extra.txt"] = state.PathOwnership{Store: "old-store", Type: "copy", Checksum: "extra"}
		if err := stateStore.SaveWorkspace(orphanID, fresh); err != nil {
			t.Fatalf("save fresh source: %v", err)
		}
		targetID := state.ComputeWorkspaceID(fp, ".")
		target := state.NewWorkspaceState(fp, ".", "copy")
		target.AbsolutePath = repoDir
		target.ActiveStore = "concurrent-store"
		target.Applied = true
		target.Paths = map[string]state.PathOwnership{
			"new.txt": {Store: "concurrent-store", Type: "copy", Checksum: "new"},
		}
		if err := stateStore.SaveWorkspace(targetID, target); err != nil {
			t.Fatalf("save concurrent target: %v", err)
		}
	})
	orphanID, _ = seedRebindOrphan(t, stateStore, repoDir)
	currentID := state.ComputeWorkspaceID(fp, ".")

	result, err := eng.RebindWorkspace(context.Background(), &RebindWorkspaceRequest{
		CWD:         repoDir,
		WorkspaceID: orphanID,
		Force:       true,
	})
	if err != nil {
		t.Fatalf("RebindWorkspace: %v", err)
	}
	if !fired {
		t.Fatal("rebind did not acquire the exclusive workspace lock")
	}
	if result.ActiveStore != "old-store" || result.AppliedPaths != 2 {
		t.Fatalf("result = %+v, want fresh source rather than the stale snapshot or the target", result)
	}

	rebound, err := stateStore.LoadWorkspace(currentID)
	if err != nil {
		t.Fatalf("load rebound workspace: %v", err)
	}
	if rebound.ActiveStore != "old-store" || rebound.ActiveStoreScope != "global" {
		t.Fatalf("identity fields = store %q scope %q, want old-store/global", rebound.ActiveStore, rebound.ActiveStoreScope)
	}
	if rebound.Paths["old.txt"].Checksum != "old" || rebound.Paths["extra.txt"].Checksum != "extra" {
		t.Fatalf("concurrent source ledger discarded: %+v", rebound.Paths)
	}
	if _, ok := rebound.Paths["new.txt"]; ok {
		t.Fatalf("force kept the overwritten target path: %+v", rebound.Paths)
	}
	if rebound.Repo != fp {
		t.Fatalf("Repo = %q, want current fingerprint", rebound.Repo)
	}
}

func TestRebindWorkspace_RelocksWhenSourcePathChangesBeforeLock(t *testing.T) {
	repoDir := setupGitRepoWithRemote(t, "https://github.com/org/repair-relock.git")
	fp := mustFingerprint(t, repoDir)
	var stateStore *state.FileStateStore
	var orphanID string
	lockCalls := 0
	eng, stateStore, _ := setupRebindTransitionEngine(t, func() {
		lockCalls++
		switch lockCalls {
		case 1:
			fresh, err := stateStore.LoadWorkspace(orphanID)
			if err != nil {
				t.Fatalf("reload source in hook: %v", err)
			}
			fresh.WorkspacePath = "nested"
			if err := stateStore.SaveWorkspace(orphanID, fresh); err != nil {
				t.Fatalf("save relocated source: %v", err)
			}
		case 3:
			targetID := state.ComputeWorkspaceID(fp, "nested")
			target := state.NewWorkspaceState(fp, "nested", "copy")
			target.AbsolutePath = repoDir
			target.ActiveStore = "concurrent-store"
			target.Paths = map[string]state.PathOwnership{
				"new.txt": {Store: "concurrent-store", Type: "copy", Checksum: "new"},
			}
			if err := stateStore.SaveWorkspace(targetID, target); err != nil {
				t.Fatalf("save relocated target: %v", err)
			}
		}
	})
	orphanID, _ = seedRebindOrphan(t, stateStore, repoDir)
	targetID := state.ComputeWorkspaceID(fp, "nested")

	_, err := eng.RebindWorkspace(context.Background(), &RebindWorkspaceRequest{
		CWD:         repoDir,
		WorkspaceID: orphanID,
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("RebindWorkspace error = %v, want refusal of the target at the fresh path", err)
	}
	if lockCalls < 3 {
		t.Fatalf("lock acquisitions = %d, want a second lock set for the fresh target", lockCalls)
	}

	target, err := stateStore.LoadWorkspace(targetID)
	if err != nil {
		t.Fatalf("load relocated target: %v", err)
	}
	if target.ActiveStore != "concurrent-store" || target.Paths["new.txt"].Checksum != "new" {
		t.Fatalf("relocated target changed: %+v", target)
	}
	source, err := stateStore.LoadWorkspace(orphanID)
	if err != nil {
		t.Fatalf("load source: %v", err)
	}
	if source.WorkspacePath != "nested" || source.ActiveStore != "old-store" {
		t.Fatalf("source was migrated despite the fresh target: %+v", source)
	}
}

func TestUseStore_RestoresAfterSSHToHTTPS(t *testing.T) {
	repoDir := setupGitRepoWithRemote(t, "git@github.com:org/identity.git")
	eng, _, _ := setupIdentityEngine(t)
	ctx := context.Background()

	if err := eng.CreateStore(ctx, &CreateStoreRequest{
		CWD:     repoDir,
		StoreID: "dev-store",
		Name:    "Dev Store",
		Scope:   stores.ScopeGlobal,
	}); err != nil {
		t.Fatalf("CreateStore: %v", err)
	}

	before, err := eng.Status(ctx, &StatusRequest{CWD: repoDir})
	if err != nil {
		t.Fatalf("Status before: %v", err)
	}
	if before.ActiveStore != "dev-store" {
		t.Fatalf("ActiveStore before = %q", before.ActiveStore)
	}

	runGit(t, repoDir, "remote", "set-url", "origin", "https://github.com/org/identity.git")

	after, err := eng.Status(ctx, &StatusRequest{CWD: repoDir})
	if err != nil {
		t.Fatalf("Status after: %v", err)
	}
	if after.WorkspaceID != before.WorkspaceID {
		t.Errorf("workspace ID changed after remote rewrite: %s vs %s", before.WorkspaceID, after.WorkspaceID)
	}
	if after.ActiveStore != "dev-store" {
		t.Errorf("ActiveStore after = %q, want restored dev-store", after.ActiveStore)
	}
}

func mustFingerprint(t *testing.T, root string) string {
	t.Helper()
	fp, err := gitx.NewRealGitRepo().Fingerprint(root)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	return fp
}
