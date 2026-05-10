package persist

import (
	"fmt"
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

	if err := fsops.ValidateCopySource(storePath); err != nil {
		return fmt.Errorf("store %q contains an unsafe copy source: %w", storeID, err)
	}

	stagedPath, err := s.stageStoreReplacement(storePath, dstPath)
	if err != nil {
		return err
	}
	stagedReady := true
	defer func() {
		if stagedReady {
			_ = s.fs.RemoveAll(stagedPath)
		}
	}()

	if err := s.writeVerificationManifest(storeID, stagedPath, hash.NewSHA256Hasher()); err != nil {
		return fmt.Errorf("failed to write verification manifest for store %q: %w", storeID, err)
	}

	if err := s.replaceWithStagedStore(dstPath, stagedPath); err != nil {
		return fmt.Errorf("failed to replace persisted store %q: %w", storeID, err)
	}
	stagedReady = false

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

	if err := fsops.ValidateCopySource(srcPath); err != nil {
		return fmt.Errorf("persisted store %q contains an unsafe copy source: %w", storeID, err)
	}

	stagedPath, err := s.stageStoreReplacement(srcPath, dstPath)
	if err != nil {
		return err
	}
	stagedReady := true
	defer func() {
		if stagedReady {
			_ = s.fs.RemoveAll(stagedPath)
		}
	}()

	if err := s.replaceWithStagedStore(dstPath, stagedPath); err != nil {
		return fmt.Errorf("failed to replace local store %q: %w", storeID, err)
	}
	stagedReady = false

	return nil
}
