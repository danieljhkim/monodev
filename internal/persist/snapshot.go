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
//
// SnapshotManager does not lock. Callers own each store's transaction lock
// (stores.LockStores): shared for Materialize, exclusive across a
// DiffAgainstLocalCopy and Dematerialize that must agree. Keeping acquisition
// in one place means a flow never takes the same lock twice.
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

// refuseReservedStoreID rejects store-root coordination names before any store
// lookup or disk access, so a caller cannot replace the live lock directory.
func refuseReservedStoreID(storeID string) error {
	if stores.ReservedStoreID(storeID) {
		return fmt.Errorf("invalid store ID: %w", stores.ErrReservedStoreID)
	}
	return nil
}

// overlayStoreDir is the on-disk store directory, the parent of OverlayRoot.
// An empty overlay path is the safe failure for an invalid ID or a scope
// lookup error. Reject it before filepath.Dir, which would turn "" into ".".
func overlayStoreDir(storeRepo stores.StoreRepo, storeID string) (string, error) {
	overlayRoot := storeRepo.OverlayRoot(storeID)
	if overlayRoot == "" {
		return "", fmt.Errorf("store %q overlay path unavailable", storeID)
	}
	return filepath.Dir(overlayRoot), nil
}

// Materialize copies a store from ~/.monodev/stores/<store-id> to
// .monodev/persist/stores/<store-id>/. The caller must hold at least a shared
// lock on the store.
func (s *SnapshotManager) Materialize(storeID string, storeRepo stores.StoreRepo, persistRoot string) error {
	// Validate store ID
	if err := s.fs.ValidateIdentifier(storeID); err != nil {
		return fmt.Errorf("invalid store ID: %w", err)
	}
	if err := refuseReservedStoreID(storeID); err != nil {
		return err
	}

	// Check if store exists
	exists, err := storeRepo.Exists(storeID)
	if err != nil {
		return fmt.Errorf("failed to check if store exists: %w", err)
	}
	if !exists {
		return fmt.Errorf("store %q not found", storeID)
	}

	// Get the store path - overlay root's parent directory.
	// Reject an unavailable overlay before deriving that parent.
	storePath, err := overlayStoreDir(storeRepo, storeID)
	if err != nil {
		return err
	}

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
// ~/.monodev/stores/<store-id>/. The caller must hold the store's exclusive
// lock.
func (s *SnapshotManager) Dematerialize(storeID string, persistRoot string, storeRepo stores.StoreRepo) error {
	// Validate store ID
	if err := s.fs.ValidateIdentifier(storeID); err != nil {
		return fmt.Errorf("invalid store ID: %w", err)
	}
	// Refuse a reserved coordination name before reading or replacing anything.
	// It must not reach the rename that swaps a store directory.
	if err := refuseReservedStoreID(storeID); err != nil {
		return err
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

	// Destination path - overlay root's parent directory.
	// Reject an unavailable overlay before deriving that parent.
	dstPath, err := overlayStoreDir(storeRepo, storeID)
	if err != nil {
		return err
	}

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

// CheckIncomingStoreSchemas reports whether a persisted store's meta.json and
// track.json are formats this binary can publish into the local store. It
// reads the persistence checkout only and does not replace local bytes.
// A missing document is absent: legacy stores may omit track.json, which
// LoadTrack treats as empty. An unparseable header is not refused here.
// Checksum manifests are ignored; schema compatibility is independent of them.
func (s *SnapshotManager) CheckIncomingStoreSchemas(storeID, persistRoot string) error {
	if err := s.fs.ValidateIdentifier(storeID); err != nil {
		return fmt.Errorf("invalid store ID: %w", err)
	}
	if err := refuseReservedStoreID(storeID); err != nil {
		return err
	}

	storePath := persistStoreDir(persistRoot, storeID)
	documents := []struct {
		name      string
		supported int
	}{
		{name: "meta.json", supported: stores.SupportedMetaSchemaVersion()},
		{name: "track.json", supported: stores.SupportedTrackSchemaVersion()},
	}
	for _, document := range documents {
		path := filepath.Join(storePath, document.name)
		data, err := s.fs.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("failed to read %s: %w", path, err)
		}
		if err := stores.RefuseFutureStoreSchema(path, data, document.supported); err != nil {
			return err
		}
	}
	return nil
}
