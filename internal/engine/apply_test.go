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
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

type cancelOnLoadTrackStoreRepo struct {
	*trackStoreRepo
	cancel context.CancelFunc
}

func (r *cancelOnLoadTrackStoreRepo) LoadTrack(id string) (*stores.TrackFile, error) {
	track, err := r.trackStoreRepo.LoadTrack(id)
	if err == nil && r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	return track, err
}

type saveCountingStateStore struct {
	*mockStateStore
	saveCalls int
}

type realOverlayStoreRepo struct {
	*trackStoreRepo
	overlayRoot string
}

func (r *realOverlayStoreRepo) OverlayRoot(string) string { return r.overlayRoot }

func newRealOverlayEngine(repoRoot, overlayRoot string, track *stores.TrackFile, stateStore *mockStateStore) *Engine {
	storeRepo := &realOverlayStoreRepo{trackStoreRepo: newTrackStoreRepo(), overlayRoot: overlayRoot}
	storeRepo.tracks["untrusted-store"] = track
	return New(
		&trackGitRepo{root: repoRoot, fingerprint: "fp1", workspacePath: "."},
		storeRepo,
		stateStore,
		fsops.NewRealFS(),
		&mockHasher{},
		&mockClock{},
		config.Paths{Root: filepath.Join(repoRoot, ".monodev"), Stores: filepath.Dir(overlayRoot), Workspaces: filepath.Join(repoRoot, ".state")},
	)
}

func writeOverlayFile(t *testing.T, overlayRoot, relPath string) {
	t.Helper()
	path := filepath.Join(overlayRoot, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("failed to create overlay parent: %v", err)
	}
	if err := os.WriteFile(path, []byte("overlay content"), 0600); err != nil {
		t.Fatalf("failed to create overlay file: %v", err)
	}
}

func requireEngineSymlink(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Symlink(oldname, newname); err != nil {
		t.Skipf("symlink creation is not supported in this environment: %v", err)
	}
}

func (s *saveCountingStateStore) SaveWorkspace(id string, ws *state.WorkspaceState) error {
	s.saveCalls++
	return s.mockStateStore.SaveWorkspace(id, ws)
}

// TestApply_WithStoreIDRequiresNoCheckout verifies that monodev apply <store-id>
// works even when no store has been checked out (no active store).
func TestApply_WithStoreIDRequiresNoCheckout(t *testing.T) {
	gitRepo := &trackGitRepo{root: "/repo", fingerprint: "fp1", workspacePath: "."}

	storeRepo := newTrackStoreRepo()
	// Store exists with an empty track file (nothing to apply)
	storeRepo.tracks["my-store"] = stores.NewTrackFile()

	stateStore := newMockStateStore()
	// No workspace state pre-loaded — simulates a fresh workspace with no checkout

	fs := newTrackFileInfoFS() // no files on disk

	eng := newTrackEngine(gitRepo, storeRepo, stateStore, fs)

	result, err := eng.Apply(context.Background(), &ApplyRequest{
		CWD:     "/repo",
		StoreID: "my-store",
		Mode:    "copy",
	})

	// Should NOT return ErrNoActiveStore — StoreID is explicitly provided
	if errors.Is(err, ErrNoActiveStore) {
		t.Fatal("Apply with explicit StoreID should not require a prior checkout")
	}

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}

	// After applying, the workspace state should record the store as active
	workspaceID := result.WorkspaceID
	ws, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("failed to load workspace state: %v", err)
	}
	if ws.ActiveStore != "my-store" {
		t.Errorf("ActiveStore = %q, want %q", ws.ActiveStore, "my-store")
	}
}

// TestApply_WithoutStoreIDStillRequiresCheckout verifies that apply without a
// store-id still requires a prior checkout.
func TestApply_WithoutStoreIDStillRequiresCheckout(t *testing.T) {
	gitRepo := &trackGitRepo{root: "/repo", fingerprint: "fp1", workspacePath: "."}
	storeRepo := newTrackStoreRepo()
	stateStore := newMockStateStore()
	fs := newTrackFileInfoFS()

	eng := newTrackEngine(gitRepo, storeRepo, stateStore, fs)

	_, err := eng.Apply(context.Background(), &ApplyRequest{
		CWD:  "/repo",
		Mode: "copy",
		// No StoreID — should require active store
	})

	if !errors.Is(err, ErrNoActiveStore) {
		t.Errorf("expected ErrNoActiveStore without StoreID, got: %v", err)
	}
}

func TestApply_RejectsPulledGitHookWithoutWriting(t *testing.T) {
	gitRepo := &trackGitRepo{root: "/repo", fingerprint: "fp1", workspacePath: "."}
	storeRepo := newTrackStoreRepo()
	track := stores.NewTrackFile()
	// This models a parsed track.json from a store received through monodev pull.
	track.Tracked = []stores.TrackedPath{{Path: ".git/hooks/pre-commit", Kind: "file"}}
	storeRepo.tracks["untrusted-store"] = track

	stateStore := newMockStateStore()
	fs := newTrackFileInfoFS("/stores/untrusted-store/overlay/.git/hooks/pre-commit")
	eng := newTrackEngine(gitRepo, storeRepo, stateStore, fs)

	_, err := eng.Apply(context.Background(), &ApplyRequest{
		CWD:     "/repo",
		StoreID: "untrusted-store",
		Mode:    "copy",
	})
	if err == nil {
		t.Fatal("expected .git hook path to be rejected")
	}
	if !strings.Contains(err.Error(), "repository .git directory") {
		t.Errorf("Apply error = %q, want repository .git directory message", err)
	}
	if len(fs.copiedPaths) != 0 {
		t.Errorf("Apply copied paths = %v, want no writes to .git/hooks/pre-commit", fs.copiedPaths)
	}
}

func TestApply_RejectsSymlinkedParentIntoGitHooksBeforeWriting(t *testing.T) {
	repoRoot := t.TempDir()
	hooksDir := filepath.Join(repoRoot, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0700); err != nil {
		t.Fatalf("failed to create hooks directory: %v", err)
	}
	requireEngineSymlink(t, filepath.Join(".git", "hooks"), filepath.Join(repoRoot, "hooks-link"))

	overlayRoot := filepath.Join(t.TempDir(), "overlay")
	writeOverlayFile(t, overlayRoot, filepath.Join("hooks-link", "pre-commit"))
	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{{Path: "hooks-link/pre-commit", Kind: "file"}}
	eng := newRealOverlayEngine(repoRoot, overlayRoot, track, newMockStateStore())

	_, err := eng.Apply(context.Background(), &ApplyRequest{CWD: repoRoot, StoreID: "untrusted-store", Mode: "copy"})
	if err == nil || !strings.Contains(err.Error(), "symlinked destination ancestor") {
		t.Fatalf("Apply error = %v, want symlinked destination ancestor rejection", err)
	}
	if _, err := os.Lstat(filepath.Join(hooksDir, "pre-commit")); !os.IsNotExist(err) {
		t.Fatalf("Git hook was created through parent symlink, lstat error: %v", err)
	}
}

func TestApply_RejectsSymlinkedParentOutsideWorkspaceWithoutMutation(t *testing.T) {
	repoRoot := t.TempDir()
	outside := t.TempDir()
	requireEngineSymlink(t, outside, filepath.Join(repoRoot, "escape"))

	overlayRoot := filepath.Join(t.TempDir(), "overlay")
	writeOverlayFile(t, overlayRoot, filepath.Join("escape", "created", "payload.txt"))
	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{{Path: "escape/created/payload.txt", Kind: "file"}}
	eng := newRealOverlayEngine(repoRoot, overlayRoot, track, newMockStateStore())

	_, err := eng.Apply(context.Background(), &ApplyRequest{CWD: repoRoot, StoreID: "untrusted-store", Mode: "copy"})
	if err == nil || !strings.Contains(err.Error(), "symlinked destination ancestor") {
		t.Fatalf("Apply error = %v, want symlinked destination ancestor rejection", err)
	}
	entries, readErr := os.ReadDir(outside)
	if readErr != nil {
		t.Fatalf("failed to inspect outside directory: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("outside directory was mutated: %v", entries)
	}
}

func TestApply_ForceRejectsSymlinkedParentBeforeRemovingTarget(t *testing.T) {
	repoRoot := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "payload.txt")
	if err := os.WriteFile(target, []byte("preserve me"), 0600); err != nil {
		t.Fatalf("failed to create outside target: %v", err)
	}
	requireEngineSymlink(t, outside, filepath.Join(repoRoot, "escape"))

	overlayRoot := filepath.Join(t.TempDir(), "overlay")
	writeOverlayFile(t, overlayRoot, filepath.Join("escape", "payload.txt"))
	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{{Path: "escape/payload.txt", Kind: "file"}}
	eng := newRealOverlayEngine(repoRoot, overlayRoot, track, newMockStateStore())

	_, err := eng.Apply(context.Background(), &ApplyRequest{CWD: repoRoot, StoreID: "untrusted-store", Mode: "copy", Force: true})
	if err == nil || !strings.Contains(err.Error(), "symlinked destination ancestor") {
		t.Fatalf("Apply --force error = %v, want symlinked destination ancestor rejection", err)
	}
	content, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("outside target was removed: %v", readErr)
	}
	if string(content) != "preserve me" {
		t.Fatalf("outside target content = %q, want preserved content", content)
	}
}

func TestApply_SymlinkModeRejectsSymlinkedParentOutsideWorkspace(t *testing.T) {
	repoRoot := t.TempDir()
	outside := t.TempDir()
	requireEngineSymlink(t, outside, filepath.Join(repoRoot, "escape"))

	overlayRoot := filepath.Join(t.TempDir(), "overlay")
	writeOverlayFile(t, overlayRoot, filepath.Join("escape", "payload.txt"))
	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{{Path: "escape/payload.txt", Kind: "file"}}
	eng := newRealOverlayEngine(repoRoot, overlayRoot, track, newMockStateStore())

	_, err := eng.Apply(context.Background(), &ApplyRequest{CWD: repoRoot, StoreID: "untrusted-store", Mode: "symlink"})
	if err == nil || !strings.Contains(err.Error(), "symlinked destination ancestor") {
		t.Fatalf("symlink-mode Apply error = %v, want symlinked destination ancestor rejection", err)
	}
	entries, readErr := os.ReadDir(outside)
	if readErr != nil {
		t.Fatalf("failed to inspect outside directory: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("outside directory was mutated in symlink mode: %v", entries)
	}
}

// TestApply_WithStoreIDPrefersComponentScopeWhenDuplicate verifies that applying
// by store ID works even when the same ID exists in both scopes.
func TestApply_WithStoreIDPrefersComponentScopeWhenDuplicate(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	componentRepo := newScopedMockStoreRepo()
	globalRepo.storeIDs["shared"] = true
	componentRepo.storeIDs["shared"] = true
	globalRepo.tracks["shared"] = stores.NewTrackFile()
	componentRepo.tracks["shared"] = stores.NewTrackFile()

	stateStore := newMockStateStore()
	eng := newScopedTestEngineWithState(globalRepo, componentRepo, stateStore)

	result, err := eng.Apply(context.Background(), &ApplyRequest{
		CWD:     "/repo",
		StoreID: "shared",
		Mode:    "copy",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ws, err := stateStore.LoadWorkspace(result.WorkspaceID)
	if err != nil {
		t.Fatalf("failed to load workspace state: %v", err)
	}
	if ws.ActiveStoreScope != stores.ScopeComponent {
		t.Errorf("ActiveStoreScope = %q, want %q", ws.ActiveStoreScope, stores.ScopeComponent)
	}
	if ws.ActiveStore != "shared" {
		t.Errorf("ActiveStore = %q, want %q", ws.ActiveStore, "shared")
	}

	// Ensure workspace ID is stable and persisted.
	wantWorkspaceID := state.ComputeWorkspaceID("", "")
	if result.WorkspaceID != wantWorkspaceID {
		t.Errorf("WorkspaceID = %q, want %q", result.WorkspaceID, wantWorkspaceID)
	}
}

func TestApply_CancellationAfterPlanningDoesNotSaveState(t *testing.T) {
	gitRepo := &trackGitRepo{root: "/repo", fingerprint: "fp1", workspacePath: "."}
	baseRepo := newTrackStoreRepo()
	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{{Path: "file.txt", Kind: "file"}}
	baseRepo.tracks["store1"] = track

	ctx, cancel := context.WithCancel(context.Background())
	storeRepo := &cancelOnLoadTrackStoreRepo{
		trackStoreRepo: baseRepo,
		cancel:         cancel,
	}

	baseStateStore := newMockStateStore()
	stateStore := &saveCountingStateStore{mockStateStore: baseStateStore}
	workspaceID := state.ComputeWorkspaceID("fp1", ".")
	ws := state.NewWorkspaceState("fp1", ".", "copy")
	ws.ActiveStore = "store1"
	baseStateStore.workspaces[workspaceID] = ws

	fs := newTrackFileInfoFS("/stores/store1/overlay/file.txt")
	eng := New(
		gitRepo,
		storeRepo,
		stateStore,
		fs,
		&mockHasher{},
		&mockClock{},
		config.Paths{Root: "/tmp/monodev", Stores: "/tmp/monodev/stores", Workspaces: "/tmp/workspaces"},
	)

	result, err := eng.Apply(ctx, &ApplyRequest{
		CWD:  "/repo",
		Mode: "copy",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Apply error = %v, want context.Canceled", err)
	}
	if result != nil {
		t.Fatalf("Apply result = %#v, want nil", result)
	}
	if stateStore.saveCalls != 0 {
		t.Fatalf("SaveWorkspace calls = %d, want 0 after cancellation", stateStore.saveCalls)
	}
	if _, ok := ws.Paths["file.txt"]; ok {
		t.Fatal("workspace state was mutated for file.txt after cancellation")
	}
}
