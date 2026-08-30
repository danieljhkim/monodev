package engine

import (
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

func TestUseStore_RestoresAfterSSHToHTTPS(t *testing.T) {
	repoDir := setupGitRepoWithRemote(t, "git@github.com:org/identity.git")
	eng, _, _ := setupIdentityEngine(t)
	ctx := context.Background()

	if err := eng.CreateStore(ctx, &CreateStoreRequest{
		CWD:     repoDir,
		StoreID: "dev-store",
		Name:    "Dev Store",
		Scope:   stores.ScopeGlobal,
		Owner:   "tester",
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
