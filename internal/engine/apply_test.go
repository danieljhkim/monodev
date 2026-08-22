package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/danieljhkim/monodev/internal/config"
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
