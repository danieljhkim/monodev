package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// Mock implementations for testing

type mockStoreRepo struct {
	stores      map[string]bool
	deleteError error
}

func newMockStoreRepo() *mockStoreRepo {
	return &mockStoreRepo{
		stores: make(map[string]bool),
	}
}

func (m *mockStoreRepo) Exists(id string) (bool, error) {
	return m.stores[id], nil
}

func (m *mockStoreRepo) Delete(id string) error {
	if m.deleteError != nil {
		return m.deleteError
	}
	delete(m.stores, id)
	return nil
}

func (m *mockStoreRepo) List() ([]string, error)                            { return nil, nil }
func (m *mockStoreRepo) Create(id string, meta *stores.StoreMeta) error     { return nil }
func (m *mockStoreRepo) LoadMeta(id string) (*stores.StoreMeta, error)      { return nil, nil }
func (m *mockStoreRepo) SaveMeta(id string, meta *stores.StoreMeta) error   { return nil }
func (m *mockStoreRepo) LoadTrack(id string) (*stores.TrackFile, error)     { return nil, nil }
func (m *mockStoreRepo) SaveTrack(id string, track *stores.TrackFile) error { return nil }
func (m *mockStoreRepo) OverlayRoot(id string) string                       { return "" }

type mockStateStore struct {
	workspaces map[string]*state.WorkspaceState
	loadError  error
	saveError  error
}

func newMockStateStore() *mockStateStore {
	return &mockStateStore{
		workspaces: make(map[string]*state.WorkspaceState),
	}
}

func (m *mockStateStore) LoadWorkspace(id string) (*state.WorkspaceState, error) {
	if m.loadError != nil {
		return nil, m.loadError
	}
	ws, ok := m.workspaces[id]
	if !ok {
		return nil, os.ErrNotExist
	}
	return ws, nil
}

func (m *mockStateStore) SaveWorkspace(id string, ws *state.WorkspaceState) error {
	if m.saveError != nil {
		return m.saveError
	}
	m.workspaces[id] = ws
	return nil
}

func (m *mockStateStore) DeleteWorkspace(id string) error {
	delete(m.workspaces, id)
	return nil
}

type mockFS struct{}

func (m *mockFS) ReadFile(path string) ([]byte, error)                         { return nil, nil }
func (m *mockFS) AtomicWrite(path string, data []byte, perm os.FileMode) error { return nil }
func (m *mockFS) Exists(path string) (bool, error)                             { return false, nil }
func (m *mockFS) MkdirAll(path string, perm os.FileMode) error                 { return nil }
func (m *mockFS) Remove(path string) error                                     { return nil }
func (m *mockFS) RemoveAll(path string) error                                  { return nil }
func (m *mockFS) Symlink(oldname, newname string) error                        { return nil }
func (m *mockFS) Readlink(name string) (string, error)                         { return "", nil }
func (m *mockFS) Lstat(name string) (os.FileInfo, error)                       { return nil, nil }
func (m *mockFS) Copy(src, dst string) error                                   { return nil }
func (m *mockFS) ValidateRelPath(relPath string) error                         { return nil }
func (m *mockFS) ValidateIdentifier(id string) error                           { return nil }

type mockGitRepo struct{}

func (m *mockGitRepo) Discover(path string) (string, error) { return "", nil }
func (m *mockGitRepo) CommonGitDir(root string) (string, error) {
	return filepath.Join(root, ".git"), nil
}
func (m *mockGitRepo) Fingerprint(repoRoot string) (string, error)   { return "", nil }
func (m *mockGitRepo) RelPath(repoRoot, path string) (string, error) { return "", nil }
func (m *mockGitRepo) GetFingerprintComponents(root string) (string, string, error) {
	return "", "", nil
}
func (m *mockGitRepo) Username(root string) string { return "user" }
func (m *mockGitRepo) IsIgnored(cwd string, relPaths []string) (map[string]bool, error) {
	return map[string]bool{}, nil
}

type mockHasher struct{}

func (m *mockHasher) HashFile(path string) (string, error) { return "", nil }

type mockClock struct{}

func (m *mockClock) Now() time.Time { return time.Now() }

// Helper to create test engine with mocks
func newTestEngine(storeRepo stores.StoreRepo, stateStore state.StateStore, workspacesDir string) *Engine {
	return New(
		&mockGitRepo{},
		storeRepo,
		stateStore,
		&mockFS{},
		&mockHasher{},
		&mockClock{},
		config.Paths{
			Root:       "/tmp/monodev",
			Stores:     "/tmp/monodev/stores",
			Workspaces: workspacesDir,
		},
	)
}

func newScopedDeleteStoreTestEngine(t *testing.T) (*Engine, *state.FileStateStore, *state.FileStateStore, *stores.FileStoreRepo, *stores.FileStoreRepo) {
	t.Helper()

	eng, globalStateStore, componentStateStore := newScopedWorkspaceStateTestEngine(t)
	globalStoreRepo := stores.NewFileStoreRepo(eng.fs, eng.scopedPaths.Global.Stores)
	componentStoreRepo := stores.NewFileStoreRepo(eng.fs, eng.scopedPaths.Component.Stores)
	eng.storeResolver = newEngineStoreResolver(globalStoreRepo, globalStoreRepo, componentStoreRepo)

	return eng, globalStateStore, componentStateStore, globalStoreRepo, componentStoreRepo
}

func TestDeleteStore_NotFound(t *testing.T) {
	storeRepo := newMockStoreRepo()
	stateStore := newMockStateStore()
	eng := newTestEngine(storeRepo, stateStore, "/tmp/workspaces")

	req := &DeleteStoreRequest{
		StoreID: "nonexistent-store",
		Force:   false,
		DryRun:  false,
	}

	result, err := eng.DeleteStore(context.Background(), req)

	if result != nil {
		t.Errorf("expected nil result for non-existent store, got %+v", result)
	}

	if err == nil {
		t.Fatal("expected error for non-existent store, got nil")
	}

	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteStore_NoWorkspaces(t *testing.T) {
	storeRepo := newMockStoreRepo()
	storeRepo.stores["test-store"] = true
	stateStore := newMockStateStore()

	// Create temporary directory for workspaces
	tmpDir := t.TempDir()
	eng := newTestEngine(storeRepo, stateStore, tmpDir)

	req := &DeleteStoreRequest{
		StoreID: "test-store",
		Force:   false,
		DryRun:  false,
	}

	result, err := eng.DeleteStore(context.Background(), req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected result, got nil")
	}

	if !result.Deleted {
		t.Error("expected store to be deleted")
	}

	if len(result.AffectedWorkspaces) != 0 {
		t.Errorf("expected 0 affected workspaces, got %d", len(result.AffectedWorkspaces))
	}

	// Verify store was deleted from repo
	exists, _ := storeRepo.Exists("test-store")
	if exists {
		t.Error("store should have been deleted from repo")
	}
}

func TestDeleteStore_InUse_WithForce(t *testing.T) {
	storeRepo := newMockStoreRepo()
	storeRepo.stores["active-store"] = true
	stateStore := newMockStateStore()

	// Create workspace state that uses the store
	ws := &state.WorkspaceState{
		Repo:          "repo1",
		WorkspacePath: "services/api",
		Applied:       true,
		Mode:          "copy",
		Stack:         []string{},
		ActiveStore:   "active-store",
		Paths: map[string]state.PathOwnership{
			"Makefile": {
				Store:     "active-store",
				Type:      "copy",
				Timestamp: time.Now(),
			},
		},
	}
	stateStore.workspaces["ws1"] = ws

	// Create temporary directory for workspaces
	tmpDir := t.TempDir()
	wsFile := filepath.Join(tmpDir, "ws1.json")
	if err := os.WriteFile(wsFile, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	eng := newTestEngine(storeRepo, stateStore, tmpDir)

	req := &DeleteStoreRequest{
		StoreID: "active-store",
		Force:   true, // Force deletion
		DryRun:  false,
	}

	result, err := eng.DeleteStore(context.Background(), req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Deleted {
		t.Error("expected store to be deleted with force")
	}

	if len(result.AffectedWorkspaces) != 1 {
		t.Errorf("expected 1 affected workspace, got %d", len(result.AffectedWorkspaces))
	}

	// Verify workspace state was cleaned
	cleanedWs, _ := stateStore.LoadWorkspace("ws1")
	if cleanedWs.ActiveStore != "" {
		t.Errorf("expected ActiveStore to be cleared, got %q", cleanedWs.ActiveStore)
	}
	if len(cleanedWs.Paths) != 0 {
		t.Errorf("expected Paths to be empty, got %d paths", len(cleanedWs.Paths))
	}
	if cleanedWs.Applied {
		t.Error("expected Applied to be false")
	}
}

func TestDeleteStore_ActiveStore(t *testing.T) {
	storeRepo := newMockStoreRepo()
	storeRepo.stores["active-store"] = true
	stateStore := newMockStateStore()

	ws := &state.WorkspaceState{
		Repo:          "repo1",
		WorkspacePath: "services/api",
		Applied:       true,
		Mode:          "symlink",
		Stack:         []string{"other-store"},
		ActiveStore:   "active-store",
		Paths: map[string]state.PathOwnership{
			"Makefile": {
				Store: "active-store",
				Type:  "symlink",
			},
		},
	}
	stateStore.workspaces["ws1"] = ws

	tmpDir := t.TempDir()
	wsFile := filepath.Join(tmpDir, "ws1.json")
	if err := os.WriteFile(wsFile, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	eng := newTestEngine(storeRepo, stateStore, tmpDir)

	req := &DeleteStoreRequest{
		StoreID: "active-store",
		Force:   true,
		DryRun:  false,
	}

	result, err := eng.DeleteStore(context.Background(), req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify usage was detected
	if len(result.AffectedWorkspaces) != 1 {
		t.Fatalf("expected 1 affected workspace, got %d", len(result.AffectedWorkspaces))
	}

	usage := result.AffectedWorkspaces[0]
	if !usage.IsActive {
		t.Error("expected IsActive to be true")
	}
	if usage.AppliedPathCount != 1 {
		t.Errorf("expected AppliedPathCount=1, got %d", usage.AppliedPathCount)
	}

	// Verify cleanup
	cleanedWs, _ := stateStore.LoadWorkspace("ws1")
	if cleanedWs.ActiveStore != "" {
		t.Errorf("expected ActiveStore to be cleared, got %q", cleanedWs.ActiveStore)
	}
}

func TestDeleteStore_InStack(t *testing.T) {
	storeRepo := newMockStoreRepo()
	storeRepo.stores["stack-store"] = true
	stateStore := newMockStateStore()

	ws := &state.WorkspaceState{
		Repo:          "repo1",
		WorkspacePath: "services/web",
		Applied:       true,
		Mode:          "copy",
		AppliedStores: []state.AppliedStore{
			{Store: "global", Type: "copy"},
			{Store: "stack-store", Type: "copy"},
			{Store: "local", Type: "copy"},
		},
		ActiveStore: "local",
		Paths: map[string]state.PathOwnership{
			"from-stack": {Store: "stack-store", Type: "copy"},
		},
	}
	stateStore.workspaces["ws1"] = ws

	tmpDir := t.TempDir()
	wsFile := filepath.Join(tmpDir, "ws1.json")
	if err := os.WriteFile(wsFile, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	eng := newTestEngine(storeRepo, stateStore, tmpDir)

	req := &DeleteStoreRequest{
		StoreID: "stack-store",
		Force:   true,
		DryRun:  false,
	}

	result, err := eng.DeleteStore(context.Background(), req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	usage := result.AffectedWorkspaces[0]
	if !usage.InStack {
		t.Error("expected InStack to be true")
	}

	cleanedWs, _ := stateStore.LoadWorkspace("ws1")
	ids := cleanedWs.AppliedStoreIDs()
	if len(ids) != 2 || ids[0] != "global" || ids[1] != "local" {
		t.Errorf("AppliedStores = %v, want [global local]", ids)
	}
	if _, ok := cleanedWs.Paths["from-stack"]; ok {
		t.Error("expected stack-store path to be removed")
	}
}

func TestDeleteStore_AppliedPaths(t *testing.T) {
	storeRepo := newMockStoreRepo()
	storeRepo.stores["path-store"] = true
	stateStore := newMockStateStore()

	ws := &state.WorkspaceState{
		Repo:          "repo1",
		WorkspacePath: "services/app",
		Applied:       true,
		Mode:          "copy",
		Stack:         []string{},
		ActiveStore:   "other-store",
		Paths: map[string]state.PathOwnership{
			"Makefile": {Store: "path-store", Type: "copy"},
			"scripts":  {Store: "path-store", Type: "copy"},
			"config":   {Store: "path-store", Type: "copy"},
			"other":    {Store: "other-store", Type: "copy"},
		},
	}
	stateStore.workspaces["ws1"] = ws

	tmpDir := t.TempDir()
	wsFile := filepath.Join(tmpDir, "ws1.json")
	if err := os.WriteFile(wsFile, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	eng := newTestEngine(storeRepo, stateStore, tmpDir)

	req := &DeleteStoreRequest{
		StoreID: "path-store",
		Force:   true,
		DryRun:  false,
	}

	result, err := eng.DeleteStore(context.Background(), req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	usage := result.AffectedWorkspaces[0]
	if usage.AppliedPathCount != 3 {
		t.Errorf("expected AppliedPathCount=3, got %d", usage.AppliedPathCount)
	}

	// Verify paths were cleaned
	cleanedWs, _ := stateStore.LoadWorkspace("ws1")
	if len(cleanedWs.Paths) != 1 {
		t.Errorf("expected 1 remaining path, got %d", len(cleanedWs.Paths))
	}
	if _, ok := cleanedWs.Paths["other"]; !ok {
		t.Error("expected 'other' path to remain")
	}
	if cleanedWs.Applied != true {
		t.Error("expected Applied to remain true since paths exist")
	}
}

func TestDeleteStore_MultipleWorkspaces(t *testing.T) {
	storeRepo := newMockStoreRepo()
	storeRepo.stores["shared-store"] = true
	stateStore := newMockStateStore()

	// Create multiple workspaces using the same store
	ws1 := &state.WorkspaceState{
		Repo:          "repo1",
		WorkspacePath: "services/api",
		Applied:       true,
		Mode:          "symlink",
		AppliedStores: []state.AppliedStore{{Store: "shared-store", Type: "symlink"}},
		ActiveStore:   "api-store",
		Paths: map[string]state.PathOwnership{
			"api.txt": {Store: "shared-store", Type: "symlink"},
		},
	}
	ws2 := &state.WorkspaceState{
		Repo:          "repo1",
		WorkspacePath: "services/web",
		Applied:       true,
		Mode:          "symlink",
		Stack:         []string{},
		ActiveStore:   "shared-store",
		Paths: map[string]state.PathOwnership{
			"Makefile": {Store: "shared-store", Type: "symlink"},
		},
	}
	stateStore.workspaces["ws1"] = ws1
	stateStore.workspaces["ws2"] = ws2

	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "ws1.json"), []byte("{}"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "ws2.json"), []byte("{}"), 0644)

	eng := newTestEngine(storeRepo, stateStore, tmpDir)

	req := &DeleteStoreRequest{
		StoreID: "shared-store",
		Force:   true,
		DryRun:  false,
	}

	result, err := eng.DeleteStore(context.Background(), req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.AffectedWorkspaces) != 2 {
		t.Errorf("expected 2 affected workspaces, got %d", len(result.AffectedWorkspaces))
	}

	// Verify both workspaces were cleaned
	cleanedWs1, _ := stateStore.LoadWorkspace("ws1")
	if len(cleanedWs1.Stack) != 0 {
		t.Error("expected ws1 stack to be empty")
	}

	cleanedWs2, _ := stateStore.LoadWorkspace("ws2")
	if cleanedWs2.ActiveStore != "" {
		t.Error("expected ws2 ActiveStore to be cleared")
	}
	if len(cleanedWs2.Paths) != 0 {
		t.Error("expected ws2 Paths to be empty")
	}
}

func TestDeleteStore_DryRun(t *testing.T) {
	storeRepo := newMockStoreRepo()
	storeRepo.stores["test-store"] = true
	stateStore := newMockStateStore()

	ws := &state.WorkspaceState{
		Repo:          "repo1",
		WorkspacePath: "services/api",
		Applied:       true,
		Mode:          "copy",
		Stack:         []string{},
		ActiveStore:   "test-store",
		Paths: map[string]state.PathOwnership{
			"Makefile": {Store: "test-store", Type: "copy"},
		},
	}
	stateStore.workspaces["ws1"] = ws

	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "ws1.json"), []byte("{}"), 0644)

	eng := newTestEngine(storeRepo, stateStore, tmpDir)

	req := &DeleteStoreRequest{
		StoreID: "test-store",
		Force:   false,
		DryRun:  true,
	}

	result, err := eng.DeleteStore(context.Background(), req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Deleted {
		t.Error("expected Deleted=false in dry-run mode")
	}

	if !result.DryRun {
		t.Error("expected DryRun=true")
	}

	if len(result.AffectedWorkspaces) != 1 {
		t.Errorf("expected 1 affected workspace, got %d", len(result.AffectedWorkspaces))
	}

	// Verify nothing was modified
	exists, _ := storeRepo.Exists("test-store")
	if !exists {
		t.Error("store should not have been deleted in dry-run mode")
	}

	originalWs, _ := stateStore.LoadWorkspace("ws1")
	if originalWs.ActiveStore != "test-store" {
		t.Error("workspace state should not have been modified in dry-run mode")
	}
}

func TestDeleteStore_ScopedComponentWorkspaceReferences(t *testing.T) {
	eng, globalStateStore, componentStateStore, _, componentStoreRepo := newScopedDeleteStoreTestEngine(t)

	if err := componentStoreRepo.Create("component-store", stores.NewStoreMeta("component-store", time.Now())); err != nil {
		t.Fatalf("failed to create component store: %v", err)
	}

	ws := state.NewWorkspaceState("repo1", "components/api", "copy")
	ws.Applied = true
	ws.ActiveStore = "component-store"
	ws.ActiveStoreScope = stores.ScopeComponent
	ws.Stack = []string{"base-store", "component-store"}
	ws.AppliedStores = []state.AppliedStore{
		{Store: "component-store", Type: "copy"},
		{Store: "other-store", Type: "copy"},
	}
	ws.Paths["Makefile"] = state.PathOwnership{Store: "component-store", Type: "copy"}
	ws.Paths["scripts/dev.sh"] = state.PathOwnership{Store: "component-store", Type: "copy"}
	ws.Paths["README.md"] = state.PathOwnership{Store: "other-store", Type: "copy"}
	if err := componentStateStore.SaveWorkspace("component-ws", ws); err != nil {
		t.Fatal(err)
	}

	dryRunResult, err := eng.DeleteStore(context.Background(), &DeleteStoreRequest{
		StoreID: "component-store",
		Scope:   stores.ScopeComponent,
		DryRun:  true,
	})
	if err != nil {
		t.Fatalf("DeleteStore() dry-run error = %v", err)
	}
	if dryRunResult == nil {
		t.Fatal("DeleteStore() dry-run returned nil result")
	}
	if dryRunResult.Deleted {
		t.Error("dry-run should not delete the store")
	}
	if len(dryRunResult.AffectedWorkspaces) != 1 {
		t.Fatalf("affected workspaces = %d, want 1", len(dryRunResult.AffectedWorkspaces))
	}
	usage := dryRunResult.AffectedWorkspaces[0]
	if usage.WorkspaceID != "component-ws" {
		t.Errorf("WorkspaceID = %q, want %q", usage.WorkspaceID, "component-ws")
	}
	if !usage.IsActive {
		t.Error("expected component workspace usage to report active store")
	}
	if !usage.InStack {
		t.Error("expected component workspace usage to report stack reference")
	}
	if usage.AppliedPathCount != 2 {
		t.Errorf("AppliedPathCount = %d, want 2", usage.AppliedPathCount)
	}

	unchangedWs, err := componentStateStore.LoadWorkspace("component-ws")
	if err != nil {
		t.Fatal(err)
	}
	if unchangedWs.ActiveStore != "component-store" || len(unchangedWs.Paths) != 3 {
		t.Error("component workspace state should not be modified by dry-run")
	}

	forceResult, err := eng.DeleteStore(context.Background(), &DeleteStoreRequest{
		StoreID: "component-store",
		Scope:   stores.ScopeComponent,
		Force:   true,
	})
	if err != nil {
		t.Fatalf("DeleteStore() force error = %v", err)
	}
	if forceResult == nil {
		t.Fatal("DeleteStore() force returned nil result")
	}
	if !forceResult.Deleted {
		t.Error("forced delete should delete the store")
	}

	cleanedWs, err := componentStateStore.LoadWorkspace("component-ws")
	if err != nil {
		t.Fatal(err)
	}
	if cleanedWs.ActiveStore != "" {
		t.Errorf("ActiveStore = %q, want empty", cleanedWs.ActiveStore)
	}
	if len(cleanedWs.Stack) != 0 {
		t.Errorf("Stack = %v, want empty", cleanedWs.Stack)
	}
	if cleanedWs.GetAppliedStore("component-store") != nil {
		t.Error("component-store should be removed from AppliedStores")
	}
	if _, ok := cleanedWs.Paths["Makefile"]; ok {
		t.Error("Makefile should be removed from component workspace paths")
	}
	if _, ok := cleanedWs.Paths["scripts/dev.sh"]; ok {
		t.Error("scripts/dev.sh should be removed from component workspace paths")
	}
	if _, ok := cleanedWs.Paths["README.md"]; !ok {
		t.Error("README.md from other-store should remain")
	}
	if !cleanedWs.Applied {
		t.Error("Applied should remain true while other paths remain")
	}
	if _, err := globalStateStore.LoadWorkspace("component-ws"); !os.IsNotExist(err) {
		t.Error("global workspace state should not be created or updated")
	}
	exists, err := componentStoreRepo.Exists("component-store")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("component store should be deleted")
	}
}

func TestDeleteStore_InUse_WithoutForce(t *testing.T) {
	storeRepo := newMockStoreRepo()
	storeRepo.stores["active-store"] = true
	stateStore := newMockStateStore()

	ws := &state.WorkspaceState{
		Repo:          "repo1",
		WorkspacePath: "services/api",
		Applied:       true,
		Mode:          "copy",
		Stack:         []string{},
		ActiveStore:   "active-store",
		Paths: map[string]state.PathOwnership{
			"Makefile": {Store: "active-store", Type: "copy"},
		},
	}
	stateStore.workspaces["ws1"] = ws

	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "ws1.json"), []byte("{}"), 0644)

	eng := newTestEngine(storeRepo, stateStore, tmpDir)

	req := &DeleteStoreRequest{
		StoreID: "active-store",
		Force:   false, // No force
		DryRun:  false,
	}

	result, err := eng.DeleteStore(context.Background(), req)

	if err == nil {
		t.Fatal("expected error when store is in use without force")
	}

	if result == nil {
		t.Fatal("expected result even with error")
	}

	if result.Deleted {
		t.Error("expected store not to be deleted without force")
	}

	if len(result.AffectedWorkspaces) != 1 {
		t.Errorf("expected 1 affected workspace in result, got %d", len(result.AffectedWorkspaces))
	}

	// Verify store was not deleted
	exists, _ := storeRepo.Exists("active-store")
	if !exists {
		t.Error("store should not have been deleted without force")
	}
}

// sameIDDeleteFixture holds component and global stores that share an ID,
// plus optional workspaces that select one of those physical stores.
type sameIDDeleteFixture struct {
	eng            *Engine
	globalState    *state.FileStateStore
	componentState *state.FileStateStore
	globalRepo     *stores.FileStoreRepo
	componentRepo  *stores.FileStoreRepo
}

func seedSameIDDeleteFixture(t *testing.T, globalWorkspace, componentWorkspace bool) sameIDDeleteFixture {
	t.Helper()

	eng, globalState, componentState, globalRepo, componentRepo := newScopedDeleteStoreTestEngine(t)
	if err := globalRepo.Create("shared", stores.NewStoreMeta("shared", time.Now())); err != nil {
		t.Fatalf("create global store: %v", err)
	}
	if err := componentRepo.Create("shared", stores.NewStoreMeta("shared", time.Now())); err != nil {
		t.Fatalf("create component store: %v", err)
	}

	if globalWorkspace {
		ws := state.NewWorkspaceState("repo-global", "services/api", "copy")
		ws.Applied = true
		ws.ActiveStore = "shared"
		ws.ActiveStoreScope = stores.ScopeGlobal
		ws.AppliedStores = []state.AppliedStore{
			{Store: "shared", Type: "copy"},
			{Store: "keeper", Type: "copy"},
		}
		ws.Paths["a.txt"] = state.PathOwnership{Store: "shared", Type: "copy"}
		ws.Paths["keep.txt"] = state.PathOwnership{Store: "keeper", Type: "copy"}
		if err := globalState.SaveWorkspace("global-ws", ws); err != nil {
			t.Fatal(err)
		}
	}
	if componentWorkspace {
		ws := state.NewWorkspaceState("repo-component", "components/api", "copy")
		ws.Applied = true
		ws.ActiveStore = "shared"
		ws.ActiveStoreScope = stores.ScopeComponent
		ws.AppliedStores = []state.AppliedStore{
			{Store: "shared", Type: "copy"},
			{Store: "comp-keeper", Type: "copy"},
		}
		ws.Paths["a.txt"] = state.PathOwnership{Store: "shared", Type: "copy"}
		ws.Paths["keep.txt"] = state.PathOwnership{Store: "comp-keeper", Type: "copy"}
		if err := componentState.SaveWorkspace("component-ws", ws); err != nil {
			t.Fatal(err)
		}
	}

	return sameIDDeleteFixture{
		eng:            eng,
		globalState:    globalState,
		componentState: componentState,
		globalRepo:     globalRepo,
		componentRepo:  componentRepo,
	}
}

func assertScopeWorkspacePreserved(t *testing.T, store *state.FileStateStore, id, scope, keeperStore string) {
	t.Helper()

	ws, err := store.LoadWorkspace(id)
	if err != nil {
		t.Fatalf("load %s: %v", id, err)
	}
	if ws.ActiveStore != "shared" || ws.ActiveStoreScope != scope {
		t.Errorf("%s active store = %q/%q, want shared/%s", id, ws.ActiveStore, ws.ActiveStoreScope, scope)
	}
	if !ws.Applied {
		t.Errorf("%s Applied was cleared", id)
	}
	if ws.GetAppliedStore("shared") == nil {
		t.Errorf("%s lost applied store shared", id)
	}
	if ws.GetAppliedStore(keeperStore) == nil {
		t.Errorf("%s lost applied store %s", id, keeperStore)
	}
	if _, ok := ws.Paths["a.txt"]; !ok {
		t.Errorf("%s lost path a.txt", id)
	}
	if _, ok := ws.Paths["keep.txt"]; !ok {
		t.Errorf("%s lost path keep.txt", id)
	}
	if len(ws.Paths) != 2 {
		t.Errorf("%s path count = %d, want 2", id, len(ws.Paths))
	}
}

func assertScopeWorkspaceCleaned(t *testing.T, store *state.FileStateStore, id, keeperStore string) {
	t.Helper()

	ws, err := store.LoadWorkspace(id)
	if err != nil {
		t.Fatalf("load %s: %v", id, err)
	}
	if ws.ActiveStore != "" || ws.ActiveStoreScope != "" {
		t.Errorf("%s active store = %q/%q, want empty", id, ws.ActiveStore, ws.ActiveStoreScope)
	}
	if ws.GetAppliedStore("shared") != nil {
		t.Errorf("%s still has applied store shared", id)
	}
	if ws.GetAppliedStore(keeperStore) == nil {
		t.Errorf("%s lost applied store %s", id, keeperStore)
	}
	if _, ok := ws.Paths["a.txt"]; ok {
		t.Errorf("%s still has path a.txt", id)
	}
	if _, ok := ws.Paths["keep.txt"]; !ok {
		t.Errorf("%s lost path keep.txt", id)
	}
	if !ws.Applied {
		t.Errorf("%s Applied should stay true while other paths remain", id)
	}
}

func assertStoreExists(t *testing.T, repo *stores.FileStoreRepo, id string, want bool) {
	t.Helper()
	exists, err := repo.Exists(id)
	if err != nil {
		t.Fatalf("exists %s: %v", id, err)
	}
	if exists != want {
		t.Errorf("store %s exists = %v, want %v", id, exists, want)
	}
}

func TestDeleteStore_SameIDPreservesOtherScope(t *testing.T) {
	t.Run("force preferred component leaves global references", func(t *testing.T) {
		fx := seedSameIDDeleteFixture(t, true, false)

		result, err := fx.eng.DeleteStore(context.Background(), &DeleteStoreRequest{
			StoreID: "shared",
			Force:   true,
		})
		if err != nil {
			t.Fatalf("DeleteStore() error = %v", err)
		}
		if result == nil || !result.Deleted {
			t.Fatalf("DeleteStore() result = %+v, want deleted", result)
		}
		if len(result.AffectedWorkspaces) != 0 {
			t.Fatalf("affected workspaces = %+v, want none", result.AffectedWorkspaces)
		}
		assertStoreExists(t, fx.componentRepo, "shared", false)
		assertStoreExists(t, fx.globalRepo, "shared", true)
		assertScopeWorkspacePreserved(t, fx.globalState, "global-ws", stores.ScopeGlobal, "keeper")
	})

	t.Run("force explicit global leaves component references", func(t *testing.T) {
		fx := seedSameIDDeleteFixture(t, true, true)

		result, err := fx.eng.DeleteStore(context.Background(), &DeleteStoreRequest{
			StoreID: "shared",
			Scope:   stores.ScopeGlobal,
			Force:   true,
		})
		if err != nil {
			t.Fatalf("DeleteStore() error = %v", err)
		}
		if result == nil || !result.Deleted {
			t.Fatalf("DeleteStore() result = %+v, want deleted", result)
		}
		if len(result.AffectedWorkspaces) != 1 || result.AffectedWorkspaces[0].WorkspaceID != "global-ws" {
			t.Fatalf("affected workspaces = %+v, want global-ws", result.AffectedWorkspaces)
		}
		usage := result.AffectedWorkspaces[0]
		if !usage.IsActive || !usage.InStack || usage.AppliedPathCount != 1 {
			t.Errorf("usage = %+v, want active applied store and 1 path", usage)
		}
		assertStoreExists(t, fx.globalRepo, "shared", false)
		assertStoreExists(t, fx.componentRepo, "shared", true)
		assertScopeWorkspaceCleaned(t, fx.globalState, "global-ws", "keeper")
		assertScopeWorkspacePreserved(t, fx.componentState, "component-ws", stores.ScopeComponent, "comp-keeper")
	})

	t.Run("force explicit component cleans only component references", func(t *testing.T) {
		fx := seedSameIDDeleteFixture(t, true, true)

		result, err := fx.eng.DeleteStore(context.Background(), &DeleteStoreRequest{
			StoreID: "shared",
			Scope:   stores.ScopeComponent,
			Force:   true,
		})
		if err != nil {
			t.Fatalf("DeleteStore() error = %v", err)
		}
		if result == nil || !result.Deleted {
			t.Fatalf("DeleteStore() result = %+v, want deleted", result)
		}
		if len(result.AffectedWorkspaces) != 1 || result.AffectedWorkspaces[0].WorkspaceID != "component-ws" {
			t.Fatalf("affected workspaces = %+v, want component-ws", result.AffectedWorkspaces)
		}
		usage := result.AffectedWorkspaces[0]
		if !usage.IsActive || !usage.InStack || usage.AppliedPathCount != 1 {
			t.Errorf("usage = %+v, want active applied store and 1 path", usage)
		}
		assertStoreExists(t, fx.componentRepo, "shared", false)
		assertStoreExists(t, fx.globalRepo, "shared", true)
		assertScopeWorkspaceCleaned(t, fx.componentState, "component-ws", "comp-keeper")
		assertScopeWorkspacePreserved(t, fx.globalState, "global-ws", stores.ScopeGlobal, "keeper")
	})

	t.Run("non-force does not treat the other scope as in use", func(t *testing.T) {
		fx := seedSameIDDeleteFixture(t, true, false)

		result, err := fx.eng.DeleteStore(context.Background(), &DeleteStoreRequest{
			StoreID: "shared",
			Scope:   stores.ScopeComponent,
		})
		if err != nil {
			t.Fatalf("DeleteStore() error = %v", err)
		}
		if result == nil || !result.Deleted {
			t.Fatalf("DeleteStore() result = %+v, want deleted", result)
		}
		if len(result.AffectedWorkspaces) != 0 {
			t.Fatalf("affected workspaces = %+v, want none", result.AffectedWorkspaces)
		}
		assertStoreExists(t, fx.componentRepo, "shared", false)
		assertStoreExists(t, fx.globalRepo, "shared", true)
		assertScopeWorkspacePreserved(t, fx.globalState, "global-ws", stores.ScopeGlobal, "keeper")
	})

	t.Run("non-force still blocks the matching scope", func(t *testing.T) {
		fx := seedSameIDDeleteFixture(t, true, true)

		result, err := fx.eng.DeleteStore(context.Background(), &DeleteStoreRequest{
			StoreID: "shared",
		})
		if err == nil {
			t.Fatal("expected in-use error")
		}
		if result == nil || result.Deleted {
			t.Fatalf("DeleteStore() result = %+v, want not deleted", result)
		}
		if len(result.AffectedWorkspaces) != 1 || result.AffectedWorkspaces[0].WorkspaceID != "component-ws" {
			t.Fatalf("affected workspaces = %+v, want component-ws", result.AffectedWorkspaces)
		}
		usage := result.AffectedWorkspaces[0]
		if !usage.IsActive || !usage.InStack || usage.AppliedPathCount != 1 {
			t.Errorf("usage = %+v, want active applied store and 1 path", usage)
		}
		assertStoreExists(t, fx.componentRepo, "shared", true)
		assertStoreExists(t, fx.globalRepo, "shared", true)
		assertScopeWorkspacePreserved(t, fx.globalState, "global-ws", stores.ScopeGlobal, "keeper")
		assertScopeWorkspacePreserved(t, fx.componentState, "component-ws", stores.ScopeComponent, "comp-keeper")
	})

	t.Run("dry-run reports only the resolved scope", func(t *testing.T) {
		fx := seedSameIDDeleteFixture(t, true, true)

		componentDry, err := fx.eng.DeleteStore(context.Background(), &DeleteStoreRequest{
			StoreID: "shared",
			DryRun:  true,
		})
		if err != nil {
			t.Fatalf("component dry-run error = %v", err)
		}
		if componentDry == nil || componentDry.Deleted || !componentDry.DryRun {
			t.Fatalf("component dry-run = %+v", componentDry)
		}
		if len(componentDry.AffectedWorkspaces) != 1 || componentDry.AffectedWorkspaces[0].WorkspaceID != "component-ws" {
			t.Fatalf("component dry-run workspaces = %+v, want component-ws", componentDry.AffectedWorkspaces)
		}
		usage := componentDry.AffectedWorkspaces[0]
		if !usage.IsActive || !usage.InStack || usage.AppliedPathCount != 1 {
			t.Errorf("component dry-run usage = %+v, want active applied store and 1 path", usage)
		}

		globalDry, err := fx.eng.DeleteStore(context.Background(), &DeleteStoreRequest{
			StoreID: "shared",
			Scope:   stores.ScopeGlobal,
			DryRun:  true,
		})
		if err != nil {
			t.Fatalf("global dry-run error = %v", err)
		}
		if globalDry == nil || globalDry.Deleted || !globalDry.DryRun {
			t.Fatalf("global dry-run = %+v", globalDry)
		}
		if len(globalDry.AffectedWorkspaces) != 1 || globalDry.AffectedWorkspaces[0].WorkspaceID != "global-ws" {
			t.Fatalf("global dry-run workspaces = %+v, want global-ws", globalDry.AffectedWorkspaces)
		}
		usage = globalDry.AffectedWorkspaces[0]
		if !usage.IsActive || !usage.InStack || usage.AppliedPathCount != 1 {
			t.Errorf("global dry-run usage = %+v, want active applied store and 1 path", usage)
		}

		assertStoreExists(t, fx.componentRepo, "shared", true)
		assertStoreExists(t, fx.globalRepo, "shared", true)
		assertScopeWorkspacePreserved(t, fx.globalState, "global-ws", stores.ScopeGlobal, "keeper")
		assertScopeWorkspacePreserved(t, fx.componentState, "component-ws", stores.ScopeComponent, "comp-keeper")
	})
}
