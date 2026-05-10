package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/state"
)

func newScopedWorkspaceStateTestEngine(t *testing.T) (*Engine, *state.FileStateStore, *state.FileStateStore) {
	t.Helper()

	tmpDir := t.TempDir()
	fs := fsops.NewRealFS()
	globalPaths := &config.Paths{
		Root:       filepath.Join(tmpDir, "global"),
		Stores:     filepath.Join(tmpDir, "global", "stores"),
		Workspaces: filepath.Join(tmpDir, "global", "workspaces"),
		Config:     filepath.Join(tmpDir, "global", "config.yaml"),
	}
	repoRoot := filepath.Join(tmpDir, "repo")
	componentPaths := &config.Paths{
		Root:       filepath.Join(repoRoot, ".monodev"),
		Stores:     filepath.Join(repoRoot, ".monodev", "stores"),
		Workspaces: filepath.Join(repoRoot, ".monodev", "workspaces"),
		Config:     filepath.Join(repoRoot, ".monodev", "config.yaml"),
	}
	scopedPaths := &config.ScopedPaths{
		Global:         globalPaths,
		Component:      componentPaths,
		HasRepoContext: true,
		RepoRoot:       repoRoot,
	}
	if err := scopedPaths.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories() error = %v", err)
	}

	globalStateStore := state.NewFileStateStore(fs, globalPaths.Workspaces)
	componentStateStore := state.NewFileStateStore(fs, componentPaths.Workspaces)
	eng := &Engine{
		stateStore:          globalStateStore,
		componentStateStore: componentStateStore,
		fs:                  fs,
		configPaths:         *globalPaths,
		scopedPaths:         scopedPaths,
	}

	return eng, globalStateStore, componentStateStore
}

func TestListWorkspaces_Empty(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	workspacesDir := filepath.Join(tmpDir, "workspaces")
	if err := os.MkdirAll(workspacesDir, 0755); err != nil {
		t.Fatal(err)
	}

	fs := fsops.NewRealFS()
	stateStore := state.NewFileStateStore(fs, workspacesDir)
	configPaths := config.Paths{Workspaces: workspacesDir}

	eng := &Engine{
		stateStore:  stateStore,
		configPaths: configPaths,
	}

	// Execute
	result, err := eng.ListWorkspaces(context.Background())

	// Verify
	if err != nil {
		t.Errorf("ListWorkspaces() error = %v, want nil", err)
	}
	if result == nil {
		t.Fatal("ListWorkspaces() returned nil result")
	}
	if len(result.Workspaces) != 0 {
		t.Errorf("ListWorkspaces() returned %d workspaces, want 0", len(result.Workspaces))
	}
}

func TestListWorkspaces_NonExistentDirectory(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	workspacesDir := filepath.Join(tmpDir, "workspaces")
	// Don't create the directory

	fs := fsops.NewRealFS()
	stateStore := state.NewFileStateStore(fs, workspacesDir)
	configPaths := config.Paths{Workspaces: workspacesDir}

	eng := &Engine{
		stateStore:  stateStore,
		configPaths: configPaths,
	}

	// Execute
	result, err := eng.ListWorkspaces(context.Background())

	// Verify - should return empty list, not error
	if err != nil {
		t.Errorf("ListWorkspaces() error = %v, want nil", err)
	}
	if result == nil {
		t.Fatal("ListWorkspaces() returned nil result")
	}
	if len(result.Workspaces) != 0 {
		t.Errorf("ListWorkspaces() returned %d workspaces, want 0", len(result.Workspaces))
	}
}

func TestListWorkspaces_MultipleWorkspaces(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	workspacesDir := filepath.Join(tmpDir, "workspaces")
	if err := os.MkdirAll(workspacesDir, 0755); err != nil {
		t.Fatal(err)
	}

	fs := fsops.NewRealFS()
	stateStore := state.NewFileStateStore(fs, workspacesDir)
	configPaths := config.Paths{Workspaces: workspacesDir}

	eng := &Engine{
		stateStore:  stateStore,
		configPaths: configPaths,
	}

	// Create test workspace states
	ws1 := state.NewWorkspaceState("repo1", "path/to/workspace1", "copy")
	ws1.Applied = true
	ws1.ActiveStore = "store1"
	ws1.Paths["file1.txt"] = state.PathOwnership{Store: "store1", Type: "copy"}

	ws2 := state.NewWorkspaceState("repo2", "path/to/workspace2", "symlink")
	ws2.Applied = false
	ws2.ActiveStore = "store2"

	if err := stateStore.SaveWorkspace("workspace1", ws1); err != nil {
		t.Fatal(err)
	}
	if err := stateStore.SaveWorkspace("workspace2", ws2); err != nil {
		t.Fatal(err)
	}

	// Execute
	result, err := eng.ListWorkspaces(context.Background())

	// Verify
	if err != nil {
		t.Errorf("ListWorkspaces() error = %v, want nil", err)
	}
	if result == nil {
		t.Fatal("ListWorkspaces() returned nil result")
	}
	if len(result.Workspaces) != 2 {
		t.Fatalf("ListWorkspaces() returned %d workspaces, want 2", len(result.Workspaces))
	}

	// Verify sorting by WorkspacePath
	if result.Workspaces[0].WorkspacePath > result.Workspaces[1].WorkspacePath {
		t.Error("Workspaces not sorted by WorkspacePath")
	}

	// Verify workspace details
	for _, ws := range result.Workspaces {
		if ws.WorkspaceID == "workspace1" {
			if !ws.Applied {
				t.Error("workspace1 Applied should be true")
			}
			if ws.ActiveStore != "store1" {
				t.Errorf("workspace1 ActiveStore = %q, want 'store1'", ws.ActiveStore)
			}
			if ws.AppliedPathCount != 1 {
				t.Errorf("workspace1 AppliedPathCount = %d, want 1", ws.AppliedPathCount)
			}
		}
	}
}

func TestListWorkspaces_SkipsCorruptedFiles(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	workspacesDir := filepath.Join(tmpDir, "workspaces")
	if err := os.MkdirAll(workspacesDir, 0755); err != nil {
		t.Fatal(err)
	}

	fs := fsops.NewRealFS()
	stateStore := state.NewFileStateStore(fs, workspacesDir)
	configPaths := config.Paths{Workspaces: workspacesDir}

	eng := &Engine{
		stateStore:  stateStore,
		configPaths: configPaths,
	}

	// Create a valid workspace
	ws1 := state.NewWorkspaceState("repo1", "path/to/workspace1", "copy")
	if err := stateStore.SaveWorkspace("workspace1", ws1); err != nil {
		t.Fatal(err)
	}

	// Create a corrupted file
	corruptedPath := filepath.Join(workspacesDir, "corrupted.json")
	if err := os.WriteFile(corruptedPath, []byte("invalid json{{{"), 0644); err != nil {
		t.Fatal(err)
	}

	// Execute
	result, err := eng.ListWorkspaces(context.Background())

	// Verify - should skip corrupted file and return valid workspace
	if err != nil {
		t.Errorf("ListWorkspaces() error = %v, want nil", err)
	}
	if result == nil {
		t.Fatal("ListWorkspaces() returned nil result")
	}
	if len(result.Workspaces) != 1 {
		t.Errorf("ListWorkspaces() returned %d workspaces, want 1 (corrupted file should be skipped)", len(result.Workspaces))
	}
}

func TestListWorkspaces_ScopedComponentOnlyWorkspace(t *testing.T) {
	eng, _, componentStateStore := newScopedWorkspaceStateTestEngine(t)

	ws := state.NewWorkspaceState("repo1", "components/api", "copy")
	ws.AbsolutePath = "/repo/components/api"
	ws.Applied = true
	ws.ActiveStore = "component-store"
	ws.Stack = []string{"base-store", "component-store"}
	ws.Paths["Makefile"] = state.PathOwnership{Store: "component-store", Type: "copy"}
	ws.Paths["scripts/dev.sh"] = state.PathOwnership{Store: "component-store", Type: "copy"}

	if err := componentStateStore.SaveWorkspace("component-ws", ws); err != nil {
		t.Fatal(err)
	}

	result, err := eng.ListWorkspaces(context.Background())
	if err != nil {
		t.Fatalf("ListWorkspaces() error = %v", err)
	}
	if result == nil {
		t.Fatal("ListWorkspaces() returned nil result")
	}
	if len(result.Workspaces) != 1 {
		t.Fatalf("ListWorkspaces() returned %d workspaces, want 1", len(result.Workspaces))
	}

	got := result.Workspaces[0]
	if got.WorkspaceID != "component-ws" {
		t.Errorf("WorkspaceID = %q, want %q", got.WorkspaceID, "component-ws")
	}
	if got.WorkspacePath != "components/api" {
		t.Errorf("WorkspacePath = %q, want %q", got.WorkspacePath, "components/api")
	}
	if got.ActiveStore != "component-store" {
		t.Errorf("ActiveStore = %q, want %q", got.ActiveStore, "component-store")
	}
	if got.StackCount != 2 {
		t.Errorf("StackCount = %d, want 2", got.StackCount)
	}
	if got.AppliedPathCount != 2 {
		t.Errorf("AppliedPathCount = %d, want 2", got.AppliedPathCount)
	}
}

func TestListWorkspaces_ScopedDuplicateWorkspaceIDPrefersGlobal(t *testing.T) {
	eng, globalStateStore, componentStateStore := newScopedWorkspaceStateTestEngine(t)

	globalWs := state.NewWorkspaceState("repo1", "global/path", "copy")
	componentWs := state.NewWorkspaceState("repo1", "component/path", "copy")
	if err := globalStateStore.SaveWorkspace("shared-ws", globalWs); err != nil {
		t.Fatal(err)
	}
	if err := componentStateStore.SaveWorkspace("shared-ws", componentWs); err != nil {
		t.Fatal(err)
	}

	result, err := eng.ListWorkspaces(context.Background())
	if err != nil {
		t.Fatalf("ListWorkspaces() error = %v", err)
	}
	if len(result.Workspaces) != 1 {
		t.Fatalf("ListWorkspaces() returned %d workspaces, want 1", len(result.Workspaces))
	}
	if result.Workspaces[0].WorkspacePath != "global/path" {
		t.Errorf("WorkspacePath = %q, want global scope to win duplicate IDs", result.Workspaces[0].WorkspacePath)
	}
}

func TestDescribeWorkspace_Found(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	workspacesDir := filepath.Join(tmpDir, "workspaces")
	if err := os.MkdirAll(workspacesDir, 0755); err != nil {
		t.Fatal(err)
	}

	fs := fsops.NewRealFS()
	stateStore := state.NewFileStateStore(fs, workspacesDir)

	eng := &Engine{
		stateStore: stateStore,
	}

	// Create test workspace state
	ws := state.NewWorkspaceState("repo1", "path/to/workspace", "copy")
	ws.Applied = true
	ws.ActiveStore = "store1"
	ws.Stack = []string{"base", "extra"}
	ws.Paths["file1.txt"] = state.PathOwnership{Store: "store1", Type: "copy"}

	if err := stateStore.SaveWorkspace("workspace1", ws); err != nil {
		t.Fatal(err)
	}

	// Execute
	result, err := eng.DescribeWorkspace(context.Background(), "workspace1")

	// Verify
	if err != nil {
		t.Errorf("DescribeWorkspace() error = %v, want nil", err)
	}
	if result == nil {
		t.Fatal("DescribeWorkspace() returned nil result")
	}
	if result.WorkspaceID != "workspace1" {
		t.Errorf("WorkspaceID = %q, want 'workspace1'", result.WorkspaceID)
	}
	if result.WorkspacePath != "path/to/workspace" {
		t.Errorf("WorkspacePath = %q, want 'path/to/workspace'", result.WorkspacePath)
	}
	if !result.Applied {
		t.Error("Applied should be true")
	}
	if len(result.Stack) != 2 {
		t.Errorf("Stack length = %d, want 2", len(result.Stack))
	}
	if len(result.Paths) != 1 {
		t.Errorf("Paths length = %d, want 1", len(result.Paths))
	}
}

func TestDescribeWorkspace_NotFound(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	workspacesDir := filepath.Join(tmpDir, "workspaces")
	if err := os.MkdirAll(workspacesDir, 0755); err != nil {
		t.Fatal(err)
	}

	fs := fsops.NewRealFS()
	stateStore := state.NewFileStateStore(fs, workspacesDir)

	eng := &Engine{
		stateStore: stateStore,
	}

	// Execute
	result, err := eng.DescribeWorkspace(context.Background(), "nonexistent")

	// Verify
	if err == nil {
		t.Error("DescribeWorkspace() error = nil, want error")
	}
	if result != nil {
		t.Errorf("DescribeWorkspace() result = %v, want nil", result)
	}
}

func TestDescribeWorkspace_ScopedComponentOnlyWorkspace(t *testing.T) {
	eng, _, componentStateStore := newScopedWorkspaceStateTestEngine(t)

	ws := state.NewWorkspaceState("repo1", "components/api", "copy")
	ws.ActiveStore = "component-store"
	ws.Stack = []string{"base-store"}
	ws.Paths["Makefile"] = state.PathOwnership{Store: "component-store", Type: "copy"}

	if err := componentStateStore.SaveWorkspace("component-ws", ws); err != nil {
		t.Fatal(err)
	}

	result, err := eng.DescribeWorkspace(context.Background(), "component-ws")
	if err != nil {
		t.Fatalf("DescribeWorkspace() error = %v", err)
	}
	if result.WorkspaceID != "component-ws" {
		t.Errorf("WorkspaceID = %q, want %q", result.WorkspaceID, "component-ws")
	}
	if result.WorkspacePath != "components/api" {
		t.Errorf("WorkspacePath = %q, want %q", result.WorkspacePath, "components/api")
	}
	if result.ActiveStore != "component-store" {
		t.Errorf("ActiveStore = %q, want %q", result.ActiveStore, "component-store")
	}
}

func TestDeleteWorkspace_Success(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	workspacesDir := filepath.Join(tmpDir, "workspaces")
	if err := os.MkdirAll(workspacesDir, 0755); err != nil {
		t.Fatal(err)
	}

	fs := fsops.NewRealFS()
	stateStore := state.NewFileStateStore(fs, workspacesDir)

	eng := &Engine{
		stateStore: stateStore,
	}

	// Create test workspace state without applied paths
	ws := state.NewWorkspaceState("repo1", "path/to/workspace", "copy")
	ws.Applied = false

	if err := stateStore.SaveWorkspace("workspace1", ws); err != nil {
		t.Fatal(err)
	}

	// Execute
	req := &DeleteWorkspaceRequest{
		WorkspaceID: "workspace1",
		Force:       false,
		DryRun:      false,
	}
	result, err := eng.DeleteWorkspace(context.Background(), req)

	// Verify
	if err != nil {
		t.Errorf("DeleteWorkspace() error = %v, want nil", err)
	}
	if result == nil {
		t.Fatal("DeleteWorkspace() returned nil result")
	}
	if !result.Deleted {
		t.Error("Deleted should be true")
	}
	if result.DryRun {
		t.Error("DryRun should be false")
	}

	// Verify file is deleted
	_, err = stateStore.LoadWorkspace("workspace1")
	if !os.IsNotExist(err) {
		t.Error("Workspace file should be deleted")
	}
}

func TestDeleteWorkspace_DryRun(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	workspacesDir := filepath.Join(tmpDir, "workspaces")
	if err := os.MkdirAll(workspacesDir, 0755); err != nil {
		t.Fatal(err)
	}

	fs := fsops.NewRealFS()
	stateStore := state.NewFileStateStore(fs, workspacesDir)

	eng := &Engine{
		stateStore: stateStore,
	}

	// Create test workspace state
	ws := state.NewWorkspaceState("repo1", "path/to/workspace", "copy")
	ws.Paths["file1.txt"] = state.PathOwnership{Store: "store1", Type: "copy"}

	if err := stateStore.SaveWorkspace("workspace1", ws); err != nil {
		t.Fatal(err)
	}

	// Execute
	req := &DeleteWorkspaceRequest{
		WorkspaceID: "workspace1",
		Force:       false,
		DryRun:      true,
	}
	result, err := eng.DeleteWorkspace(context.Background(), req)

	// Verify
	if err != nil {
		t.Errorf("DeleteWorkspace() error = %v, want nil", err)
	}
	if result == nil {
		t.Fatal("DeleteWorkspace() returned nil result")
	}
	if result.Deleted {
		t.Error("Deleted should be false in dry-run")
	}
	if !result.DryRun {
		t.Error("DryRun should be true")
	}
	if result.PathsRemoved != 1 {
		t.Errorf("PathsRemoved = %d, want 1", result.PathsRemoved)
	}

	// Verify file is NOT deleted
	_, err = stateStore.LoadWorkspace("workspace1")
	if err != nil {
		t.Error("Workspace file should still exist after dry-run")
	}
}

func TestDeleteWorkspace_HasAppliedPathsWithoutForce(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	workspacesDir := filepath.Join(tmpDir, "workspaces")
	if err := os.MkdirAll(workspacesDir, 0755); err != nil {
		t.Fatal(err)
	}

	fs := fsops.NewRealFS()
	stateStore := state.NewFileStateStore(fs, workspacesDir)

	eng := &Engine{
		stateStore: stateStore,
	}

	// Create test workspace state with applied paths
	ws := state.NewWorkspaceState("repo1", "path/to/workspace", "copy")
	ws.Applied = true
	ws.Paths["file1.txt"] = state.PathOwnership{Store: "store1", Type: "copy"}

	if err := stateStore.SaveWorkspace("workspace1", ws); err != nil {
		t.Fatal(err)
	}

	// Execute
	req := &DeleteWorkspaceRequest{
		WorkspaceID: "workspace1",
		Force:       false,
		DryRun:      false,
	}
	result, err := eng.DeleteWorkspace(context.Background(), req)

	// Verify - should error without force
	if err == nil {
		t.Error("DeleteWorkspace() error = nil, want error")
	}
	if result != nil {
		t.Errorf("DeleteWorkspace() result = %v, want nil", result)
	}

	// Verify file is NOT deleted
	_, err = stateStore.LoadWorkspace("workspace1")
	if err != nil {
		t.Error("Workspace file should still exist when delete fails")
	}
}

func TestDeleteWorkspace_HasAppliedPathsWithForce(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	workspacesDir := filepath.Join(tmpDir, "workspaces")
	if err := os.MkdirAll(workspacesDir, 0755); err != nil {
		t.Fatal(err)
	}

	fs := fsops.NewRealFS()
	stateStore := state.NewFileStateStore(fs, workspacesDir)

	eng := &Engine{
		stateStore: stateStore,
	}

	// Create test workspace state with applied paths
	ws := state.NewWorkspaceState("repo1", "path/to/workspace", "copy")
	ws.Applied = true
	ws.Paths["file1.txt"] = state.PathOwnership{Store: "store1", Type: "copy"}

	if err := stateStore.SaveWorkspace("workspace1", ws); err != nil {
		t.Fatal(err)
	}

	// Execute with force
	req := &DeleteWorkspaceRequest{
		WorkspaceID: "workspace1",
		Force:       true,
		DryRun:      false,
	}
	result, err := eng.DeleteWorkspace(context.Background(), req)

	// Verify - should succeed with force
	if err != nil {
		t.Errorf("DeleteWorkspace() error = %v, want nil", err)
	}
	if result == nil {
		t.Fatal("DeleteWorkspace() returned nil result")
	}
	if !result.Deleted {
		t.Error("Deleted should be true")
	}

	// Verify file is deleted
	_, err = stateStore.LoadWorkspace("workspace1")
	if !os.IsNotExist(err) {
		t.Error("Workspace file should be deleted with force")
	}
}

func TestDeleteWorkspace_NotFound(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	workspacesDir := filepath.Join(tmpDir, "workspaces")
	if err := os.MkdirAll(workspacesDir, 0755); err != nil {
		t.Fatal(err)
	}

	fs := fsops.NewRealFS()
	stateStore := state.NewFileStateStore(fs, workspacesDir)

	eng := &Engine{
		stateStore: stateStore,
	}

	// Execute
	req := &DeleteWorkspaceRequest{
		WorkspaceID: "nonexistent",
		Force:       false,
		DryRun:      false,
	}
	result, err := eng.DeleteWorkspace(context.Background(), req)

	// Verify
	if err == nil {
		t.Error("DeleteWorkspace() error = nil, want error")
	}
	if result != nil {
		t.Errorf("DeleteWorkspace() result = %v, want nil", result)
	}
}

func TestDeleteWorkspace_ScopedComponentOnlyWorkspace(t *testing.T) {
	eng, globalStateStore, componentStateStore := newScopedWorkspaceStateTestEngine(t)

	ws := state.NewWorkspaceState("repo1", "components/api", "copy")
	if err := componentStateStore.SaveWorkspace("component-ws", ws); err != nil {
		t.Fatal(err)
	}

	result, err := eng.DeleteWorkspace(context.Background(), &DeleteWorkspaceRequest{
		WorkspaceID: "component-ws",
	})
	if err != nil {
		t.Fatalf("DeleteWorkspace() error = %v", err)
	}
	if result == nil {
		t.Fatal("DeleteWorkspace() returned nil result")
	}
	if !result.Deleted {
		t.Error("Deleted should be true")
	}

	if _, err := componentStateStore.LoadWorkspace("component-ws"); !os.IsNotExist(err) {
		t.Error("component workspace file should be deleted")
	}
	if _, err := globalStateStore.LoadWorkspace("component-ws"); !os.IsNotExist(err) {
		t.Error("global workspace state should not contain component workspace")
	}
}
