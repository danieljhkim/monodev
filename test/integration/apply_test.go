package integration

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/danieljhkim/monodev/internal/engine"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

func TestApply_FullCycle(t *testing.T) {
	eng, fs, stateStore, storeRepo, _ := setupTestEngine(t)
	ctx := context.Background()

	// Setup: Create a store with tracked files
	storeID := "test-store"
	overlayRoot := "/stores/test-store/overlay"
	storeRepo.setOverlayRoot(storeID, overlayRoot)

	// Create source files in store
	fs.files[filepath.Join(overlayRoot, "Makefile")] = []byte("all: build\n")
	fs.dirs[overlayRoot] = true

	// Track the file
	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{
		{Path: "Makefile", Kind: "file"},
	}
	storeRepo.setTrack(storeID, track)

	// Setup repo state with active store
	repoState := state.NewRepoState("repo-fingerprint-123")
	repoState.ActiveStore = storeID
	_ = stateStore.SaveRepoState("repo-fingerprint-123", repoState)

	// Apply
	req := &engine.ApplyRequest{
		CWD:  "/repo/workspace",
		Mode: "symlink",
	}

	result, err := eng.Apply(ctx, req)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Verify plan was generated
	if result.Plan == nil {
		t.Fatal("expected plan to be generated")
	}

	// Verify operations were applied
	if len(result.Applied) == 0 {
		t.Fatal("expected operations to be applied")
	}

	// Verify symlink was created
	symlinkPath := "/repo/workspace/Makefile"
	if _, ok := fs.symlinks[symlinkPath]; !ok {
		t.Errorf("expected symlink at %s", symlinkPath)
	}

	// Verify workspace state was saved
	workspaceID := result.WorkspaceID
	ws, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("failed to load workspace state: %v", err)
	}

	if !ws.Applied {
		t.Error("expected workspace to be marked as applied")
	}

	if ws.Mode != "symlink" {
		t.Errorf("expected mode='symlink', got %q", ws.Mode)
	}

	if len(ws.Paths) != 1 {
		t.Errorf("expected 1 path in workspace state, got %d", len(ws.Paths))
	}

	if _, ok := ws.Paths["Makefile"]; !ok {
		t.Error("expected Makefile in workspace state paths")
	}
}

func TestApply_Idempotency(t *testing.T) {
	eng, fs, stateStore, storeRepo, _ := setupTestEngine(t)
	ctx := context.Background()

	// Setup store
	storeID := "test-store"
	overlayRoot := "/stores/test-store/overlay"
	storeRepo.setOverlayRoot(storeID, overlayRoot)
	fs.files[filepath.Join(overlayRoot, "Makefile")] = []byte("all: build\n")
	fs.dirs[overlayRoot] = true

	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{
		{Path: "Makefile", Kind: "file"},
	}
	storeRepo.setTrack(storeID, track)

	repoState := state.NewRepoState("repo-fingerprint-123")
	repoState.ActiveStore = storeID
	_ = stateStore.SaveRepoState("repo-fingerprint-123", repoState)

	req := &engine.ApplyRequest{
		CWD:  "/repo/workspace",
		Mode: "symlink",
	}

	// First apply
	result1, err := eng.Apply(ctx, req)
	if err != nil {
		t.Fatalf("First Apply() error = %v", err)
	}

	ops1 := len(result1.Applied)

	// Second apply (should be idempotent)
	result2, err := eng.Apply(ctx, req)
	if err != nil {
		t.Fatalf("Second Apply() error = %v", err)
	}

	// Should have fewer or same operations (removing old, creating new)
	ops2 := len(result2.Applied)
	if ops2 > ops1*2 {
		t.Errorf("expected idempotent apply, got %d operations (first: %d)", ops2, ops1)
	}

	// Verify state is consistent
	workspaceID := result1.WorkspaceID
	ws, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("failed to load workspace state: %v", err)
	}

	if !ws.Applied {
		t.Error("expected workspace to still be applied")
	}
}

func TestApply_StatePersistence(t *testing.T) {
	eng, fs, stateStore, storeRepo, _ := setupTestEngine(t)
	ctx := context.Background()

	// Setup
	storeID := "test-store"
	overlayRoot := "/stores/test-store/overlay"
	storeRepo.setOverlayRoot(storeID, overlayRoot)
	fs.files[filepath.Join(overlayRoot, "config.json")] = []byte(`{"key": "value"}`)
	fs.dirs[overlayRoot] = true

	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{
		{Path: "config.json", Kind: "file"},
	}
	storeRepo.setTrack(storeID, track)

	repoState := state.NewRepoState("repo-fingerprint-123")
	repoState.ActiveStore = storeID
	_ = stateStore.SaveRepoState("repo-fingerprint-123", repoState)

	// Apply
	req := &engine.ApplyRequest{
		CWD:  "/repo/workspace",
		Mode: "copy",
	}

	result, err := eng.Apply(ctx, req)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Verify state was persisted
	workspaceID := result.WorkspaceID
	ws, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("failed to load workspace state: %v", err)
	}

	// Verify all fields are persisted
	if ws.Repo != "repo-fingerprint-123" {
		t.Errorf("expected Repo='repo-fingerprint-123', got %q", ws.Repo)
	}

	if ws.WorkspacePath != "workspace" {
		t.Errorf("expected WorkspacePath='workspace', got %q", ws.WorkspacePath)
	}

	if ws.Mode != "copy" {
		t.Errorf("expected Mode='copy', got %q", ws.Mode)
	}

	if ws.ActiveStore != storeID {
		t.Errorf("expected ActiveStore=%q, got %q", storeID, ws.ActiveStore)
	}

	// Verify path ownership is persisted
	if len(ws.Paths) != 1 {
		t.Errorf("expected 1 path, got %d", len(ws.Paths))
	}

	ownership, ok := ws.Paths["config.json"]
	if !ok {
		t.Fatal("expected config.json in paths")
	}

	if ownership.Store != storeID {
		t.Errorf("expected Store=%q, got %q", storeID, ownership.Store)
	}

	if ownership.Type != "copy" {
		t.Errorf("expected Type='copy', got %q", ownership.Type)
	}
}

func TestApply_DryRun(t *testing.T) {
	eng, fs, stateStore, storeRepo, _ := setupTestEngine(t)
	ctx := context.Background()

	// Setup
	storeID := "test-store"
	overlayRoot := "/stores/test-store/overlay"
	storeRepo.setOverlayRoot(storeID, overlayRoot)
	fs.files[filepath.Join(overlayRoot, "script.sh")] = []byte("#!/bin/bash\necho test\n")
	fs.dirs[overlayRoot] = true

	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{
		{Path: "script.sh", Kind: "file"},
	}
	storeRepo.setTrack(storeID, track)

	repoState := state.NewRepoState("repo-fingerprint-123")
	repoState.ActiveStore = storeID
	_ = stateStore.SaveRepoState("repo-fingerprint-123", repoState)

	// Dry run apply
	req := &engine.ApplyRequest{
		CWD:    "/repo/workspace",
		Mode:   "symlink",
		DryRun: true,
	}

	result, err := eng.Apply(ctx, req)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Verify plan was generated
	if result.Plan == nil {
		t.Fatal("expected plan to be generated")
	}

	// Verify no operations were applied
	if len(result.Applied) != 0 {
		t.Errorf("expected 0 applied operations in dry-run, got %d", len(result.Applied))
	}

	// Verify no symlink was created
	symlinkPath := "/repo/workspace/script.sh"
	if _, ok := fs.symlinks[symlinkPath]; ok {
		t.Error("expected no symlink to be created in dry-run mode")
	}

	// Verify workspace state was NOT saved (dry-run)
	workspaceID := result.WorkspaceID
	_, err = stateStore.LoadWorkspace(workspaceID)
	if err == nil {
		t.Error("expected workspace state to not be saved in dry-run mode")
	}
}

func TestApply_CopyMode(t *testing.T) {
	eng, fs, stateStore, storeRepo, _ := setupTestEngine(t)
	ctx := context.Background()

	// Setup
	storeID := "test-store"
	overlayRoot := "/stores/test-store/overlay"
	storeRepo.setOverlayRoot(storeID, overlayRoot)
	sourceFile := filepath.Join(overlayRoot, "data.txt")
	fs.files[sourceFile] = []byte("test data")
	fs.dirs[overlayRoot] = true

	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{
		{Path: "data.txt", Kind: "file"},
	}
	storeRepo.setTrack(storeID, track)

	repoState := state.NewRepoState("repo-fingerprint-123")
	repoState.ActiveStore = storeID
	_ = stateStore.SaveRepoState("repo-fingerprint-123", repoState)

	// Apply in copy mode
	req := &engine.ApplyRequest{
		CWD:  "/repo/workspace",
		Mode: "copy",
	}

	result, err := eng.Apply(ctx, req)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Verify file was copied (not symlinked)
	destPath := "/repo/workspace/data.txt"
	if _, ok := fs.symlinks[destPath]; ok {
		t.Error("expected no symlink in copy mode")
	}

	if _, ok := fs.files[destPath]; !ok {
		t.Error("expected file to be copied")
	}

	// Verify checksum was computed and stored
	workspaceID := result.WorkspaceID
	ws, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("failed to load workspace state: %v", err)
	}

	ownership, ok := ws.Paths["data.txt"]
	if !ok {
		t.Fatal("expected data.txt in paths")
	}

	if ownership.Checksum == "" {
		t.Error("expected checksum to be computed in copy mode")
	}
}

func TestApply_MultipleStores(t *testing.T) {
	eng, fs, stateStore, storeRepo, _ := setupTestEngine(t)
	ctx := context.Background()

	// Setup two stores
	store1 := "store1"
	store2 := "store2"
	overlayRoot1 := "/stores/store1/overlay"
	overlayRoot2 := "/stores/store2/overlay"

	storeRepo.setOverlayRoot(store1, overlayRoot1)
	storeRepo.setOverlayRoot(store2, overlayRoot2)

	// Store1 has Makefile
	fs.files[filepath.Join(overlayRoot1, "Makefile")] = []byte("all: build\n")
	fs.dirs[overlayRoot1] = true

	// Store2 also has Makefile (should override)
	fs.files[filepath.Join(overlayRoot2, "Makefile")] = []byte("all: test\n")
	fs.dirs[overlayRoot2] = true

	track1 := stores.NewTrackFile()
	track1.Tracked = []stores.TrackedPath{
		{Path: "Makefile", Kind: "file"},
	}
	storeRepo.setTrack(store1, track1)

	track2 := stores.NewTrackFile()
	track2.Tracked = []stores.TrackedPath{
		{Path: "Makefile", Kind: "file"},
	}
	storeRepo.setTrack(store2, track2)

	// Setup repo state with stack + active store
	repoState := state.NewRepoState("repo-fingerprint-123")
	repoState.Stack = []string{store1}
	repoState.ActiveStore = store2
	_ = stateStore.SaveRepoState("repo-fingerprint-123", repoState)

	// Apply
	req := &engine.ApplyRequest{
		CWD:  "/repo/workspace",
		Mode: "symlink",
	}

	result, err := eng.Apply(ctx, req)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Verify store2's Makefile was applied (later store takes precedence)
	symlinkPath := "/repo/workspace/Makefile"
	target, ok := fs.symlinks[symlinkPath]
	if !ok {
		t.Fatal("expected symlink to be created")
	}

	expectedTarget := filepath.Join(overlayRoot2, "Makefile")
	if target != expectedTarget {
		t.Errorf("expected symlink target %q, got %q", expectedTarget, target)
	}

	// Verify workspace state shows store2 as owner
	workspaceID := result.WorkspaceID
	ws, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("failed to load workspace state: %v", err)
	}

	ownership, ok := ws.Paths["Makefile"]
	if !ok {
		t.Fatal("expected Makefile in paths")
	}

	if ownership.Store != store2 {
		t.Errorf("expected Store=%q (store2 takes precedence), got %q", store2, ownership.Store)
	}
}
