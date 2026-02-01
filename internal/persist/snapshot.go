package persist

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/stores"
)

// SnapshotManager handles materialization and dematerialization of stores
// between the user's home directory (~/.monodev/stores) and the persistence
// directory (.monodev/persist/stores).
type SnapshotManager struct {
	fs fsops.FS
}

// NewSnapshotManager creates a new SnapshotManager.
func NewSnapshotManager(fs fsops.FS) *SnapshotManager {
	return &SnapshotManager{fs: fs}
}

// persistStoresDir returns the path to the persist stores directory.
func persistStoresDir(persistRoot string) string {
	return filepath.Join(persistRoot, ".monodev", "persist", "stores")
}

// persistStoreDir returns the path to a specific store in the persist directory.
func persistStoreDir(persistRoot, storeID string) string {
	return filepath.Join(persistStoresDir(persistRoot), storeID)
}

// Materialize copies a store from ~/.monodev/stores/<store-id> to
// .monodev/persist/stores/<store-id>/.
func (s *SnapshotManager) Materialize(storeID string, storeRepo stores.StoreRepo, persistRoot string) error {
	// Validate store ID
	if err := s.fs.ValidateIdentifier(storeID); err != nil {
		return fmt.Errorf("invalid store ID: %w", err)
	}

	// Check if store exists
	exists, err := storeRepo.Exists(storeID)
	if err != nil {
		return fmt.Errorf("failed to check if store exists: %w", err)
	}
	if !exists {
		return fmt.Errorf("store %q not found", storeID)
	}

	// Get the store path - overlay root's parent directory
	storePath := filepath.Dir(storeRepo.OverlayRoot(storeID))

	// Destination path
	dstPath := persistStoreDir(persistRoot, storeID)

	// Remove existing destination if present
	if exists, err := s.fs.Exists(dstPath); err != nil {
		return fmt.Errorf("failed to check destination: %w", err)
	} else if exists {
		if err := s.fs.RemoveAll(dstPath); err != nil {
			return fmt.Errorf("failed to remove existing destination: %w", err)
		}
	}

	// Copy the store directory
	if err := s.fs.Copy(storePath, dstPath); err != nil {
		return fmt.Errorf("failed to copy store: %w", err)
	}

	return nil
}

// Dematerialize copies a store from .monodev/persist/stores/<store-id>/ to
// ~/.monodev/stores/<store-id>/.
func (s *SnapshotManager) Dematerialize(storeID string, persistRoot string, storeRepo stores.StoreRepo) error {
	// Validate store ID
	if err := s.fs.ValidateIdentifier(storeID); err != nil {
		return fmt.Errorf("invalid store ID: %w", err)
	}

	// Source path in persist directory
	srcPath := persistStoreDir(persistRoot, storeID)

	// Check if source exists
	exists, err := s.fs.Exists(srcPath)
	if err != nil {
		return fmt.Errorf("failed to check if persist store exists: %w", err)
	}
	if !exists {
		return fmt.Errorf("store %q not found in persist directory at %s", storeID, srcPath)
	}

	// Destination path - overlay root's parent directory
	dstPath := filepath.Dir(storeRepo.OverlayRoot(storeID))

	// Remove existing destination if present
	if exists, err := s.fs.Exists(dstPath); err != nil {
		return fmt.Errorf("failed to check destination: %w", err)
	} else if exists {
		if err := s.fs.RemoveAll(dstPath); err != nil {
			return fmt.Errorf("failed to remove existing destination: %w", err)
		}
	}

	// Copy the store directory
	if err := s.fs.Copy(srcPath, dstPath); err != nil {
		return fmt.Errorf("failed to copy store: %w", err)
	}

	return nil
}

// Verify verifies the integrity of a store in the persist directory using checksums.
// This is optional for v1 and can be used with the --verify flag.
func (s *SnapshotManager) Verify(storeID string, persistRoot string, hasher hash.Hasher) error {
	// Validate store ID
	if err := s.fs.ValidateIdentifier(storeID); err != nil {
		return fmt.Errorf("invalid store ID: %w", err)
	}

	// Path to store in persist directory
	storePath := persistStoreDir(persistRoot, storeID)

	// Check if store exists
	exists, err := s.fs.Exists(storePath)
	if err != nil {
		return fmt.Errorf("failed to check if store exists: %w", err)
	}
	if !exists {
		return fmt.Errorf("store %q not found in persist directory", storeID)
	}

	// TODO: Implement checksum verification
	// For v1, we can just check that the directory exists and contains the expected files.
	// Full checksum verification can be added later.

	return nil
}

// ListPersistedStores returns a list of store IDs available in the persist directory.
func (s *SnapshotManager) ListPersistedStores(persistRoot string) ([]string, error) {
	storesDir := persistStoresDir(persistRoot)

	// Check if persist directory exists
	exists, err := s.fs.Exists(storesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to check persist stores directory: %w", err)
	}
	if !exists {
		// No persisted stores yet
		return []string{}, nil
	}

	// Read directory entries
	entries, err := os.ReadDir(storesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read persist stores directory: %w", err)
	}

	var storeIDs []string
	for _, entry := range entries {
		if entry.IsDir() {
			storeIDs = append(storeIDs, entry.Name())
		}
	}

	return storeIDs, nil
}
