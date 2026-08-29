package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/clock"
	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/engine"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/gitx"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/state"
)

func assertRemovedOrder(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %d paths removed, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("removed[%d] = %q, want %q; full order: %v", i, got[i], want[i], got)
		}
	}
}

func assertWorkspaceDeleted(t *testing.T, stateStore *testStateStore, workspaceID string) {
	t.Helper()
	_, err := stateStore.LoadWorkspace(workspaceID)
	if !os.IsNotExist(err) {
		t.Fatalf("expected workspace state to be deleted, got err=%v", err)
	}
}

func TestUnapply_DeepestFirstRemoval(t *testing.T) {
	eng, fs, stateStore, _, _ := setupTestEngine(t)
	ctx := context.Background()

	// Setup workspace state with nested paths
	// Compute workspace ID the same way the engine does
	workspaceID := state.ComputeWorkspaceID("repo-fingerprint-123", "workspace")
	workspaceState := state.NewWorkspaceState("repo-fingerprint-123", "workspace", "symlink")
	workspaceState.Applied = true
	workspaceState.ActiveStore = "store1" // Set active store so unapply knows which paths to remove
	workspaceState.Paths = map[string]state.PathOwnership{
		"scripts": {
			Store: "store1",
			Type:  "symlink",
		},
		"scripts/init.sh": {
			Store: "store1",
			Type:  "symlink",
		},
		"scripts/utils": {
			Store: "store1",
			Type:  "symlink",
		},
		"scripts/utils/helper.sh": {
			Store: "store1",
			Type:  "symlink",
		},
		"Makefile": {
			Store: "store1",
			Type:  "symlink",
		},
	}
	_ = stateStore.SaveWorkspace(workspaceID, workspaceState)

	// Create files in filesystem
	cwd := "/repo/workspace"
	fs.symlinks[filepath.Join(cwd, "Makefile")] = "/store1/Makefile"
	fs.symlinks[filepath.Join(cwd, "scripts")] = "/store1/scripts"
	fs.symlinks[filepath.Join(cwd, "scripts/init.sh")] = "/store1/scripts/init.sh"
	fs.symlinks[filepath.Join(cwd, "scripts/utils")] = "/store1/scripts/utils"
	fs.symlinks[filepath.Join(cwd, "scripts/utils/helper.sh")] = "/store1/scripts/utils/helper.sh"

	// Unapply
	req := &engine.UnapplyRequest{
		CWD: cwd,
	}

	result, err := eng.Unapply(ctx, req)
	if err != nil {
		t.Fatalf("Unapply() error = %v", err)
	}

	expectedPaths := []string{"scripts/utils/helper.sh", "scripts/utils", "scripts/init.sh", "scripts", "Makefile"}
	assertRemovedOrder(t, result.Removed, expectedPaths)

	// Verify files were removed from filesystem
	for _, path := range expectedPaths {
		absPath := filepath.Join(cwd, path)
		if _, ok := fs.symlinks[absPath]; ok {
			t.Errorf("expected %q to be removed from filesystem", absPath)
		}
	}

	// Verify workspace state was deleted (all paths removed)
	assertWorkspaceDeleted(t, stateStore, workspaceID)
}

func TestUnapply_StateCleanup(t *testing.T) {
	eng, fs, stateStore, _, _ := setupTestEngine(t)
	ctx := context.Background()

	// Setup workspace state
	workspaceID := state.ComputeWorkspaceID("repo-fingerprint-123", "workspace")
	workspaceState := state.NewWorkspaceState("repo-fingerprint-123", "workspace", "symlink")
	workspaceState.Applied = true
	workspaceState.ActiveStore = "store1" // Set active store
	workspaceState.Paths = map[string]state.PathOwnership{
		"file1.txt": {
			Store: "store1",
			Type:  "symlink",
		},
		"file2.txt": {
			Store: "store1",
			Type:  "symlink",
		},
	}
	_ = stateStore.SaveWorkspace(workspaceID, workspaceState)

	cwd := "/repo/workspace"
	fs.symlinks[filepath.Join(cwd, "file1.txt")] = "/store1/file1.txt"
	fs.symlinks[filepath.Join(cwd, "file2.txt")] = "/store1/file2.txt"

	// Unapply
	req := &engine.UnapplyRequest{
		CWD: cwd,
	}

	result, err := eng.Unapply(ctx, req)
	if err != nil {
		t.Fatalf("Unapply() error = %v", err)
	}

	assertRemovedOrder(t, result.Removed, []string{"file2.txt", "file1.txt"})
	for _, path := range result.Removed {
		if _, ok := fs.symlinks[filepath.Join(cwd, path)]; ok {
			t.Errorf("expected %q to be removed from filesystem", path)
		}
	}

	// Verify workspace state was deleted (all paths removed)
	assertWorkspaceDeleted(t, stateStore, workspaceID)
}

func TestUnapply_PartialRemoval(t *testing.T) {
	eng, fs, stateStore, _, _ := setupTestEngine(t)
	ctx := context.Background()

	// Setup workspace state with paths from multiple stores
	// Active store is "store1", stack has "store2"
	workspaceID := state.ComputeWorkspaceID("repo-fingerprint-123", "workspace")
	workspaceState := state.NewWorkspaceState("repo-fingerprint-123", "workspace", "symlink")
	workspaceState.Applied = true
	workspaceState.ActiveStore = "store1"     // Active store
	workspaceState.Stack = []string{"store2"} // Stack store
	workspaceState.Paths = map[string]state.PathOwnership{
		"file1.txt": {
			Store: "store1", // From active store
			Type:  "symlink",
		},
		"file2.txt": {
			Store: "store2", // From stack store
			Type:  "symlink",
		},
	}
	_ = stateStore.SaveWorkspace(workspaceID, workspaceState)

	cwd := "/repo/workspace"
	fs.symlinks[filepath.Join(cwd, "file1.txt")] = "/store1/file1.txt"
	fs.symlinks[filepath.Join(cwd, "file2.txt")] = "/store2/file2.txt"

	// Unapply (removes only active store paths, not stack paths)
	req := &engine.UnapplyRequest{
		CWD: cwd,
	}

	result, err := eng.Unapply(ctx, req)
	if err != nil {
		t.Fatalf("Unapply() error = %v", err)
	}

	assertRemovedOrder(t, result.Removed, []string{"file1.txt"})
	if _, ok := fs.symlinks[filepath.Join(cwd, "file1.txt")]; ok {
		t.Error("expected active store path file1.txt to be removed from filesystem")
	}
	if _, ok := fs.symlinks[filepath.Join(cwd, "file2.txt")]; !ok {
		t.Error("expected stack-owned path file2.txt to remain in filesystem")
	}

	// Verify workspace state still exists (stack path remains)
	updatedState, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("expected workspace state to exist (stack path remains): %v", err)
	}

	// Verify only stack path remains
	if len(updatedState.Paths) != 1 {
		t.Errorf("expected 1 path remaining (stack path), got %d", len(updatedState.Paths))
	}
	if _, exists := updatedState.Paths["file2.txt"]; !exists {
		t.Error("expected file2.txt (stack path) to remain in workspace state")
	}
	if !updatedState.Applied {
		t.Error("expected workspace state to remain applied while stack-owned paths exist")
	}
}

func TestUnapply_DryRun(t *testing.T) {
	eng, fs, stateStore, _, _ := setupTestEngine(t)
	ctx := context.Background()

	// Setup workspace state
	workspaceID := state.ComputeWorkspaceID("repo-fingerprint-123", "workspace")
	workspaceState := state.NewWorkspaceState("repo-fingerprint-123", "workspace", "symlink")
	workspaceState.Applied = true
	workspaceState.ActiveStore = "store1" // Set active store
	workspaceState.Paths = map[string]state.PathOwnership{
		"test.txt": {
			Store: "store1",
			Type:  "symlink",
		},
	}
	_ = stateStore.SaveWorkspace(workspaceID, workspaceState)

	cwd := "/repo/workspace"
	fs.symlinks[filepath.Join(cwd, "test.txt")] = "/store1/test.txt"

	// Dry run unapply
	req := &engine.UnapplyRequest{
		CWD:    cwd,
		DryRun: true,
	}

	result, err := eng.Unapply(ctx, req)
	if err != nil {
		t.Fatalf("Unapply() error = %v", err)
	}

	// Verify paths that would be removed are listed
	if len(result.Removed) != 1 {
		t.Errorf("expected 1 path in removed list, got %d", len(result.Removed))
	}

	if result.Removed[0] != "test.txt" {
		t.Errorf("expected 'test.txt' in removed list, got %q", result.Removed[0])
	}

	// Verify files were NOT removed from filesystem (dry-run)
	if _, ok := fs.symlinks[filepath.Join(cwd, "test.txt")]; !ok {
		t.Error("expected file to still exist in dry-run mode")
	}

	// Verify workspace state was NOT deleted (dry-run)
	_, err = stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Errorf("expected workspace state to still exist in dry-run mode: %v", err)
	}
}

func TestUnapply_DriftDetection(t *testing.T) {
	const (
		cwd              = "/repo/workspace"
		relPath          = "config.json"
		originalChecksum = "original-hash"
		modifiedContent  = `{"modified": true}`
	)

	setupDriftedCopy := func(t *testing.T) (*engine.Engine, *testFS, *testStateStore, string, string) {
		t.Helper()

		eng, fs, stateStore, _, hasher := setupTestEngine(t)
		workspaceID := state.ComputeWorkspaceID("repo-fingerprint-123", "workspace")
		workspaceState := state.NewWorkspaceState("repo-fingerprint-123", "workspace", "copy")
		workspaceState.Applied = true
		workspaceState.ActiveStore = "store1"
		workspaceState.Paths = map[string]state.PathOwnership{
			relPath: {
				Store:    "store1",
				Type:     "copy",
				Checksum: originalChecksum,
			},
		}
		_ = stateStore.SaveWorkspace(workspaceID, workspaceState)

		configPath := filepath.Join(cwd, relPath)
		fs.files[configPath] = []byte(modifiedContent)
		hasher.SetHash(configPath, "modified-hash")

		return eng, fs, stateStore, workspaceID, configPath
	}

	t.Run("non-force protects drifted copy", func(t *testing.T) {
		eng, fs, stateStore, workspaceID, configPath := setupDriftedCopy(t)

		result, err := eng.Unapply(context.Background(), &engine.UnapplyRequest{
			CWD:   cwd,
			Force: false,
		})

		if result != nil {
			t.Fatalf("Unapply() result = %#v, want nil", result)
		}
		if err == nil {
			t.Fatal("Unapply() error = nil, want drift validation error")
		}
		if !errors.Is(err, engine.ErrValidation) {
			t.Fatalf("Unapply() error = %v, want ErrValidation", err)
		}
		if !errors.Is(err, engine.ErrDrift) {
			t.Fatalf("Unapply() error = %v, want ErrDrift", err)
		}
		errText := err.Error()
		if !strings.Contains(errText, relPath) {
			t.Fatalf("Unapply() error = %q, want workspace-relative path", errText)
		}
		if !strings.Contains(errText, "local modifications detected") {
			t.Fatalf("Unapply() error = %q, want local modifications message", errText)
		}

		content, ok := fs.files[configPath]
		if !ok {
			t.Fatal("expected drifted file to remain on disk")
		}
		if string(content) != modifiedContent {
			t.Fatalf("drifted file content = %q, want %q", string(content), modifiedContent)
		}

		updatedState, err := stateStore.LoadWorkspace(workspaceID)
		if err != nil {
			t.Fatalf("expected workspace state to remain: %v", err)
		}
		ownership, ok := updatedState.Paths[relPath]
		if !ok {
			t.Fatalf("expected workspace state to retain %s", relPath)
		}
		if ownership.Checksum != originalChecksum {
			t.Fatalf("checksum = %q, want %q", ownership.Checksum, originalChecksum)
		}
		if !updatedState.Applied {
			t.Fatal("expected workspace state to remain applied")
		}
	})

	t.Run("force removes drifted copy", func(t *testing.T) {
		eng, fs, stateStore, workspaceID, configPath := setupDriftedCopy(t)

		result, err := eng.Unapply(context.Background(), &engine.UnapplyRequest{
			CWD:   cwd,
			Force: true,
		})
		if err != nil {
			t.Fatalf("Unapply() error = %v", err)
		}

		assertRemovedOrder(t, result.Removed, []string{relPath})
		if _, ok := fs.files[configPath]; ok {
			t.Fatal("expected force unapply to remove drifted copy")
		}
		assertWorkspaceDeleted(t, stateStore, workspaceID)
	})
}

func TestUnapply_ForceMode(t *testing.T) {
	eng, fs, stateStore, _, _ := setupTestEngine(t)
	ctx := context.Background()

	// Setup workspace state
	workspaceID := state.ComputeWorkspaceID("repo-fingerprint-123", "workspace")
	workspaceState := state.NewWorkspaceState("repo-fingerprint-123", "workspace", "symlink")
	workspaceState.Applied = true
	workspaceState.ActiveStore = "store1" // Set active store
	workspaceState.Paths = map[string]state.PathOwnership{
		"file.txt": {
			Store: "store1",
			Type:  "symlink",
		},
	}
	_ = stateStore.SaveWorkspace(workspaceID, workspaceState)

	cwd := "/repo/workspace"
	filePath := filepath.Join(cwd, "file.txt")
	fs.symlinks[filePath] = "/store1/file.txt"

	// Unapply with force
	req := &engine.UnapplyRequest{
		CWD:   cwd,
		Force: true,
	}

	result, err := eng.Unapply(ctx, req)
	if err != nil {
		t.Fatalf("Unapply() error = %v", err)
	}

	assertRemovedOrder(t, result.Removed, []string{"file.txt"})

	// Verify workspace state was deleted
	assertWorkspaceDeleted(t, stateStore, workspaceID)
}

func TestUnapply_NoState(t *testing.T) {
	eng, _, _, _, _ := setupTestEngine(t)
	ctx := context.Background()

	// Try to unapply when no workspace state exists
	req := &engine.UnapplyRequest{
		CWD: "/repo/workspace",
	}

	_, err := eng.Unapply(ctx, req)
	if err == nil {
		t.Error("expected error when unapplying with no workspace state")
	}
}

func TestUnapply_EmptyState(t *testing.T) {
	eng, _, stateStore, _, _ := setupTestEngine(t)
	ctx := context.Background()

	// Setup empty workspace state
	workspaceID := state.ComputeWorkspaceID("repo-fingerprint-123", "workspace")
	workspaceState := state.NewWorkspaceState("repo-fingerprint-123", "workspace", "symlink")
	workspaceState.Applied = true
	workspaceState.Paths = map[string]state.PathOwnership{} // Empty
	_ = stateStore.SaveWorkspace(workspaceID, workspaceState)

	req := &engine.UnapplyRequest{
		CWD: "/repo/workspace",
	}

	result, err := eng.Unapply(ctx, req)
	if err != nil {
		t.Fatalf("Unapply() error = %v", err)
	}

	// Should return empty removed list
	if len(result.Removed) != 0 {
		t.Errorf("expected 0 paths removed, got %d", len(result.Removed))
	}
}

func TestUnapply_NestedDirectories(t *testing.T) {
	eng, fs, stateStore, _, _ := setupTestEngine(t)
	ctx := context.Background()

	// Setup workspace state with deeply nested structure
	workspaceID := state.ComputeWorkspaceID("repo-fingerprint-123", "workspace")
	workspaceState := state.NewWorkspaceState("repo-fingerprint-123", "workspace", "symlink")
	workspaceState.Applied = true
	workspaceState.ActiveStore = "store1" // Set active store
	workspaceState.Paths = map[string]state.PathOwnership{
		"a": {
			Store: "store1",
			Type:  "symlink",
		},
		"a/b": {
			Store: "store1",
			Type:  "symlink",
		},
		"a/b/c": {
			Store: "store1",
			Type:  "symlink",
		},
		"a/b/c/d.txt": {
			Store: "store1",
			Type:  "symlink",
		},
	}
	_ = stateStore.SaveWorkspace(workspaceID, workspaceState)

	cwd := "/repo/workspace"
	fs.symlinks[filepath.Join(cwd, "a")] = "/store1/a"
	fs.symlinks[filepath.Join(cwd, "a/b")] = "/store1/a/b"
	fs.symlinks[filepath.Join(cwd, "a/b/c")] = "/store1/a/b/c"
	fs.symlinks[filepath.Join(cwd, "a/b/c/d.txt")] = "/store1/a/b/c/d.txt"

	req := &engine.UnapplyRequest{
		CWD: cwd,
	}

	result, err := eng.Unapply(ctx, req)
	if err != nil {
		t.Fatalf("Unapply() error = %v", err)
	}

	// Verify all paths were removed
	if len(result.Removed) != 4 {
		t.Errorf("expected 4 paths removed, got %d", len(result.Removed))
	}

	// Verify deepest path (a/b/c/d.txt) appears before parent directories
	// This tests the deepest-first ordering
	removed := result.Removed
	foundDeepest := false
	for i, path := range removed {
		if path == "a/b/c/d.txt" {
			foundDeepest = true
			// Verify it comes before its parent directories
			if i >= len(removed)-3 {
				t.Error("expected deepest path to be removed before parent directories")
			}
		}
	}
	if !foundDeepest {
		t.Error("expected a/b/c/d.txt in removed list")
	}
}

func writeIntegrationFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func newRealFSUnapplyEngine(t *testing.T, repoRoot string, stateStore *testStateStore) *engine.Engine {
	t.Helper()
	return engine.New(
		gitx.NewFakeGitRepo(repoRoot, "repo-fingerprint-123"),
		newTestStoreRepo(),
		stateStore,
		fsops.NewRealFS(),
		hash.NewSHA256Hasher(),
		clock.NewFakeClock(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)),
		config.Paths{
			Root:       filepath.Join(repoRoot, ".monodev"),
			Stores:     filepath.Join(repoRoot, "stores"),
			Workspaces: filepath.Join(repoRoot, ".state"),
		},
	)
}

func TestUnapply_CopiedDirectoryProtectsUserChanges(t *testing.T) {
	repoRoot := t.TempDir()
	scriptsDir := filepath.Join(repoRoot, "scripts")
	writeIntegrationFile(t, filepath.Join(scriptsDir, "init.sh"), "echo init\n")
	writeIntegrationFile(t, filepath.Join(scriptsDir, "utils", "helper.sh"), "echo helper\n")

	hasher := hash.NewSHA256Hasher()
	initHash, err := hasher.HashFile(filepath.Join(scriptsDir, "init.sh"))
	if err != nil {
		t.Fatalf("hash init.sh: %v", err)
	}
	helperHash, err := hasher.HashFile(filepath.Join(scriptsDir, "utils", "helper.sh"))
	if err != nil {
		t.Fatalf("hash helper.sh: %v", err)
	}

	stateStore := newTestStateStore()
	workspaceID := state.ComputeWorkspaceID("repo-fingerprint-123", ".")
	ws := state.NewWorkspaceState("repo-fingerprint-123", ".", "copy")
	ws.Applied = true
	ws.ActiveStore = "store1"
	ws.Paths["scripts"] = state.PathOwnership{
		Store: "store1",
		Type:  "copy",
		Contents: &state.DirContents{Files: map[string]string{
			"init.sh":         initHash,
			"utils/helper.sh": helperHash,
		}},
	}
	_ = stateStore.SaveWorkspace(workspaceID, ws)
	eng := newRealFSUnapplyEngine(t, repoRoot, stateStore)

	writeIntegrationFile(t, filepath.Join(scriptsDir, "notes.txt"), "user work\n")
	writeIntegrationFile(t, filepath.Join(scriptsDir, "init.sh"), "echo changed\n")
	if err := os.Remove(filepath.Join(scriptsDir, "utils", "helper.sh")); err != nil {
		t.Fatalf("remove helper.sh: %v", err)
	}

	result, err := eng.Unapply(context.Background(), &engine.UnapplyRequest{CWD: repoRoot})
	if result != nil {
		t.Fatalf("Unapply() result = %#v, want nil", result)
	}
	if !errors.Is(err, engine.ErrValidation) || !errors.Is(err, engine.ErrDrift) {
		t.Fatalf("Unapply() error = %v, want ErrValidation and ErrDrift", err)
	}
	errText := err.Error()
	for _, want := range []string{"scripts/notes.txt", "scripts/init.sh", "scripts/utils/helper.sh", "--force", "inspect"} {
		if !strings.Contains(errText, want) {
			t.Fatalf("Unapply() error = %q, want %q", errText, want)
		}
	}
	if _, statErr := os.Stat(filepath.Join(scriptsDir, "notes.txt")); statErr != nil {
		t.Fatalf("expected user-added file to remain: %v", statErr)
	}

	forceResult, err := eng.Unapply(context.Background(), &engine.UnapplyRequest{CWD: repoRoot, Force: true})
	if err != nil {
		t.Fatalf("force Unapply() error = %v", err)
	}
	assertRemovedOrder(t, forceResult.Removed, []string{"scripts"})
	if _, err := os.Stat(scriptsDir); !os.IsNotExist(err) {
		t.Fatalf("expected force unapply to remove scripts, err=%v", err)
	}
	assertWorkspaceDeleted(t, stateStore, workspaceID)
}

func TestUnapply_UnchangedCopiedDirectoryCleansOwnershipState(t *testing.T) {
	repoRoot := t.TempDir()
	scriptsDir := filepath.Join(repoRoot, "scripts")
	writeIntegrationFile(t, filepath.Join(scriptsDir, "init.sh"), "echo init\n")
	writeIntegrationFile(t, filepath.Join(scriptsDir, "utils", "helper.sh"), "echo helper\n")

	hasher := hash.NewSHA256Hasher()
	initHash, err := hasher.HashFile(filepath.Join(scriptsDir, "init.sh"))
	if err != nil {
		t.Fatalf("hash init.sh: %v", err)
	}
	helperHash, err := hasher.HashFile(filepath.Join(scriptsDir, "utils", "helper.sh"))
	if err != nil {
		t.Fatalf("hash helper.sh: %v", err)
	}

	stateStore := newTestStateStore()
	workspaceID := state.ComputeWorkspaceID("repo-fingerprint-123", ".")
	ws := state.NewWorkspaceState("repo-fingerprint-123", ".", "copy")
	ws.Applied = true
	ws.ActiveStore = "store1"
	ws.Paths["scripts"] = state.PathOwnership{
		Store: "store1",
		Type:  "copy",
		Contents: &state.DirContents{Files: map[string]string{
			"init.sh":         initHash,
			"utils/helper.sh": helperHash,
		}},
	}
	_ = stateStore.SaveWorkspace(workspaceID, ws)
	eng := newRealFSUnapplyEngine(t, repoRoot, stateStore)

	result, err := eng.Unapply(context.Background(), &engine.UnapplyRequest{CWD: repoRoot})
	if err != nil {
		t.Fatalf("Unapply() error = %v", err)
	}
	assertRemovedOrder(t, result.Removed, []string{"scripts"})
	if _, err := os.Stat(filepath.Join(repoRoot, "scripts")); !os.IsNotExist(err) {
		t.Fatalf("expected scripts to be removed, err=%v", err)
	}
	assertWorkspaceDeleted(t, stateStore, workspaceID)
}

func TestUnapply_LegacyCopiedDirectoryWithoutManifestIsConservative(t *testing.T) {
	repoRoot := t.TempDir()
	scriptsDir := filepath.Join(repoRoot, "scripts")
	writeIntegrationFile(t, filepath.Join(scriptsDir, "init.sh"), "echo init\n")
	writeIntegrationFile(t, filepath.Join(scriptsDir, "notes.txt"), "user work\n")

	stateStore := newTestStateStore()
	workspaceID := state.ComputeWorkspaceID("repo-fingerprint-123", ".")
	ws := state.NewWorkspaceState("repo-fingerprint-123", ".", "copy")
	ws.Applied = true
	ws.ActiveStore = "store1"
	ws.Paths["scripts"] = state.PathOwnership{Store: "store1", Type: "copy"}
	_ = stateStore.SaveWorkspace(workspaceID, ws)
	eng := newRealFSUnapplyEngine(t, repoRoot, stateStore)

	result, err := eng.Unapply(context.Background(), &engine.UnapplyRequest{CWD: repoRoot})
	if result != nil {
		t.Fatalf("Unapply() result = %#v, want nil", result)
	}
	if !errors.Is(err, engine.ErrDrift) {
		t.Fatalf("Unapply() error = %v, want ErrDrift", err)
	}
	if !strings.Contains(err.Error(), "ownership manifest") || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("Unapply() error = %q, want legacy manifest guidance", err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(scriptsDir, "notes.txt")); statErr != nil {
		t.Fatalf("expected legacy directory to remain: %v", statErr)
	}
}
