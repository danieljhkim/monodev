package persist

import (
	"encoding/json"
	"errors"
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
	meta := stores.NewStoreMeta("Test Store", "global", time.Now())
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
		newFilePath := filepath.Join(persistStorePath, "overlay", "new.txt")
		if _, err := os.Stat(newFilePath); os.IsNotExist(err) {
			t.Error("New file was not materialized in second materialize")
		}
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
