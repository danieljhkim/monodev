package planner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// mockStoreRepo is a mock implementation of stores.StoreRepo for testing
type mockStoreRepo struct {
	tracks       map[string]*stores.TrackFile
	overlayRoots map[string]string
}

func newMockStoreRepo() *mockStoreRepo {
	return &mockStoreRepo{
		tracks:       make(map[string]*stores.TrackFile),
		overlayRoots: make(map[string]string),
	}
}

func (m *mockStoreRepo) setTrack(storeID string, track *stores.TrackFile) {
	m.tracks[storeID] = track
}

func (m *mockStoreRepo) setOverlayRoot(storeID, root string) {
	m.overlayRoots[storeID] = root
}

func (m *mockStoreRepo) LoadTrack(id string) (*stores.TrackFile, error) {
	if track, ok := m.tracks[id]; ok {
		return track, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockStoreRepo) OverlayRoot(id string) string {
	if root, ok := m.overlayRoots[id]; ok {
		return root
	}
	return filepath.Join("/stores", id, "overlay")
}

// Unused methods for mockStoreRepo
func (m *mockStoreRepo) List() ([]string, error)                            { return nil, nil }
func (m *mockStoreRepo) Exists(id string) (bool, error)                     { return false, nil }
func (m *mockStoreRepo) Create(id string, meta *stores.StoreMeta) error     { return nil }
func (m *mockStoreRepo) LoadMeta(id string) (*stores.StoreMeta, error)      { return nil, nil }
func (m *mockStoreRepo) SaveMeta(id string, meta *stores.StoreMeta) error   { return nil }
func (m *mockStoreRepo) SaveTrack(id string, track *stores.TrackFile) error { return nil }
func (m *mockStoreRepo) Delete(id string) error                             { return nil }

func TestBuildApplyPlan_SingleStore(t *testing.T) {
	fs := newMockFS()
	storeRepo := newMockStoreRepo()
	workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")

	// Setup store with one tracked file
	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{
		{Path: "Makefile", Kind: "file"},
	}
	storeRepo.setTrack("store1", track)
	storeRepo.setOverlayRoot("store1", "/stores/store1/overlay")

	// Source file exists
	fs.setExists("/stores/store1/overlay/Makefile", true)
	fs.setExists("/workspace/Makefile", false)

	plan, err := BuildApplyPlan(workspace, []string{"store1"}, "symlink", "/workspace", storeRepo, fs, false)
	if err != nil {
		t.Fatalf("BuildApplyPlan failed: %v", err)
	}

	if len(plan.Stores) != 1 || plan.Stores[0] != "store1" {
		t.Errorf("expected store1 in plan, got %v", plan.Stores)
	}
	if len(plan.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(plan.Operations))
	}
	if plan.Operations[0].Type != OpCreateSymlink {
		t.Errorf("expected create_symlink operation, got %q", plan.Operations[0].Type)
	}
	if plan.Operations[0].RelPath != "Makefile" {
		t.Errorf("expected RelPath='Makefile', got %q", plan.Operations[0].RelPath)
	}
	if plan.Operations[0].Store != "store1" {
		t.Errorf("expected Store='store1', got %q", plan.Operations[0].Store)
	}
	if len(plan.Conflicts) != 0 {
		t.Errorf("expected no conflicts, got %d", len(plan.Conflicts))
	}
}

func TestBuildApplyPlan_MultipleStores_Precedence(t *testing.T) {
	fs := newMockFS()
	storeRepo := newMockStoreRepo()
	workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")

	// Store1 has Makefile
	track1 := stores.NewTrackFile()
	track1.Tracked = []stores.TrackedPath{
		{Path: "Makefile", Kind: "file"},
	}
	storeRepo.setTrack("store1", track1)
	storeRepo.setOverlayRoot("store1", "/stores/store1/overlay")

	// Store2 also has Makefile (should override store1)
	track2 := stores.NewTrackFile()
	track2.Tracked = []stores.TrackedPath{
		{Path: "Makefile", Kind: "file"},
	}
	storeRepo.setTrack("store2", track2)
	storeRepo.setOverlayRoot("store2", "/stores/store2/overlay")

	// Both source files exist
	fs.setExists("/stores/store1/overlay/Makefile", true)
	fs.setExists("/stores/store2/overlay/Makefile", true)
	fs.setExists("/workspace/Makefile", false)

	plan, err := BuildApplyPlan(workspace, []string{"store1", "store2"}, "symlink", "/workspace", storeRepo, fs, false)
	if err != nil {
		t.Fatalf("BuildApplyPlan failed: %v", err)
	}

	// Should have: create(store1), remove(store1), create(store2)
	// Store1 processes first and adds create, then store2 adds remove for store1 and create for itself
	if len(plan.Operations) != 3 {
		t.Fatalf("expected 3 operations, got %d", len(plan.Operations))
	}

	// First operation should be create from store1
	if plan.Operations[0].Type != OpCreateSymlink {
		t.Errorf("expected first operation to be create_symlink, got %q", plan.Operations[0].Type)
	}
	if plan.Operations[0].Store != "store1" {
		t.Errorf("expected first operation from store1, got %q", plan.Operations[0].Store)
	}

	// Second operation should be remove from store1
	if plan.Operations[1].Type != OpRemove {
		t.Errorf("expected second operation to be remove, got %q", plan.Operations[1].Type)
	}
	if plan.Operations[1].Store != "store1" {
		t.Errorf("expected remove operation from store1, got %q", plan.Operations[1].Store)
	}

	// Third operation should be create from store2
	if plan.Operations[2].Type != OpCreateSymlink {
		t.Errorf("expected third operation to be create_symlink, got %q", plan.Operations[2].Type)
	}
	if plan.Operations[2].Store != "store2" {
		t.Errorf("expected create operation from store2, got %q", plan.Operations[2].Store)
	}
}

func TestBuildApplyPlan_ConflictDetection(t *testing.T) {
	fs := newMockFS()
	storeRepo := newMockStoreRepo()
	workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")

	// Unmanaged file exists at destination
	workspace.Paths = make(map[string]state.PathOwnership) // Empty - path is unmanaged

	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{
		{Path: "Makefile", Kind: "file"},
	}
	storeRepo.setTrack("store1", track)
	storeRepo.setOverlayRoot("store1", "/stores/store1/overlay")

	fs.setExists("/stores/store1/overlay/Makefile", true)
	fs.setExists("/workspace/Makefile", true) // Unmanaged file exists
	fs.setLstat("/workspace/Makefile", &mockFileInfo{name: "Makefile", isDir: false})

	plan, err := BuildApplyPlan(workspace, []string{"store1"}, "symlink", "/workspace", storeRepo, fs, false)
	if err != nil {
		t.Fatalf("BuildApplyPlan failed: %v", err)
	}

	if !plan.HasConflicts() {
		t.Error("expected conflicts but plan has none")
	}
	if len(plan.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(plan.Conflicts))
	}
	if plan.Conflicts[0].Path != "Makefile" {
		t.Errorf("expected conflict at 'Makefile', got %q", plan.Conflicts[0].Path)
	}
	if plan.Conflicts[0].Existing != "unmanaged" {
		t.Errorf("expected Existing='unmanaged', got %q", plan.Conflicts[0].Existing)
	}
}

func TestBuildApplyPlan_ForceMode(t *testing.T) {
	fs := newMockFS()
	storeRepo := newMockStoreRepo()
	workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")

	// Unmanaged file exists
	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{
		{Path: "Makefile", Kind: "file"},
	}
	storeRepo.setTrack("store1", track)
	storeRepo.setOverlayRoot("store1", "/stores/store1/overlay")

	fs.setExists("/stores/store1/overlay/Makefile", true)
	fs.setExists("/workspace/Makefile", true) // Unmanaged file exists
	fs.setLstat("/workspace/Makefile", &mockFileInfo{name: "Makefile", isDir: false})

	plan, err := BuildApplyPlan(workspace, []string{"store1"}, "symlink", "/workspace", storeRepo, fs, true)
	if err != nil {
		t.Fatalf("BuildApplyPlan failed: %v", err)
	}

	// With force, should have remove + create operations, no conflicts
	if plan.HasConflicts() {
		t.Error("expected no conflicts with force mode")
	}
	if len(plan.Operations) != 2 {
		t.Fatalf("expected 2 operations (remove + create), got %d", len(plan.Operations))
	}
	if plan.Operations[0].Type != OpRemove {
		t.Errorf("expected first operation to be remove, got %q", plan.Operations[0].Type)
	}
	if plan.Operations[1].Type != OpCreateSymlink {
		t.Errorf("expected second operation to be create_symlink, got %q", plan.Operations[1].Type)
	}
}

func TestBuildApplyPlan_RequiredPathMissing(t *testing.T) {
	fs := newMockFS()
	storeRepo := newMockStoreRepo()
	workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")

	// Track a required path that doesn't exist in store
	required := true
	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{
		{Path: "Makefile", Kind: "file", Required: &required},
	}
	storeRepo.setTrack("store1", track)
	storeRepo.setOverlayRoot("store1", "/stores/store1/overlay")

	// Source file does NOT exist
	fs.setExists("/stores/store1/overlay/Makefile", false)
	fs.setExists("/workspace/Makefile", false)

	plan, err := BuildApplyPlan(workspace, []string{"store1"}, "symlink", "/workspace", storeRepo, fs, false)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Should skip missing path with a warning, no operations
	if len(plan.Operations) != 0 {
		t.Errorf("expected 0 operations for missing path, got %d", len(plan.Operations))
	}
	if len(plan.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(plan.Warnings))
	}
	if plan.Warnings[0] != "tracked path Makefile not found in store store1 (skipping)" {
		t.Errorf("unexpected warning: %s", plan.Warnings[0])
	}
}

func TestBuildApplyPlan_OptionalPathMissing(t *testing.T) {
	fs := newMockFS()
	storeRepo := newMockStoreRepo()
	workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")

	// Track an optional path that doesn't exist in store
	required := false
	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{
		{Path: "Makefile", Kind: "file", Required: &required},
	}
	storeRepo.setTrack("store1", track)
	storeRepo.setOverlayRoot("store1", "/stores/store1/overlay")

	// Source file does NOT exist
	fs.setExists("/stores/store1/overlay/Makefile", false)
	fs.setExists("/workspace/Makefile", false)

	plan, err := BuildApplyPlan(workspace, []string{"store1"}, "symlink", "/workspace", storeRepo, fs, false)
	if err != nil {
		t.Fatalf("BuildApplyPlan failed: %v", err)
	}

	// Should skip optional path, no operations
	if len(plan.Operations) != 0 {
		t.Errorf("expected 0 operations for missing optional path, got %d", len(plan.Operations))
	}
}

func TestBuildApplyPlan_CopyMode(t *testing.T) {
	fs := newMockFS()
	storeRepo := newMockStoreRepo()
	workspace := state.NewWorkspaceState("repo1", "workspace", "copy")

	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{
		{Path: "Makefile", Kind: "file"},
	}
	storeRepo.setTrack("store1", track)
	storeRepo.setOverlayRoot("store1", "/stores/store1/overlay")

	fs.setExists("/stores/store1/overlay/Makefile", true)
	fs.setExists("/workspace/Makefile", false)

	plan, err := BuildApplyPlan(workspace, []string{"store1"}, "copy", "/workspace", storeRepo, fs, false)
	if err != nil {
		t.Fatalf("BuildApplyPlan failed: %v", err)
	}

	if len(plan.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(plan.Operations))
	}
	if plan.Operations[0].Type != OpCopy {
		t.Errorf("expected copy operation, got %q", plan.Operations[0].Type)
	}
}

func TestBuildApplyPlan_DirectoryHandling(t *testing.T) {
	fs := newMockFS()
	storeRepo := newMockStoreRepo()
	workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")

	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{
		{Path: "scripts", Kind: "dir"},
	}
	storeRepo.setTrack("store1", track)
	storeRepo.setOverlayRoot("store1", "/stores/store1/overlay")

	fs.setExists("/stores/store1/overlay/scripts", true)
	fs.setExists("/workspace/scripts", false)

	plan, err := BuildApplyPlan(workspace, []string{"store1"}, "symlink", "/workspace", storeRepo, fs, false)
	if err != nil {
		t.Fatalf("BuildApplyPlan failed: %v", err)
	}

	if len(plan.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(plan.Operations))
	}
	if plan.Operations[0].Type != OpCreateSymlink {
		t.Errorf("expected create_symlink operation, got %q", plan.Operations[0].Type)
	}
}

func TestBuildApplyPlan_MultiplePaths(t *testing.T) {
	fs := newMockFS()
	storeRepo := newMockStoreRepo()
	workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")

	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{
		{Path: "Makefile", Kind: "file"},
		{Path: "scripts", Kind: "dir"},
		{Path: "config.json", Kind: "file"},
	}
	storeRepo.setTrack("store1", track)
	storeRepo.setOverlayRoot("store1", "/stores/store1/overlay")

	fs.setExists("/stores/store1/overlay/Makefile", true)
	fs.setExists("/stores/store1/overlay/scripts", true)
	fs.setExists("/stores/store1/overlay/config.json", true)
	fs.setExists("/workspace/Makefile", false)
	fs.setExists("/workspace/scripts", false)
	fs.setExists("/workspace/config.json", false)

	plan, err := BuildApplyPlan(workspace, []string{"store1"}, "symlink", "/workspace", storeRepo, fs, false)
	if err != nil {
		t.Fatalf("BuildApplyPlan failed: %v", err)
	}

	if len(plan.Operations) != 3 {
		t.Fatalf("expected 3 operations, got %d", len(plan.Operations))
	}

	// Check that all paths are included
	paths := make(map[string]bool)
	for _, op := range plan.Operations {
		paths[op.RelPath] = true
	}
	if !paths["Makefile"] || !paths["scripts"] || !paths["config.json"] {
		t.Errorf("expected all paths in operations, got %v", paths)
	}
}

func TestBuildApplyPlan_StoreNotFound(t *testing.T) {
	fs := newMockFS()
	storeRepo := newMockStoreRepo()
	workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")

	// Don't set track for store1, so LoadTrack will return error

	_, err := BuildApplyPlan(workspace, []string{"store1"}, "symlink", "/workspace", storeRepo, fs, false)
	if err == nil {
		t.Fatal("expected error for store not found")
	}
}

func TestBuildApplyPlan_StoreToStoreOverride(t *testing.T) {
	fs := newMockFS()
	storeRepo := newMockStoreRepo()
	workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")

	// Store1 already has Makefile applied
	workspace.Paths["Makefile"] = state.PathOwnership{
		Store: "store1",
		Type:  "symlink",
	}

	// Store1 has Makefile
	track1 := stores.NewTrackFile()
	track1.Tracked = []stores.TrackedPath{
		{Path: "Makefile", Kind: "file"},
	}
	storeRepo.setTrack("store1", track1)
	storeRepo.setOverlayRoot("store1", "/stores/store1/overlay")

	// Store2 also has Makefile (should override)
	track2 := stores.NewTrackFile()
	track2.Tracked = []stores.TrackedPath{
		{Path: "Makefile", Kind: "file"},
	}
	storeRepo.setTrack("store2", track2)
	storeRepo.setOverlayRoot("store2", "/stores/store2/overlay")

	fs.setExists("/stores/store1/overlay/Makefile", true)
	fs.setExists("/stores/store2/overlay/Makefile", true)
	fs.setExists("/workspace/Makefile", true)
	fs.setLstat("/workspace/Makefile", &mockFileInfo{name: "Makefile", isDir: false})
	fs.setReadlink("/workspace/Makefile", "/stores/store1/overlay/Makefile", nil)

	plan, err := BuildApplyPlan(workspace, []string{"store1", "store2"}, "symlink", "/workspace", storeRepo, fs, false)
	if err != nil {
		t.Fatalf("BuildApplyPlan failed: %v", err)
	}

	// Should have: create(store1), remove(store1), create(store2)
	// Store1 processes first and adds create, then store2 adds remove for store1 and create for itself
	if len(plan.Operations) != 3 {
		t.Fatalf("expected 3 operations, got %d", len(plan.Operations))
	}
	if plan.Operations[0].Type != OpCreateSymlink || plan.Operations[0].Store != "store1" {
		t.Errorf("expected first operation to be create_symlink from store1, got %q from %q", plan.Operations[0].Type, plan.Operations[0].Store)
	}
	if plan.Operations[1].Type != OpRemove || plan.Operations[1].Store != "store1" {
		t.Errorf("expected second operation to be remove from store1, got %q from %q", plan.Operations[1].Type, plan.Operations[1].Store)
	}
	if plan.Operations[2].Type != OpCreateSymlink || plan.Operations[2].Store != "store2" {
		t.Errorf("expected third operation to be create_symlink from store2, got %q from %q", plan.Operations[2].Type, plan.Operations[2].Store)
	}
}

func TestBuildApplyPlan_ModeMismatchConflict(t *testing.T) {
	fs := newMockFS()
	storeRepo := newMockStoreRepo()
	workspace := state.NewWorkspaceState("repo1", "workspace", "copy")

	// Existing path is symlink, but we're trying to apply in copy mode
	workspace.Paths["Makefile"] = state.PathOwnership{
		Store: "store1",
		Type:  "symlink",
	}

	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{
		{Path: "Makefile", Kind: "file"},
	}
	storeRepo.setTrack("store2", track)
	storeRepo.setOverlayRoot("store2", "/stores/store2/overlay")

	fs.setExists("/stores/store2/overlay/Makefile", true)
	fs.setExists("/workspace/Makefile", true)
	fs.setLstat("/workspace/Makefile", &mockFileInfo{name: "Makefile", isDir: false})
	fs.setReadlink("/workspace/Makefile", "/stores/store1/overlay/Makefile", nil)

	plan, err := BuildApplyPlan(workspace, []string{"store2"}, "copy", "/workspace", storeRepo, fs, false)
	if err != nil {
		t.Fatalf("BuildApplyPlan failed: %v", err)
	}

	if !plan.HasConflicts() {
		t.Error("expected conflict for mode mismatch")
	}
	if len(plan.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(plan.Conflicts))
	}
	if plan.Conflicts[0].Existing != "symlink" || plan.Conflicts[0].Incoming != "copy" {
		t.Errorf("expected mode mismatch conflict, got %v", plan.Conflicts[0])
	}
}

func TestBuildApplyPlan_TypeMismatchConflict(t *testing.T) {
	fs := newMockFS()
	storeRepo := newMockStoreRepo()
	workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")

	// Existing path is a directory
	workspace.Paths["path"] = state.PathOwnership{
		Store: "store1",
		Type:  "symlink",
	}

	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{
		{Path: "path", Kind: "file"}, // Trying to create file where directory exists
	}
	storeRepo.setTrack("store2", track)
	storeRepo.setOverlayRoot("store2", "/stores/store2/overlay")

	fs.setExists("/stores/store2/overlay/path", true)
	fs.setExists("/workspace/path", true)
	fs.setLstat("/workspace/path", &mockFileInfo{name: "path", isDir: true})
	fs.setReadlink("/workspace/path", "/stores/store1/overlay/path", nil)

	plan, err := BuildApplyPlan(workspace, []string{"store2"}, "symlink", "/workspace", storeRepo, fs, false)
	if err != nil {
		t.Fatalf("BuildApplyPlan failed: %v", err)
	}

	if !plan.HasConflicts() {
		t.Error("expected conflict for type mismatch")
	}
	if len(plan.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(plan.Conflicts))
	}
	if plan.Conflicts[0].Existing != "directory" || plan.Conflicts[0].Incoming != "file" {
		t.Errorf("expected type mismatch conflict, got %v", plan.Conflicts[0])
	}
}

func TestBuildApplyPlan_EmptyStore(t *testing.T) {
	fs := newMockFS()
	storeRepo := newMockStoreRepo()
	workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")

	// Store with no tracked paths
	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{}
	storeRepo.setTrack("store1", track)
	storeRepo.setOverlayRoot("store1", "/stores/store1/overlay")

	plan, err := BuildApplyPlan(workspace, []string{"store1"}, "symlink", "/workspace", storeRepo, fs, false)
	if err != nil {
		t.Fatalf("BuildApplyPlan failed: %v", err)
	}

	if len(plan.Operations) != 0 {
		t.Errorf("expected 0 operations for empty store, got %d", len(plan.Operations))
	}
	if len(plan.Conflicts) != 0 {
		t.Errorf("expected 0 conflicts for empty store, got %d", len(plan.Conflicts))
	}
}

func TestBuildApplyPlan_PathOwnershipTracking(t *testing.T) {
	fs := newMockFS()
	storeRepo := newMockStoreRepo()
	workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")

	// Two stores with different paths
	track1 := stores.NewTrackFile()
	track1.Tracked = []stores.TrackedPath{
		{Path: "Makefile", Kind: "file"},
	}
	storeRepo.setTrack("store1", track1)
	storeRepo.setOverlayRoot("store1", "/stores/store1/overlay")

	track2 := stores.NewTrackFile()
	track2.Tracked = []stores.TrackedPath{
		{Path: "script.sh", Kind: "file"},
	}
	storeRepo.setTrack("store2", track2)
	storeRepo.setOverlayRoot("store2", "/stores/store2/overlay")

	fs.setExists("/stores/store1/overlay/Makefile", true)
	fs.setExists("/stores/store2/overlay/script.sh", true)
	fs.setExists("/workspace/Makefile", false)
	fs.setExists("/workspace/script.sh", false)

	plan, err := BuildApplyPlan(workspace, []string{"store1", "store2"}, "symlink", "/workspace", storeRepo, fs, false)
	if err != nil {
		t.Fatalf("BuildApplyPlan failed: %v", err)
	}

	if len(plan.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(plan.Operations))
	}

	// Check that each operation has correct store
	storeMap := make(map[string]string)
	for _, op := range plan.Operations {
		storeMap[op.RelPath] = op.Store
	}
	if storeMap["Makefile"] != "store1" {
		t.Errorf("expected Makefile from store1, got %q", storeMap["Makefile"])
	}
	if storeMap["script.sh"] != "store2" {
		t.Errorf("expected script.sh from store2, got %q", storeMap["script.sh"])
	}
}
