package persist

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/stores"
)

// setupTestEnv creates test directories and managers for testing.
func setupTestEnv(t *testing.T) (storesDir string, persistRoot string, fs fsops.FS, repo stores.StoreRepo, mgr *SnapshotManager) {
	t.Helper()

	// Create temp directories
	tmpDir, err := os.MkdirTemp("", "persist-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	storesDir = filepath.Join(tmpDir, "stores")
	persistRoot = filepath.Join(tmpDir, "repo")

	if err := os.MkdirAll(storesDir, 0755); err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("failed to create stores dir: %v", err)
	}

	if err := os.MkdirAll(persistRoot, 0755); err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("failed to create persist root: %v", err)
	}

	fs = fsops.NewRealFS()
	repo = stores.NewFileStoreRepo(fs, storesDir)
	mgr = NewSnapshotManager(fs)

	return storesDir, persistRoot, fs, repo, mgr
}

// createTestStore creates a test store with some files.
func createTestStore(t *testing.T, repo stores.StoreRepo, storeID string) {
	t.Helper()

	// Create store
	meta := stores.NewStoreMeta("Test Store", time.Now())
	if err := repo.Create(storeID, meta); err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Add some files to the overlay
	overlayRoot := repo.OverlayRoot(storeID)
	testFile := filepath.Join(overlayRoot, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Create subdirectory with file
	subDir := filepath.Join(overlayRoot, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	subFile := filepath.Join(subDir, "nested.txt")
	if err := os.WriteFile(subFile, []byte("nested content"), 0644); err != nil {
		t.Fatalf("failed to write nested file: %v", err)
	}
}

func TestSnapshotManager_Materialize(t *testing.T) {
	t.Run("materializes store successfully", func(t *testing.T) {
		storesDir, persistRoot, _, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		storeID := "test-store"
		createTestStore(t, repo, storeID)

		// Materialize
		err := mgr.Materialize(storeID, repo, persistRoot)
		if err != nil {
			t.Fatalf("Materialize failed: %v", err)
		}

		// Verify store exists in persist directory
		persistStorePath := filepath.Join(persistRoot, ".monodev", "persist", "stores", storeID)
		if _, err := os.Stat(persistStorePath); os.IsNotExist(err) {
			t.Error("Store was not materialized to persist directory")
		}

		// Verify meta.json exists
		metaPath := filepath.Join(persistStorePath, "meta.json")
		if _, err := os.Stat(metaPath); os.IsNotExist(err) {
			t.Error("meta.json was not materialized")
		}

		// Verify track.json exists
		trackPath := filepath.Join(persistStorePath, "track.json")
		if _, err := os.Stat(trackPath); os.IsNotExist(err) {
			t.Error("track.json was not materialized")
		}

		// Verify overlay directory exists
		overlayPath := filepath.Join(persistStorePath, "overlay")
		if _, err := os.Stat(overlayPath); os.IsNotExist(err) {
			t.Error("overlay directory was not materialized")
		}

		// Verify checksum manifest exists
		manifestPath := filepath.Join(persistStorePath, verificationManifestName)
		if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
			t.Error("verification manifest was not materialized")
		}

		// Verify test file exists
		testFilePath := filepath.Join(overlayPath, "test.txt")
		if _, err := os.Stat(testFilePath); os.IsNotExist(err) {
			t.Error("test file was not materialized")
		}

		// Verify nested file exists
		nestedFilePath := filepath.Join(overlayPath, "subdir", "nested.txt")
		if _, err := os.Stat(nestedFilePath); os.IsNotExist(err) {
			t.Error("nested file was not materialized")
		}
	})

	t.Run("overwrites existing materialized store", func(t *testing.T) {
		storesDir, persistRoot, _, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		storeID := "test-store"
		createTestStore(t, repo, storeID)

		// Materialize first time
		if err := mgr.Materialize(storeID, repo, persistRoot); err != nil {
			t.Fatalf("First materialize failed: %v", err)
		}

		// Modify the store
		overlayRoot := repo.OverlayRoot(storeID)
		newFile := filepath.Join(overlayRoot, "new.txt")
		if err := os.WriteFile(newFile, []byte("new content"), 0644); err != nil {
			t.Fatalf("failed to write new file: %v", err)
		}

		// Materialize again
		if err := mgr.Materialize(storeID, repo, persistRoot); err != nil {
			t.Fatalf("Second materialize failed: %v", err)
		}

		// Verify new file is in persist directory
		persistStorePath := filepath.Join(persistRoot, ".monodev", "persist", "stores", storeID)
		staleFilePath := filepath.Join(persistStorePath, "overlay", "stale.txt")
		if err := os.WriteFile(staleFilePath, []byte("stale content"), 0644); err != nil {
			t.Fatalf("failed to write stale persisted file: %v", err)
		}

		// Materialize again after adding stale destination-only content
		if err := mgr.Materialize(storeID, repo, persistRoot); err != nil {
			t.Fatalf("Third materialize failed: %v", err)
		}

		newFilePath := filepath.Join(persistStorePath, "overlay", "new.txt")
		if _, err := os.Stat(newFilePath); os.IsNotExist(err) {
			t.Error("New file was not materialized in second materialize")
		}
		if _, err := os.Stat(staleFilePath); !os.IsNotExist(err) {
			t.Fatalf("stale destination-only file should have been removed, stat error: %v", err)
		}
		requireNoSnapshotTempDirs(t, filepath.Dir(persistStorePath))
	})

	t.Run("preserves existing persisted store when replacement copy fails", func(t *testing.T) {
		storesDir, persistRoot, fs, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		storeID := "test-store"
		createTestStore(t, repo, storeID)

		if err := mgr.Materialize(storeID, repo, persistRoot); err != nil {
			t.Fatalf("Materialize failed: %v", err)
		}

		persistStorePath := filepath.Join(persistRoot, ".monodev", "persist", "stores", storeID)
		persistedFile := filepath.Join(persistStorePath, "overlay", "test.txt")
		if err := os.WriteFile(persistedFile, []byte("existing persisted content"), 0644); err != nil {
			t.Fatalf("failed to write existing persisted content: %v", err)
		}

		localFile := filepath.Join(repo.OverlayRoot(storeID), "test.txt")
		if err := os.WriteFile(localFile, []byte("replacement local content"), 0644); err != nil {
			t.Fatalf("failed to write replacement local content: %v", err)
		}

		failingMgr := NewSnapshotManager(&copyFailingFS{
			FS:  fs,
			err: errors.New("injected copy failure"),
		})
		err := failingMgr.Materialize(storeID, repo, persistRoot)
		if err == nil {
			t.Fatal("Materialize succeeded, want injected copy failure")
		}
		if !strings.Contains(err.Error(), "injected copy failure") {
			t.Fatalf("Materialize error = %v, want injected copy failure", err)
		}

		requireFileContent(t, persistedFile, "existing persisted content")
		requireNoSnapshotTempDirs(t, filepath.Dir(persistStorePath))
	})

	t.Run("returns error for non-existent store", func(t *testing.T) {
		storesDir, persistRoot, _, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		err := mgr.Materialize("nonexistent", repo, persistRoot)
		if err == nil {
			t.Error("Expected error for non-existent store, got nil")
		}
	})

	t.Run("returns error for invalid store ID", func(t *testing.T) {
		storesDir, persistRoot, _, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		err := mgr.Materialize("../invalid", repo, persistRoot)
		if err == nil {
			t.Error("Expected error for invalid store ID, got nil")
		}
	})

	t.Run("rejects overlay symlink before copying target contents", func(t *testing.T) {
		storesDir, persistRoot, _, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		storeID := "test-store"
		createTestStore(t, repo, storeID)

		outsidePath := filepath.Join(filepath.Dir(storesDir), "outside-secret.txt")
		if err := os.WriteFile(outsidePath, []byte("do-not-persist"), 0644); err != nil {
			t.Fatalf("failed to write outside file: %v", err)
		}
		requireSymlink(t, outsidePath, filepath.Join(repo.OverlayRoot(storeID), "leak.txt"))

		persistStorePath := filepath.Join(persistRoot, ".monodev", "persist", "stores", storeID)
		if err := os.MkdirAll(filepath.Join(persistStorePath, "overlay"), 0755); err != nil {
			t.Fatalf("failed to create existing persisted store: %v", err)
		}
		sentinelPath := filepath.Join(persistStorePath, "sentinel.txt")
		if err := os.WriteFile(sentinelPath, []byte("keep"), 0644); err != nil {
			t.Fatalf("failed to write persisted sentinel: %v", err)
		}

		err := mgr.Materialize(storeID, repo, persistRoot)
		if err == nil {
			t.Fatal("Materialize succeeded, want symlink rejection")
		}
		if !strings.Contains(err.Error(), "overlay/leak.txt") || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("Materialize error %q should name the offending symlink path", err)
		}

		content, err := os.ReadFile(sentinelPath)
		if err != nil {
			t.Fatalf("existing persisted store should not be removed before validation: %v", err)
		}
		if string(content) != "keep" {
			t.Fatalf("persisted sentinel = %q, want %q", content, "keep")
		}

		leakedPath := filepath.Join(persistStorePath, "overlay", "leak.txt")
		if _, err := os.Lstat(leakedPath); !os.IsNotExist(err) {
			t.Fatalf("persisted symlink target contents should not be copied, stat error: %v", err)
		}
	})
}

func TestSnapshotManager_Dematerialize(t *testing.T) {
	t.Run("dematerializes store successfully", func(t *testing.T) {
		storesDir, persistRoot, _, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		storeID := "test-store"
		createTestStore(t, repo, storeID)

		// Materialize first
		if err := mgr.Materialize(storeID, repo, persistRoot); err != nil {
			t.Fatalf("Materialize failed: %v", err)
		}

		// Delete the store from storesDir
		storePath := filepath.Dir(repo.OverlayRoot(storeID))
		if err := os.RemoveAll(storePath); err != nil {
			t.Fatalf("failed to remove store: %v", err)
		}

		// Dematerialize
		err := mgr.Dematerialize(storeID, persistRoot, repo)
		if err != nil {
			t.Fatalf("Dematerialize failed: %v", err)
		}

		// Verify store exists in storesDir
		if _, err := os.Stat(storePath); os.IsNotExist(err) {
			t.Error("Store was not dematerialized to stores directory")
		}

		// Verify files exist
		testFilePath := filepath.Join(repo.OverlayRoot(storeID), "test.txt")
		if _, err := os.Stat(testFilePath); os.IsNotExist(err) {
			t.Error("test file was not dematerialized")
		}

		nestedFilePath := filepath.Join(repo.OverlayRoot(storeID), "subdir", "nested.txt")
		if _, err := os.Stat(nestedFilePath); os.IsNotExist(err) {
			t.Error("nested file was not dematerialized")
		}

		// Verify content
		content, err := os.ReadFile(testFilePath)
		if err != nil {
			t.Fatalf("failed to read test file: %v", err)
		}
		if string(content) != "test content" {
			t.Errorf("test file content = %q, want %q", content, "test content")
		}
	})

	t.Run("overwrites existing store in stores directory", func(t *testing.T) {
		storesDir, persistRoot, _, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		storeID := "test-store"
		createTestStore(t, repo, storeID)

		// Materialize
		if err := mgr.Materialize(storeID, repo, persistRoot); err != nil {
			t.Fatalf("Materialize failed: %v", err)
		}

		// Modify the store in storesDir
		overlayRoot := repo.OverlayRoot(storeID)
		modifiedFile := filepath.Join(overlayRoot, "test.txt")
		if err := os.WriteFile(modifiedFile, []byte("modified content"), 0644); err != nil {
			t.Fatalf("failed to modify file: %v", err)
		}
		staleLocalFile := filepath.Join(overlayRoot, "local-only.txt")
		if err := os.WriteFile(staleLocalFile, []byte("local-only content"), 0644); err != nil {
			t.Fatalf("failed to write stale local file: %v", err)
		}

		// Dematerialize (should overwrite)
		if err := mgr.Dematerialize(storeID, persistRoot, repo); err != nil {
			t.Fatalf("Dematerialize failed: %v", err)
		}

		// Verify file has original content, not modified
		content, err := os.ReadFile(modifiedFile)
		if err != nil {
			t.Fatalf("failed to read file: %v", err)
		}
		if string(content) != "test content" {
			t.Error("Dematerialize should have overwritten modified content")
		}
		if _, err := os.Stat(staleLocalFile); !os.IsNotExist(err) {
			t.Fatalf("stale local-only file should have been removed, stat error: %v", err)
		}
		requireNoSnapshotTempDirs(t, filepath.Dir(filepath.Dir(overlayRoot)))
	})

	t.Run("preserves existing local store when replacement copy fails", func(t *testing.T) {
		storesDir, persistRoot, fs, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		storeID := "test-store"
		createTestStore(t, repo, storeID)

		if err := mgr.Materialize(storeID, repo, persistRoot); err != nil {
			t.Fatalf("Materialize failed: %v", err)
		}

		localFile := filepath.Join(repo.OverlayRoot(storeID), "test.txt")
		if err := os.WriteFile(localFile, []byte("existing local content"), 0644); err != nil {
			t.Fatalf("failed to write existing local content: %v", err)
		}
		localOnlyFile := filepath.Join(repo.OverlayRoot(storeID), "local-only.txt")
		if err := os.WriteFile(localOnlyFile, []byte("existing local-only content"), 0644); err != nil {
			t.Fatalf("failed to write existing local-only content: %v", err)
		}

		persistedFile := filepath.Join(persistRoot, ".monodev", "persist", "stores", storeID, "overlay", "test.txt")
		if err := os.WriteFile(persistedFile, []byte("replacement persisted content"), 0644); err != nil {
			t.Fatalf("failed to write replacement persisted content: %v", err)
		}

		failingMgr := NewSnapshotManager(&copyFailingFS{
			FS:  fs,
			err: errors.New("injected copy failure"),
		})
		err := failingMgr.Dematerialize(storeID, persistRoot, repo)
		if err == nil {
			t.Fatal("Dematerialize succeeded, want injected copy failure")
		}
		if !strings.Contains(err.Error(), "injected copy failure") {
			t.Fatalf("Dematerialize error = %v, want injected copy failure", err)
		}

		requireFileContent(t, localFile, "existing local content")
		requireFileContent(t, localOnlyFile, "existing local-only content")
		requireNoSnapshotTempDirs(t, storesDir)
	})

	t.Run("returns error for non-existent persisted store", func(t *testing.T) {
		storesDir, persistRoot, _, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		err := mgr.Dematerialize("nonexistent", persistRoot, repo)
		if err == nil {
			t.Error("Expected error for non-existent persisted store, got nil")
		}
	})

	t.Run("returns error for invalid store ID", func(t *testing.T) {
		storesDir, persistRoot, _, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		err := mgr.Dematerialize("../invalid", persistRoot, repo)
		if err == nil {
			t.Error("Expected error for invalid store ID, got nil")
		}
	})

	t.Run("refuses reserved coordination name before replacing locks", func(t *testing.T) {
		storesDir, persistRoot, _, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		sentinel := filepath.Join(storesDir, ".locks", "victim.lock")
		if err := os.MkdirAll(filepath.Dir(sentinel), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sentinel, []byte("held"), 0600); err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat(sentinel)
		if err != nil {
			t.Fatal(err)
		}

		persistStore := filepath.Join(persistRoot, ".monodev", "persist", "stores", ".locks", "overlay")
		if err := os.MkdirAll(persistStore, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(persistStore, "payload.txt"), []byte("remote"), 0600); err != nil {
			t.Fatal(err)
		}

		if err := mgr.Materialize(".locks", repo, persistRoot); !errors.Is(err, stores.ErrReservedStoreID) {
			t.Fatalf("Materialize error = %v, want ErrReservedStoreID", err)
		}
		if err := mgr.Dematerialize(".locks", persistRoot, repo); !errors.Is(err, stores.ErrReservedStoreID) {
			t.Fatalf("Dematerialize error = %v, want ErrReservedStoreID", err)
		}
		if _, err := mgr.DiffAgainstLocalCopy(".locks", persistRoot, repo, hash.NewSHA256Hasher()); !errors.Is(err, stores.ErrReservedStoreID) {
			t.Fatalf("DiffAgainstLocalCopy error = %v, want ErrReservedStoreID", err)
		}

		after, err := os.Stat(sentinel)
		if err != nil {
			t.Fatalf("lock sentinel missing: %v", err)
		}
		if !os.SameFile(before, after) {
			t.Fatal("lock sentinel inode changed")
		}
		if _, err := os.Stat(filepath.Join(storesDir, ".locks", "overlay", "payload.txt")); !os.IsNotExist(err) {
			t.Fatalf("dematerialize wrote into the coordination directory: %v", err)
		}
		if _, err := os.Stat(filepath.Join(persistRoot, ".monodev", "persist", "stores", ".locks", "victim.lock")); !os.IsNotExist(err) {
			t.Fatalf("materialize copied the coordination directory: %v", err)
		}
	})

	t.Run("rejects persisted symlink before replacing local store", func(t *testing.T) {
		storesDir, persistRoot, _, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		storeID := "test-store"
		createTestStore(t, repo, storeID)

		if err := mgr.Materialize(storeID, repo, persistRoot); err != nil {
			t.Fatalf("Materialize failed: %v", err)
		}

		localFile := filepath.Join(repo.OverlayRoot(storeID), "test.txt")
		if err := os.WriteFile(localFile, []byte("local content"), 0644); err != nil {
			t.Fatalf("failed to modify local file: %v", err)
		}

		outsidePath := filepath.Join(filepath.Dir(storesDir), "outside-secret.txt")
		if err := os.WriteFile(outsidePath, []byte("do-not-dematerialize"), 0644); err != nil {
			t.Fatalf("failed to write outside file: %v", err)
		}
		persistLeakPath := filepath.Join(persistRoot, ".monodev", "persist", "stores", storeID, "overlay", "leak.txt")
		requireSymlink(t, outsidePath, persistLeakPath)

		err := mgr.Dematerialize(storeID, persistRoot, repo)
		if err == nil {
			t.Fatal("Dematerialize succeeded, want symlink rejection")
		}
		if !strings.Contains(err.Error(), "overlay/leak.txt") || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("Dematerialize error %q should name the offending symlink path", err)
		}

		content, err := os.ReadFile(localFile)
		if err != nil {
			t.Fatalf("local store should not be removed before validation: %v", err)
		}
		if string(content) != "local content" {
			t.Fatalf("local file content = %q, want %q", content, "local content")
		}

		localLeakPath := filepath.Join(repo.OverlayRoot(storeID), "leak.txt")
		if _, err := os.Lstat(localLeakPath); !os.IsNotExist(err) {
			t.Fatalf("local store should not receive symlink target contents, stat error: %v", err)
		}
	})
}

func TestSnapshotManager_Verify(t *testing.T) {
	t.Run("verifies existing store with manifest", func(t *testing.T) {
		storesDir, persistRoot, _, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		storeID := "test-store"
		createTestStore(t, repo, storeID)

		// Materialize
		if err := mgr.Materialize(storeID, repo, persistRoot); err != nil {
			t.Fatalf("Materialize failed: %v", err)
		}

		// Verify
		hasher := hash.NewSHA256Hasher()
		err := mgr.Verify(storeID, persistRoot, hasher)
		if err != nil {
			t.Errorf("Verify failed: %v", err)
		}
	})

	t.Run("returns legacy sentinel when manifest is missing", func(t *testing.T) {
		storesDir, persistRoot, _, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		storeID := "test-store"
		createTestStore(t, repo, storeID)
		if err := mgr.Materialize(storeID, repo, persistRoot); err != nil {
			t.Fatalf("Materialize failed: %v", err)
		}

		manifestPath := filepath.Join(persistRoot, ".monodev", "persist", "stores", storeID, verificationManifestName)
		if err := os.Remove(manifestPath); err != nil {
			t.Fatalf("failed to remove manifest: %v", err)
		}

		hasher := hash.NewSHA256Hasher()
		err := mgr.Verify(storeID, persistRoot, hasher)
		if !errors.Is(err, ErrVerificationManifestMissing) {
			t.Fatalf("Verify error = %v, want ErrVerificationManifestMissing", err)
		}
		if !strings.Contains(err.Error(), storeID) || !strings.Contains(err.Error(), manifestPath) {
			t.Fatalf("Verify error %q should name store %q and path %q", err, storeID, manifestPath)
		}
	})

	t.Run("returns error for corrupted overlay file", func(t *testing.T) {
		storesDir, persistRoot, _, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		storeID := "test-store"
		createTestStore(t, repo, storeID)
		if err := mgr.Materialize(storeID, repo, persistRoot); err != nil {
			t.Fatalf("Materialize failed: %v", err)
		}

		corruptPath := filepath.Join(persistRoot, ".monodev", "persist", "stores", storeID, "overlay", "test.txt")
		if err := os.WriteFile(corruptPath, []byte("tampered"), 0644); err != nil {
			t.Fatalf("failed to corrupt overlay file: %v", err)
		}

		hasher := hash.NewSHA256Hasher()
		err := mgr.Verify(storeID, persistRoot, hasher)
		if err == nil {
			t.Fatal("Expected verification error for corrupted overlay file, got nil")
		}
		if !strings.Contains(err.Error(), storeID) || !strings.Contains(err.Error(), corruptPath) || !strings.Contains(err.Error(), "checksum mismatch") {
			t.Fatalf("Verify error %q should name store, path, and checksum mismatch", err)
		}
	})

	t.Run("returns error for missing persisted file", func(t *testing.T) {
		storesDir, persistRoot, _, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		storeID := "test-store"
		createTestStore(t, repo, storeID)
		if err := mgr.Materialize(storeID, repo, persistRoot); err != nil {
			t.Fatalf("Materialize failed: %v", err)
		}

		missingPath := filepath.Join(persistRoot, ".monodev", "persist", "stores", storeID, "overlay", "subdir", "nested.txt")
		if err := os.Remove(missingPath); err != nil {
			t.Fatalf("failed to remove persisted file: %v", err)
		}

		hasher := hash.NewSHA256Hasher()
		err := mgr.Verify(storeID, persistRoot, hasher)
		if err == nil {
			t.Fatal("Expected verification error for missing persisted file, got nil")
		}
		if !strings.Contains(err.Error(), storeID) || !strings.Contains(err.Error(), missingPath) {
			t.Fatalf("Verify error %q should name store %q and path %q", err, storeID, missingPath)
		}
	})

	t.Run("returns error for manifest hash mismatch", func(t *testing.T) {
		storesDir, persistRoot, _, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		storeID := "test-store"
		createTestStore(t, repo, storeID)
		if err := mgr.Materialize(storeID, repo, persistRoot); err != nil {
			t.Fatalf("Materialize failed: %v", err)
		}

		storePath := filepath.Join(persistRoot, ".monodev", "persist", "stores", storeID)
		manifestPath := filepath.Join(storePath, verificationManifestName)
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatalf("failed to read manifest: %v", err)
		}
		var manifest verificationManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatalf("failed to decode manifest: %v", err)
		}
		for i := range manifest.Files {
			if manifest.Files[i].Path == "track.json" {
				manifest.Files[i].Hash = "not-the-recorded-hash"
			}
		}
		data, err = json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			t.Fatalf("failed to encode manifest: %v", err)
		}
		data = append(data, '\n')
		if err := os.WriteFile(manifestPath, data, 0644); err != nil {
			t.Fatalf("failed to write manifest: %v", err)
		}

		hasher := hash.NewSHA256Hasher()
		err = mgr.Verify(storeID, persistRoot, hasher)
		if err == nil {
			t.Fatal("Expected verification error for manifest hash mismatch, got nil")
		}
		trackPath := filepath.Join(storePath, "track.json")
		if !strings.Contains(err.Error(), storeID) || !strings.Contains(err.Error(), trackPath) || !strings.Contains(err.Error(), "checksum mismatch") {
			t.Fatalf("Verify error %q should name store, path, and checksum mismatch", err)
		}
	})

	t.Run("returns error for non-existent store", func(t *testing.T) {
		storesDir, persistRoot, _, _, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		hasher := hash.NewSHA256Hasher()
		err := mgr.Verify("nonexistent", persistRoot, hasher)
		if err == nil {
			t.Error("Expected error for non-existent store, got nil")
		}
	})

	t.Run("returns error for invalid store ID", func(t *testing.T) {
		storesDir, persistRoot, _, _, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		hasher := hash.NewSHA256Hasher()
		err := mgr.Verify("../invalid", persistRoot, hasher)
		if err == nil {
			t.Error("Expected error for invalid store ID, got nil")
		}
	})
}

func TestSnapshotManager_VerifyLegacyManifestFixtureAndRejectsFutureSchema(t *testing.T) {
	storesDir, persistRoot, _, _, mgr := setupTestEnv(t)
	defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

	storeID := "legacy-store"
	storePath := persistStoreDir(persistRoot, storeID)
	if err := os.MkdirAll(storePath, 0700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"meta.json", "track.json"} {
		source := filepath.Join("..", "stores", "testdata", "legacy_"+strings.TrimSuffix(file, ".json")+".json")
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatalf("read legacy %s fixture: %v", file, err)
		}
		if err := os.WriteFile(filepath.Join(storePath, file), data, 0600); err != nil {
			t.Fatalf("write legacy %s fixture: %v", file, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(storePath, "overlay"), 0700); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "legacy_verification_manifest.json"))
	if err != nil {
		t.Fatalf("read legacy verification manifest fixture: %v", err)
	}
	manifestPath := filepath.Join(storePath, verificationManifestName)
	if err := os.WriteFile(manifestPath, fixture, 0600); err != nil {
		t.Fatalf("write legacy verification manifest fixture: %v", err)
	}
	if err := mgr.Verify(storeID, persistRoot, hash.NewSHA256Hasher()); err != nil {
		t.Fatalf("Verify() legacy manifest fixture: %v", err)
	}

	if err := os.WriteFile(manifestPath, []byte(`{"schemaVersion":2}`), 0600); err != nil {
		t.Fatal(err)
	}
	err = mgr.Verify(storeID, persistRoot, hash.NewSHA256Hasher())
	if err == nil {
		t.Fatal("Verify() error = nil, want future schema refusal")
	}
	for _, want := range []string{manifestPath, "schemaVersion 2", "supported schemaVersion 1", "upgrade monodev"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Verify() error = %q, want %q", err, want)
		}
	}
}

func TestSnapshotManager_CheckIncomingStoreSchemas(t *testing.T) {
	newStore := func(t *testing.T) (persistRoot, storePath string, mgr *SnapshotManager) {
		t.Helper()
		storesDir, root, _, repo, manager := setupTestEnv(t)
		t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(storesDir)) })
		const storeID = "schema-store"
		createTestStore(t, repo, storeID)
		if err := manager.Materialize(storeID, repo, root); err != nil {
			t.Fatalf("Materialize: %v", err)
		}
		local := filepath.Join(repo.OverlayRoot(storeID), "test.txt")
		before, err := os.ReadFile(local)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			after, err := os.ReadFile(local)
			if err != nil || string(after) != string(before) {
				t.Errorf("local store changed to %q, %v", after, err)
			}
		})
		return root, persistStoreDir(root, storeID), manager
	}

	t.Run("accepts the supported store", func(t *testing.T) {
		persistRoot, _, mgr := newStore(t)
		if err := mgr.CheckIncomingStoreSchemas("schema-store", persistRoot); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("accepts a missing track file", func(t *testing.T) {
		persistRoot, storePath, mgr := newStore(t)
		if err := os.Remove(filepath.Join(storePath, "track.json")); err != nil {
			t.Fatal(err)
		}
		if err := mgr.CheckIncomingStoreSchemas("schema-store", persistRoot); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("accepts legacy and unparseable headers", func(t *testing.T) {
		persistRoot, storePath, mgr := newStore(t)
		metaPath := filepath.Join(storePath, "meta.json")
		for _, document := range []string{
			`{"schemaVersion":1,"name":"legacy"}`,
			`{"name":"legacy-zero"}`,
			`{"schemaVersion":`,
		} {
			if err := os.WriteFile(metaPath, []byte(document), 0600); err != nil {
				t.Fatal(err)
			}
			if err := mgr.CheckIncomingStoreSchemas("schema-store", persistRoot); err != nil {
				t.Errorf("document %q: %v", document, err)
			}
		}
	})

	for _, document := range []string{"meta.json", "track.json"} {
		t.Run("refuses future "+document, func(t *testing.T) {
			persistRoot, storePath, mgr := newStore(t)
			supported := stores.SupportedMetaSchemaVersion()
			if document == "track.json" {
				supported = stores.SupportedTrackSchemaVersion()
			}
			future := supported + 1
			path := filepath.Join(storePath, document)
			payload := fmt.Sprintf(`{"schemaVersion":%d}`, future)
			if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(storePath, verificationManifestName)); err != nil {
				t.Fatal(err)
			}
			err := mgr.CheckIncomingStoreSchemas("schema-store", persistRoot)
			if err == nil {
				t.Fatal("error = nil, want future schema refusal")
			}
			for _, want := range []string{
				path,
				fmt.Sprintf("schemaVersion %d", future),
				fmt.Sprintf("supported schemaVersion %d", supported),
				"upgrade monodev",
			} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

func TestSnapshotManager_ListPersistedStores(t *testing.T) {
	t.Run("returns empty list when persist directory does not exist", func(t *testing.T) {
		storesDir, persistRoot, _, _, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		stores, err := mgr.ListPersistedStores(persistRoot)
		if err != nil {
			t.Fatalf("ListPersistedStores failed: %v", err)
		}

		if len(stores) != 0 {
			t.Errorf("Expected empty list, got %d stores", len(stores))
		}
	})

	t.Run("returns empty list when persist stores directory is empty", func(t *testing.T) {
		storesDir, persistRoot, _, _, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		// Create persist directory structure but no stores
		persistStoresDir := filepath.Join(persistRoot, ".monodev", "persist", "stores")
		if err := os.MkdirAll(persistStoresDir, 0755); err != nil {
			t.Fatalf("failed to create persist stores dir: %v", err)
		}

		stores, err := mgr.ListPersistedStores(persistRoot)
		if err != nil {
			t.Fatalf("ListPersistedStores failed: %v", err)
		}

		if len(stores) != 0 {
			t.Errorf("Expected empty list, got %d stores", len(stores))
		}
	})

	t.Run("returns list of persisted stores", func(t *testing.T) {
		storesDir, persistRoot, _, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		// Create and materialize multiple stores
		storeIDs := []string{"store1", "store2", "store3"}
		for _, id := range storeIDs {
			createTestStore(t, repo, id)
			if err := mgr.Materialize(id, repo, persistRoot); err != nil {
				t.Fatalf("Materialize %s failed: %v", id, err)
			}
		}

		// List persisted stores
		stores, err := mgr.ListPersistedStores(persistRoot)
		if err != nil {
			t.Fatalf("ListPersistedStores failed: %v", err)
		}

		if len(stores) != len(storeIDs) {
			t.Errorf("Expected %d stores, got %d", len(storeIDs), len(stores))
		}

		// Check all expected stores are present
		storeMap := make(map[string]bool)
		for _, id := range stores {
			storeMap[id] = true
		}

		for _, expectedID := range storeIDs {
			if !storeMap[expectedID] {
				t.Errorf("Expected store %q not found in list", expectedID)
			}
		}
	})

	t.Run("ignores files in persist stores directory", func(t *testing.T) {
		storesDir, persistRoot, _, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		storeID := "test-store"
		createTestStore(t, repo, storeID)

		// Materialize
		if err := mgr.Materialize(storeID, repo, persistRoot); err != nil {
			t.Fatalf("Materialize failed: %v", err)
		}

		// Create a regular file in persist stores directory
		persistStoresDir := filepath.Join(persistRoot, ".monodev", "persist", "stores")
		regularFile := filepath.Join(persistStoresDir, "regular-file.txt")
		if err := os.WriteFile(regularFile, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create regular file: %v", err)
		}

		// List should only return the store, not the file
		stores, err := mgr.ListPersistedStores(persistRoot)
		if err != nil {
			t.Fatalf("ListPersistedStores failed: %v", err)
		}

		if len(stores) != 1 {
			t.Errorf("Expected 1 store, got %d", len(stores))
		}

		if stores[0] != storeID {
			t.Errorf("Expected store %q, got %q", storeID, stores[0])
		}
	})
}

func TestSnapshotManager_Roundtrip(t *testing.T) {
	t.Run("materialize and dematerialize preserves store content", func(t *testing.T) {
		storesDir, persistRoot, _, repo, mgr := setupTestEnv(t)
		defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()

		storeID := "test-store"
		createTestStore(t, repo, storeID)

		// Save original content
		originalFile := filepath.Join(repo.OverlayRoot(storeID), "test.txt")
		originalContent, err := os.ReadFile(originalFile)
		if err != nil {
			t.Fatalf("failed to read original file: %v", err)
		}

		// Materialize
		if err := mgr.Materialize(storeID, repo, persistRoot); err != nil {
			t.Fatalf("Materialize failed: %v", err)
		}

		// Delete store from storesDir
		storePath := filepath.Dir(repo.OverlayRoot(storeID))
		if err := os.RemoveAll(storePath); err != nil {
			t.Fatalf("failed to remove store: %v", err)
		}

		// Dematerialize
		if err := mgr.Dematerialize(storeID, persistRoot, repo); err != nil {
			t.Fatalf("Dematerialize failed: %v", err)
		}

		// Verify content matches original
		restoredContent, err := os.ReadFile(originalFile)
		if err != nil {
			t.Fatalf("failed to read restored file: %v", err)
		}

		if string(restoredContent) != string(originalContent) {
			t.Errorf("Restored content = %q, want %q", restoredContent, originalContent)
		}
	})
}

func requireSymlink(t *testing.T, oldname, newname string) {
	t.Helper()

	if err := os.Symlink(oldname, newname); err != nil {
		t.Skipf("symlink creation is not supported in this environment: %v", err)
	}
}

type copyFailingFS struct {
	fsops.FS
	err error
}

func (fs *copyFailingFS) Copy(src, dst string) error {
	if strings.Contains(filepath.Base(dst), "-replacement-") {
		if err := fs.FS.Copy(src, dst); err != nil {
			return err
		}
		if fs.err != nil {
			return fs.err
		}
		return errors.New("injected copy failure")
	}
	return fs.FS.Copy(src, dst)
}

func requireFileContent(t *testing.T, path, want string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("%s content = %q, want %q", path, content, want)
	}
}

type emptyOverlayRepo struct {
	exists bool
}

func (emptyOverlayRepo) List() ([]string, error)       { return nil, nil }
func (r emptyOverlayRepo) Exists(string) (bool, error) { return r.exists, nil }
func (emptyOverlayRepo) Create(string, *stores.StoreMeta) error {
	return errors.New("create called")
}
func (emptyOverlayRepo) LoadMeta(string) (*stores.StoreMeta, error) {
	return nil, errors.New("load meta called")
}
func (emptyOverlayRepo) SaveMeta(string, *stores.StoreMeta) error {
	return errors.New("save meta called")
}
func (emptyOverlayRepo) LoadTrack(string) (*stores.TrackFile, error) {
	return nil, errors.New("load track called")
}
func (emptyOverlayRepo) SaveTrack(string, *stores.TrackFile) error {
	return errors.New("save track called")
}
func (emptyOverlayRepo) OverlayRoot(string) string { return "" }
func (emptyOverlayRepo) Delete(string) error       { return errors.New("delete called") }

func TestSnapshotManager_RejectsEmptyOverlayBeforeDerivingPaths(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	sentinel := filepath.Join(dir, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	fs := fsops.NewRealFS()
	mgr := NewSnapshotManager(fs)
	repo := emptyOverlayRepo{exists: true}
	persistRoot := filepath.Join(dir, "persist")
	if err := os.MkdirAll(persistRoot, 0o700); err != nil {
		t.Fatal(err)
	}

	const storeID = "store"
	err = mgr.Materialize(storeID, repo, persistRoot)
	if err == nil || !strings.Contains(err.Error(), "overlay path unavailable") {
		t.Fatalf("Materialize err = %v, want overlay path unavailable", err)
	}
	if _, statErr := os.Stat(filepath.Join(persistRoot, ".monodev")); !os.IsNotExist(statErr) {
		t.Fatalf("materialize derived a destination from an empty overlay, stat err %v", statErr)
	}
	requireFileContent(t, sentinel, "keep")

	src := persistStoreDir(persistRoot, storeID)
	if err := os.MkdirAll(filepath.Join(src, "overlay"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "meta.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "overlay", "file.txt"), []byte("persisted"), 0o600); err != nil {
		t.Fatal(err)
	}

	err = mgr.Dematerialize(storeID, persistRoot, repo)
	if err == nil || !strings.Contains(err.Error(), "overlay path unavailable") {
		t.Fatalf("Dematerialize err = %v, want overlay path unavailable", err)
	}
	requireFileContent(t, sentinel, "keep")
	requireFileContent(t, filepath.Join(src, "overlay", "file.txt"), "persisted")

	changed, err := mgr.DiffAgainstLocalCopy(storeID, persistRoot, repo, nil)
	if err == nil || changed != nil || !strings.Contains(err.Error(), "overlay path unavailable") {
		t.Fatalf("DiffAgainstLocalCopy = %v, %v; want overlay path unavailable", changed, err)
	}
}

func TestSnapshotManager_ScopedRoutingPreservesScope(t *testing.T) {
	fs := fsops.NewRealFS()
	globalDir := t.TempDir()
	componentDir := t.TempDir()
	global := stores.NewFileStoreRepo(fs, globalDir)
	component := stores.NewFileStoreRepo(fs, componentDir)
	repo := stores.NewScopedRepo(global, component)
	mgr := NewSnapshotManager(fs)
	persistRoot := t.TempDir()
	now := time.Now()

	writeMarker := func(t *testing.T, store stores.StoreRepo, id, body string) {
		t.Helper()
		path := filepath.Join(store.OverlayRoot(id), "marker.txt")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write marker: %v", err)
		}
	}
	readMarker := func(t *testing.T, path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read marker %s: %v", path, err)
		}
		return string(data)
	}

	t.Run("same id materializes the component store", func(t *testing.T) {
		if err := global.Create("shared", stores.NewStoreMeta("GLOBAL", now)); err != nil {
			t.Fatal(err)
		}
		if err := component.Create("shared", stores.NewStoreMeta("COMPONENT", now)); err != nil {
			t.Fatal(err)
		}
		writeMarker(t, global, "shared", "GLOBAL")
		writeMarker(t, component, "shared", "COMPONENT")

		if err := mgr.Materialize("shared", repo, persistRoot); err != nil {
			t.Fatal(err)
		}
		persisted := persistStoreDir(persistRoot, "shared")
		if got := readMarker(t, filepath.Join(persisted, "overlay", "marker.txt")); got != "COMPONENT" {
			t.Fatalf("persisted marker = %q, want COMPONENT", got)
		}
	})

	t.Run("confirmed missing component store falls back to global", func(t *testing.T) {
		if err := global.Create("only-global", stores.NewStoreMeta("GLOBAL", now)); err != nil {
			t.Fatal(err)
		}
		writeMarker(t, global, "only-global", "OLD")
		if err := mgr.Materialize("only-global", repo, persistRoot); err != nil {
			t.Fatal(err)
		}
		persistedMarker := filepath.Join(persistStoreDir(persistRoot, "only-global"), "overlay", "marker.txt")
		if err := os.WriteFile(persistedMarker, []byte("NEW"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := mgr.Dematerialize("only-global", persistRoot, repo); err != nil {
			t.Fatal(err)
		}
		if got := readMarker(t, filepath.Join(global.OverlayRoot("only-global"), "marker.txt")); got != "NEW" {
			t.Fatalf("global marker = %q, want NEW", got)
		}
		if _, err := os.Stat(filepath.Join(componentDir, "only-global")); !os.IsNotExist(err) {
			t.Fatalf("fallback wrote the component scope: %v", err)
		}
	})

	t.Run("component lookup error does not rewrite global", func(t *testing.T) {
		if os.Geteuid() <= 0 {
			t.Skip("directory search permission is not enforced for this user")
		}
		if err := global.Create("hidden", stores.NewStoreMeta("GLOBAL", now)); err != nil {
			t.Fatal(err)
		}
		if err := component.Create("hidden", stores.NewStoreMeta("COMPONENT", now)); err != nil {
			t.Fatal(err)
		}
		writeMarker(t, global, "hidden", "GLOBAL")
		writeMarker(t, component, "hidden", "COMPONENT")
		if err := mgr.Materialize("hidden", repo, persistRoot); err != nil {
			t.Fatal(err)
		}

		if err := os.Chmod(componentDir, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(componentDir, 0o700) })

		persistedMarker := filepath.Join(persistStoreDir(persistRoot, "hidden"), "overlay", "marker.txt")
		if err := os.WriteFile(persistedMarker, []byte("WRONG"), 0o600); err != nil {
			t.Fatal(err)
		}

		err := mgr.Dematerialize("hidden", persistRoot, repo)
		if err == nil || !strings.Contains(err.Error(), "overlay path unavailable") {
			t.Fatalf("Dematerialize err = %v, want overlay path unavailable", err)
		}
		if got := readMarker(t, filepath.Join(globalDir, "hidden", "overlay", "marker.txt")); got != "GLOBAL" {
			t.Fatalf("global marker = %q, want GLOBAL", got)
		}

		err = mgr.Materialize("hidden", repo, persistRoot)
		if !errors.Is(err, os.ErrPermission) {
			t.Fatalf("Materialize err = %v, want permission denied", err)
		}
		if got := readMarker(t, persistedMarker); got != "WRONG" {
			t.Fatalf("persisted marker = %q, want WRONG (materialize must not copy another scope)", got)
		}

		changed, err := mgr.DiffAgainstLocalCopy("hidden", persistRoot, repo, hash.NewSHA256Hasher())
		if err == nil || changed != nil || !strings.Contains(err.Error(), "overlay path unavailable") {
			t.Fatalf("DiffAgainstLocalCopy = %v, %v; want overlay path unavailable", changed, err)
		}

		if err := os.Chmod(componentDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if got := readMarker(t, filepath.Join(componentDir, "hidden", "overlay", "marker.txt")); got != "COMPONENT" {
			t.Fatalf("component marker = %q, want COMPONENT", got)
		}
	})
}

func requireNoSnapshotTempDirs(t *testing.T, parent string) {
	t.Helper()

	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("failed to read %s: %v", parent, err)
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), ".monodev-") {
			t.Fatalf("temporary snapshot directory %q was left in %s", entry.Name(), parent)
		}
	}
}

func TestSnapshotsRejectSymlinkedSourceAncestors(t *testing.T) {
	for _, materialize := range []bool{true, false} {
		t.Run(map[bool]string{true: "materialize", false: "dematerialize"}[materialize], func(t *testing.T) {
			storesDir, persistRoot, _, repo, mgr := setupTestEnv(t)
			defer func() { _ = os.RemoveAll(filepath.Dir(storesDir)) }()
			const storeID = "test-store"
			createTestStore(t, repo, storeID)
			if err := mgr.Materialize(storeID, repo, persistRoot); err != nil {
				t.Fatal(err)
			}
			ancestor, dest := storesDir, persistStoreDir(persistRoot, storeID)
			if !materialize {
				ancestor, dest = filepath.Dir(persistStoreDir(persistRoot, storeID)), filepath.Join(storesDir, storeID)
			}
			original, err := os.ReadFile(filepath.Join(dest, "overlay/test.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(ancestor, ancestor+"-outside"); err != nil {
				t.Fatal(err)
			}
			requireSymlink(t, ancestor+"-outside", ancestor)
			if err := os.WriteFile(filepath.Join(ancestor+"-outside", storeID, "overlay/test.txt"), []byte("outside-secret-sentinel"), 0600); err != nil {
				t.Fatal(err)
			}
			if materialize {
				err = mgr.Materialize(storeID, repo, persistRoot)
			} else {
				err = mgr.Dematerialize(storeID, persistRoot, repo)
			}
			if err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("snapshot error = %v", err)
			}
			if got, err := os.ReadFile(filepath.Join(dest, "overlay/test.txt")); err != nil || string(got) != string(original) {
				t.Fatalf("destination changed to %q, %v", got, err)
			}
		})
	}
}
