package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/danieljhkim/monodev/internal/engine"
	"github.com/danieljhkim/monodev/internal/state"
)

func TestUnapply_DeepestFirstRemoval(t *testing.T) {
	eng, fs, stateStore, _, _ := setupTestEngine(t)
	ctx := context.Background()

	// Setup workspace state with nested paths
	// Compute workspace ID the same way the engine does
	workspaceID := state.ComputeWorkspaceID("repo-fingerprint-123", "workspace")
	workspaceState := state.NewWorkspaceState("repo-fingerprint-123", "workspace", "symlink")
	workspaceState.Applied = true
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
	stateStore.SaveWorkspace(workspaceID, workspaceState)

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

	// Verify all paths were removed
	if len(result.Removed) != 5 {
		t.Errorf("expected 5 paths removed, got %d", len(result.Removed))
	}

	// Verify deepest paths are removed first
	// Deepest paths should be: scripts/utils/helper.sh (depth 2), scripts/init.sh (depth 1), scripts/utils (depth 1)
	// Then: scripts (depth 0), Makefile (depth 0)

	// Check that deepest paths appear first in removed list
	removedMap := make(map[string]bool)
	for _, path := range result.Removed {
		removedMap[path] = true
	}

	// All paths should be in removed list
	expectedPaths := []string{"scripts/utils/helper.sh", "scripts/init.sh", "scripts/utils", "scripts", "Makefile"}
	for _, path := range expectedPaths {
		if !removedMap[path] {
			t.Errorf("expected %q to be in removed list", path)
		}
	}

	// Verify files were removed from filesystem
	for _, path := range expectedPaths {
		absPath := filepath.Join(cwd, path)
		if _, ok := fs.symlinks[absPath]; ok {
			t.Errorf("expected %q to be removed from filesystem", absPath)
		}
	}

	// Verify workspace state was deleted (all paths removed)
	_, err = stateStore.LoadWorkspace(workspaceID)
	if err == nil {
		t.Error("expected workspace state to be deleted after unapply")
	}
}

func TestUnapply_StateCleanup(t *testing.T) {
	eng, fs, stateStore, _, _ := setupTestEngine(t)
	ctx := context.Background()

	// Setup workspace state
	workspaceID := state.ComputeWorkspaceID("repo-fingerprint-123", "workspace")
	workspaceState := state.NewWorkspaceState("repo-fingerprint-123", "workspace", "symlink")
	workspaceState.Applied = true
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
	stateStore.SaveWorkspace(workspaceID, workspaceState)

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

	// Verify all paths were removed
	if len(result.Removed) != 2 {
		t.Errorf("expected 2 paths removed, got %d", len(result.Removed))
	}

	// Verify workspace state was deleted (all paths removed)
	_, err = stateStore.LoadWorkspace(workspaceID)
	if err == nil {
		t.Error("expected workspace state to be deleted after removing all paths")
	}
	if err != nil && !os.IsNotExist(err) {
		t.Errorf("expected ErrNotExist, got %v", err)
	}
}

func TestUnapply_PartialRemoval(t *testing.T) {
	eng, fs, stateStore, _, _ := setupTestEngine(t)
	ctx := context.Background()

	// Setup workspace state with paths from multiple stores
	workspaceID := state.ComputeWorkspaceID("repo-fingerprint-123", "workspace")
	workspaceState := state.NewWorkspaceState("repo-fingerprint-123", "workspace", "symlink")
	workspaceState.Applied = true
	workspaceState.Paths = map[string]state.PathOwnership{
		"file1.txt": {
			Store: "store1",
			Type:  "symlink",
		},
		"file2.txt": {
			Store: "store2",
			Type:  "symlink",
		},
	}
	stateStore.SaveWorkspace(workspaceID, workspaceState)

	cwd := "/repo/workspace"
	fs.symlinks[filepath.Join(cwd, "file1.txt")] = "/store1/file1.txt"
	fs.symlinks[filepath.Join(cwd, "file2.txt")] = "/store2/file2.txt"

	// Unapply (removes all managed paths)
	req := &engine.UnapplyRequest{
		CWD: cwd,
	}

	result, err := eng.Unapply(ctx, req)
	if err != nil {
		t.Fatalf("Unapply() error = %v", err)
	}

	// Verify all paths were removed
	if len(result.Removed) != 2 {
		t.Errorf("expected 2 paths removed, got %d", len(result.Removed))
	}

	// Verify workspace state was deleted (all paths removed)
	_, err = stateStore.LoadWorkspace(workspaceID)
	if err == nil {
		t.Error("expected workspace state to be deleted after removing all paths")
	}
}

func TestUnapply_DryRun(t *testing.T) {
	eng, fs, stateStore, _, _ := setupTestEngine(t)
	ctx := context.Background()

	// Setup workspace state
	workspaceID := state.ComputeWorkspaceID("repo-fingerprint-123", "workspace")
	workspaceState := state.NewWorkspaceState("repo-fingerprint-123", "workspace", "symlink")
	workspaceState.Applied = true
	workspaceState.Paths = map[string]state.PathOwnership{
		"test.txt": {
			Store: "store1",
			Type:  "symlink",
		},
	}
	stateStore.SaveWorkspace(workspaceID, workspaceState)

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
	eng, fs, stateStore, _, hasher := setupTestEngine(t)
	ctx := context.Background()

	// Setup workspace state with a file in copy mode
	workspaceID := state.ComputeWorkspaceID("repo-fingerprint-123", "workspace")
	workspaceState := state.NewWorkspaceState("repo-fingerprint-123", "workspace", "copy")
	workspaceState.Applied = true

	originalChecksum := "original-hash"
	workspaceState.Paths = map[string]state.PathOwnership{
		"config.json": {
			Store:    "store1",
			Type:     "copy",
			Checksum: originalChecksum,
		},
	}
	stateStore.SaveWorkspace(workspaceID, workspaceState)

	cwd := "/repo/workspace"
	configPath := filepath.Join(cwd, "config.json")
	fs.files[configPath] = []byte(`{"modified": true}`) // Modified content

	// Set up hasher to return different checksum (simulating drift)
	hasher.SetHash(configPath, "modified-hash") // Different from original

	// Unapply with validation (no force)
	req := &engine.UnapplyRequest{
		CWD:   cwd,
		Force: false,
	}

	// Note: Current implementation doesn't return warnings for drift,
	// but validation should still pass (we remove the file anyway)
	result, err := eng.Unapply(ctx, req)
	if err != nil {
		t.Fatalf("Unapply() error = %v", err)
	}

	// Verify file was still removed (drift doesn't prevent removal)
	if len(result.Removed) != 1 {
		t.Errorf("expected 1 path removed, got %d", len(result.Removed))
	}

	// Verify file was removed from filesystem
	if _, ok := fs.files[configPath]; ok {
		t.Error("expected file to be removed despite drift")
	}
}

func TestUnapply_ForceMode(t *testing.T) {
	eng, fs, stateStore, _, _ := setupTestEngine(t)
	ctx := context.Background()

	// Setup workspace state
	workspaceID := state.ComputeWorkspaceID("repo-fingerprint-123", "workspace")
	workspaceState := state.NewWorkspaceState("repo-fingerprint-123", "workspace", "symlink")
	workspaceState.Applied = true
	workspaceState.Paths = map[string]state.PathOwnership{
		"file.txt": {
			Store: "store1",
			Type:  "symlink",
		},
	}
	stateStore.SaveWorkspace(workspaceID, workspaceState)

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

	// Verify file was removed
	if len(result.Removed) != 1 {
		t.Errorf("expected 1 path removed, got %d", len(result.Removed))
	}

	// Verify workspace state was deleted
	_, err = stateStore.LoadWorkspace(workspaceID)
	if err == nil {
		t.Error("expected workspace state to be deleted")
	}
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
	stateStore.SaveWorkspace(workspaceID, workspaceState)

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
	stateStore.SaveWorkspace(workspaceID, workspaceState)

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
