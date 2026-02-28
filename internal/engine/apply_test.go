package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/danieljhkim/monodev/internal/stores"
)

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
