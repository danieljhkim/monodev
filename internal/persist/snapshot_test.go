package persist

import (
	"os"
	"path/filepath"
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
}

func TestSnapshotManager_Verify(t *testing.T) {
	t.Run("verifies existing store", func(t *testing.T) {
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
