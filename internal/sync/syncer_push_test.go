package sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/remote"
	"github.com/danieljhkim/monodev/internal/stores"
)

func TestSyncer_PushStore(t *testing.T) {
	t.Run("pushes single store successfully", func(t *testing.T) {
		repoRoot, _, syncer, git, storeRepo, configStore, cleanup := setupSyncerTest(t)
		defer cleanup()

		// Create a test store
		storeID := "test-store"
		meta := stores.NewStoreMeta("Test Store", "global", time.Now())
		if err := storeRepo.Create(storeID, meta); err != nil {
			t.Fatalf("failed to create store: %v", err)
		}

		// Create store directory with a file
		overlayDir := storeRepo.OverlayRoot(storeID)
		if err := os.MkdirAll(overlayDir, 0755); err != nil {
			t.Fatalf("failed to create overlay dir: %v", err)
		}
		testFile := filepath.Join(overlayDir, "test.txt")
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		// Push
		req := &PushRequest{
			RepoRoot: repoRoot,
			StoreIDs: []string{storeID},
			Remote:   "origin",
		}

		result, err := syncer.PushStore(context.Background(), req)
		if err != nil {
			t.Fatalf("PushStore failed: %v", err)
		}

		// Verify result
		if len(result.PushedStores) != 1 {
			t.Errorf("Expected 1 pushed store, got %d", len(result.PushedStores))
		}

		if result.PushedStores[0] != storeID {
			t.Errorf("PushedStores[0] = %s, want %s", result.PushedStores[0], storeID)
		}

		if result.Remote != "origin" {
			t.Errorf("Remote = %s, want origin", result.Remote)
		}

		// Verify git operations were called
		if len(git.EnsureRepoCalls) == 0 {
			t.Error("EnsureRepo should have been called")
		}

		if len(git.CommitCalls) == 0 {
			t.Error("Commit should have been called")
		}

		if len(git.PushCalls) == 0 {
			t.Error("Push should have been called")
		}

		// Verify config was saved
		config, err := configStore.Load(repoRoot)
		if err != nil {
			t.Fatalf("Config not saved: %v", err)
		}

		if config.Remote != "origin" {
			t.Errorf("Config.Remote = %s, want origin", config.Remote)
		}
	})

	t.Run("pushes all stores when none specified", func(t *testing.T) {
		repoRoot, _, syncer, _, storeRepo, _, cleanup := setupSyncerTest(t)
		defer cleanup()

		// Create multiple stores
		for i := 1; i <= 3; i++ {
			storeID := fmt.Sprintf("store-%d", i)
			meta := stores.NewStoreMeta(fmt.Sprintf("Store %d", i), "global", time.Now())
			if err := storeRepo.Create(storeID, meta); err != nil {
				t.Fatalf("failed to create store %s: %v", storeID, err)
			}

			// Create minimal directory structure
			overlayDir := storeRepo.OverlayRoot(storeID)
			if err := os.MkdirAll(overlayDir, 0755); err != nil {
				t.Fatalf("failed to create overlay dir: %v", err)
			}
		}

		// Push without specifying store IDs
		req := &PushRequest{
			RepoRoot: repoRoot,
			Remote:   "origin",
		}

		result, err := syncer.PushStore(context.Background(), req)
		if err != nil {
			t.Fatalf("PushStore failed: %v", err)
		}

		// Should push all 3 stores
		if len(result.PushedStores) != 3 {
			t.Errorf("Expected 3 pushed stores, got %d", len(result.PushedStores))
		}
	})

	t.Run("dry run does not execute git operations", func(t *testing.T) {
		repoRoot, _, syncer, git, storeRepo, _, cleanup := setupSyncerTest(t)
		defer cleanup()

		// Create a test store
		storeID := "test-store"
		meta := stores.NewStoreMeta("Test", "global", time.Now())
		if err := storeRepo.Create(storeID, meta); err != nil {
			t.Fatalf("failed to create store: %v", err)
		}

		overlayDir := storeRepo.OverlayRoot(storeID)
		if err := os.MkdirAll(overlayDir, 0755); err != nil {
			t.Fatalf("failed to create overlay dir: %v", err)
		}

		// Push with DryRun
		req := &PushRequest{
			RepoRoot: repoRoot,
			StoreIDs: []string{storeID},
			Remote:   "origin",
			DryRun:   true,
		}

		result, err := syncer.PushStore(context.Background(), req)
		if err != nil {
			t.Fatalf("PushStore failed: %v", err)
		}

		if !result.DryRun {
			t.Error("Expected DryRun = true in result")
		}

		// Git operations should not have been called
		if len(git.EnsureRepoCalls) > 0 {
			t.Error("EnsureRepo should not be called in dry run")
		}

		if len(git.CommitCalls) > 0 {
			t.Error("Commit should not be called in dry run")
		}

		if len(git.PushCalls) > 0 {
			t.Error("Push should not be called in dry run")
		}
	})

	t.Run("returns error when repo root is empty", func(t *testing.T) {
		_, _, syncer, _, _, _, cleanup := setupSyncerTest(t)
		defer cleanup()

		req := &PushRequest{
			RepoRoot: "",
			StoreIDs: []string{"store1"},
		}

		_, err := syncer.PushStore(context.Background(), req)
		if err == nil {
			t.Error("Expected error for empty repo root, got nil")
		}
	})

	t.Run("returns error when no stores exist and none specified", func(t *testing.T) {
		repoRoot, _, syncer, _, _, _, cleanup := setupSyncerTest(t)
		defer cleanup()

		req := &PushRequest{
			RepoRoot: repoRoot,
		}

		_, err := syncer.PushStore(context.Background(), req)
		if err == nil {
			t.Error("Expected error when no stores exist, got nil")
		}
	})
}

func TestSyncer_PushStoreCancellationStopsBeforeLaterGitSteps(t *testing.T) {
	repoRoot, _, syncer, git, storeRepo, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	storeID := "test-store"
	if err := storeRepo.Create(storeID, stores.NewStoreMeta("Test Store", "global", time.Now())); err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	git.EnsureRepoHook = func(context.Context, remote.EnsureRepoCall) error {
		cancel()
		return nil
	}

	_, err := syncer.PushStore(ctx, &PushRequest{
		RepoRoot: repoRoot,
		StoreIDs: []string{storeID},
		Remote:   "origin",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PushStore error = %v, want context.Canceled", err)
	}
	if len(git.EnsureRepoCalls) != 1 {
		t.Fatalf("EnsureRepo calls = %d, want 1", len(git.EnsureRepoCalls))
	}
	if len(git.GetRemoteCalls) != 0 {
		t.Fatalf("GetRemoteURL calls = %d, want 0 after cancellation", len(git.GetRemoteCalls))
	}
	if len(git.SetRemoteCalls) != 0 || len(git.CommitCalls) != 0 || len(git.PushCalls) != 0 {
		t.Fatalf("later git calls ran after cancellation: set=%d commit=%d push=%d", len(git.SetRemoteCalls), len(git.CommitCalls), len(git.PushCalls))
	}
}
