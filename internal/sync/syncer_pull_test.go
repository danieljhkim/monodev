package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/persist"
	"github.com/danieljhkim/monodev/internal/remote"
	"github.com/danieljhkim/monodev/internal/stores"
)

func savePullRemoteConfig(t *testing.T, repoRoot string, configStore *fakeRemoteConfigStore) {
	t.Helper()

	config := remote.DefaultRemoteConfig()
	config.Remote = "origin"
	if err := configStore.Save(repoRoot, config); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}
}

func stagePersistedStoreForPull(t *testing.T, repoRoot string, snapshotMgr *persist.SnapshotManager, storeRepo *fakeStoreRepo, storeID string) (storeDir string, persistStorePath string) {
	t.Helper()

	meta := stores.NewStoreMeta("Remote Store", "global", time.Now())
	if err := storeRepo.Create(storeID, meta); err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	overlayDir := storeRepo.OverlayRoot(storeID)
	if err := os.MkdirAll(overlayDir, 0755); err != nil {
		t.Fatalf("failed to create overlay dir: %v", err)
	}

	testFile := filepath.Join(overlayDir, "remote.txt")
	if err := os.WriteFile(testFile, []byte("remote content"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if err := snapshotMgr.Materialize(storeID, storeRepo, repoRoot); err != nil {
		t.Fatalf("failed to materialize: %v", err)
	}

	storeDir = filepath.Dir(overlayDir)
	if err := os.RemoveAll(storeDir); err != nil {
		t.Fatalf("failed to remove local store dir: %v", err)
	}

	persistStorePath = filepath.Join(repoRoot, ".monodev", "persist", "stores", storeID)
	return storeDir, persistStorePath
}

func assertPullVerificationPathError(t *testing.T, err error, storeID, path string) {
	t.Helper()

	if err == nil {
		t.Fatal("Expected PullStore verification error, got nil")
	}
	if !strings.Contains(err.Error(), storeID) || !strings.Contains(err.Error(), path) {
		t.Fatalf("PullStore error %q should name store %q and path %q", err, storeID, path)
	}
}

func rewriteManifestHash(t *testing.T, manifestPath, targetPath, newHash string) {
	t.Helper()

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read manifest: %v", err)
	}

	var manifest struct {
		SchemaVersion int    `json:"schemaVersion"`
		HashAlgorithm string `json:"hashAlgorithm"`
		Files         []struct {
			Path string `json:"path"`
			Hash string `json:"hash"`
		} `json:"files"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("failed to decode manifest: %v", err)
	}

	found := false
	for i := range manifest.Files {
		if manifest.Files[i].Path == targetPath {
			manifest.Files[i].Hash = newHash
			found = true
		}
	}
	if !found {
		t.Fatalf("manifest did not contain path %q", targetPath)
	}

	data, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("failed to encode manifest: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}
}

func TestSyncer_PullStore(t *testing.T) {
	t.Run("pulls stores successfully", func(t *testing.T) {
		repoRoot, _, syncer, git, storeRepo, configStore, cleanup := setupSyncerTest(t)
		defer cleanup()

		// Setup remote config
		config := remote.DefaultRemoteConfig()
		config.Remote = "origin"
		if err := configStore.Save(repoRoot, config); err != nil {
			t.Fatalf("failed to save config: %v", err)
		}

		// Create a store in the persist directory (simulating remote store)
		storeID := "remote-store"
		meta := stores.NewStoreMeta("Remote Store", "global", time.Now())
		if err := storeRepo.Create(storeID, meta); err != nil {
			t.Fatalf("failed to create store: %v", err)
		}

		// Materialize to persist directory
		fs := fsops.NewRealFS()
		snapshotMgr := persist.NewSnapshotManager(fs)

		overlayDir := storeRepo.OverlayRoot(storeID)
		if err := os.MkdirAll(overlayDir, 0755); err != nil {
			t.Fatalf("failed to create overlay dir: %v", err)
		}

		testFile := filepath.Join(overlayDir, "remote.txt")
		if err := os.WriteFile(testFile, []byte("remote content"), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		if err := snapshotMgr.Materialize(storeID, storeRepo, repoRoot); err != nil {
			t.Fatalf("failed to materialize: %v", err)
		}

		// Delete from stores dir to simulate it only existing remotely
		storeDir := filepath.Dir(overlayDir)
		if err := os.RemoveAll(storeDir); err != nil {
			t.Fatalf("failed to remove store dir: %v", err)
		}

		// Pull
		req := &PullRequest{
			RepoRoot: repoRoot,
			StoreIDs: []string{storeID},
		}

		result, err := syncer.PullStore(context.Background(), req)
		if err != nil {
			t.Fatalf("PullStore failed: %v", err)
		}

		// Verify result
		if len(result.PulledStores) != 1 {
			t.Errorf("Expected 1 pulled store, got %d", len(result.PulledStores))
		}

		if result.PulledStores[0] != storeID {
			t.Errorf("PulledStores[0] = %s, want %s", result.PulledStores[0], storeID)
		}

		// Verify git operations were called
		if len(git.EnsureRepoCalls) == 0 {
			t.Error("EnsureRepo should have been called")
		}

		if len(git.FetchCalls) == 0 {
			t.Error("Fetch should have been called")
		}

		if len(git.CheckoutCalls) == 0 {
			t.Error("Checkout should have been called")
		}

		// Verify store was dematerialized back to stores dir
		if _, err := os.Stat(storeDir); os.IsNotExist(err) {
			t.Error("Store was not dematerialized to stores directory")
		}
	})

	t.Run("pull verify succeeds when persisted files match manifest", func(t *testing.T) {
		repoRoot, _, syncer, _, storeRepo, configStore, cleanup := setupSyncerTest(t)
		defer cleanup()

		savePullRemoteConfig(t, repoRoot, configStore)
		storeID := "remote-store"
		stagePersistedStoreForPull(t, repoRoot, syncer.snapshotMgr, storeRepo, storeID)

		result, err := syncer.PullStore(context.Background(), &PullRequest{
			RepoRoot: repoRoot,
			StoreIDs: []string{storeID},
			Verify:   true,
		})
		if err != nil {
			t.Fatalf("PullStore failed: %v", err)
		}
		if !result.Verified {
			t.Fatal("Verified = false, want true for manifest-backed pull")
		}
	})

	t.Run("pull verify fails when persisted overlay file is corrupted", func(t *testing.T) {
		repoRoot, _, syncer, _, storeRepo, configStore, cleanup := setupSyncerTest(t)
		defer cleanup()

		savePullRemoteConfig(t, repoRoot, configStore)
		storeID := "remote-store"
		storeDir, persistStorePath := stagePersistedStoreForPull(t, repoRoot, syncer.snapshotMgr, storeRepo, storeID)
		corruptPath := filepath.Join(persistStorePath, "overlay", "remote.txt")
		if err := os.WriteFile(corruptPath, []byte("tampered"), 0644); err != nil {
			t.Fatalf("failed to corrupt persisted file: %v", err)
		}

		_, err := syncer.PullStore(context.Background(), &PullRequest{
			RepoRoot: repoRoot,
			StoreIDs: []string{storeID},
			Verify:   true,
		})
		assertPullVerificationPathError(t, err, storeID, corruptPath)
		if _, statErr := os.Stat(storeDir); !os.IsNotExist(statErr) {
			t.Fatalf("store should not be dematerialized after failed verification, stat err = %v", statErr)
		}
	})

	t.Run("pull verify fails when persisted overlay file is missing", func(t *testing.T) {
		repoRoot, _, syncer, _, storeRepo, configStore, cleanup := setupSyncerTest(t)
		defer cleanup()

		savePullRemoteConfig(t, repoRoot, configStore)
		storeID := "remote-store"
		stagePersistedStoreForPull(t, repoRoot, syncer.snapshotMgr, storeRepo, storeID)
		missingPath := filepath.Join(repoRoot, ".monodev", "persist", "stores", storeID, "overlay", "remote.txt")
		if err := os.Remove(missingPath); err != nil {
			t.Fatalf("failed to remove persisted file: %v", err)
		}

		_, err := syncer.PullStore(context.Background(), &PullRequest{
			RepoRoot: repoRoot,
			StoreIDs: []string{storeID},
			Verify:   true,
		})
		assertPullVerificationPathError(t, err, storeID, missingPath)
	})

	t.Run("pull verify fails when manifest hash mismatches", func(t *testing.T) {
		repoRoot, _, syncer, _, storeRepo, configStore, cleanup := setupSyncerTest(t)
		defer cleanup()

		savePullRemoteConfig(t, repoRoot, configStore)
		storeID := "remote-store"
		_, persistStorePath := stagePersistedStoreForPull(t, repoRoot, syncer.snapshotMgr, storeRepo, storeID)
		manifestPath := filepath.Join(persistStorePath, "verification-manifest.json")
		rewriteManifestHash(t, manifestPath, "overlay/remote.txt", "not-the-recorded-hash")

		mismatchedPath := filepath.Join(persistStorePath, "overlay", "remote.txt")
		_, err := syncer.PullStore(context.Background(), &PullRequest{
			RepoRoot: repoRoot,
			StoreIDs: []string{storeID},
			Verify:   true,
		})
		assertPullVerificationPathError(t, err, storeID, mismatchedPath)
	})

	t.Run("pull verify keeps legacy stores pullable without reporting verified", func(t *testing.T) {
		repoRoot, _, syncer, _, storeRepo, configStore, cleanup := setupSyncerTest(t)
		defer cleanup()

		savePullRemoteConfig(t, repoRoot, configStore)
		storeID := "legacy-store"
		storeDir, persistStorePath := stagePersistedStoreForPull(t, repoRoot, syncer.snapshotMgr, storeRepo, storeID)
		if err := os.Remove(filepath.Join(persistStorePath, "verification-manifest.json")); err != nil {
			t.Fatalf("failed to remove manifest: %v", err)
		}

		result, err := syncer.PullStore(context.Background(), &PullRequest{
			RepoRoot: repoRoot,
			StoreIDs: []string{storeID},
			Verify:   true,
		})
		if err != nil {
			t.Fatalf("PullStore failed for legacy manifest-free store: %v", err)
		}
		if result.Verified {
			t.Fatal("Verified = true for legacy manifest-free store, want false")
		}
		if _, err := os.Stat(storeDir); err != nil {
			t.Fatalf("legacy store should still be dematerialized: %v", err)
		}
	})

	t.Run("pulls all stores when none specified", func(t *testing.T) {
		repoRoot, _, syncer, _, storeRepo, configStore, cleanup := setupSyncerTest(t)
		defer cleanup()

		// Setup remote config
		config := remote.DefaultRemoteConfig()
		if err := configStore.Save(repoRoot, config); err != nil {
			t.Fatalf("failed to save config: %v", err)
		}

		// Create multiple stores in persist directory
		fs := fsops.NewRealFS()
		snapshotMgr := persist.NewSnapshotManager(fs)

		for i := 1; i <= 2; i++ {
			storeID := fmt.Sprintf("store-%d", i)
			meta := stores.NewStoreMeta(fmt.Sprintf("Store %d", i), "global", time.Now())
			if err := storeRepo.Create(storeID, meta); err != nil {
				t.Fatalf("failed to create store: %v", err)
			}

			overlayDir := storeRepo.OverlayRoot(storeID)
			if err := os.MkdirAll(overlayDir, 0755); err != nil {
				t.Fatalf("failed to create overlay dir: %v", err)
			}

			if err := snapshotMgr.Materialize(storeID, storeRepo, repoRoot); err != nil {
				t.Fatalf("failed to materialize: %v", err)
			}
		}

		// Pull without specifying store IDs
		req := &PullRequest{
			RepoRoot: repoRoot,
		}

		result, err := syncer.PullStore(context.Background(), req)
		if err != nil {
			t.Fatalf("PullStore failed: %v", err)
		}

		// Should pull all stores
		if len(result.PulledStores) != 2 {
			t.Errorf("Expected 2 pulled stores, got %d", len(result.PulledStores))
		}
	})

	t.Run("returns empty result when no persisted stores exist", func(t *testing.T) {
		repoRoot, _, syncer, _, _, configStore, cleanup := setupSyncerTest(t)
		defer cleanup()

		// Setup remote config
		config := remote.DefaultRemoteConfig()
		if err := configStore.Save(repoRoot, config); err != nil {
			t.Fatalf("failed to save config: %v", err)
		}

		// Pull without any persisted stores
		req := &PullRequest{
			RepoRoot: repoRoot,
		}

		result, err := syncer.PullStore(context.Background(), req)
		if err != nil {
			t.Fatalf("PullStore failed: %v", err)
		}

		if len(result.PulledStores) != 0 {
			t.Errorf("Expected 0 pulled stores, got %d", len(result.PulledStores))
		}
	})

	t.Run("returns error when repo root is empty", func(t *testing.T) {
		_, _, syncer, _, _, _, cleanup := setupSyncerTest(t)
		defer cleanup()

		req := &PullRequest{
			RepoRoot: "",
		}

		_, err := syncer.PullStore(context.Background(), req)
		if err == nil {
			t.Error("Expected error for empty repo root, got nil")
		}
	})

	t.Run("returns error when remote config not found", func(t *testing.T) {
		repoRoot, _, syncer, _, _, _, cleanup := setupSyncerTest(t)
		defer cleanup()

		// Don't set up config - should fail
		req := &PullRequest{
			RepoRoot: repoRoot,
		}

		_, err := syncer.PullStore(context.Background(), req)
		if err == nil {
			t.Error("Expected error when config not found, got nil")
		}
	})
}

func TestSyncer_PullStoreCancellationStopsBeforeCheckout(t *testing.T) {
	repoRoot, _, syncer, git, _, configStore, cleanup := setupSyncerTest(t)
	defer cleanup()

	config := remote.DefaultRemoteConfig()
	config.Remote = "origin"
	if err := configStore.Save(repoRoot, config); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	git.FetchHook = func(context.Context, remote.FetchCall) error {
		cancel()
		return nil
	}

	_, err := syncer.PullStore(ctx, &PullRequest{
		RepoRoot: repoRoot,
		StoreIDs: []string{"remote-store"},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PullStore error = %v, want context.Canceled", err)
	}
	if len(git.FetchCalls) != 1 {
		t.Fatalf("Fetch calls = %d, want 1", len(git.FetchCalls))
	}
	if len(git.CheckoutCalls) != 0 {
		t.Fatalf("Checkout calls = %d, want 0 after cancellation", len(git.CheckoutCalls))
	}
}
